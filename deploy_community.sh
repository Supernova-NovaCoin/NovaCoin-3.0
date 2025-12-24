#!/bin/bash
set -e

# Configuration
USER="ubuntu"
BIN="./supernova_linux_amd64"
GENESIS_PEER="52.221.167.113:9000"

# Community Nodes
IP1="18.193.197.239"
KEY1="$HOME/.ssh/Germany_User-1.pem"

IP2="13.134.190.77"
KEY2="$HOME/.ssh/UK_User-2.pem"

echo "🚀 Supernova Community Onboarding"
echo "================================="

deploy_community() {
    local NAME=$1
    local IP=$2
    local KEY=$3

    echo "🔹 [$NAME] Onboarding Node at $IP..."
    
    # SSH Options
    SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10"

    # 1. Stop existing & Clean
    ssh $SSH_OPTS -i $KEY $USER@$IP "pkill nova || true"

    # 2. Upload Binary
    echo "   Uploading binary..."
    scp $SSH_OPTS -i $KEY $BIN $USER@$IP:~/nova
    ssh $SSH_OPTS -i $KEY $USER@$IP "chmod +x ~/nova && mkdir -p ~/data"

    # 3. Generate Identity (Remote)
    echo "   Generating unique Miner Identity..."
    # We run genkey and parse the seed from the output
    # Output format: "MINER SEED: <hex>"
    # We'll just dump to file and read it back or just use grep in one go
    ssh $SSH_OPTS -i $KEY $USER@$IP "./nova -genkey > identity.txt"
    
    # 4. Start Node
    echo "   Starting Node..."
    # Read the key we just made
    local REMOTE_CMD="KEY=\$(grep 'SEED' identity.txt | awk '{print \$NF}'); nohup ~/nova -miner -minerkey \"\$KEY\" -p2p :9000 -udp 8080 -peers $GENESIS_PEER > node.log 2>&1 &"
    
    ssh $SSH_OPTS -i $KEY $USER@$IP "$REMOTE_CMD"
    
    echo "   ✅ $NAME Deployed & Mining!"
}

# --- Execution ---

deploy_community "Community-1 (DE)" $IP1 $KEY1
deploy_community "Community-2 (UK)" $IP2 $KEY2

echo "================================="
echo "🌍 Community Nodes Active."
