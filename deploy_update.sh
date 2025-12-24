#!/bin/bash
set -e

SERVER_IP="52.221.167.113"
SERVER_USER="root" # Change if needed

echo "🚀 Building Supernova for Linux/AMD64..."
GOOS=linux GOARCH=amd64 go build -o supernova_linux_amd64

echo "📦 Uploading binary to $SERVER_IP..."
scp supernova_linux_amd64 $SERVER_USER@$SERVER_IP:~/supernova_new

echo "🔄 Restarting Service..."
ssh $SERVER_USER@$SERVER_IP <<EOF
  mv ~/supernova_new /usr/local/bin/supernova
  chmod +x /usr/local/bin/supernova
  systemctl restart supernova
EOF

echo "✅ Update Complete!"
