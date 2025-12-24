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
	Address          [32]byte
	Balance          uint64
	Stake            uint64
	GrantStake       uint64 // Locked "License" stake
	UnbondingBalance uint64 // Amount waiting to be released
	UnbondingRelease int64  // Unix timestamp when funds can be withdrawn
	Nonce            uint64
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

// GetUnbonding returns the unbonding balance and release time.
func (sm *StateManager) GetUnbonding(addr [32]byte) (uint64, int64) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if acc, ok := sm.Accounts[addr]; ok {
		return acc.UnbondingBalance, acc.UnbondingRelease
	}
	if acc := sm.loadAccount(addr); acc != nil {
		sm.Accounts[addr] = acc
		return acc.UnbondingBalance, acc.UnbondingRelease
	}
	return 0, 0
}

// SetUnbonding updates the unbonding balance and release time.
func (sm *StateManager) SetUnbonding(addr [32]byte, amount uint64, releaseTime int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.Accounts[addr]; !ok {
		sm.Accounts[addr] = &Account{Address: addr}
	}
	sm.Accounts[addr].UnbondingBalance = amount
	sm.Accounts[addr].UnbondingRelease = releaseTime
	sm.persistAccount(sm.Accounts[addr])
}

// GetGrantStake returns the locked license stake.
func (sm *StateManager) GetGrantStake(addr [32]byte) uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if acc, ok := sm.Accounts[addr]; ok {
		return acc.GrantStake
	}
	if acc := sm.loadAccount(addr); acc != nil {
		sm.Accounts[addr] = acc
		return acc.GrantStake
	}
	return 0
}

// SetGrantStake updates the locked license stake.
func (sm *StateManager) SetGrantStake(addr [32]byte, amount uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.Accounts[addr]; !ok {
		sm.Accounts[addr] = &Account{Address: addr}
	}
	sm.Accounts[addr].GrantStake = amount
	sm.persistAccount(sm.Accounts[addr])
}

// GetNonce returns the last executed nonce for an account.
func (sm *StateManager) GetNonce(addr [32]byte) uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if acc, ok := sm.Accounts[addr]; ok {
		return acc.Nonce
	}
	if acc := sm.loadAccount(addr); acc != nil {
		sm.Accounts[addr] = acc
		return acc.Nonce
	}
	return 0
}

// SetNonce updates the nonce.
func (sm *StateManager) SetNonce(addr [32]byte, nonce uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.Accounts[addr]; !ok {
		sm.Accounts[addr] = &Account{Address: addr}
	}
	sm.Accounts[addr].Nonce = nonce
	sm.persistAccount(sm.Accounts[addr])
}
