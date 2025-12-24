#!/bin/bash
set -e

#===============================================================================
# SUPERNOVA VALIDATOR NODE DEPLOYMENT
# For community validators joining the network
#===============================================================================

echo "=============================================="
echo "   SUPERNOVA VALIDATOR NODE DEPLOYMENT"
echo "=============================================="

USER="ubuntu"
DATA_DIR="/opt/supernova"

# Genesis Peers (connect to all 3)
GENESIS_PEERS="52.221.167.113:9000,13.39.245.31:9000,35.170.165.166:9000"

# Validator Node 1 - Germany
NODE1_IP="18.193.197.239"
NODE1_KEY="$HOME/.ssh/Germany_User-1.pem"
NODE1_NAME="Validator-Germany"

# Validator Node 2 - UK
NODE2_IP="13.134.190.77"
NODE2_KEY="$HOME/.ssh/UK_User-2.pem"
NODE2_NAME="Validator-UK"

#===============================================================================
# BUILD
#===============================================================================

echo ""
echo "[1/3] Building Linux Binary..."
GOOS=linux GOARCH=amd64 go build -o supernova_linux_amd64 .
echo "Done: supernova_linux_amd64"

#===============================================================================
# DEPLOY FUNCTION
#===============================================================================

deploy_validator() {
    local NAME=$1
    local IP=$2
    local KEY=$3

    echo ""
    echo "Deploying $NAME ($IP)..."
    echo "----------------------------------------------"

    # Stop existing
    echo "  Stopping existing..."
    ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$KEY" "$USER@$IP" "sudo systemctl stop supernova 2>/dev/null || sudo pkill -f nova || true"

    # Create dir
    echo "  Creating directories..."
    ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$KEY" "$USER@$IP" "sudo mkdir -p $DATA_DIR && sudo chown $USER:$USER $DATA_DIR"

    # Upload binary
    echo "  Uploading binary..."
    scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$KEY" supernova_linux_amd64 "$USER@$IP:$DATA_DIR/nova"
    ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$KEY" "$USER@$IP" "chmod +x $DATA_DIR/nova"

    # Generate unique seed on server
    echo "  Generating validator key..."
    SEED=$(ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$KEY" "$USER@$IP" "openssl rand -hex 32" 2>/dev/null)
    echo "  Seed: $SEED"

    # Create service file
    echo "  Creating systemd service..."
    ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$KEY" "$USER@$IP" "sudo tee /etc/systemd/system/supernova.service > /dev/null" << EOF
[Unit]
Description=Supernova Validator Node
After=network.target

[Service]
Type=simple
User=$USER
WorkingDirectory=$DATA_DIR
ExecStart=$DATA_DIR/nova -miner -minerkey $SEED -p2p :9000 -udp 8080 -peers $GENESIS_PEERS
Restart=always
RestartSec=10
LimitNOFILE=65535
StandardOutput=append:$DATA_DIR/node.log
StandardError=append:$DATA_DIR/node.log

[Install]
WantedBy=multi-user.target
EOF

    # Firewall
    echo "  Configuring firewall..."
    ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$KEY" "$USER@$IP" "sudo ufw allow 9000/tcp 2>/dev/null; sudo ufw allow 8080/udp 2>/dev/null; sudo ufw allow 8000/tcp 2>/dev/null" || true

    # Start service
    echo "  Starting service..."
    ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$KEY" "$USER@$IP" "sudo systemctl daemon-reload && sudo systemctl enable supernova && sudo systemctl start supernova"

    # Verify
    sleep 2
    STATUS=$(ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$KEY" "$USER@$IP" "sudo systemctl is-active supernova" 2>/dev/null || echo "unknown")

    if [ "$STATUS" = "active" ]; then
        echo "  ✅ $NAME deployed and running!"
    else
        echo "  ⚠️  Check logs: ssh -i $KEY $USER@$IP 'journalctl -u supernova -f'"
    fi
}

#===============================================================================
# DEPLOY VALIDATORS
#===============================================================================

echo ""
echo "[2/3] Deploying Validators..."

deploy_validator "$NODE1_NAME" "$NODE1_IP" "$NODE1_KEY"
deploy_validator "$NODE2_NAME" "$NODE2_IP" "$NODE2_KEY"

#===============================================================================
# VERIFY
#===============================================================================

echo ""
echo "[3/3] Verifying Network..."
sleep 5

echo ""
echo "Checking APIs..."
echo -n "  Germany ($NODE1_IP): "
curl -s --connect-timeout 5 "http://$NODE1_IP:8000/api/stats" && echo "" || echo "Starting..."

echo -n "  UK ($NODE2_IP): "
curl -s --connect-timeout 5 "http://$NODE2_IP:8000/api/stats" && echo "" || echo "Starting..."

echo ""
echo "=============================================="
echo "   VALIDATORS DEPLOYED"
echo "=============================================="
echo ""
echo "SSH Access:"
echo "  Germany: ssh -i ~/.ssh/Germany_User-1.pem ubuntu@$NODE1_IP"
echo "  UK:      ssh -i ~/.ssh/UK_User-2.pem ubuntu@$NODE2_IP"
echo ""
echo "Logs:"
echo "  ssh -i <key> ubuntu@<ip> 'tail -f /opt/supernova/node.log'"
echo ""
