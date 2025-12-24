package execution

import (
	"bytes"
	"encoding/gob"
	"errors"
	"log"
	"novacoin/core/store"
	"sync"

	"github.com/dgraph-io/badger/v4"
)

// Common errors
var (
	ErrAccountNotFound = errors.New("account not found")
	ErrPersistFailed   = errors.New("failed to persist account")
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
	// Delegations: ValidatorID -> Amount
	Delegations map[[32]byte]uint64
}

// StateManager handles a sharded set of accounts.
// Thread-safe with proper locking to prevent race conditions.
type StateManager struct {
	Accounts map[[32]byte]*Account
	mu       sync.RWMutex
}

func NewStateManager() *StateManager {
	return &StateManager{
		Accounts: make(map[[32]byte]*Account),
	}
}

// getOrLoadAccount retrieves account from cache or loads from DB.
// MUST be called with write lock held.
func (sm *StateManager) getOrLoadAccountLocked(addr [32]byte) *Account {
	if acc, ok := sm.Accounts[addr]; ok {
		return acc
	}

	// Try load from DB
	if acc := sm.loadAccountFromDB(addr); acc != nil {
		sm.Accounts[addr] = acc
		return acc
	}

	return nil
}

// getOrCreateAccountLocked retrieves or creates account.
// MUST be called with write lock held.
func (sm *StateManager) getOrCreateAccountLocked(addr [32]byte) *Account {
	if acc := sm.getOrLoadAccountLocked(addr); acc != nil {
		return acc
	}

	// Create new account
	acc := &Account{
		Address:     addr,
		Balance:     0,
		Stake:       0,
		Nonce:       0,
		Delegations: make(map[[32]byte]uint64),
	}
	sm.Accounts[addr] = acc
	return acc
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

// loadAccountFromDB loads account from database (no locking, caller must handle)
func (sm *StateManager) loadAccountFromDB(addr [32]byte) *Account {
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
	// Ensure delegations map is initialized
	if acc.Delegations == nil {
		acc.Delegations = make(map[[32]byte]uint64)
	}
	return &acc
}

// persistAccount saves account to database
func (sm *StateManager) persistAccount(acc *Account) error {
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
		return ErrPersistFailed
	}
	return nil
}

// GetBalance returns the balance for an address (thread-safe)
func (sm *StateManager) GetBalance(addr [32]byte) uint64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if acc := sm.getOrLoadAccountLocked(addr); acc != nil {
		return acc.Balance
	}
	return 0
}

// SetBalance sets the balance for an address (thread-safe)
func (sm *StateManager) SetBalance(addr [32]byte, amount uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	acc := sm.getOrCreateAccountLocked(addr)
	acc.Balance = amount
	sm.persistAccount(acc)
}

// GetStake returns the stake for an address (thread-safe)
func (sm *StateManager) GetStake(addr [32]byte) uint64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if acc := sm.getOrLoadAccountLocked(addr); acc != nil {
		return acc.Stake
	}
	return 0
}

// SetStake sets the stake for an address (thread-safe)
func (sm *StateManager) SetStake(addr [32]byte, amount uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	acc := sm.getOrCreateAccountLocked(addr)
	acc.Stake = amount
	sm.persistAccount(acc)
}

// GetUnbonding returns the unbonding balance and release time (thread-safe)
func (sm *StateManager) GetUnbonding(addr [32]byte) (uint64, int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if acc := sm.getOrLoadAccountLocked(addr); acc != nil {
		return acc.UnbondingBalance, acc.UnbondingRelease
	}
	return 0, 0
}

// SetUnbonding updates the unbonding balance and release time (thread-safe)
func (sm *StateManager) SetUnbonding(addr [32]byte, amount uint64, releaseTime int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	acc := sm.getOrCreateAccountLocked(addr)
	acc.UnbondingBalance = amount
	acc.UnbondingRelease = releaseTime
	sm.persistAccount(acc)
}

// GetGrantStake returns the locked license stake (thread-safe)
func (sm *StateManager) GetGrantStake(addr [32]byte) uint64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if acc := sm.getOrLoadAccountLocked(addr); acc != nil {
		return acc.GrantStake
	}
	return 0
}

// SetGrantStake updates the locked license stake (thread-safe)
func (sm *StateManager) SetGrantStake(addr [32]byte, amount uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	acc := sm.getOrCreateAccountLocked(addr)
	acc.GrantStake = amount
	sm.persistAccount(acc)
}

// AddGrantStake adds to the locked license stake (thread-safe)
func (sm *StateManager) AddGrantStake(addr [32]byte, amount uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	acc := sm.getOrCreateAccountLocked(addr)
	acc.GrantStake += amount
	sm.persistAccount(acc)
}

// GetDelegation returns the amount delegated to a specific validator (thread-safe)
func (sm *StateManager) GetDelegation(delegator [32]byte, validator [32]byte) uint64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if acc := sm.getOrLoadAccountLocked(delegator); acc != nil {
		if acc.Delegations != nil {
			return acc.Delegations[validator]
		}
	}
	return 0
}

// SetDelegation updates the delegation amount (thread-safe)
func (sm *StateManager) SetDelegation(delegator [32]byte, validator [32]byte, amount uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	acc := sm.getOrCreateAccountLocked(delegator)
	if acc.Delegations == nil {
		acc.Delegations = make(map[[32]byte]uint64)
	}
	acc.Delegations[validator] = amount
	sm.persistAccount(acc)
}

// GetNonce returns the last executed nonce for an account (thread-safe)
func (sm *StateManager) GetNonce(addr [32]byte) uint64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if acc := sm.getOrLoadAccountLocked(addr); acc != nil {
		return acc.Nonce
	}
	return 0
}

// SetNonce updates the nonce (thread-safe)
func (sm *StateManager) SetNonce(addr [32]byte, nonce uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	acc := sm.getOrCreateAccountLocked(addr)
	acc.Nonce = nonce
	sm.persistAccount(acc)
}

// IterateDelegations iterates over all delegations for a delegator
func (sm *StateManager) IterateDelegations(delegator [32]byte, fn func(validator [32]byte, amount uint64)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if acc := sm.getOrLoadAccountLocked(delegator); acc != nil {
		if acc.Delegations != nil {
			for v, a := range acc.Delegations {
				fn(v, a)
			}
		}
	}
}

// GetAccount returns a copy of the account (thread-safe)
func (sm *StateManager) GetAccount(addr [32]byte) *Account {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if acc := sm.getOrLoadAccountLocked(addr); acc != nil {
		// Return a copy to prevent external mutation
		copy := *acc
		if acc.Delegations != nil {
			copy.Delegations = make(map[[32]byte]uint64)
			for k, v := range acc.Delegations {
				copy.Delegations[k] = v
			}
		}
		return &copy
	}
	return nil
}

// AccountExists checks if an account exists
func (sm *StateManager) AccountExists(addr [32]byte) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.getOrLoadAccountLocked(addr) != nil
}
