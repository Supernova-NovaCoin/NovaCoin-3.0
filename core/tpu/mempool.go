package tpu

import (
	"novacoin/core/execution"
	"novacoin/core/types"
	"sort"
	"sync"
	"time"
)

// Fee market constants
const (
	MinFee          = uint64(1000)          // Minimum fee in nanoNVN (0.001 NVN)
	MaxMempoolSize  = 10000                 // Maximum pending transactions
	TxTTL           = 1 * time.Hour         // Transaction time-to-live
	CleanupInterval = 5 * time.Minute       // How often to clean expired txs
)

// MempoolEntry wraps a transaction with metadata
type MempoolEntry struct {
	Tx        types.Transaction
	AddedAt   time.Time
	ExpiresAt time.Time
}

// Mempool stores pending transactions that have explicitly NOT typically been included in a block yet.
// Typical lifecycle: P2P -> Mempool -> Miner -> Block -> Executor
type Mempool struct {
	Pending map[[32]byte]*MempoolEntry // Sig -> Entry
	State   *execution.StateManager
	mu      sync.RWMutex

	// Cleanup goroutine control
	stopCleanup chan struct{}
}

func NewMempool(state *execution.StateManager) *Mempool {
	mp := &Mempool{
		Pending:     make(map[[32]byte]*MempoolEntry),
		State:       state,
		stopCleanup: make(chan struct{}),
	}

	// Start background cleanup goroutine
	go mp.cleanupLoop()

	return mp
}

// cleanupLoop periodically removes expired transactions
func (mp *Mempool) cleanupLoop() {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mp.RemoveExpired()
		case <-mp.stopCleanup:
			return
		}
	}
}

// Stop stops the cleanup goroutine
func (mp *Mempool) Stop() {
	close(mp.stopCleanup)
}

// RemoveExpired removes all expired transactions from the mempool
func (mp *Mempool) RemoveExpired() int {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	now := time.Now()
	removed := 0

	for key, entry := range mp.Pending {
		if now.After(entry.ExpiresAt) {
			delete(mp.Pending, key)
			removed++
		}
	}

	return removed
}

// Add inserts a transaction if it's not already present.
// Returns true if added.
func (mp *Mempool) Add(tx types.Transaction) bool {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// 1. Check mempool size limit
	if len(mp.Pending) >= MaxMempoolSize {
		// Try to evict lowest fee transaction if new tx has higher fee
		if !mp.evictLowestFee(tx.Fee) {
			return false // Mempool full and new tx doesn't have higher fee
		}
	}

	// 2. Check minimum fee (anti-spam)
	if tx.Fee < MinFee {
		return false // Fee too low
	}

	// 3. Check transaction expiry (prevent delayed replay attacks)
	// Timestamp of 0 indicates legacy transaction - skip check for backwards compatibility
	if tx.Timestamp != 0 && tx.IsExpired(time.Now().UnixNano()) {
		return false // Transaction expired or from the future
	}

	// 4. Check State Nonce (Prevent Replay/Zombie Txs)
	if mp.State != nil {
		currentNonce := mp.State.GetNonce(tx.From)
		if tx.Nonce <= currentNonce {
			return false // Already processed
		}
	}

	// 5. Use Signature as ID
	var sigID [32]byte
	copy(sigID[:], tx.Sig)

	if _, exists := mp.Pending[sigID]; exists {
		return false // Duplicate
	}

	// 6. Add with TTL
	now := time.Now()
	mp.Pending[sigID] = &MempoolEntry{
		Tx:        tx,
		AddedAt:   now,
		ExpiresAt: now.Add(TxTTL),
	}

	return true
}

// evictLowestFee removes the transaction with the lowest fee if the new fee is higher.
// Must be called with lock held.
func (mp *Mempool) evictLowestFee(newFee uint64) bool {
	if len(mp.Pending) == 0 {
		return true
	}

	var lowestKey [32]byte
	var lowestFee uint64 = ^uint64(0) // Max uint64

	for key, entry := range mp.Pending {
		if entry.Tx.Fee < lowestFee {
			lowestFee = entry.Tx.Fee
			lowestKey = key
		}
	}

	// Only evict if new tx has higher fee
	if newFee > lowestFee {
		delete(mp.Pending, lowestKey)
		return true
	}

	return false
}

// Count returns the number of pending transactions.
func (mp *Mempool) Count() int {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return len(mp.Pending)
}

// GetBatch returns up to 'limit' transactions sorted by fee (highest first).
// Removes them from the pool.
func (mp *Mempool) GetBatch(limit int) []types.Transaction {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if len(mp.Pending) == 0 {
		return nil
	}

	// Collect all entries
	entries := make([]*MempoolEntry, 0, len(mp.Pending))
	keys := make([][32]byte, 0, len(mp.Pending))
	for key, entry := range mp.Pending {
		entries = append(entries, entry)
		keys = append(keys, key)
	}

	// Sort by fee (descending) - higher fee first
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Tx.Fee > entries[j].Tx.Fee
	})

	// Take top 'limit' transactions
	var batch []types.Transaction
	keysToRemove := make(map[[32]byte]bool)

	for i := 0; i < len(entries) && i < limit; i++ {
		batch = append(batch, entries[i].Tx)
		// Find the key for this entry
		var sigID [32]byte
		copy(sigID[:], entries[i].Tx.Sig)
		keysToRemove[sigID] = true
	}

	// Remove selected transactions
	for key := range keysToRemove {
		delete(mp.Pending, key)
	}

	return batch
}

// GetAll returns all pending transactions safely (for Explorer).
func (mp *Mempool) GetAll() []types.Transaction {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	var all []types.Transaction
	for _, entry := range mp.Pending {
		all = append(all, entry.Tx)
	}
	return all
}

// GetStats returns mempool statistics
func (mp *Mempool) GetStats() MempoolStats {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	stats := MempoolStats{
		TxCount: len(mp.Pending),
	}

	for _, entry := range mp.Pending {
		stats.TotalFees += entry.Tx.Fee
		if entry.Tx.Fee < stats.MinFee || stats.MinFee == 0 {
			stats.MinFee = entry.Tx.Fee
		}
		if entry.Tx.Fee > stats.MaxFee {
			stats.MaxFee = entry.Tx.Fee
		}
	}

	if stats.TxCount > 0 {
		stats.AvgFee = stats.TotalFees / uint64(stats.TxCount)
	}

	return stats
}

// MempoolStats contains mempool statistics
type MempoolStats struct {
	TxCount   int
	TotalFees uint64
	MinFee    uint64
	MaxFee    uint64
	AvgFee    uint64
}
