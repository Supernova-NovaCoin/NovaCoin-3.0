#!/bin/bash
# deploy_update.sh - Updates binaries without wiping data
set -e

# Configuration
USER="ubuntu"
BIN="./supernova_linux_amd64"

# Nodes
IP1="52.221.167.113"
KEY1="$HOME/.ssh/Singapore_Nova-1.pem"

IP2="13.205.59.208"
KEY2="$HOME/.ssh/Mumbai_Nova-2.pem"

IP3="35.170.165.166"
KEY3="$HOME/.ssh/USA_Nova-3.pem"

echo "🚀 Supernova Update Sequence (Rolling Update)"
echo "============================================"

# Rebuild
echo "🔨 Building Linux Binary..."
GOOS=linux GOARCH=amd64 go build -o supernova_linux_amd64 main.go explorer.go

update_node() {
    local NAME=$1
    local IP=$2
    local KEY=$3
    
    echo "🔹 [$NAME] Updating..."
    # SSH Options
    SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10"

    # Upload
    scp $SSH_OPTS -i $KEY $BIN $USER@$IP:~/nova_new
    
    # Swap & Restart
    # We kill the process, replace binary, and restart using nohup
    # Note: We need to preserve the command arguments!
    # Simple way: The nodes are running via nohup. We can just kill and restart with same args.
    # BUT we need to know the args.
    # A cleaner devops way is systemd, but we are using nohup.
    # For now, we will just restart them with the KNOWN args from deploy_genesis.sh.
    
    # Singapore
    if [[ "$IP" == "$IP1" ]]; then
       ARGS="-miner -p2p :9000 -udp 8080"
    elif [[ "$IP" == "$IP2" ]]; then
       ARGS="-miner -minerkey 3456dd1fde79a7e4da4c22897cff695db252aba429ea715b68a2b8e6aaf78fdb -p2p :9000 -udp 8080 -peers $IP1:9000"
    else
       ARGS="-miner -minerkey acc7c020b65d1d18f63b1e8cbabec25f1a755006759ced445c06c4c8bbb9be32 -p2p :9000 -udp 8080 -peers $IP1:9000,$IP2:9000"
    fi

    # Restart
    ssh $SSH_OPTS -i $KEY $USER@$IP "pkill nova || true; mv ~/nova_new ~/nova; chmod +x ~/nova; nohup ~/nova $ARGS > node.log 2>&1 &"
    
    echo "   ✅ $NAME Updated."
}

update_node "Genesis-1 (SG)" $IP1 $KEY1
sleep 2 # Let it start
update_node "Genesis-2 (IN)" $IP2 $KEY2
update_node "Genesis-3 (US)" $IP3 $KEY3

echo "✅ All Genesis Nodes Updated."
