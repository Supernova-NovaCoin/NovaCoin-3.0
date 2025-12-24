package execution

import (
	"fmt"
	safemath "novacoin/core/math"
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
func (e *Executor) ProcessBatch(txs []types.Transaction, timestamp int64) {
	// Simple sequential for prototype, but structured for parallel
	for _, tx := range txs {
		e.Execute(tx, timestamp)
	}
}

func (e *Executor) Execute(tx types.Transaction, timestamp int64) bool {
	// 1. Replay Protection (Nonce) - STRICT ORDERING REQUIRED
	currentNonce := e.State.GetNonce(tx.From)
	expectedNonce := currentNonce + 1

	// SECURITY: Nonce MUST be exactly currentNonce + 1
	// This prevents:
	// - Replay attacks (reusing old transactions)
	// - Transaction reordering attacks
	// - Nonce gap exploits
	if tx.Nonce != expectedNonce {
		fmt.Printf("Tx Rejected: Invalid Nonce %d (Expected exactly %d)\n", tx.Nonce, expectedNonce)
		return false
	}

	success := false
	switch tx.Type {
	case types.TxTransfer:
		success = e.executeTransfer(tx)
	case types.TxStake:
		success = e.executeStake(tx)
	case types.TxUnstake:
		success = e.executeUnstake(tx, timestamp)
	case types.TxDelegate:
		success = e.executeDelegate(tx)
	case types.TxWithdraw:
		success = e.executeWithdraw(tx, timestamp)
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

	// Prevent self-transfer
	if tx.From == tx.To {
		fmt.Printf("Tx Failed: Self-transfer not allowed %x\n", tx.From[:4])
		return false
	}

	// Safe arithmetic: check for overflow when adding amount + fee
	totalCost, err := safemath.SafeAdd(tx.Amount, tx.Fee)
	if err != nil {
		fmt.Printf("Tx Failed: Overflow in cost calculation %x\n", tx.From[:4])
		return false
	}

	senderBal := e.State.GetBalance(tx.From)
	if senderBal < totalCost {
		fmt.Printf("Tx Failed: Insufficient Funds %x (Need %d)\n", tx.From[:4], totalCost)
		return false
	}

	// Safe subtraction (already validated above, but using safe math for consistency)
	newSenderBal, err := safemath.SafeSub(senderBal, totalCost)
	if err != nil {
		fmt.Printf("Tx Failed: Underflow in sender balance %x\n", tx.From[:4])
		return false
	}
	e.State.SetBalance(tx.From, newSenderBal)

	// Safe addition for receiver
	receiverBal := e.State.GetBalance(tx.To)
	newReceiverBal, err := safemath.SafeAdd(receiverBal, tx.Amount)
	if err != nil {
		// Overflow in receiver balance - this would be catastrophic, rollback
		e.State.SetBalance(tx.From, senderBal) // Restore sender
		fmt.Printf("Tx Failed: Overflow in receiver balance %x\n", tx.To[:4])
		return false
	}
	e.State.SetBalance(tx.To, newReceiverBal)

	return true
}

// ApplyBlockReward issues new coins and fees to the block author.
// Economic Policy: "Supernova Curve" - Starts at 500 NVN, Halves every 2 Years.
// + 50% of Fees Burned, 50% to Author.
func (e *Executor) ApplyBlockReward(author [32]byte, collectedFees uint64, timestamp int64) {
	// 1. Calculate Base Reward based on Time (Halving)
	// Genesis: Jan 1 2025 (Approx). 2 Years = 63072000s
	// Pulse uses UnixNano. We need Seconds.
	tsSeconds := timestamp / 1_000_000_000
	elapsed := tsSeconds - 1735689600 // roughly 2025-01-01
	if elapsed < 0 {
		elapsed = 0
	}

	halvings := elapsed / 63072000
	baseReward := uint64(500 * 1_000_000) // 500 NVN

	// Apply Halvings (Bitwise shift for integer division)
	if halvings > 0 {
		baseReward >>= halvings
	}
	if baseReward == 0 {
		baseReward = 1 // Minimum tail emission
	}

	// 2. Fee Burning Logic
	validatorFeeShare := collectedFees / 2
	burned := collectedFees - validatorFeeShare

	// Safe addition for total reward
	totalReward, err := safemath.SafeAdd(baseReward, validatorFeeShare)
	if err != nil {
		// Overflow in reward calculation - use base only
		totalReward = baseReward
		fmt.Printf("⚠️ Block Reward: Fee overflow, using base only\n")
	}

	// 3. Safe addition for author balance
	currentBal := e.State.GetBalance(author)
	newBalance, err := safemath.SafeAdd(currentBal, totalReward)
	if err != nil {
		// This is catastrophic - author has max balance
		fmt.Printf("⚠️ Block Reward: Author balance overflow, reward lost\n")
		return
	}
	e.State.SetBalance(author, newBalance)

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

	// Safe subtraction for balance
	newBalance, err := safemath.SafeSub(senderBal, tx.Amount)
	if err != nil {
		fmt.Printf("Stake Failed: Underflow %x\n", tx.From[:4])
		return false
	}

	// Safe addition for stake
	currentStake := e.State.GetStake(tx.From)
	newStake, err := safemath.SafeAdd(currentStake, tx.Amount)
	if err != nil {
		fmt.Printf("Stake Failed: Overflow %x\n", tx.From[:4])
		return false
	}

	e.State.SetBalance(tx.From, newBalance)
	e.State.SetStake(tx.From, newStake)

	fmt.Printf("🔒 Staked %d NVN for %x\n", tx.Amount/1_000_000, tx.From[:4])
	return true
}

func (e *Executor) executeUnstake(tx types.Transaction, timestamp int64) bool {
	if tx.Amount == 0 {
		return false
	}
	currentStake := e.State.GetStake(tx.From)
	if currentStake < tx.Amount {
		fmt.Printf("Unstake Failed: Insufficient Stake %x\n", tx.From[:4])
		return false
	}

	// Safe subtraction for stake
	newStake, err := safemath.SafeSub(currentStake, tx.Amount)
	if err != nil {
		fmt.Printf("Unstake Failed: Underflow %x\n", tx.From[:4])
		return false
	}

	// Safe addition for unbonding
	currentUnbonding, _ := e.State.GetUnbonding(tx.From)
	newUnbonding, err := safemath.SafeAdd(currentUnbonding, tx.Amount)
	if err != nil {
		fmt.Printf("Unstake Failed: Unbonding Overflow %x\n", tx.From[:4])
		return false
	}

	// Enforce 14-day unbonding period
	releaseTime := timestamp + (14 * 24 * time.Hour).Nanoseconds()

	e.State.SetStake(tx.From, newStake)
	e.State.SetUnbonding(tx.From, newUnbonding, releaseTime)

	fmt.Printf("⏳ Unstaked %d NVN for %x. Locked for 14 days.\n", tx.Amount/1_000_000, tx.From[:4])
	return true
}

func (e *Executor) executeWithdraw(tx types.Transaction, timestamp int64) bool {
	currentUnbonding, releaseTime := e.State.GetUnbonding(tx.From)
	if currentUnbonding == 0 {
		fmt.Printf("Withdraw Failed: No unbonding funds %x\n", tx.From[:4])
		return false
	}

	if timestamp < releaseTime {
		fmt.Printf("Withdraw Failed: Funds locked until %d (Current %d)\n", releaseTime, timestamp)
		return false
	}

	// Safe addition for balance
	currentBal := e.State.GetBalance(tx.From)
	newBalance, err := safemath.SafeAdd(currentBal, currentUnbonding)
	if err != nil {
		fmt.Printf("Withdraw Failed: Balance Overflow %x\n", tx.From[:4])
		return false
	}

	// Move Unbonding -> Balance
	e.State.SetUnbonding(tx.From, 0, 0) // Clear unbonding
	e.State.SetBalance(tx.From, newBalance)

	fmt.Printf("🔓 Withdrawn %d NVN for %x\n", currentUnbonding/1_000_000, tx.From[:4])
	return true
}

// MinStakeForGrantAuthority defines minimum stake required to grant licenses
// Only accounts with >= 1 billion NVN stake can grant (genesis validators)
const MinStakeForGrantAuthority = uint64(1_000_000_000 * 1_000_000) // 1B NVN in nanoNVN

func (e *Executor) executeGrant(tx types.Transaction) bool {
	// SECURITY: Authorization check - only high-stake validators can grant
	// This prevents arbitrary stake creation and protects economic model
	senderStake := e.State.GetStake(tx.From)
	senderGrant := e.State.GetGrantStake(tx.From)
	totalSenderStake, err := safemath.SafeAdd(senderStake, senderGrant)
	if err != nil {
		fmt.Printf("Grant Failed: Stake calculation overflow %x\n", tx.From[:4])
		return false
	}

	if totalSenderStake < MinStakeForGrantAuthority {
		fmt.Printf("Grant Failed: Unauthorized - sender %x has insufficient stake (%d < %d)\n",
			tx.From[:4], totalSenderStake/1_000_000, MinStakeForGrantAuthority/1_000_000)
		return false
	}

	// Prevent self-grant (use BuyLicense for self)
	if tx.From == tx.To {
		fmt.Printf("Grant Failed: Cannot self-grant, use BuyLicense instead %x\n", tx.From[:4])
		return false
	}

	totalCost, err := safemath.SafeAdd(tx.Amount, tx.Fee)
	if err != nil {
		fmt.Printf("Grant Failed: Cost Overflow %x\n", tx.From[:4])
		return false
	}

	senderBal := e.State.GetBalance(tx.From)
	if senderBal < totalCost {
		fmt.Printf("Grant Failed: Insufficient balance %x\n", tx.From[:4])
		return false
	}

	newBalance, err := safemath.SafeSub(senderBal, totalCost)
	if err != nil {
		return false
	}

	// Safe addition for grant stake
	currentGrant := e.State.GetGrantStake(tx.To)
	newGrant, err := safemath.SafeAdd(currentGrant, tx.Amount)
	if err != nil {
		fmt.Printf("Grant Failed: GrantStake Overflow %x\n", tx.To[:4])
		return false
	}

	e.State.SetBalance(tx.From, newBalance)
	e.State.SetGrantStake(tx.To, newGrant)

	fmt.Printf("🛡️  LICENSE GRANTED to %x by %x. %d NVN Locked Forever.\n",
		tx.To[:4], tx.From[:4], tx.Amount/1_000_000)
	return true
}

func (e *Executor) executeBuyLicense(tx types.Transaction) bool {
	// User buys license for themselves. Amount is BURNED.
	totalCost, err := safemath.SafeAdd(tx.Amount, tx.Fee)
	if err != nil {
		fmt.Printf("BuyLicense Failed: Cost Overflow %x\n", tx.From[:4])
		return false
	}

	senderBal := e.State.GetBalance(tx.From)
	if senderBal < totalCost {
		return false
	}

	newBalance, err := safemath.SafeSub(senderBal, totalCost)
	if err != nil {
		return false
	}

	// Safe addition for grant stake
	currentGrant := e.State.GetGrantStake(tx.From)
	newGrant, err := safemath.SafeAdd(currentGrant, tx.Amount)
	if err != nil {
		fmt.Printf("BuyLicense Failed: GrantStake Overflow %x\n", tx.From[:4])
		return false
	}

	// 1. Burn the Cost (Move from Balance -> Nowhere)
	e.State.SetBalance(tx.From, newBalance)

	// 2. Credit License (GrantStake)
	e.State.SetGrantStake(tx.From, newGrant)

	fmt.Printf("🔥 LICENSE PURCHASED by %x. %d NVN Burned for rights.\n", tx.From[:4], tx.Amount/1_000_000)
	return true
}

// MinValidatorStakeForDelegation is the minimum stake a validator needs to accept delegations
const MinValidatorStakeForDelegation = uint64(1000 * 1_000_000) // 1000 NVN (same as MinStakeRequired)

func (e *Executor) executeDelegate(tx types.Transaction) bool {
	if tx.Amount == 0 {
		return false
	}

	// Prevent self-delegation
	if tx.From == tx.To {
		fmt.Printf("Delegate Failed: Cannot delegate to self %x\n", tx.From[:4])
		return false
	}

	// SECURITY: Verify target is a valid validator (has sufficient stake)
	// This prevents delegating to random addresses which could lock funds
	validatorStake := e.State.GetStake(tx.To)
	validatorGrant := e.State.GetGrantStake(tx.To)
	totalValidatorStake, err := safemath.SafeAdd(validatorStake, validatorGrant)
	if err != nil {
		fmt.Printf("Delegate Failed: Validator stake overflow %x\n", tx.To[:4])
		return false
	}

	if totalValidatorStake < MinValidatorStakeForDelegation {
		fmt.Printf("Delegate Failed: Target %x is not a validator (stake %d < %d required)\n",
			tx.To[:4], totalValidatorStake/1_000_000, MinValidatorStakeForDelegation/1_000_000)
		return false
	}

	senderBal := e.State.GetBalance(tx.From)
	if senderBal < tx.Amount {
		fmt.Printf("Delegate Failed: Insufficient Funds %x\n", tx.From[:4])
		return false
	}

	// Safe subtraction for balance
	newBalance, err := safemath.SafeSub(senderBal, tx.Amount)
	if err != nil {
		return false
	}

	// Safe addition for pool stake
	currentPoolStake := e.State.GetStake(tx.To)
	newPoolStake, err := safemath.SafeAdd(currentPoolStake, tx.Amount)
	if err != nil {
		fmt.Printf("Delegate Failed: Pool Stake Overflow %x\n", tx.To[:4])
		return false
	}

	// Safe addition for delegation tracking
	currentDelegation := e.State.GetDelegation(tx.From, tx.To)
	newDelegation, err := safemath.SafeAdd(currentDelegation, tx.Amount)
	if err != nil {
		fmt.Printf("Delegate Failed: Delegation Overflow %x\n", tx.From[:4])
		return false
	}

	// Apply all changes
	e.State.SetBalance(tx.From, newBalance)
	e.State.SetStake(tx.To, newPoolStake)
	e.State.SetDelegation(tx.From, tx.To, newDelegation)

	fmt.Printf("🗳️  Delegated %d NVN to Validator %x\n", tx.Amount/1_000_000, tx.To[:4])
	return true
}
