# NovaCoin 3.0: Supernova Engine

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go](https://img.shields.io/badge/go-1.23-blue.svg)
![TPS](https://img.shields.io/badge/tps-650k-green.svg)
![Status](https://img.shields.io/badge/status-Production%20Ready-brightgreen.svg)

**NovaCoin 3.0 (Supernova)** is a high-performance Layer 1 blockchain featuring a Block-DAG architecture, achieving sub-second finality and 600k+ TPS on commodity hardware.

---

## Key Features

| Feature | Description |
|---------|-------------|
| **Pulse DAG** | Mesh-based consensus with parallel block processing |
| **Zero-Copy TPU** | High-performance UDP transaction ingestion (100k+ tx/sec) |
| **DPoS Consensus** | Delegated Proof-of-Stake with slashing protection |
| **Ed25519 Signatures** | Fast, secure cryptographic signatures |
| **Built-in Wallet** | Encrypted wallet with mnemonic backup |
| **REST API** | Full-featured explorer and wallet API |
| **WebSocket** | Real-time block streaming |

---

## Quick Start

### Prerequisites
- Go 1.23+
- Linux/macOS/Windows

### Build
```bash
go build -o supernova .
```

### Generate Validator Key
```bash
./supernova -genkey
```
> Save your SEED securely! This is your validator identity.

### Start a Node
```bash
./supernova -p2p :9000 -udp 8080
```

### Start Mining (Validator)
```bash
./supernova -miner -minerkey <YOUR_HEX_SEED>
```

### Connect to Peers
```bash
./supernova -peers "node1.example.com:9000,node2.example.com:9000"
```

---

## Configuration

### Environment Variables
| Variable | Description | Default |
|----------|-------------|---------|
| `SUPERNOVA_P2P_PORT` | P2P listening port | `:9000` |
| `SUPERNOVA_UDP_PORT` | UDP ingest port | `8080` |
| `SUPERNOVA_API_PORT` | REST API port | `:8000` |
| `SUPERNOVA_DATA_DIR` | Data directory | `./data` |
| `SUPERNOVA_GENESIS_SEED_1` | Genesis validator 1 seed | Required |
| `SUPERNOVA_GENESIS_SEED_2` | Genesis validator 2 seed | Optional |
| `SUPERNOVA_GENESIS_SEED_3` | Genesis validator 3 seed | Optional |
| `SUPERNOVA_ALLOWED_ORIGINS` | CORS allowed origins (comma-separated) | `*` (dev) |
| `SUPERNOVA_ENABLE_TLS` | Enable HTTPS for API | `false` |
| `SUPERNOVA_TLS_CERT` | TLS certificate path | - |
| `SUPERNOVA_TLS_KEY` | TLS key path | - |

### config.json
```json
{
  "p2p_port": ":9000",
  "udp_port": 8080,
  "api_port": ":8000",
  "max_peers": 100,
  "max_per_ip": 5,
  "data_dir": "./data",
  "genesis_seed_1": "your-genesis-seed-hex",
  "community_validators": [
    "pubkey-hex-1",
    "pubkey-hex-2"
  ]
}
```

---

## Transaction Types

| Type | Code | Description |
|------|------|-------------|
| `TxTransfer` | 0 | Transfer NVN between accounts |
| `TxStake` | 1 | Lock NVN as validator stake |
| `TxUnstake` | 2 | Begin 14-day unbonding period |
| `TxDelegate` | 3 | Delegate stake to a validator |
| `TxWithdraw` | 4 | Claim unbonded funds |
| `TxGrant` | 5 | Grant validator license (genesis only) |
| `TxBuyLicense` | 6 | Purchase validator license (burns NVN) |

---

## REST API Endpoints

### Explorer API
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/stats` | GET | Network statistics |
| `/api/blocks?page=1&limit=20` | GET | Paginated block list |
| `/api/block?hash=...` | GET | Block details |
| `/api/block/finality?hash=...` | GET | Block finality status |
| `/api/account?addr=...` | GET | Account balance & history |
| `/api/transaction?hash=...` | GET | Transaction details |
| `/api/search?q=...` | GET | Search blocks/txs/addresses |
| `/api/mempool` | GET | Pending transactions |
| `/api/mempool/stats` | GET | Mempool statistics |
| `/api/peers` | GET | Connected peers |
| `/api/peers/reputation` | GET | Peer reputation scores |
| `/api/slashing` | GET | Slashing records |
| `/api/tx` | POST | Submit transaction |

### Wallet API
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/wallet/create` | POST | Create new wallet |
| `/api/wallet/import` | POST | Import from mnemonic |
| `/api/wallet/list` | GET | List all wallets |
| `/api/wallet/info?address=...` | GET | Wallet details |
| `/api/wallet/unlock` | POST | Unlock wallet |
| `/api/wallet/lock` | POST | Lock wallet |
| `/api/wallet/send` | POST | Send transaction |
| `/api/wallet/sign` | POST | Sign transaction |
| `/api/wallet/export` | POST | Export private key |

### WebSocket
```
ws://localhost:8000/ws
```
Streams new blocks in real-time as JSON.

---

## Architecture

```
supernova/
├── core/
│   ├── pulse/          # DAG consensus (vertices, finality)
│   ├── tpu/            # Transaction Processing Unit (ingest, mempool)
│   ├── execution/      # State machine & transaction executor
│   ├── p2p/            # Networking (TCP gossip, peer management)
│   ├── staking/        # DPoS validation & slashing
│   ├── wallet/         # Encrypted wallet management
│   ├── types/          # Core data structures
│   ├── crypto/         # Cryptographic utilities
│   ├── math/           # Safe arithmetic operations
│   ├── network/        # Network security (size limits, safe decode)
│   └── cache/          # LRU caches for indexing
├── main.go             # Node entry point
├── explorer.go         # REST API & WebSocket server
└── cmd/
    └── wallet/         # CLI wallet tool
```

---

## Security Features

- **Strict Nonce Validation**: Prevents transaction reordering attacks
- **Authorized Grants**: Only genesis validators can grant licenses
- **Safe Arithmetic**: Overflow-protected calculations throughout
- **Validator-Only Delegation**: Prevents locking funds in invalid addresses
- **Transaction Expiry**: 5-minute TTL prevents delayed replay attacks
- **API Signature Verification**: Ed25519 validation at API layer
- **Rate Limiting**: 10 tx/address/minute on UDP ingest
- **Peer Reputation**: Automatic banning of malicious peers
- **Slashing**: Penalties for double-signing and downtime

---

## Economics

| Parameter | Value |
|-----------|-------|
| **Total Supply** | 10 Billion NVN |
| **Smallest Unit** | 1 nanoNVN (10^-6 NVN) |
| **Block Reward** | 500 NVN + 50% fees |
| **Halving** | Every 10M blocks |
| **Min Stake** | 1,000 NVN |
| **Unbonding Period** | 14 days |
| **Double-Sign Slash** | 10% of stake |
| **Downtime Slash** | 1% of stake |
| **Jail Duration** | 7 days |

---

## CLI Commands

```bash
# Generate new validator key
./supernova -genkey

# Start node with mining
./supernova -miner -minerkey <SEED_HEX>

# Send NVN
./supernova -send -to <ADDRESS> -amount 100 -key <SEED_HEX>

# Stake NVN
./supernova -stake -amount 1000 -key <SEED_HEX>
```

---

## Development

### Run Tests
```bash
go test ./... -v
```

### Build for Linux
```bash
GOOS=linux GOARCH=amd64 go build -o supernova_linux_amd64
```

---

## Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) - Technical deep-dive
- [miners_guide.md](miners_guide.md) - Validator setup guide
- [deployment.md](deployment.md) - Production deployment

---

## Ecosystem

- [Website](https://github.com/Supernova-NovaCoin/Website)
- [Web Wallet](https://github.com/Supernova-NovaCoin/WebWallet)
- [Block Explorer](https://github.com/Supernova-NovaCoin/Explorer)

---

## License

MIT License - see [LICENSE](LICENSE) for details.
