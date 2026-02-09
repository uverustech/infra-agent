#!/bin/bash
set -e

echo "=== Uverus Infra Agent Installer ==="
echo "Detecting system..."

# Variables — change only these two lines if needed
CONFIG_REPO="git@github.com:uverustech/gtw-config.git"
NODE_ID="${NODE_ID:-$(hostname -f)}"
RELEASE_URL="https://github.com/uverustech/infra-agent/releases/latest/download/infra-agent-linux-amd64"

if [[ -z "$NODE_ID" || "$NODE_ID" == "localhost" ]]; then
  echo "Error: Cannot detect proper hostname. Set NODE_ID manually."
  echo "Example: NODE_ID=svr-gtw-nd1.uvrs.xyz $0"
  exit 1
fi

echo "Node will register as: $NODE_ID"
echo "Config repo: $CONFIG_REPO"
sleep 3

echo "Updating system & installing dependencies..."
export DEBIAN_FRONTEND=noninteractive
apt update -qq
apt install -y git caddy curl jq > /dev/null

echo "Setting up /etc/caddy directory..."
rm -rf /etc/caddy
mkdir -p /etc/caddy
cd /etc/caddy

echo "Cloning config repo..."
git clone $CONFIG_REPO . || { echo "Failed to clone repo. Check SSH key!"; exit 1; }

echo "Installing infra-agent binary..."
curl -sSfL "$RELEASE_URL" -o /usr/local/bin/infra-agent.NEW
chmod +x /usr/local/bin/infra-agent.NEW
mv /usr/local/bin/infra-agent.NEW /usr/local/bin/infra-agent
systemctl restart infra-agent || true

echo "Creating systemd service..."
cat > /etc/systemd/system/infra-agent.service <<SERVICE
[Unit]
Description=Uverus Infra Agent
After=network.target

[Service]
Type=simple
Environment="NODE_ID=$NODE_ID"
ExecStart=/usr/local/bin/infra-agent
Restart=always
RestartSec=5
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
SERVICE

systemctl daemon-reload
systemctl enable --now infra-agent

echo "Reloading Caddy with your config..."
caddy reload --config /etc/caddy/Caddyfile || echo "Warning: Caddy reload failed (will retry automatically)"

echo ""
echo "All done! Your gateway node is live."
echo "Node ID: $NODE_ID"
echo "Health check: http://$NODE_ID/health → should return OK"
echo "DNS can now point to this IP"
echo ""
