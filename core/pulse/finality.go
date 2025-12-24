package pulse

import (
	"sync"
	"time"
)

// FinalityConfig contains parameters for determining block finality
type FinalityConfig struct {
	// MinConfirmations is the minimum number of child vertices required for finality
	MinConfirmations int

	// MinAge is the minimum age (in seconds) for a vertex to be considered final
	MinAge int64

	// MinStakePercent is the minimum percentage of total stake that must
	// have produced blocks referencing this vertex (0-100)
	MinStakePercent int
}

// DefaultFinalityConfig returns sensible defaults for finality
func DefaultFinalityConfig() *FinalityConfig {
	return &FinalityConfig{
		MinConfirmations: 6,    // 6 confirmations (like Bitcoin)
		MinAge:           30,   // 30 seconds minimum age
		MinStakePercent:  66,   // 2/3 stake threshold
	}
}

// FinalityTracker tracks the finality status of vertices in the DAG
type FinalityTracker struct {
	config *FinalityConfig
	store  *VertexStore

	// Track confirmation counts: hash -> number of descendants
	confirmations map[Hash]int

	// Track which validators have built on each vertex
	stakeWeight map[Hash]map[[32]byte]bool

	// Cache of finalized vertices
	finalized map[Hash]bool

	mu sync.RWMutex
}

// NewFinalityTracker creates a new finality tracker
func NewFinalityTracker(store *VertexStore, config *FinalityConfig) *FinalityTracker {
	if config == nil {
		config = DefaultFinalityConfig()
	}
	return &FinalityTracker{
		config:        config,
		store:         store,
		confirmations: make(map[Hash]int),
		stakeWeight:   make(map[Hash]map[[32]byte]bool),
		finalized:     make(map[Hash]bool),
	}
}

// RecordVertex records a new vertex and updates finality tracking
func (ft *FinalityTracker) RecordVertex(v *Vertex) {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	// For each parent, increment its confirmation count
	for _, parentHash := range v.Parents {
		ft.confirmations[parentHash]++

		// Track which validator built on this parent
		if ft.stakeWeight[parentHash] == nil {
			ft.stakeWeight[parentHash] = make(map[[32]byte]bool)
		}
		ft.stakeWeight[parentHash][v.Author] = true

		// Propagate confirmations up the DAG (simplified - only direct parents)
		// In a full implementation, we'd traverse ancestors recursively
	}

	// Initialize tracking for the new vertex
	if ft.confirmations[v.Hash] == 0 {
		ft.confirmations[v.Hash] = 0
	}
	if ft.stakeWeight[v.Hash] == nil {
		ft.stakeWeight[v.Hash] = make(map[[32]byte]bool)
	}
}

// IsFinalized checks if a vertex has reached finality
func (ft *FinalityTracker) IsFinalized(hash Hash) bool {
	ft.mu.RLock()
	if ft.finalized[hash] {
		ft.mu.RUnlock()
		return true
	}
	ft.mu.RUnlock()

	// Check finality conditions
	if ft.checkFinality(hash) {
		ft.mu.Lock()
		ft.finalized[hash] = true
		ft.mu.Unlock()
		return true
	}

	return false
}

// checkFinality checks all finality conditions for a vertex
func (ft *FinalityTracker) checkFinality(hash Hash) bool {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	// Get the vertex
	v := ft.store.GetVertex(hash)
	if v == nil {
		return false
	}

	// Condition 1: Minimum confirmations
	confirmCount := ft.confirmations[hash]
	if confirmCount < ft.config.MinConfirmations {
		return false
	}

	// Condition 2: Minimum age
	age := (time.Now().UnixNano() - v.Timestamp) / 1_000_000_000 // Convert to seconds
	if age < ft.config.MinAge {
		return false
	}

	// Condition 3: Stake weight (simplified - just count unique validators)
	// In production, we'd weight by actual stake amounts
	validators := ft.stakeWeight[hash]
	if len(validators) < ft.config.MinStakePercent/10 { // Simplified: 66% -> at least 6 validators
		return false
	}

	return true
}

// GetConfirmations returns the confirmation count for a vertex
func (ft *FinalityTracker) GetConfirmations(hash Hash) int {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	return ft.confirmations[hash]
}

// GetFinalityStatus returns detailed finality information for a vertex
func (ft *FinalityTracker) GetFinalityStatus(hash Hash) *FinalityStatus {
	ft.mu.RLock()
	defer ft.mu.RUnlock()

	v := ft.store.GetVertex(hash)
	if v == nil {
		return nil
	}

	confirmCount := ft.confirmations[hash]
	validators := ft.stakeWeight[hash]
	validatorCount := 0
	if validators != nil {
		validatorCount = len(validators)
	}

	age := (time.Now().UnixNano() - v.Timestamp) / 1_000_000_000

	status := &FinalityStatus{
		Hash:           hash,
		Confirmations:  confirmCount,
		Age:            age,
		ValidatorCount: validatorCount,
		IsFinalized:    ft.finalized[hash],
	}

	// Compute progress towards finality
	confirmProgress := float64(confirmCount) / float64(ft.config.MinConfirmations) * 100
	if confirmProgress > 100 {
		confirmProgress = 100
	}

	ageProgress := float64(age) / float64(ft.config.MinAge) * 100
	if ageProgress > 100 {
		ageProgress = 100
	}

	stakeProgress := float64(validatorCount*10) / float64(ft.config.MinStakePercent) * 100
	if stakeProgress > 100 {
		stakeProgress = 100
	}

	status.Progress = (confirmProgress + ageProgress + stakeProgress) / 3

	return status
}

// FinalityStatus contains finality information for a vertex
type FinalityStatus struct {
	Hash           Hash    `json:"hash"`
	Confirmations  int     `json:"confirmations"`
	Age            int64   `json:"age"`
	ValidatorCount int     `json:"validatorCount"`
	IsFinalized    bool    `json:"isFinalized"`
	Progress       float64 `json:"progress"` // 0-100%
}

// Cleanup removes old entries from the tracker to prevent memory growth
func (ft *FinalityTracker) Cleanup(keepRecent int) {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	// Get all vertices sorted by timestamp
	all := ft.store.GetAllVertices()
	if len(all) <= keepRecent {
		return
	}

	// Build a set of recent hashes to keep
	keepSet := make(map[Hash]bool)
	for i := len(all) - keepRecent; i < len(all); i++ {
		keepSet[all[i].Hash] = true
	}

	// Clean up old entries that are finalized
	for hash := range ft.confirmations {
		if !keepSet[hash] && ft.finalized[hash] {
			delete(ft.confirmations, hash)
			delete(ft.stakeWeight, hash)
			// Keep finalized status for lookup
		}
	}
}
