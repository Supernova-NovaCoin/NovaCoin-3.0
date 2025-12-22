# NovaCoin 3.0: Core Engine ("Supernova")

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go](https://img.shields.io/badge/go-1.23-blue.svg)
![TPS](https://img.shields.io/badge/tps-650k-green.svg)

**NovaCoin 3.0** is a hyper-performance Layer 1 blockchain utilizing the "Pulse" Block-DAG architecture to achieve 50ms finality and 600k+ TPS on commodity hardware.

## 🚀 Features
*   **Pulse DAG**: A mesh-based consensus engine (no linear blocks).
*   **Zero-Copy TPU**: High-performance UDP packet ingestion pipeline.
*   **Quantum-Ready**: Built-in Ed25519 and Dilithium signature support.
*   **0-Copy State**: Memory-mapped account state for parallel execution.

## 📦 Ecosystem
This repository contains the **Core Node** and **CLI Tools**.
For other components, visit:
*   [Website](https://github.com/Supernova-NovaCoin/Website)
*   [Web Wallet](https://github.com/Supernova-NovaCoin/WebWallet)
*   [Block Explorer](https://github.com/Supernova-NovaCoin/Explorer)

## 🛠 Building & Running
### Prerequisites
*   Go 1.23+

### Start the Node
```bash
go build -o nova main.go
./nova
```
The node will start listening on UDP port `8080`.

### CLI Wallet
```bash
go build -o wallet cmd/wallet/main.go
./wallet keygen
./wallet send -priv <KEY> -to <ADDR> -amount <AMT>
```

## 🌌 Architecture
*   `core/pulse`: DAG Consensus Logic
*   `core/tpu`: Transaction Processing Unit
*   `core/execution`: Parallel Executor
*   `core/crypto`: Cryptography Primitives
