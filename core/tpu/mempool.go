package tpu

import (
	"novacoin/core/types"
	"sync"
)

// Mempool stores pending transactions that have explicitly NOT typically been included in a block yet.
// Typical lifecycle: P2P -> Mempool -> Miner -> Block -> Executor
type Mempool struct {
	Pending map[[32]byte]types.Transaction // Sig -> Tx
	mu      sync.RWMutex
}

func NewMempool() *Mempool {
	return &Mempool{
		Pending: make(map[[32]byte]types.Transaction),
	}
}

// Add inserts a transaction if it's not already present.
// Returns true if added.
func (mp *Mempool) Add(tx types.Transaction) bool {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Use Signature as ID
	var sigID [32]byte
	copy(sigID[:], tx.Sig)

	if _, exists := mp.Pending[sigID]; exists {
		return false
	}
	mp.Pending[sigID] = tx
	return true
}

// Count returns the number of pending transactions.
func (mp *Mempool) Count() int {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return len(mp.Pending)
}

// GetBatch returns up to 'limit' transactions and removes them from the pool (Conceptually).
// In a real system, we only remove upon block confirmation.
// For Simplicity: We remove them immediately when Miner pulls them (assuming they will execute).
func (mp *Mempool) GetBatch(limit int) []types.Transaction {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	var batch []types.Transaction
	count := 0
	for key, tx := range mp.Pending {
		if count >= limit {
			break
		}
		batch = append(batch, tx)
		delete(mp.Pending, key) // Remove immediately for prototype
		count++
	}
	return batch
}

// GetAll returns all pending transactions safely (for Explorer).
func (mp *Mempool) GetAll() []types.Transaction {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	var all []types.Transaction
	for _, tx := range mp.Pending {
		all = append(all, tx)
	}
	return all
}
