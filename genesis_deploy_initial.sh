#!/bin/bash
set -e

#===============================================================================
# SUPERNOVA GENESIS NETWORK - INITIAL DEPLOYMENT
# Deploys 3 Genesis Validator Nodes for Production Launch
#===============================================================================

echo "=============================================="
echo "   SUPERNOVA GENESIS NETWORK DEPLOYMENT"
echo "   Production Initial Setup"
echo "=============================================="

# --- CONFIGURATION ---

USER="ubuntu"
BINARY_NAME="nova"
DATA_DIR="/opt/supernova"
SERVICE_NAME="supernova"

# Genesis Node 1 - Singapore
NODE1_IP="52.221.167.113"
NODE1_KEY="$HOME/.ssh/Singapore_Nova-1.pem"
NODE1_SEED="bb00616aa658a469e6a62e6615365d642291119c9440c760205087371f1aae9a"
NODE1_NAME="Genesis-1-Singapore"

# Genesis Node 2 - Paris
NODE2_IP="13.39.245.31"
NODE2_KEY="$HOME/.ssh/paris.pem"
NODE2_SEED="ca7d08d833197744b3b7785347af1bcd4b8c3a2afdd3dd29df691d50d63d6239"
NODE2_NAME="Genesis-2-Paris"

# Genesis Node 3 - USA
NODE3_IP="35.170.165.166"
NODE3_KEY="$HOME/.ssh/USA_Nova-3.pem"
NODE3_SEED="0e67d464775b64fd1112dabb9b57f0220c5e6f8a471fe12e59309558d3b5cb73"
NODE3_NAME="Genesis-3-USA"

# SSH Options
SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=30 -o ServerAliveInterval=60"

#===============================================================================
# STEP 1: BUILD BINARY FOR LINUX
#===============================================================================

echo ""
echo "[1/5] Building Linux AMD64 Binary..."
echo "----------------------------------------------"

GOOS=linux GOARCH=amd64 go build -o supernova_linux_amd64 .

if [ ! -f "supernova_linux_amd64" ]; then
    echo "ERROR: Build failed!"
    exit 1
fi

echo "Binary built: supernova_linux_amd64 ($(ls -lh supernova_linux_amd64 | awk '{print $5}'))"

#===============================================================================
# STEP 2: DEPLOY FUNCTION
#===============================================================================

deploy_node() {
    local NAME=$1
    local IP=$2
    local KEY=$3
    local SEED=$4
    local PEERS=$5

    echo ""
    echo "Deploying $NAME to $IP..."
    echo "----------------------------------------------"

    # Verify SSH key exists
    if [ ! -f "$KEY" ]; then
        echo "ERROR: SSH key not found: $KEY"
        exit 1
    fi

    # 1. Stop existing process
    echo "  [1/6] Stopping existing node..."
    ssh $SSH_OPTS -i "$KEY" "$USER@$IP" "sudo systemctl stop $SERVICE_NAME 2>/dev/null || sudo pkill -f nova || true" 2>/dev/null || true

    # 2. Create directories
    echo "  [2/6] Creating directories..."
    ssh $SSH_OPTS -i "$KEY" "$USER@$IP" "sudo mkdir -p $DATA_DIR && sudo chown $USER:$USER $DATA_DIR"

    # 3. Upload binary
    echo "  [3/6] Uploading binary..."
    scp $SSH_OPTS -i "$KEY" supernova_linux_amd64 "$USER@$IP:$DATA_DIR/$BINARY_NAME"
    ssh $SSH_OPTS -i "$KEY" "$USER@$IP" "chmod +x $DATA_DIR/$BINARY_NAME"

    # 4. Create systemd service
    echo "  [4/6] Creating systemd service..."
    ssh $SSH_OPTS -i "$KEY" "$USER@$IP" "sudo tee /etc/systemd/system/$SERVICE_NAME.service > /dev/null << 'SERVICEEOF'
[Unit]
Description=Supernova Blockchain Node
After=network.target

[Service]
Type=simple
User=$USER
WorkingDirectory=$DATA_DIR
ExecStart=$DATA_DIR/$BINARY_NAME -miner -minerkey $SEED -p2p :9000 -udp 8080 $PEERS
Restart=always
RestartSec=10
LimitNOFILE=65535

# Logging
StandardOutput=append:$DATA_DIR/node.log
StandardError=append:$DATA_DIR/node.log

[Install]
WantedBy=multi-user.target
SERVICEEOF"

    # 5. Configure firewall
    echo "  [5/6] Configuring firewall..."
    ssh $SSH_OPTS -i "$KEY" "$USER@$IP" "sudo ufw allow 9000/tcp comment 'Supernova P2P' 2>/dev/null || true"
    ssh $SSH_OPTS -i "$KEY" "$USER@$IP" "sudo ufw allow 8080/udp comment 'Supernova UDP Ingest' 2>/dev/null || true"
    ssh $SSH_OPTS -i "$KEY" "$USER@$IP" "sudo ufw allow 8000/tcp comment 'Supernova API' 2>/dev/null || true"

    # 6. Start service
    echo "  [6/6] Starting service..."
    ssh $SSH_OPTS -i "$KEY" "$USER@$IP" "sudo systemctl daemon-reload && sudo systemctl enable $SERVICE_NAME && sudo systemctl start $SERVICE_NAME"

    # Verify
    sleep 3
    local STATUS=$(ssh $SSH_OPTS -i "$KEY" "$USER@$IP" "sudo systemctl is-active $SERVICE_NAME" 2>/dev/null || echo "unknown")

    if [ "$STATUS" = "active" ]; then
        echo "  ✅ $NAME deployed and running!"
    else
        echo "  ⚠️  $NAME may have issues. Check logs with:"
        echo "      ssh -i $KEY $USER@$IP 'sudo journalctl -u $SERVICE_NAME -f'"
    fi
}

