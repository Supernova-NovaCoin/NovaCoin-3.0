package staking

import (
	"novacoin/core/pulse"
	"testing"
	"time"
)

func TestDoubleSignDetection(t *testing.T) {
	slasher := NewSlasher(nil)

	var validator [32]byte
	validator[0] = 0xAA

	// Create first block
	v1 := &pulse.Vertex{
		Author: validator,
		Round:  100,
	}
	v1.Hash = [32]byte{1} // Unique hash

	// Record first signature
	isDouble, _ := slasher.RecordBlockSigned(v1)
	if isDouble {
		t.Error("First block should not be double-sign")
	}

	// Create second block at same round with different hash
	v2 := &pulse.Vertex{
		Author: validator,
		Round:  100,
	}
	v2.Hash = [32]byte{2} // Different hash = double sign!

	isDouble, record := slasher.RecordBlockSigned(v2)
	if !isDouble {
		t.Error("Second block at same round should be detected as double-sign")
	}
	if record == nil {
		t.Error("Expected slash record")
	}
	if record.Offense != OffenseDoubleSign {
		t.Errorf("Expected DoubleSign offense, got %d", record.Offense)
	}
}

func TestSameBlockNotDoubleSign(t *testing.T) {
	slasher := NewSlasher(nil)

	var validator [32]byte
	validator[0] = 0xBB

	v := &pulse.Vertex{
		Author: validator,
		Round:  50,
	}
	v.Hash = [32]byte{1}

	// Record same block twice
	slasher.RecordBlockSigned(v)
	isDouble, _ := slasher.RecordBlockSigned(v)

	if isDouble {
		t.Error("Same block signed twice should not be double-sign")
	}
}

func TestSlashCalculation(t *testing.T) {
	config := &SlashingConfig{
		DoubleSignSlashPercent: 10,
		DowntimeSlashPercent:   1,
	}
	slasher := NewSlasher(config)

	// Test double-sign slash (10%)
	stake := uint64(1000 * 1_000_000) // 1000 NVN
	amount, err := slasher.CalculateSlashAmount(stake, OffenseDoubleSign)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expected := uint64(100 * 1_000_000) // 10% = 100 NVN
	if amount != expected {
		t.Errorf("Expected slash amount %d, got %d", expected, amount)
	}

	// Test downtime slash (1%)
	amount, err = slasher.CalculateSlashAmount(stake, OffenseDowntime)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expected = uint64(10 * 1_000_000) // 1% = 10 NVN
	if amount != expected {
		t.Errorf("Expected slash amount %d, got %d", expected, amount)
	}
}

func TestSlashAndJail(t *testing.T) {
	config := DefaultSlashingConfig()
	slasher := NewSlasher(config)

	var validator [32]byte
	validator[0] = 0xCC

	stake := uint64(1000 * 1_000_000)
	timestamp := time.Now().UnixNano()

	amount, err := slasher.Slash(validator, stake, OffenseDoubleSign, timestamp)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if amount == 0 {
		t.Error("Slash amount should not be zero")
	}

	// Check if jailed
	if !slasher.IsJailed(validator, timestamp) {
		t.Error("Validator should be jailed after slash")
	}

	// Check records
	records := slasher.GetSlashRecords(validator)
	if len(records) != 1 {
		t.Errorf("Expected 1 slash record, got %d", len(records))
	}

	// Check jail expires
	futureTime := timestamp + config.JailDuration*1_000_000_000 + 1
	if slasher.IsJailed(validator, futureTime) {
		t.Error("Validator should not be jailed after jail duration")
	}
}

func TestNotJailed(t *testing.T) {
	slasher := NewSlasher(nil)

	var validator [32]byte
	validator[0] = 0xDD

	// Validator should not be jailed if never slashed
	if slasher.IsJailed(validator, time.Now().UnixNano()) {
		t.Error("Unslashed validator should not be jailed")
	}
}
