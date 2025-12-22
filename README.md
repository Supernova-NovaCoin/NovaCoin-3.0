# NovaCoin 3.0: Core Engine ("Supernova")

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go](https://img.shields.io/badge/go-1.23-blue.svg)
![TPS](https://img.shields.io/badge/tps-650k-green.svg)
![Status](https://img.shields.io/badge/status-Release%20Candidate-orange.svg)

**NovaCoin 3.0** is a hyper-performance Layer 1 blockchain utilizing the "Pulse" Block-DAG architecture to achieve 50ms finality and 600k+ TPS on commodity hardware.

## 🚀 Features
*   **Pulse DAG**: A mesh-based consensus engine (no linear blocks).
*   **Zero-Copy TPU**: High-performance UDP packet ingestion pipeline.
*   **Quantum-Ready**: Built-in Ed25519 and Dilithium signature support.
*   **Delegated Proof of Stake (DPoS)**: Staking Pools and Liquid Delegation.
*   **Economic Security**: 100 Billion NVN Cap, 100 NVN Block Rewards.

## 📚 Documentation
*   **[Miner's Guide](miners_guide.md)**: How to generate keys and start mining.
*   **[Deployment Guide](deployment.md)**: How to launch a Foundation Node (VPS).
*   **[Project Handbook](project_handbook.md)**: Technical Architecture & Design Decisions.

## 📦 Ecosystem
This repository contains the **Core Node** and **CLI Tools**.
For other components, visit:
*   [Website](https://github.com/Supernova-NovaCoin/Website)
*   [Web Wallet](https://github.com/Supernova-NovaCoin/WebWallet)
*   [Block Explorer](https://github.com/Supernova-NovaCoin/Explorer)

## 🛠 Quick Start

### 1. Build
```bash
go build -o nova main.go
```

### 2. Generate Identity
```bash
./nova -genkey
```
*Save your SEED securely!*

### 3. Start a Node
```bash
./nova -p2p :9000 -udp 8080 -maxpeers 100
```

### 4. Start Mining
```bash
./nova -miner -minerkey <YOUR_HEX_SEED>
```

## 🌌 Architecture
*   `core/pulse`: DAG Consensus Logic (Vertices & Edges)
*   `core/tpu`: Transaction Processing Unit (Zero-Copy)
*   `core/execution`: Parallel Executor (State & Economics)
*   `core/p2p`: TCP Transport & Gossip Protocol
*   `core/staking`: DPoS Validation Logic
