package execution

import (
	"fmt"
	"novacoin/core/types"
	"time"
)

type Executor struct {
	State *StateManager
}

func NewExecutor(state *StateManager) *Executor {
	return &Executor{State: state}
}

// ProcessBatch simulates parallel execution.
// In a real "Supernova", this would use goroutines per shard.
func (e *Executor) ProcessBatch(txs []types.Transaction) {
	// Simple sequential for prototype, but structured for parallel
	for _, tx := range txs {
		e.Execute(tx)
	}
}

func (e *Executor) Execute(tx types.Transaction) bool {
	// 1. Replay Protection (Nonce)
	currentNonce := e.State.GetNonce(tx.From)
	// Strict Ordering: Tx Nonce must be exactly Current Nonce + 1
	if tx.Nonce <= currentNonce {
		fmt.Printf("Tx Rejected: Old Nonce %d (Expected > %d)\n", tx.Nonce, currentNonce)
		return false
	}
	// For MVP, we allow gaps to make testing easier (so just > current),
	// but normally it should be == current + 1.
	// We will enforce > current.

	success := false
	switch tx.Type {
	case types.TxTransfer:
		success = e.executeTransfer(tx)
	case types.TxStake:
		success = e.executeStake(tx)
	case types.TxUnstake:
		success = e.executeUnstake(tx)
	case types.TxDelegate:
		success = e.executeDelegate(tx)
	case types.TxWithdraw:
		success = e.executeWithdraw(tx)
	case types.TxGrant:
		success = e.executeGrant(tx)
	case types.TxBuyLicense:
		success = e.executeBuyLicense(tx)
	default:
		fmt.Printf("Unknown Tx Type: %d\n", tx.Type)
		success = false
	}

	if success {
		e.State.SetNonce(tx.From, tx.Nonce)
	}
	return success
}

func (e *Executor) executeTransfer(tx types.Transaction) bool {
	if tx.Amount == 0 {
		return false
	}
	totalCost := tx.Amount + tx.Fee
	senderBal := e.State.GetBalance(tx.From)
	if senderBal < totalCost {
		fmt.Printf("Tx Failed: Insufficient Funds %x (Need %d)\n", tx.From[:4], totalCost)
		return false
	}

	e.State.SetBalance(tx.From, senderBal-totalCost) // Deduct Amount + Fee
	receiverBal := e.State.GetBalance(tx.To)
	e.State.SetBalance(tx.To, receiverBal+tx.Amount) // Credit Amount only

	// Fee is effectively "collected" by not being credited to anyone yet.
	// In a real batch, we'd sum this up. here we return true.
	return true
}

// ApplyBlockReward issues new coins and fees to the block author.
// Economic Policy: "Supernova Curve" - Starts at 500 NVN, Halves every 2 Years.
// + 50% of Fees Burned, 50% to Author.
func (e *Executor) ApplyBlockReward(author [32]byte, collectedFees uint64) {
	// 1. Calculate Base Reward based on Time (Halving)
	// Genesis: Jan 1 2025 (Approx). 2 Years = 63072000s
	elapsed := time.Now().Unix() - 1735689600 // roughly 2025-01-01
	if elapsed < 0 {
		elapsed = 0
	} // Safety

	halvings := elapsed / 63072000
	baseReward := uint64(500 * 1_000_000) // 500 NVN

	// Apply Halvings (Bitwise shift for integer division)
	if halvings > 0 {
		baseReward >>= halvings
	}
	if baseReward == 0 {
		baseReward = 1
	} // Minimum tail emission

	// 2. Fee Burning Logic
	validatorFeeShare := collectedFees / 2
	burned := collectedFees - validatorFeeShare

	totalReward := baseReward + validatorFeeShare

	// 3. Credit Author
	currentBal := e.State.GetBalance(author)
	e.State.SetBalance(author, currentBal+totalReward)

	fmt.Printf("🎁 Block Reward: %d NVN (Base) + %d NVN (Fees) | 🔥 Burned: %d NVN\n",
		baseReward/1_000_000, validatorFeeShare/1_000_000, burned/1_000_000)
}

func (e *Executor) executeStake(tx types.Transaction) bool {
	if tx.Amount == 0 {
		return false
	}
	senderBal := e.State.GetBalance(tx.From)
	if senderBal < tx.Amount {
		fmt.Printf("Stake Failed: Insufficient Funds %x\n", tx.From[:4])
		return false
	}

	// Move Balance -> Stake
	e.State.SetBalance(tx.From, senderBal-tx.Amount)
	currentStake := e.State.GetStake(tx.From)
	e.State.SetStake(tx.From, currentStake+tx.Amount)

	fmt.Printf("🔒 Staked %d NVN for %x\n", tx.Amount/1_000_000, tx.From[:4])
	return true
}

