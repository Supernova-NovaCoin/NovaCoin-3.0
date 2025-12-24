# Supernova Blockchain - Complete Architecture Documentation

## Table of Contents
1. [Overview](#overview)
2. [Token Economics](#token-economics)
3. [Transaction System](#transaction-system)
4. [Staking & Validators](#staking--validators)
5. [Wallet System](#wallet-system)
6. [Explorer API](#explorer-api)
7. [P2P Network](#p2p-network)
8. [Security Measures](#security-measures)
9. [File Reference](#file-reference)

---

## Overview

**Supernova** is a high-performance Layer 1 blockchain implementing a **Pulse DAG consensus mechanism** with Delegated Proof of Stake (DPoS).

### Key Specifications
```
Consensus:          Pulse DAG (Directed Acyclic Graph)
Cryptography:       Ed25519 signatures, SHA-256 hashing
Target TPS:         600,000+ transactions per second
Finality:           ~30 seconds (6 confirmations)
Block Time:         ~3 seconds
Min Validator Stake: 1,000 NVN
Unbonding Period:   14 days
```

### Architecture Diagram
```
┌──────────────────────────────────────────────────────────────────┐
│                         SUPERNOVA NODE                            │
├──────────────────────────────────────────────────────────────────┤
│                                                                   │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────────────┐   │
│  │   Wallet    │    │   Explorer  │    │     P2P Server      │   │
│  │    API      │    │     API     │    │    (TCP :9000)      │   │
│  └──────┬──────┘    └──────┬──────┘    └──────────┬──────────┘   │
│         │                  │                       │              │
│         └──────────────────┼───────────────────────┘              │
│                            │                                      │
│  ┌─────────────────────────┴─────────────────────────────────┐   │
│  │                    EXECUTION ENGINE                        │   │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  │   │
│  │  │ Executor │  │  State   │  │ Mempool  │  │ Slasher  │  │   │
│  │  │          │  │ Manager  │  │          │  │          │  │   │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘  │   │
│  └───────────────────────────────────────────────────────────┘   │
│                            │                                      │
│  ┌─────────────────────────┴─────────────────────────────────┐   │
│  │                      PULSE DAG                             │   │
│  │  ┌──────────────────────────────────────────────────────┐ │   │
│  │  │   [V1] ──→ [V2] ──→ [V4] ──→ [V6]                   │ │   │
│  │  │     ↘       ↗         ↘       ↗                      │ │   │
│  │  │      [V3] ───────→ [V5] ──→ [V7] (tips)              │ │   │
│  │  └──────────────────────────────────────────────────────┘ │   │
│  └───────────────────────────────────────────────────────────┘   │
│                            │                                      │
│  ┌─────────────────────────┴─────────────────────────────────┐   │
│  │                    TPU (Ingest)                            │   │
│  │         UDP Port 8080 → Worker Pool → Mempool              │   │
│  └───────────────────────────────────────────────────────────┘   │
│                            │                                      │
│  ┌─────────────────────────┴─────────────────────────────────┐   │
│  │                   BADGER DB                                │   │
│  │         Accounts, Vertices, Tips, Recent Blocks            │   │
│  └───────────────────────────────────────────────────────────┘   │
│                                                                   │
└──────────────────────────────────────────────────────────────────┘
```

---

## Token Economics

### NVN Token

```
Token Name:     NovaCoin (NVN)
Total Supply:   100 Billion NVN (maximum)
Decimals:       6 (1 NVN = 1,000,000 nanoNVN)
Type:           Native utility token
```

### Genesis Distribution

| Validator    | Balance      | Stake        | Purpose              |
|--------------|--------------|--------------|----------------------|
| Singapore    | 3.33B NVN    | 1.67B NVN    | Genesis Validator 1  |
| Mumbai       | 3.33B NVN    | 1.67B NVN    | Genesis Validator 2  |
| USA          | 3.33B NVN    | 1.67B NVN    | Genesis Validator 3  |
| Germany      | 10M NVN      | 5M NVN       | Community Validator  |
| UK           | 10M NVN      | 5M NVN       | Community Validator  |

**Code Reference:** `main.go:180-250`

### Block Rewards

```go
// Block reward calculation (executor.go:114-159)
//
// Halving Schedule:
//   Genesis (2025): 500 NVN per block
//   Year 3:         250 NVN per block (halving 1)
//   Year 5:         125 NVN per block (halving 2)
//   Year 7:         62.5 NVN per block (halving 3)
//   ...continues halving every 2 years
//
// Minimum: 1 nanoNVN (prevents complete halving)
//
// Total Reward = BaseReward + (CollectedFees / 2)
// - 50% of fees go to validator
// - 50% of fees are burned (deflationary)
```

### Transaction Fees

```go
// Fee structure (mempool.go)
MinimumFee = 1,000 nanoNVN (0.001 NVN)

// Fee distribution:
// - 50% → Block Producer (validator)
// - 50% → Burned (reduces total supply)

// Priority: Higher fee = faster inclusion
// Mempool eviction: Lowest fee tx evicted when full
```

### Deflationary Mechanics

1. **Fee Burning**: 50% of all transaction fees are destroyed
2. **License Purchase**: TxBuyLicense burns the payment amount
3. **Halving**: Block rewards decrease every 2 years

---

## Transaction System

### Transaction Types

| Type | Code | Description | State Effect |
|------|------|-------------|--------------|
| Transfer | 0 | Send NVN to address | Balance[From] -= Amount+Fee; Balance[To] += Amount |
| Stake | 1 | Lock NVN as stake | Balance[From] -= Amount; Stake[From] += Amount |
| Unstake | 2 | Begin unstaking | Stake[From] -= Amount; Unbonding[From] += Amount |
| Delegate | 3 | Delegate to validator | Balance[From] -= Amount; Stake[To] += Amount |
| Withdraw | 4 | Claim unbonded | Unbonding[From] → Balance[From] |
| Grant | 5 | Genesis grant | Balance[From] -= Amount; GrantStake[To] += Amount |
| BuyLicense | 6 | Purchase license | Balance[From] -= Amount (burned); GrantStake[From] += Amount |

**Code Reference:** `core/types/transaction.go`

### Transaction Lifecycle

```
1. CREATION (wallet)
   └── User creates tx with nonce = current_nonce + 1
   └── Signed with Ed25519 private key

2. SUBMISSION (UDP ingest or P2P)
   └── Sent to node via UDP port 8080 or P2P broadcast

3. VALIDATION (ingest.go)
   └── Size check: <= 64KB
   └── Signature verification: ed25519.Verify()
   └── Rate limit: <= 10 tx/address/minute

4. MEMPOOL (mempool.go)
   └── Duplicate check: signature not already present
   └── Fee check: >= 1,000 nanoNVN
   └── Nonce check: > current account nonce
   └── Added to priority queue (by fee)

5. BLOCK INCLUSION (miner)
   └── Miner gets batch of highest-fee txs (up to 100)
   └── Creates vertex with Merkle root of txs
   └── Signs and broadcasts block

6. EXECUTION (executor.go)
   └── Nonce exact check: == current_nonce + 1
   └── Balance check: balance >= amount + fee
   └── Type-specific execution
   └── State update and persistence
```

### Replay Protection

```go
// Nonce-based protection (executor.go:28-36)
//
// Each account has a monotonically increasing nonce.
// Transactions MUST have nonce == state.nonce + 1
//
// This prevents:
// - Replay attacks (same tx executed twice)
// - Out-of-order execution
// - Transaction substitution
```

---

## Staking & Validators

### Becoming a Validator

```bash
# 1. Generate validator identity
./nova -genkey
# Output: SEED (save this!), ADDRESS (public)

# 2. Acquire 1000+ NVN stake (via transfer or grant)

# 3. Start validator node
./nova -miner -minerkey YOUR_SEED_HEX -p2p :9000

# 4. Node automatically participates in block production
```

### Stake Types

| Type | Description | Locked? | Counts for Voting? |
|------|-------------|---------|-------------------|
| Balance | Liquid NVN | No | No |
| Stake | Self-staked | Yes | Yes |
| GrantStake | License stake | Yes (permanent) | Yes |
| Unbonding | Unstaking | Yes (14 days) | No |
| Delegation | Delegated to validator | Yes | Yes (for validator) |

**Code Reference:** `core/execution/state.go:21-31`

### Minimum Stake Requirement

```go
// stake.go:10
const MinStakeRequired = 1000 * 1_000_000  // 1000 NVN

// Block validation (stake.go:14-29)
func ValidateBlock(v *Vertex, state *StateManager) error {
    stake := state.GetStake(v.Author)
    grant := state.GetGrantStake(v.Author)
    total := stake + grant

    if total < MinStakeRequired {
        return ErrInsufficientStake
    }
    return nil
}
```

### Unbonding Period

```go
// Unstaking process (executor.go:220-280)
//
// 1. User sends TxUnstake
// 2. Stake[From] -= Amount
// 3. UnbondingBalance[From] += Amount
// 4. UnbondingRelease[From] = now + 14 days
//
// 5. After 14 days, user sends TxWithdraw
// 6. UnbondingBalance[From] → Balance[From]
```

### Slashing

```go
// Slashable offenses (slashing.go)
OffenseDoubleSign  = 10% stake slashed
OffenseDowntime    = 1% stake slashed
OffenseInvalidBlock = 10% stake slashed

// Jail duration: 7 days
// During jail: Cannot produce valid blocks

// Detection (p2p/server.go:330-357)
// - Each round, track validator → block hash
// - If same validator signs different block in same round:
//   → Double-sign detected → Apply slashing
```

### Delegation

```go
// Delegation flow (executor.go:328-374)
//
// Delegator sends TxDelegate(validator, amount)
// 1. Deduct from delegator's balance
// 2. Add to validator's stake
// 3. Track in Delegations[delegator][validator]
//
// Validator's total voting power:
// VotingPower = Stake + GrantStake + sum(Delegations)
```

---

## Wallet System

### Security Architecture

```
┌─────────────────────────────────────────────────┐
│                 WALLET FILE                      │
├─────────────────────────────────────────────────┤
│  Address:       Public key (32 bytes, hex)       │
│  EncPrivKey:    AES-256-GCM encrypted key        │
│  Salt:          PBKDF2 salt (16 bytes)           │
│  CreatedAt:     Unix timestamp                   │
│  Label:         User-friendly name               │
└─────────────────────────────────────────────────┘

Encryption:
┌──────────────────────────────────────────────────┐
│  Password ─→ PBKDF2(100k iterations) ─→ AES Key  │
│  AES Key + Nonce ─→ AES-256-GCM ─→ EncPrivKey    │
└──────────────────────────────────────────────────┘
```

**Code Reference:** `core/wallet/wallet.go`

### Mnemonic System

```go
// Mnemonic generation (wallet.go:69-120)
//
// 1. Generate 12 random words from 256-word list
// 2. Derive seed: SHA256(SHA256(words joined with spaces))
// 3. Generate Ed25519 keypair from seed[:32]
//
// Example mnemonic:
// "ocean blue forest mountain river valley sunset dawn..."
```

### Wallet API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| /api/wallet/create | POST | Create new encrypted wallet |
| /api/wallet/import | POST | Import from 12-word mnemonic |
| /api/wallet/list | GET | List all wallets (safe info) |
| /api/wallet/info | GET | Get wallet details + balance |
| /api/wallet/unlock | POST | Decrypt private key |
| /api/wallet/lock | POST | Clear private key from memory |
| /api/wallet/send | POST | Create and sign transaction |
| /api/wallet/sign | POST | Sign arbitrary transaction |
| /api/wallet/export | POST | Export private key (dangerous!) |
| /api/wallet/delete | POST | Delete wallet (requires password) |
| /api/wallet/quick | POST | Quick create (unencrypted, demo) |

**Code Reference:** `explorer.go:660-989`

### Transaction Signing Flow

```go
// Signing a transaction (wallet.go:420-480)
//
// 1. Create unsigned transaction
tx := Transaction{
    Type:   TxTransfer,
    From:   wallet.PublicKey,
    To:     recipientAddress,
    Amount: amountNanoNVN,
    Fee:    feeNanoNVN,
    Nonce:  currentNonce + 1,
}

// 2. Serialize for signing
message := tx.SerializeForSigning()

// 3. Sign with Ed25519
signature := ed25519.Sign(privateKey, message)
tx.Sig = signature

// 4. Submit to network
```

---

## Explorer API

### REST Endpoints

| Endpoint | Description |
|----------|-------------|
| GET / | Health check |
| GET /api/stats | Network statistics |
| GET /api/blocks | Paginated block list |
| GET /api/block?hash= | Single block details |
| GET /api/block/finality?hash= | Block finality status |
| GET /api/account?addr= | Account details + history |
| GET /api/transaction?hash= | Transaction details |
| POST /api/tx | Submit transaction |
| GET /api/mempool | Pending transactions |
| GET /api/mempool/stats | Mempool statistics |
| GET /api/peers | Connected peers |
| GET /api/peers/reputation | Peer reputation scores |
| GET /api/slashing | Slashing records |
| GET /api/search?q= | Search blocks/txs/addresses |
| WS /ws | Real-time block events |

**Code Reference:** `explorer.go:50-1000`

### Response Formats

```json
// Block Response
{
  "hash": "abc123...",
  "timestamp": 1234567890,
  "author": "def456...",
  "parents": ["parent1", "parent2"],
  "round": 123,
  "merkleRoot": "root...",
  "reward": 500.5,
  "size": 1024,
  "tx_count": 15
}

// Account Response
{
  "address": "abc123...",
  "balance": 1000.5,
  "stake": 5000.0,
  "grantStake": 100.0,
  "unbonding": 0.0,
  "delegations": {
    "validator1": 500.0
  },
  "history": [...]
}

// Finality Status
{
  "hash": "abc123...",
  "confirmations": 7,
  "age": 45,
  "validatorCount": 8,
  "isFinalized": true,
  "progress": 95.5
}
```

### WebSocket Events

```javascript
// Connect to WebSocket
const ws = new WebSocket('ws://localhost:8000/ws');

// Receive real-time blocks
ws.onmessage = (event) => {
  const block = JSON.parse(event.data);
  console.log('New block:', block.hash);
};
```

---

## P2P Network

### Protocol Stack

```
Layer 4: Application (Block, Tx, Handshake messages)
Layer 3: Network (Gob-encoded binary)
Layer 2: Transport (TCP, optional TLS)
Layer 1: Physical (TCP/IP)
```

### Message Types

| Type | Code | Description |
|------|------|-------------|
| Handshake | 0x01 | Peer identification |
| Block | 0x02 | Broadcast vertex |
| Tx | 0x03 | Broadcast transaction |
| GetAddr | 0x04 | Request peer addresses |
| Addr | 0x05 | Respond with addresses |
| GetDAG | 0x06 | Request recent vertices |
| DAG | 0x07 | Respond with vertices |

**Code Reference:** `core/p2p/message.go`

### Connection Flow

```
1. Node A dials Node B on port 9000
2. Both send MsgHandshake:
   - Protocol version
   - NodeID (public key hex)
   - GenesisHash (chain identifier)
3. Validate GenesisHash matches
4. Register peer in Peers map
5. Begin message exchange
```

### Peer Discovery

```go
// Peer exchange (server.go)
//
// 1. Send MsgGetAddr to connected peer
// 2. Receive MsgAddr with list of known peers
// 3. Add to KnownPeers map
// 4. Optionally dial new peers
```

---

## Security Measures

### Cryptographic Security

| Component | Algorithm |
|-----------|-----------|
| Signatures | Ed25519 (RFC 8032) |
| Hashing | SHA-256 |
| Wallet Encryption | AES-256-GCM |
| Key Derivation | PBKDF2 (100k iterations) |
| Merkle Trees | Binary SHA-256 |

### DDoS Protection

```go
// Rate limiting (ingest.go)
MaxTxPerAddressPerMinute = 10
RateLimitWindowSeconds = 60

// Connection limits (server.go)
MaxPeers = 100
MaxPerIP = 5

// Payload limits (safedecode.go)
MaxMessageSize = 10 MB
MaxTxSize = 64 KB
MaxBlockSize = 5 MB
```

### Reputation System

```go
// Peer scoring (reputation.go)
ValidBlock:    +5 points
InvalidBlock: -20 points
ValidTx:       +1 point
InvalidTx:    -5 points
ProtocolError: -15 points
Timeout:      -10 points

// Auto-ban at score <= -50
// Ban duration: 1 hour
```

### Safe Arithmetic

```go
// Overflow protection (math/safe.go)
SafeAdd(a, b uint64) (uint64, error)
SafeSub(a, b uint64) (uint64, error)
SafeMul(a, b uint64) (uint64, error)
SafeDiv(a, b uint64) (uint64, error)

// Used for all financial operations
```

---

## File Reference

### Core Packages

| File | Lines | Purpose |
|------|-------|---------|
| `core/types/transaction.go` | 246 | Transaction types and serialization |
| `core/execution/state.go` | 308 | Account state management |
| `core/execution/executor.go` | 375 | Transaction execution engine |
| `core/pulse/vertex.go` | 62 | DAG vertex (block) structure |
| `core/pulse/store.go` | 224 | DAG storage and tips |
| `core/pulse/finality.go` | 233 | Finality tracking |
| `core/tpu/ingest.go` | 368 | UDP transaction ingestion |
| `core/tpu/mempool.go` | 256 | Transaction pool |
| `core/p2p/server.go` | 430 | P2P networking |
| `core/p2p/reputation.go` | 367 | Peer reputation |
| `core/staking/stake.go` | 31 | Stake validation |
| `core/staking/slashing.go` | 223 | Slashing logic |
| `core/wallet/wallet.go` | 603 | Wallet management |
| `core/cache/lru.go` | 218 | LRU cache |
| `core/math/safe.go` | 95 | Safe arithmetic |
| `core/network/safedecode.go` | 93 | Safe message decoding |
| `core/crypto/keys.go` | 41 | Key generation |
| `core/crypto/merkle.go` | 38 | Merkle trees |
| `core/store/db.go` | 27 | Database wrapper |

### Entry Points

| File | Purpose |
|------|---------|
| `main.go` | Node orchestrator, CLI, mining |
| `explorer.go` | REST API + WebSocket |
| `cmd/wallet/main.go` | Wallet CLI |

### Configuration

| File | Purpose |
|------|---------|
| `config.json` | Node configuration |
| `go.mod` | Go dependencies |

---

## Quick Start

```bash
# Generate validator identity
./nova -genkey

# Start node
./nova -p2p :9000 -udp 8080

# Start with mining enabled
./nova -miner -minerkey YOUR_SEED_HEX

# Connect to peers
./nova -peers 192.168.1.100:9000,192.168.1.101:9000

# Access explorer
curl http://localhost:8000/api/stats
```

## Environment Variables

```bash
SUPERNOVA_P2P_PORT=:9000
SUPERNOVA_UDP_PORT=8080
SUPERNOVA_API_PORT=:8000
SUPERNOVA_DATA_DIR=./data
SUPERNOVA_GENESIS_SEED_1=<hex>
SUPERNOVA_GENESIS_SEED_2=<hex>
SUPERNOVA_GENESIS_SEED_3=<hex>
SUPERNOVA_ENABLE_TLS=true
SUPERNOVA_TLS_CERT=/path/to/cert.pem
SUPERNOVA_TLS_KEY=/path/to/key.pem
```

---

## Version History

- **v3.0.1**: Production security hardening, worker pools, slashing
- **v3.0.0**: Initial Supernova release with Pulse DAG

---

*Generated for Supernova Blockchain Project*
*Last Updated: 2025*
