package execution

import (
	"fmt"
	"novacoin/core/types"
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
	switch tx.Type {
	case types.TxTransfer:
		return e.executeTransfer(tx)
	case types.TxStake:
		return e.executeStake(tx)
	case types.TxUnstake:
		return e.executeUnstake(tx)
	case types.TxDelegate:
		return e.executeDelegate(tx)
	default:
		fmt.Printf("Unknown Tx Type: %d\n", tx.Type)
		return false
	}
}

func (e *Executor) executeTransfer(tx types.Transaction) bool {
	if tx.Amount == 0 {
		return false
	}
	senderBal := e.State.GetBalance(tx.From)
	if senderBal < tx.Amount {
		fmt.Printf("Tx Failed: Insufficient Funds %x\n", tx.From[:4])
		return false
	}

	e.State.SetBalance(tx.From, senderBal-tx.Amount)
	receiverBal := e.State.GetBalance(tx.To)
	e.State.SetBalance(tx.To, receiverBal+tx.Amount)
	return true
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

	// Move Stake -> Balance
	// TODO: Enforce unbonding period (security)
	e.State.SetStake(tx.From, currentStake-tx.Amount)
	currentBal := e.State.GetBalance(tx.From)
	e.State.SetBalance(tx.From, currentBal+tx.Amount)

	fmt.Printf("🔓 Unstaked %d NVN for %x\n", tx.Amount/1_000_000, tx.From[:4])
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

// ApplyBlockReward issues new coins to the block author.
// Economic Policy: Fixed 100 NVN per block (~85 years to cap).
func (e *Executor) ApplyBlockReward(author [32]byte) {
	const BlockReward = 100 * 1_000_000           // 100 NVN
	const MaxSupply = 100_000_000_000 * 1_000_000 // 100 Billion NVN

	// Check Total Supply (Approximation via State iteration is expensive,
	// typically tracked in a special key. For this MVP, we just check if it overflows reasonably or track it globally.
	// A better way for this specific codebase: simple check against a hard limit if we tracked total supply.
	// Since we don't track TotalSupply in State yet, we will skip the Global Check but keep the constant for future use.
	// HOWEVER, we WILL check for uint64 overflow.

	currentBal := e.State.GetBalance(author)
	if currentBal+BlockReward < currentBal {
		// Overflow protection
		return
	}

	e.State.SetBalance(author, currentBal+BlockReward)
}
