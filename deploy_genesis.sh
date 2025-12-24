#!/bin/bash
set -e

# Configuration
USER="ubuntu" # Assuming Ubuntu servers
BIN="./supernova_linux_amd64"

# Nodes
IP1="52.221.167.113"
KEY1="$HOME/.ssh/Singapore_Nova-1.pem"

IP2="13.205.59.208"
KEY2="$HOME/.ssh/Mumbai_Nova-2.pem"
SEED2="3456dd1fde79a7e4da4c22897cff695db252aba429ea715b68a2b8e6aaf78fdb"

IP3="35.170.165.166"
KEY3="$HOME/.ssh/USA_Nova-3.pem"
SEED3="acc7c020b65d1d18f63b1e8cbabec25f1a755006759ced445c06c4c8bbb9be32"

echo "🚀 Supernova Genesis Deployment Sequence"
echo "========================================"

# Function to Deploy
deploy() {
    local NAME=$1
    local IP=$2
    local KEY=$3
    local CMD_ARGS=$4

    echo "🔹 [$NAME] Connecting to $IP..."
    
    # SSH Options to avoid prompts
    SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10"
    
    # 1. Stop existing
    ssh $SSH_OPTS -i $KEY $USER@$IP "pkill nova || true"

    # 2. Upload Binary
    echo "   Uploading binary..."
    scp $SSH_OPTS -i $KEY $BIN $USER@$IP:~/nova
    ssh $SSH_OPTS -i $KEY $USER@$IP "chmod +x ~/nova && mkdir -p ~/data"

    # 3. Start Node
    echo "   Starting Node..."
    # We use nohup and redirect output to a log file
    ssh $SSH_OPTS -i $KEY $USER@$IP "nohup ~/nova -miner $CMD_ARGS > node.log 2>&1 &"
    
    echo "   ✅ $NAME Deployed & Running!"
}

# --- Execution ---

# Node 1: Singapore (Bootstrap)
# No peers (it is the first), Default Key (Dev Key)
deploy "Genesis-1 (SG)" $IP1 $KEY1 "-p2p :9000 -udp 8080"
echo "   Waiting 5s for bootstrap..."
sleep 5

# Node 2: Mumbai
# Peer: SG
deploy "Genesis-2 (IN)" $IP2 $KEY2 "-p2p :9000 -udp 8080 -peers $IP1:9000 -minerkey $SEED2"

# Node 3: USA
# Peers: SG, IN
deploy "Genesis-3 (US)" $IP3 $KEY3 "-p2p :9000 -udp 8080 -peers $IP1:9000,$IP2:9000 -minerkey $SEED3"

echo "========================================"
echo "🌟 All Genesis Nodes Deployed!"
echo "📡 Checking Connectivity via API..."

# Verify Node 1 API
echo "   Checking SG API ($IP1:8000)..."
curl -s --connect-timeout 5 "http://$IP1:8000/api/stats" || echo "   ⚠️  API Access Failed (Firewall?)"

echo "✅ Done."