func (e *Executor) executeUnstake(tx types.Transaction) bool {
	if tx.Amount == 0 {
		return false
	}
	currentStake := e.State.GetStake(tx.From)
	if currentStake < tx.Amount {
		fmt.Printf("Unstake Failed: Insufficient Stake %x\n", tx.From[:4])
		return false
	}

	// Move Stake -> Unbonding (Locked)
	// Enforce 14-day unbonding period
	e.State.SetStake(tx.From, currentStake-tx.Amount)

	currentUnbonding, _ := e.State.GetUnbonding(tx.From)
	e.State.SetUnbonding(tx.From, currentUnbonding+tx.Amount, time.Now().Add(14*24*time.Hour).Unix())

	fmt.Printf("⏳ Unstaked %d NVN for %x. Locked for 14 days.\n", tx.Amount/1_000_000, tx.From[:4])
	return true
}

func (e *Executor) executeWithdraw(tx types.Transaction) bool {
	currentUnbonding, releaseTime := e.State.GetUnbonding(tx.From)
	if currentUnbonding == 0 {
		fmt.Printf("Withdraw Failed: No unbonding funds %x\n", tx.From[:4])
		return false
	}

	if time.Now().Unix() < releaseTime {
		fmt.Printf("Withdraw Failed: Funds locked until %s\n", time.Unix(releaseTime, 0))
		return false
	}

	// Move Unbonding -> Balance
	e.State.SetUnbonding(tx.From, 0, 0) // Clear unbonding
	currentBal := e.State.GetBalance(tx.From)
	e.State.SetBalance(tx.From, currentBal+currentUnbonding)

	fmt.Printf("🔓 Withdrawn %d NVN for %x\n", currentUnbonding/1_000_000, tx.From[:4])
	return true
}

func (e *Executor) executeGrant(tx types.Transaction) bool {
	// Genesis only command (Simple permission check: only allow if from Genesis 1 for now)
	// In real logic, we'd check if sender is a "Governance" address.
	// For this MVP, anyone can grant if they pay (meaning Genesis pays 1000 NVN to grant).

	totalCost := tx.Amount + tx.Fee
	senderBal := e.State.GetBalance(tx.From)
	if senderBal < totalCost {
		return false
	}
	e.State.SetBalance(tx.From, senderBal-totalCost)

	// Add to RECIPIENT's GrantStake
	// Note: tx.To is the beneficiary
	currentGrant := e.State.GetGrantStake(tx.To)
	e.State.SetGrantStake(tx.To, currentGrant+tx.Amount)

	fmt.Printf("🛡️  LICENSE GRANTED to %x. %d NVN Locked Forever.\n", tx.To[:4], tx.Amount/1_000_000)
	return true
}

func (e *Executor) executeBuyLicense(tx types.Transaction) bool {
	// User buys license for themselves. Amount is BURNED.
	totalCost := tx.Amount + tx.Fee
	senderBal := e.State.GetBalance(tx.From)
	if senderBal < totalCost {
		return false
	}

	// 1. Burn the Cost (Move from Balance -> Nowhere)
	e.State.SetBalance(tx.From, senderBal-totalCost)

	// 2. Credit License (GrantStake)
	currentGrant := e.State.GetGrantStake(tx.From)
	e.State.SetGrantStake(tx.From, currentGrant+tx.Amount)

	fmt.Printf("🔥 LICENSE PURCHASED by %x. %d NVN Burned for rights.\n", tx.From[:4], tx.Amount/1_000_000)
	return true
}

func (e *Executor) executeDelegate(tx types.Transaction) bool {
	if tx.Amount == 0 {
		return false
	}
	senderBal := e.State.GetBalance(tx.From)
	if senderBal < tx.Amount {
		fmt.Printf("Delegate Failed: Insufficient Funds %x\n", tx.From[:4])
		return false
	}

	// Move Balance (Delegator) -> Stake (Pool)
	e.State.SetBalance(tx.From, senderBal-tx.Amount)

	currentPoolStake := e.State.GetStake(tx.To)
	e.State.SetStake(tx.To, currentPoolStake+tx.Amount)

	fmt.Printf("🗳️  Delegated %d NVN to Pool %x\n", tx.Amount/1_000_000, tx.To[:4])
	return true
}
