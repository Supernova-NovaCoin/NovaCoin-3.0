#!/bin/bash
# deploy_reset.sh - THE BIG RED BUTTON
# Wipes all data users, logs, and restarts the Supernova Network from Block 0.
set -e

# Configuration
USER="ubuntu"
BIN="./supernova_linux_amd64"

# 1. Singapore (Genesis 1)
IP1="52.221.167.113"
KEY1="$HOME/.ssh/Singapore_Nova-1.pem"

# 2. Mumbai (Genesis 2)
IP2="13.205.59.208"
KEY2="$HOME/.ssh/Mumbai_Nova-2.pem"
SEED2="3456dd1fde79a7e4da4c22897cff695db252aba429ea715b68a2b8e6aaf78fdb"

# 3. USA (Genesis 3)
IP3="35.170.165.166"
KEY3="$HOME/.ssh/USA_Nova-3.pem"
SEED3="acc7c020b65d1d18f63b1e8cbabec25f1a755006759ced445c06c4c8bbb9be32"

# 4. Germany (Community 1)
IP4="18.193.197.239"
KEY4="$HOME/.ssh/Germany_User-1.pem"

# 5. UK (Community 2)
IP5="13.134.190.77"
KEY5="$HOME/.ssh/UK_User-2.pem"

echo "☢️  INITIATING SUPERNOVA NETWORK RESET ☢️"
echo "============================================"
echo "⚠️  ALL DATA WILL BE WIPED. STARTING FRESH."
echo "============================================"

# Rebuild
echo "🔨 Building Linux Binary..."
GOOS=linux GOARCH=amd64 go build -o supernova_linux_amd64 main.go explorer.go

reset_node() {
    local NAME=$1
    local IP=$2
    local KEY=$3
    local ARGS=$4
    
    echo "🔹 [$NAME] Resetting..."
    SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10"

    # 1. Graceful Stop
    ssh $SSH_OPTS -i $KEY $USER@$IP "pkill nova || true"
    
    # 2. Safety Sleep (Allow DB flush)
    sleep 3
    
    # 3. Force Kill & Wipe
    # We remove data/ and the binary to be clean
    ssh $SSH_OPTS -i $KEY $USER@$IP "pkill -9 nova; rm -rf ~/data ~/nova ~/node.log"
    
    # 4. Upload New Binary
    scp $SSH_OPTS -i $KEY $BIN $USER@$IP:~/nova
    ssh $SSH_OPTS -i $KEY $USER@$IP "chmod +x ~/nova && mkdir -p ~/data"

    # 5. Start
    ssh $SSH_OPTS -i $KEY $USER@$IP "nohup ~/nova $ARGS > node.log 2>&1 &"
    
    echo "   ✅ $NAME Active (Block 0)"
}

# --- EXECUTION ORDER MATTERS ---

# 1. Genesis 1 (Bootstrap)
reset_node "Genesis-1 (SG)" $IP1 $KEY1 "-miner -p2p :9000 -udp 8080"

echo "   Waiting 5s for Pulse..."
sleep 5

# 2. Genesis Peers
reset_node "Genesis-2 (IN)" $IP2 $KEY2 "-miner -minerkey $SEED2 -p2p :9000 -udp 8080 -peers $IP1:9000"
reset_node "Genesis-3 (US)" $IP3 $KEY3 "-miner -minerkey $SEED3 -p2p :9000 -udp 8080 -peers $IP1:9000,$IP2:9000"

# 3. Community Nodes (They need their miner keys generated properly)
# For reset, we can re-generate or re-use.
# Simplest: Just run them pointing to Genesis 1.
# But we need their keys.
# Option A: We assume they still have their identity.txt? NO, we wiped it (if in home or data).
# Actually, deploy_community.sh wrote 'identity.txt' to ~/identity.txt. My rm command was `rm -rf ~/data ~/nova ~/node.log`.
# So identity.txt might survive!
# Let's check if identity.txt exists, if so use it, if not gen new.
# Or simpler: Just Run the command that grabs it or generates it.

reset_community() {
    local NAME=$1
    local IP=$2
    local KEY=$3
    
    echo "🔹 [$NAME] Resetting..."
    SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10"

    # Wipe & Kill
    ssh $SSH_OPTS -i $KEY $USER@$IP "pkill nova || true; sleep 2; pkill -9 nova; rm -rf ~/data ~/nova ~/node.log"
    
    # Upload
    scp $SSH_OPTS -i $KEY $BIN $USER@$IP:~/nova
    ssh $SSH_OPTS -i $KEY $USER@$IP "chmod +x ~/nova"

    # Start (Re-using Identity if present, otherwise we'd need to regen. 
    # Since we didn't delete identity.txt in home, it should be safe.
    # We will use the 'grep' trick again)
    
    REMOTE_CMD="if [ ! -f identity.txt ]; then ./nova -genkey > identity.txt; fi; MINERKEY=\$(grep 'SEED' identity.txt | awk '{print \$NF}'); nohup ~/nova -miner -minerkey \"\$MINERKEY\" -p2p :9000 -udp 8080 -peers $IP1:9000 > node.log 2>&1 &"
    
    ssh $SSH_OPTS -i $KEY $USER@$IP "$REMOTE_CMD"
    echo "   ✅ $NAME Active (Block 0)"
}

reset_community "Community-1 (DE)" $IP4 $KEY4
reset_community "Community-2 (UK)" $IP5 $KEY5

echo "============================================"
echo "🌟 NETWORK RESET COMPLETE."
echo "📡 Verifying..."
sleep 5
curl -s http://$IP1:8000/api/stats
