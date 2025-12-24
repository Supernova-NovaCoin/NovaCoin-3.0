// Package types defines the core data structures used throughout the Supernova blockchain.
// This package contains transaction definitions, serialization logic, and type constants
// that are shared between all other packages (execution, p2p, wallet, etc.)
package types

import (
	"bytes"
	"encoding/binary"
)

// ============================================================================
// TRANSACTION TYPE DEFINITIONS
// ============================================================================

// TxType represents the type of transaction being executed.
// Each transaction type has different validation rules and state effects.
// The type is stored as a single byte (uint8) for efficient serialization.
type TxType uint8

// Transaction type constants define all supported operations in the network.
// Each type triggers different execution logic in the Executor (see executor.go).
//
// Value transfer types:
//   - TxTransfer: Move NVN between accounts (most common operation)
//
// Staking types (Proof-of-Stake participation):
//   - TxStake: Lock NVN as stake to become a validator or increase voting power
//   - TxUnstake: Begin the 14-day unbonding process to withdraw stake
//   - TxWithdraw: Claim unbonded funds after the 14-day period ends
//   - TxDelegate: Delegate stake to another validator's pool (liquid staking)
//
// License/Grant types (special validator permissions):
//   - TxGrant: Genesis validators can grant locked stake to new validators
//   - TxBuyLicense: Users can purchase validator license (stake is locked/burned)
const (
	// TxTransfer moves NVN from sender to recipient.
	// Effect: Balance[From] -= (Amount + Fee), Balance[To] += Amount
	// Validation: Sender must have sufficient balance, nonce must be correct
	TxTransfer TxType = 0

	// TxStake locks NVN as stake to participate in consensus.
	// Effect: Balance[From] -= Amount, Stake[From] += Amount
	// Validation: Sender must have sufficient balance
	// Note: Staked funds cannot be transferred until unstaked
	TxStake TxType = 1

	// TxUnstake initiates the unstaking process (14-day unbonding period).
	// Effect: Stake[From] -= Amount, UnbondingBalance[From] += Amount
	//         UnbondingRelease[From] = now + 14 days
	// Validation: Sender must have sufficient stake
	// Note: Funds are locked during unbonding and cannot be used
	TxUnstake TxType = 2

	// TxDelegate moves funds from delegator to validator's stake pool.
	// Effect: Balance[From] -= Amount, Stake[To] += Amount
	//         Delegations[From][To] += Amount
	// Validation: Sender must have sufficient balance
	// Note: This allows users to participate in staking without running a node
	TxDelegate TxType = 3

	// TxWithdraw claims funds after the unbonding period expires.
	// Effect: UnbondingBalance[From] -> Balance[From]
	// Validation: UnbondingRelease[From] must be in the past
	// Note: This is the final step to convert stake back to liquid balance
	TxWithdraw TxType = 4

	// TxGrant allows genesis validators to grant locked stake to new validators.
	// Effect: Balance[From] -= Amount, GrantStake[To] += Amount
	// Validation: Only genesis addresses can execute grants (checked by policy)
	// Note: GrantStake is locked and counts toward validator eligibility
	TxGrant TxType = 5

	// TxBuyLicense allows users to purchase validator license with NVN.
	// Effect: Balance[From] -= Amount (burned), GrantStake[From] += Amount
	// Validation: Sender must have sufficient balance
	// Note: The payment is burned (deflationary), stake is locked
	TxBuyLicense TxType = 6
)

// ============================================================================
// TRANSACTION STRUCTURE
// ============================================================================

// TxMaxAge is the maximum age of a transaction in nanoseconds (5 minutes)
// Transactions older than this are rejected to prevent delayed replay attacks
const TxMaxAge = int64(5 * 60 * 1_000_000_000) // 5 minutes in nanoseconds

// Transaction is the fundamental unit of value transfer in Supernova.
// Every state change in the network originates from a signed transaction.
//
// SECURITY PROPERTIES:
//   - Authenticity: Ed25519 signature proves sender authorized the transaction
//   - Integrity: Any modification invalidates the signature
//   - Non-repudiation: Sender cannot deny sending a valid signed transaction
//   - Replay Protection: Nonce prevents replay attacks (each nonce used once)
//   - Expiry Protection: Timestamp ensures transactions cannot be delayed
//
// SERIALIZATION:
//   - Wire format: Gob encoding for P2P transmission
//   - Signing format: Custom binary format (see SerializeForSigning)
//   - Size limit: Max 64KB per transaction (enforced at network layer)
//
// LIFECYCLE:
//   1. Created by wallet with current nonce and timestamp
//   2. Signed with sender's private key
//   3. Submitted to mempool (UDP ingest or P2P broadcast)
//   4. Validated (signature, nonce, balance, expiry)
//   5. Included in block by validator
//   6. Executed by all nodes (deterministic state change)
type Transaction struct {
	// Type determines which execution path to take.
	// See TxType constants above for all supported types.
	// Stored as single byte for efficient serialization.
	Type TxType

	// From is the sender's public key (Ed25519, 32 bytes).
	// This address must sign the transaction and pay fees.
	// The signature is verified against this public key.
	From [32]byte

	// To is the recipient's public key (Ed25519, 32 bytes).
	// For TxTransfer: receives the Amount
	// For TxDelegate: the validator receiving delegation
	// For TxGrant: the user receiving locked stake
	// For TxStake/TxUnstake/TxWithdraw: typically same as From or zero
	To [32]byte

	// Amount is the value being transferred in nanoNVN (smallest unit).
	// 1 NVN = 1,000,000 nanoNVN (6 decimal places)
	// Maximum: 2^64-1 nanoNVN ≈ 18.4 billion billion nanoNVN
	// All arithmetic uses safe overflow-protected operations.
	Amount uint64

	// Fee is the transaction fee in nanoNVN paid to the block producer.
	// Minimum fee: 1,000 nanoNVN (0.001 NVN) - enforced by mempool
	// Fee distribution: 50% to validator, 50% burned (deflationary)
	// Higher fees = higher priority in mempool (fee market)
	Fee uint64

	// Nonce is a monotonically increasing sequence number per account.
	// Each transaction must have Nonce = state.Nonce[From] + 1
	// This prevents:
	//   - Replay attacks (same transaction executed twice)
	//   - Transaction reordering within same account
	// The nonce is incremented in state after successful execution.
	Nonce uint64

	// Timestamp is when the transaction was created (Unix nanoseconds).
	// Transactions expire after TxMaxAge (5 minutes) to prevent:
	//   - Delayed replay attacks
	//   - Transaction hoarding
	//   - Stale transactions clogging the network
	// If zero (legacy), expiry check is skipped for backwards compatibility.
	Timestamp int64

	// Sig is the Ed25519 signature over the serialized transaction.
	// Length: 64 bytes (Ed25519 signature size)
	// Signs: SerializeForSigning() output (Type + From + To + Amount + Fee + Nonce + Timestamp)
	// Verification: ed25519.Verify(From, message, Sig)
	Sig []byte
}

