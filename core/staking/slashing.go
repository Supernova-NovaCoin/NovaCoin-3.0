package staking

import (
	"fmt"
	safemath "novacoin/core/math"
	"novacoin/core/pulse"
	"sync"
	"time"
)

// SlashableOffense represents the type of slashable behavior
type SlashableOffense uint8

const (
	OffenseDoubleSign SlashableOffense = iota // Signing two different blocks at same height
	OffenseDowntime                           // Extended validator downtime
	OffenseInvalidBlock                       // Producing invalid blocks
)

// SlashingConfig contains the slashing parameters
type SlashingConfig struct {
	DoubleSignSlashPercent uint64 // Percentage of stake to slash for double signing (e.g., 10 = 10%)
	DowntimeSlashPercent   uint64 // Percentage of stake to slash for downtime
	DowntimeThreshold      int64  // Seconds of downtime before slashing
	JailDuration           int64  // Seconds a validator is jailed after slashing
}

// DefaultSlashingConfig returns default slashing parameters
func DefaultSlashingConfig() *SlashingConfig {
	return &SlashingConfig{
		DoubleSignSlashPercent: 10,                                  // 10% slash for double signing
		DowntimeSlashPercent:   1,                                   // 1% slash for downtime
		DowntimeThreshold:      int64(24 * time.Hour / time.Second), // 24 hours
		JailDuration:           int64(7 * 24 * time.Hour / time.Second), // 7 days jail
	}
}

// SlashRecord tracks slashing events for a validator
type SlashRecord struct {
	Validator   [32]byte
	Offense     SlashableOffense
	Amount      uint64
	Timestamp   int64
	BlockHash   pulse.Hash
	JailedUntil int64 // Unix timestamp when jail ends
}

// Slasher manages slashing logic and evidence tracking
type Slasher struct {
	config *SlashingConfig

	// Evidence tracking for double-sign detection
	// Maps: round -> validator -> block hash
	signedBlocks map[uint64]map[[32]byte]pulse.Hash

	// Slashing history
	slashRecords []SlashRecord

	// Jailed validators: validator -> jail release time
	jailed map[[32]byte]int64

	mu sync.RWMutex
}

// NewSlasher creates a new slashing manager
func NewSlasher(config *SlashingConfig) *Slasher {
	if config == nil {
		config = DefaultSlashingConfig()
	}
	return &Slasher{
		config:       config,
		signedBlocks: make(map[uint64]map[[32]byte]pulse.Hash),
		slashRecords: make([]SlashRecord, 0),
		jailed:       make(map[[32]byte]int64),
	}
}

// RecordBlockSigned records that a validator signed a block at a given round.
// Returns true if this is a double-sign (same round, different block).
func (s *Slasher) RecordBlockSigned(v *pulse.Vertex) (bool, *SlashRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	round := v.Round

	// Initialize round map if needed
	if s.signedBlocks[round] == nil {
		s.signedBlocks[round] = make(map[[32]byte]pulse.Hash)
	}

	// Check if this validator already signed a DIFFERENT block at this round
	if existingHash, exists := s.signedBlocks[round][v.Author]; exists {
		if existingHash != v.Hash {
			// DOUBLE SIGN DETECTED!
			record := &SlashRecord{
				Validator: v.Author,
				Offense:   OffenseDoubleSign,
				Timestamp: v.Timestamp,
				BlockHash: v.Hash,
			}
			return true, record
		}
		// Same block, not double-sign
		return false, nil
	}

	// Record this signature
	s.signedBlocks[round][v.Author] = v.Hash

	// Cleanup old rounds (keep last 1000 rounds)
	if round > 1000 {
		delete(s.signedBlocks, round-1000)
	}

	return false, nil
}

// CalculateSlashAmount calculates the amount to slash based on offense type
func (s *Slasher) CalculateSlashAmount(currentStake uint64, offense SlashableOffense) (uint64, error) {
	var percent uint64
	switch offense {
	case OffenseDoubleSign:
		percent = s.config.DoubleSignSlashPercent
	case OffenseDowntime:
		percent = s.config.DowntimeSlashPercent
	case OffenseInvalidBlock:
		percent = s.config.DoubleSignSlashPercent // Same as double sign
	default:
		return 0, fmt.Errorf("unknown offense type: %d", offense)
	}

	// Calculate: stake * percent / 100
	slashAmount, err := safemath.SafeMul(currentStake, percent)
	if err != nil {
		return 0, err
	}
	return slashAmount / 100, nil
}

// Slash applies slashing to a validator
// Returns the amount slashed
func (s *Slasher) Slash(validator [32]byte, stake uint64, offense SlashableOffense, timestamp int64) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Calculate slash amount
	slashAmount, err := s.CalculateSlashAmount(stake, offense)
	if err != nil {
		return 0, err
	}

	if slashAmount == 0 {
		return 0, nil
	}

	// Record the slash
	record := SlashRecord{
		Validator:   validator,
		Offense:     offense,
		Amount:      slashAmount,
		Timestamp:   timestamp,
		JailedUntil: timestamp + s.config.JailDuration*1_000_000_000, // Convert to nanoseconds
	}
	s.slashRecords = append(s.slashRecords, record)

	// Jail the validator
	s.jailed[validator] = record.JailedUntil

	fmt.Printf("⚠️ SLASHED: Validator %x slashed %d NVN for %s. Jailed until %d\n",
		validator[:4], slashAmount/1_000_000, offenseString(offense), record.JailedUntil)

	return slashAmount, nil
}

// IsJailed checks if a validator is currently jailed
func (s *Slasher) IsJailed(validator [32]byte, currentTime int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jailEnd, exists := s.jailed[validator]
	if !exists {
		return false
	}
	return currentTime < jailEnd
}

// GetSlashRecords returns all slash records for a validator
func (s *Slasher) GetSlashRecords(validator [32]byte) []SlashRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var records []SlashRecord
	for _, r := range s.slashRecords {
		if r.Validator == validator {
			records = append(records, r)
		}
	}
	return records
}

// GetAllSlashRecords returns all slash records
func (s *Slasher) GetAllSlashRecords() []SlashRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]SlashRecord, len(s.slashRecords))
	copy(result, s.slashRecords)
	return result
}

func offenseString(o SlashableOffense) string {
	switch o {
	case OffenseDoubleSign:
		return "DOUBLE_SIGN"
	case OffenseDowntime:
		return "DOWNTIME"
	case OffenseInvalidBlock:
		return "INVALID_BLOCK"
	default:
		return "UNKNOWN"
	}
}
