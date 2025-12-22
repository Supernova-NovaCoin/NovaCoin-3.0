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
	senderBal := e.State.GetBalance(tx.From)
	if senderBal < tx.Amount {
		fmt.Printf("Tx Failed: Insufficient Funds %x\n", tx.From[:4])
		return false
	}

	e.State.SetBalance(tx.From, senderBal-tx.Amount)
	receiverBal := e.State.GetBalance(tx.To)
	e.State.SetBalance(tx.To, receiverBal+tx.Amount)

	// Burn logic (50% fee) would go here

	return true
}
