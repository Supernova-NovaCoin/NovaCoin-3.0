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
		Nonce:  1,
	}

	// Fake Block Time: Now
	blockTime := time.Now().UnixNano()

	if !executor.Execute(unstakeTx, blockTime) {
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

	expectedRelease := blockTime + (14 * 24 * time.Hour).Nanoseconds()
	if release != expectedRelease {
		t.Errorf("Release time mismatch. Got %d, Want %d", release, expectedRelease)
	}

	// 2. Test Withdraw (Too Early)
	withdrawTx := types.Transaction{
		Type:  types.TxWithdraw,
		From:  addr,
		Nonce: 2,
	}

	// Execute at same block time (Should Fail)
	if executor.Execute(withdrawTx, blockTime) {
		t.Error("Withdraw succeeded too early")
	}

	// 3. Test Withdraw (Time Travel)
	// Execute at future time
	futureTime := expectedRelease + 1

	if !executor.Execute(withdrawTx, futureTime) {
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

func TestDelegation(t *testing.T) {
	state := NewStateManager()
	executor := NewExecutor(state)

	var delegator [32]byte
	delegator[0] = 0xAA
	var validator [32]byte
	validator[0] = 0xBB

	// Fund Delegator
	state.SetBalance(delegator, 2000)

	// Validator must have minimum stake (1000 NVN = 1000 * 1_000_000 nanoNVN) to accept delegations
	// This is a security feature to prevent delegating to random addresses
	state.SetStake(validator, MinValidatorStakeForDelegation)

	tx := types.Transaction{
		Type:   types.TxDelegate,
		From:   delegator,
		To:     validator,
		Amount: 500,
		Nonce:  1,
	}

	if !executor.Execute(tx, 0) {
		t.Fatal("Delegation failed")
	}

	// Check State
	if state.GetBalance(delegator) != 1500 {
		t.Errorf("Balance not deducted")
	}
	// Validator stake should be initial stake + delegation
	expectedValidatorStake := MinValidatorStakeForDelegation + 500
	if state.GetStake(validator) != expectedValidatorStake {
		t.Errorf("Validator stake incorrect: got %d, expected %d", state.GetStake(validator), expectedValidatorStake)
	}
	// Check Delegation Tracking
	if state.GetDelegation(delegator, validator) != 500 {
		t.Errorf("Delegation map not updated")
	}
}

func TestDelegationToNonValidator(t *testing.T) {
	state := NewStateManager()
	executor := NewExecutor(state)

	var delegator [32]byte
	delegator[0] = 0xCC
	var nonValidator [32]byte
	nonValidator[0] = 0xDD

	// Fund Delegator
	state.SetBalance(delegator, 2000)

	// Don't set stake for nonValidator - they're not a validator

	tx := types.Transaction{
		Type:   types.TxDelegate,
		From:   delegator,
		To:     nonValidator,
		Amount: 500,
		Nonce:  1,
	}

	// Delegation should FAIL because target is not a validator
	if executor.Execute(tx, 0) {
		t.Fatal("Delegation to non-validator should have failed")
	}

	// Balance should remain unchanged
	if state.GetBalance(delegator) != 2000 {
		t.Errorf("Balance should not have been deducted")
	}
}