// IsExpired checks if the transaction has exceeded its maximum age.
// Returns false for legacy transactions (Timestamp == 0) for backwards compatibility.
func (tx *Transaction) IsExpired(currentTime int64) bool {
	if tx.Timestamp == 0 {
		return false // Legacy transaction, skip expiry check
	}
	age := currentTime - tx.Timestamp
	return age > TxMaxAge || age < -TxMaxAge // Also reject future timestamps
}

// ============================================================================
// SERIALIZATION
// ============================================================================

// SerializeForSigning returns the canonical byte representation of the transaction
// that is used for signature generation and verification.
//
// FORMAT (97 bytes total):
//   Offset  Size   Field
//   0       1      Type (uint8)
//   1       32     From (public key)
//   33      32     To (public key)
//   65      8      Amount (uint64, big-endian)
//   73      8      Fee (uint64, big-endian)
//   81      8      Nonce (uint64, big-endian)
//   89      8      Timestamp (int64, big-endian)
//
// IMPORTANT: The Sig field is NOT included in this serialization.
// This is because:
//   1. The signature is generated from this output
//   2. Including signature would create circular dependency
//   3. All nodes must produce identical bytes to verify signature
//
// ENDIANNESS: All multi-byte integers use big-endian encoding.
// This ensures consistent serialization across different CPU architectures.
//
// USAGE:
//   message := tx.SerializeForSigning()
//   signature := ed25519.Sign(privateKey, message)
//   tx.Sig = signature
//
//   // Verification (by any node):
//   message := tx.SerializeForSigning()
//   valid := ed25519.Verify(tx.From, message, tx.Sig)
func (tx *Transaction) SerializeForSigning() []byte {
	// Create buffer to accumulate serialized bytes
	// Using bytes.Buffer for efficient append operations
	var buf bytes.Buffer

	// 1. Write transaction type (1 byte)
	// This ensures different transaction types produce different signatures
	buf.WriteByte(byte(tx.Type))

	// 2. Write sender's public key (32 bytes)
	// This binds the transaction to the sender's identity
	buf.Write(tx.From[:])

	// 3. Write recipient's public key (32 bytes)
	// This specifies who receives the value/stake
	buf.Write(tx.To[:])

	// 4. Write amount (8 bytes, big-endian)
	// Reusable buffer for uint64 encoding
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, tx.Amount)
	buf.Write(b)

	// 5. Write fee (8 bytes, big-endian)
	// This commits to the fee being paid
	binary.BigEndian.PutUint64(b, tx.Fee)
	buf.Write(b)

	// 6. Write nonce (8 bytes, big-endian)
	// This provides replay protection
	binary.BigEndian.PutUint64(b, tx.Nonce)
	buf.Write(b)

	// 7. Write timestamp (8 bytes, big-endian)
	// This provides expiry protection
	binary.BigEndian.PutUint64(b, uint64(tx.Timestamp))
	buf.Write(b)

	// Return the complete serialized message for signing
	return buf.Bytes()
}

// ============================================================================
// TRANSACTION VALIDATION (Summary)
// ============================================================================
//
// Transactions are validated at multiple layers:
//
// 1. NETWORK LAYER (core/network/safedecode.go):
//    - Size limit: <= 64KB
//    - Valid gob encoding
//
// 2. INGEST LAYER (core/tpu/ingest.go):
//    - Signature length: == 64 bytes
//    - Signature valid: ed25519.Verify(From, message, Sig)
//    - Rate limit: <= 10 txs per address per minute
//
// 3. MEMPOOL LAYER (core/tpu/mempool.go):
//    - Not duplicate: signature not already in pool
//    - Minimum fee: >= 1,000 nanoNVN
//    - Nonce check: > current account nonce
//
// 4. EXECUTION LAYER (core/execution/executor.go):
//    - Nonce exact: == current account nonce + 1
//    - Sufficient balance: balance >= amount + fee
//    - Type-specific: each TxType has additional rules
//
// ============================================================================
