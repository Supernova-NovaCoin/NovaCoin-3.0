#!/bin/bash
# deploy_community_update.sh
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

echo "🚀 Supernova Community Update Sequence"
echo "======================================"

update_community() {
    local NAME=$1
    local IP=$2
    local KEY=$3

    echo "🔹 [$NAME] Updating..."
    SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10"

    # Upload New Binary
    scp $SSH_OPTS -i $KEY $BIN $USER@$IP:~/nova_new

    # Fetch existing ID to preserve it (grep from running process or file?)
    # We used "identity.txt" in deploy_community.sh. Let's assume it's still there.
    # Command to restart:
    # 1. Kill old
    # 2. Swap binary
    # 3. Read key from identity.txt
    # 4. Start new
    
    REMOTE_CMD="pkill -9 nova; rm -f data/nova-9000/LOCK; mv ~/nova_new ~/nova; chmod +x ~/nova; MINERKEY=\$(grep 'SEED' identity.txt | awk '{print \$NF}'); nohup ~/nova -miner -minerkey \"\$MINERKEY\" -p2p :9000 -udp 8080 -peers $GENESIS_PEER > node.log 2>&1 &"
    
    ssh $SSH_OPTS -i $KEY $USER@$IP "$REMOTE_CMD"
    
    echo "   ✅ $NAME Updated."
}

update_community "Community-1 (DE)" $IP1 $KEY1
update_community "Community-2 (UK)" $IP2 $KEY2

echo "✅ All Community Nodes Updated."