#===============================================================================
# STEP 3: DEPLOY GENESIS NODES
#===============================================================================

echo ""
echo "[2/5] Deploying Genesis Node 1 (Bootstrap)..."
echo "=============================================="
deploy_node "$NODE1_NAME" "$NODE1_IP" "$NODE1_KEY" "$NODE1_SEED" ""

echo ""
echo "Waiting 10 seconds for bootstrap node to initialize..."
sleep 10

echo ""
echo "[3/5] Deploying Genesis Node 2..."
echo "=============================================="
deploy_node "$NODE2_NAME" "$NODE2_IP" "$NODE2_KEY" "$NODE2_SEED" "-peers $NODE1_IP:9000"

echo ""
echo "[4/5] Deploying Genesis Node 3..."
echo "=============================================="
deploy_node "$NODE3_NAME" "$NODE3_IP" "$NODE3_KEY" "$NODE3_SEED" "-peers $NODE1_IP:9000,$NODE2_IP:9000"

#===============================================================================
# STEP 4: VERIFY NETWORK
#===============================================================================

echo ""
echo "[5/5] Verifying Network..."
echo "=============================================="

sleep 5

echo ""
echo "Checking API endpoints..."

for NODE_IP in $NODE1_IP $NODE2_IP $NODE3_IP; do
    echo -n "  $NODE_IP:8000 ... "
    RESULT=$(curl -s --connect-timeout 5 "http://$NODE_IP:8000/api/stats" 2>/dev/null || echo "FAILED")
    if [[ "$RESULT" == *"totalBlocks"* ]]; then
        echo "✅ Online"
    else
        echo "⚠️  Not responding (may need a moment)"
    fi
done

#===============================================================================
# DEPLOYMENT COMPLETE
#===============================================================================

echo ""
echo "=============================================="
echo "   GENESIS NETWORK DEPLOYMENT COMPLETE"
echo "=============================================="
echo ""
echo "Node Status Commands:"
echo "  Singapore: ssh -i ~/.ssh/Singapore_Nova-1.pem ubuntu@$NODE1_IP 'sudo systemctl status supernova'"
echo "  Paris:     ssh -i ~/.ssh/paris.pem ubuntu@$NODE2_IP 'sudo systemctl status supernova'"
echo "  USA:       ssh -i ~/.ssh/USA_Nova-3.pem ubuntu@$NODE3_IP 'sudo systemctl status supernova'"
echo ""
echo "View Logs:"
echo "  ssh -i <key> ubuntu@<ip> 'tail -f /opt/supernova/node.log'"
echo ""
echo "API Endpoints:"
echo "  http://$NODE1_IP:8000/api/stats"
echo "  http://$NODE2_IP:8000/api/stats"
echo "  http://$NODE3_IP:8000/api/stats"
echo ""
echo "WebSocket (Real-time blocks):"
echo "  ws://$NODE1_IP:8000/ws"
echo ""
