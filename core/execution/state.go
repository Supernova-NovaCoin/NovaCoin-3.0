package execution

import (
	"sync"
)

// Account represents a user's state in the ledger.
type Account struct {
	Address [32]byte
	Balance uint64
	Nonce   uint64
}

// StateManager handles a sharded set of accounts.
// In "Supernova", this would be Partitioned across memory banks.
type StateManager struct {
	Accounts map[[32]byte]*Account
	mu       sync.RWMutex
}

func NewStateManager() *StateManager {
	return &StateManager{
		Accounts: make(map[[32]byte]*Account),
	}
}

func (sm *StateManager) GetBalance(addr [32]byte) uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if acc, ok := sm.Accounts[addr]; ok {
		return acc.Balance
	}
	return 0
}

func (sm *StateManager) SetBalance(addr [32]byte, amount uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.Accounts[addr]; !ok {
		sm.Accounts[addr] = &Account{Address: addr, Balance: 0, Nonce: 0}
	}
	sm.Accounts[addr].Balance = amount
}
