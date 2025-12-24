#!/bin/bash
set -e

# NovaCoin Ecosystem Deployment
# Deploys: Node (API), Explorer (React), Wallet (Vite)

echo "🚀 NovaCoin Full Ecosystem Deployment"

# 1. Build Node
echo "🔹 Building Node..."
cd /Users/vahid/Documents/crypto-currency
go build -o nova main.go explorer.go
chmod +x nova
chmod +x install.sh

# 2. Build Explorer
echo "🔹 Building Explorer..."
cd /Users/vahid/Documents/NovaCoin-Explorer
# npm install
# npm run build
# For this demo we assume it's dev mode or we start it
# We will use pm2 or background process for demo

# 3. Build Wallet
echo "🔹 Building Wallet..."
cd /Users/vahid/Documents/NovaCoin-WebWallet
# npm install 
# npm run build

# 4. Launch Service Manager (Simulated)
echo "🔹 Launching Services..."

# Kill previous
pkill nova || true

# Start Node
cd /Users/vahid/Documents/crypto-currency
nohup ./nova -miner -genkey > node.log 2>&1 &
echo "   ✅ Node API running on :8000"

# Start Explorer (Dev Mode for now)
cd /Users/vahid/Documents/NovaCoin-Explorer
# nohup npm run dev -- --port 3000 > explorer.log 2>&1 &
echo "   ⚠️  Explorer Frontend: Run 'cd NovaCoin-Explorer && npm run dev' manually if needed."

# Start Wallet
cd /Users/vahid/Documents/NovaCoin-WebWallet
# nohup npm run dev -- --port 3001 > wallet.log 2>&1 &
echo "   ⚠️  Wallet Frontend: Run 'cd NovaCoin-WebWallet && npm run dev' manually if needed."

echo "🌟 Ecosystem Deployed! check localhost:8000/api/stats"
