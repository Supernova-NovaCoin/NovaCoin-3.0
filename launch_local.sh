#!/bin/bash
echo "🚀 Launching Local Frontends (Connected to Live Network)..."

# 1. Explorer
echo "🔹 Starting Explorer at http://localhost:3000..."
cd /Users/vahid/Documents/NovaCoin-Explorer
# Use nohup to keep it running
nohup npm start > ../explorer.log 2>&1 &

# 2. Wallet
echo "🔹 Starting Wallet at http://localhost:5173..."
cd /Users/vahid/Documents/NovaCoin-WebWallet
nohup npm run dev > ../wallet.log 2>&1 &

echo "✅ Apps Running!"
echo "   Explorer: http://localhost:3000"
echo "   Wallet:   http://localhost:5173"
echo "   Logs:     explorer.log, wallet.log"
echo "   To stop:  pkill node"
