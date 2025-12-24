package tpu

import (
	"novacoin/core/types"
	"testing"
	"time"
)

func TestMempoolAdd(t *testing.T) {
	mp := NewMempool(nil)
	defer mp.Stop()

	tx := types.Transaction{
		Type:   types.TxTransfer,
		Amount: 1000,
		Fee:    MinFee, // Exactly minimum fee
		Nonce:  1,
		Sig:    make([]byte, 64),
	}
	tx.Sig[0] = 1 // Make signature unique

	// Should succeed with minimum fee
	if !mp.Add(tx) {
		t.Error("Add should succeed with minimum fee")
	}

	// Should reject duplicate
	if mp.Add(tx) {
		t.Error("Add should reject duplicate")
	}

	// Should reject below minimum fee
	tx2 := tx
	tx2.Fee = MinFee - 1
	tx2.Sig = make([]byte, 64)
	tx2.Sig[0] = 2
	if mp.Add(tx2) {
		t.Error("Add should reject tx with fee below minimum")
	}
}

func TestMempoolFeeOrdering(t *testing.T) {
	mp := NewMempool(nil)
	defer mp.Stop()

	// Add transactions with different fees
	fees := []uint64{MinFee, MinFee * 5, MinFee * 2, MinFee * 10}

	for i, fee := range fees {
		tx := types.Transaction{
			Fee:   fee,
			Nonce: uint64(i + 1),
			Sig:   make([]byte, 64),
		}
		tx.Sig[0] = byte(i + 1)
		if !mp.Add(tx) {
			t.Fatalf("Failed to add tx with fee %d", fee)
		}
	}

	// Get batch - should be sorted by fee descending
	batch := mp.GetBatch(10)

	if len(batch) != 4 {
		t.Fatalf("Expected 4 txs, got %d", len(batch))
	}

	// Verify descending order
	for i := 1; i < len(batch); i++ {
		if batch[i-1].Fee < batch[i].Fee {
			t.Errorf("Batch not sorted: %d < %d", batch[i-1].Fee, batch[i].Fee)
		}
	}

	// First should be highest fee (MinFee * 10)
	if batch[0].Fee != MinFee*10 {
		t.Errorf("Expected highest fee %d first, got %d", MinFee*10, batch[0].Fee)
	}
}

func TestMempoolMaxSize(t *testing.T) {
	mp := NewMempool(nil)
	defer mp.Stop()

	// Fill mempool to max
	for i := 0; i < MaxMempoolSize; i++ {
		tx := types.Transaction{
			Fee:   MinFee,
			Nonce: uint64(i + 1),
			Sig:   make([]byte, 64),
		}
		// Create unique signature
		tx.Sig[0] = byte(i % 256)
		tx.Sig[1] = byte((i / 256) % 256)
		tx.Sig[2] = byte((i / 65536) % 256)
		mp.Add(tx)
	}

	if mp.Count() != MaxMempoolSize {
		t.Errorf("Expected %d txs, got %d", MaxMempoolSize, mp.Count())
	}

	// Adding tx with same fee should fail
	tx := types.Transaction{
		Fee:   MinFee,
		Nonce: MaxMempoolSize + 1,
		Sig:   make([]byte, 64),
	}
	tx.Sig[0] = 0xFF
	tx.Sig[1] = 0xFF
	tx.Sig[2] = 0xFF
	if mp.Add(tx) {
		t.Error("Should reject tx when full and fee not higher")
	}

	// Adding tx with higher fee should succeed (evicting lowest)
	tx.Fee = MinFee * 2
	if !mp.Add(tx) {
		t.Error("Should accept higher fee tx when full")
	}
}

func TestMempoolExpiry(t *testing.T) {
	// Use a short TTL for testing
	mp := &Mempool{
		Pending:     make(map[[32]byte]*MempoolEntry),
		State:       nil,
		stopCleanup: make(chan struct{}),
	}

	// Add a transaction with expired time
	tx := types.Transaction{
		Fee:   MinFee,
		Nonce: 1,
		Sig:   make([]byte, 64),
	}
	tx.Sig[0] = 1

	var sigID [32]byte
	copy(sigID[:], tx.Sig)

	mp.Pending[sigID] = &MempoolEntry{
		Tx:        tx,
		AddedAt:   time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Already expired
	}

	// Should have 1 tx
	if mp.Count() != 1 {
		t.Errorf("Expected 1 tx, got %d", mp.Count())
	}

	// Remove expired
	removed := mp.RemoveExpired()
	if removed != 1 {
		t.Errorf("Expected 1 removed, got %d", removed)
	}

	// Should be empty
	if mp.Count() != 0 {
		t.Errorf("Expected 0 txs after expiry, got %d", mp.Count())
	}
}

func TestMempoolStats(t *testing.T) {
	mp := NewMempool(nil)
	defer mp.Stop()

	fees := []uint64{MinFee, MinFee * 2, MinFee * 3}

	for i, fee := range fees {
		tx := types.Transaction{
			Fee:   fee,
			Nonce: uint64(i + 1),
			Sig:   make([]byte, 64),
		}
		tx.Sig[0] = byte(i + 1)
		mp.Add(tx)
	}

	stats := mp.GetStats()

	if stats.TxCount != 3 {
		t.Errorf("Expected TxCount 3, got %d", stats.TxCount)
	}

	expectedTotal := MinFee + MinFee*2 + MinFee*3
	if stats.TotalFees != expectedTotal {
		t.Errorf("Expected TotalFees %d, got %d", expectedTotal, stats.TotalFees)
	}

	if stats.MinFee != MinFee {
		t.Errorf("Expected MinFee %d, got %d", MinFee, stats.MinFee)
	}

	if stats.MaxFee != MinFee*3 {
		t.Errorf("Expected MaxFee %d, got %d", MinFee*3, stats.MaxFee)
	}
}
