package execution

import (
	"novacoin/core/types"
	"testing"
	"time"
)

func TestUnstakeAndWithdraw(t *testing.T) {
	state := NewStateManager()
	executor := NewExecutor(state)

	// Setup account with stake
	var addr [32]byte
	addr[0] = 1
	initialStake := uint64(1000)
	state.SetStake(addr, initialStake)

	// 1. Test Unstake
	unstakeTx := types.Transaction{
		Type:   types.TxUnstake,
		From:   addr,
		Amount: 500,
	}

	if !executor.Execute(unstakeTx) {
		t.Fatal("Unstake failed")
	}

	// Verify Stake reduced
	if state.GetStake(addr) != 500 {
		t.Errorf("Expected stake 500, got %d", state.GetStake(addr))
	}

	// Verify Unbonding
	unbonding, release := state.GetUnbonding(addr)
	if unbonding != 500 {
		t.Errorf("Expected unbonding 500, got %d", unbonding)
	}
	if release <= time.Now().Unix() {
		t.Error("Release time should be in future")
	}

	// 2. Test Withdraw (Too Early)
	withdrawTx := types.Transaction{
		Type: types.TxWithdraw,
		From: addr,
	}
	if executor.Execute(withdrawTx) {
		t.Error("Withdraw succeeded too early")
	}

	// 3. Test Withdraw (Time Travel)
	// Manually hack state time or mock time?
	// Since we use time.Now() directly in code, we can't easily mock without refactor.
	// For this quick verification, we can reduce the unbonding time in state manually to past.
	state.SetUnbonding(addr, 500, time.Now().Add(-1*time.Hour).Unix())

	if !executor.Execute(withdrawTx) {
		t.Fatal("Withdraw failed after release time")
	}

	// Verify Balance increased
	if state.GetBalance(addr) != 500 {
		t.Errorf("Expected balance 500, got %d", state.GetBalance(addr))
	}

	// Verify Unbonding Cleared
	unbonding, _ = state.GetUnbonding(addr)
	if unbonding != 0 {
		t.Errorf("Expected unbonding 0, got %d", unbonding)
	}
}
