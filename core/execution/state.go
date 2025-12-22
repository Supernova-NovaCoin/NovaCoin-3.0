package execution

import (
	"bytes"
	"encoding/gob"
	"log"
	"novacoin/core/store"
	"sync"

	"github.com/dgraph-io/badger/v4"
)

// Account represents a user's state in the ledger.
type Account struct {
	Address [32]byte
	Balance uint64
	Stake   uint64
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
	// Try load from DB
	if acc := sm.loadAccount(addr); acc != nil {
		sm.Accounts[addr] = acc // Cache it
		return acc.Balance
	}
	return 0
}

// Helper to access global DB
func store_DB_Update(fn func(txn *badger.Txn) error) error {
	if store.DB == nil {
		return nil
	}
	return store.DB.Update(fn)
}

// Helper to access global DB (View)
func store_DB_View(fn func(txn *badger.Txn) error) error {
	if store.DB == nil {
		return nil
	}
	return store.DB.View(fn)
}

func (sm *StateManager) loadAccount(addr [32]byte) *Account {
	var acc Account
	err := store_DB_View(func(txn *badger.Txn) error {
		key := append([]byte("a:"), addr[:]...)
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return gob.NewDecoder(bytes.NewReader(val)).Decode(&acc)
		})
	})
	if err != nil {
		return nil
	}
	return &acc
}

func (sm *StateManager) persistAccount(acc *Account) {
	err := store_DB_Update(func(txn *badger.Txn) error {
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(acc); err != nil {
			return err
		}
		key := append([]byte("a:"), acc.Address[:]...)
		return txn.Set(key, buf.Bytes())
	})
	if err != nil {
		log.Printf("Failed to persist account: %v", err)
	}
}

func (sm *StateManager) SetBalance(addr [32]byte, amount uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.Accounts[addr]; !ok {
		sm.Accounts[addr] = &Account{Address: addr, Balance: 0, Nonce: 0}
	}
	sm.Accounts[addr].Balance = amount
	sm.persistAccount(sm.Accounts[addr])
}

func (sm *StateManager) GetStake(addr [32]byte) uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if acc, ok := sm.Accounts[addr]; ok {
		return acc.Stake
	}
	// Try load from DB
	if acc := sm.loadAccount(addr); acc != nil {
		sm.Accounts[addr] = acc // Cache it
		return acc.Stake
	}
	return 0
}

func (sm *StateManager) SetStake(addr [32]byte, amount uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.Accounts[addr]; !ok {
		sm.Accounts[addr] = &Account{Address: addr, Balance: 0, Stake: 0, Nonce: 0}
	}
	sm.Accounts[addr].Stake = amount
	sm.persistAccount(sm.Accounts[addr])
}
