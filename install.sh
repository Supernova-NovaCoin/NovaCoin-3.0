#!/bin/bash
set -e

echo "🌟 Supernova Pulse Network - Community Node Installer"
echo "==================================================="

# 1. Check Dependencies (Go)
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.21+ first: https://go.dev/dl/"
    exit 1
fi

echo "✅ Dependencies Checked."

# 2. Build Node
echo "🏗️  Building NovaCoin Node..."
# Assuming running from repo root or pulling. For now we assume repo is present or user cloned.
# Ideally this script clones the repo if not present.
# git clone https://github.com/novacoin-project/core.git supernova-node
# cd supernova-node
go build -o nova main.go explorer.go

echo "✅ Build Successful: ./nova"

# 3. Setup Environment
mkdir -p data
echo "📁 Data directory created."

# 4. Generate Key (Optional)
if [ ! -f "miner.key" ]; then
    echo "🔑 Generating new Miner Key..."
    ./nova -genkey > miner_identity.txt
    # Extract seed (hacky for demo)
    grep "SEED" miner_identity.txt | awk '{print $NF}' > miner.key
    echo "   Saved to miner.key"
else
    echo "🔑 Found existing miner.key"
fi

KEY=$(cat miner.key)
echo "   Miner Seed: $KEY"

# 5. Run Node
echo "🚀 Launching Supernova Node..."
echo "   P2P Port: 9000"
echo "   API Port: 8000"
echo "   Mining: ENABLED"
echo "   Peers: 52.221.167.113:9000 (Singapore Genesis)"

./nova -miner -minerkey "$KEY" -p2p :9000 -udp 8080 -peers 52.221.167.113:9000 &

PID=$!
echo "✅ Node running (PID: $PID). View logs with: tail -f nohup.out (if redirected)"
echo "   Explorer API: http://localhost:8000"
echo "   Press Ctrl+C to stop."

wait $PID
