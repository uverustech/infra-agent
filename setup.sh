#!/bin/bash
set -e

echo "=== Uverus Infra Agent Installer ==="
echo "Detecting system..."

# Variables — change only these lines if needed
CONFIG_REPO="git@github.com:uverustech/gtw-config.git"
RELEASE_URL="https://github.com/uverustech/infra-agent/releases/latest/download/infra-agent-linux-amd64"

if [[ -z "$NODE_ID" ]]; then
  read -p "Enter Node ID (e.g. svr-gtw-nd1.uvrs.xyz) [$(hostname -f)]: " input_id < /dev/tty
  NODE_ID="${input_id:-$(hostname -f)}"
fi

if [[ -z "$NODE_TYPE" ]]; then
  echo "Select Node Type:"
  echo "1) gateway"
  echo "2) server:build"
  echo "3) server:applications"
  echo "4) server:banking"
  echo "5) server (default)"
  echo "6) service:analytics"
  echo "7) custom"
  read -p "Choice [1-7]: " type_choice < /dev/tty

  case $type_choice in
    1) NODE_TYPE="gateway" ;;
    2) NODE_TYPE="server:build" ;;
    3) NODE_TYPE="server:applications" ;;
    4) NODE_TYPE="server:banking" ;;
    5|"") NODE_TYPE="server" ;;
    6) NODE_TYPE="service:analytics" ;;
    7) read -p "Enter custom node type: " custom_type < /dev/tty; NODE_TYPE="$custom_type" ;;
    *) NODE_TYPE="server" ;;
  esac
fi

if [[ -z "$NODE_ID" || "$NODE_ID" == "localhost" ]]; then
  echo "Error: Cannot detect proper hostname. Set NODE_ID manually."
  echo "Example: NODE_ID=svr-gtw-nd1.uvrs.xyz $0"
  exit 1
fi

echo "Node will register as: $NODE_ID (Type: $NODE_TYPE)"
echo "Config repo: $CONFIG_REPO"
sleep 2

echo "Updating system & installing dependencies..."
export DEBIAN_FRONTEND=noninteractive
apt update -qq
apt install -y git curl jq > /dev/null

if [[ "$NODE_TYPE" == "gateway" ]]; then
  echo "Installing Caddy..."
  apt install -y caddy > /dev/null

  echo "Setting up /etc/caddy directory..."
  rm -rf /etc/caddy
  mkdir -p /etc/caddy
  cd /etc/caddy

  echo "Cloning config repo..."
  git clone $CONFIG_REPO . || { echo "Failed to clone repo. Check SSH key!"; exit 1; }
fi

echo "Installing infra-agent binary..."
curl -sSfL "$RELEASE_URL" -o /usr/local/bin/infra-agent.NEW
chmod +x /usr/local/bin/infra-agent.NEW
mv /usr/local/bin/infra-agent.NEW /usr/local/bin/infra-agent

echo "Running system setup..."
/usr/local/bin/infra-agent --setup -y || echo "Warning: System setup failed"

systemctl restart infra-agent || true

echo "Creating systemd service..."
cat > /etc/systemd/system/infra-agent.service <<SERVICE
[Unit]
Description=Uverus Infra Agent
After=network.target

[Service]
Type=simple
Environment="NODE_ID=$NODE_ID"
Environment="NODE_TYPE=$NODE_TYPE"
ExecStart=/usr/local/bin/infra-agent
Restart=always
RestartSec=5
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
SERVICE

systemctl daemon-reload
systemctl enable --now infra-agent

if [[ "$NODE_TYPE" == "gateway" ]]; then
  echo "Reloading Caddy with your config..."
  caddy reload --config /etc/caddy/Caddyfile || echo "Warning: Caddy reload failed (will retry automatically)"
fi

echo ""
echo "All done! Your gateway node is live."
echo "Node ID: $NODE_ID"
echo "Health check: http://$NODE_ID/health → should return OK"
echo "DNS can now point to this IP"
echo ""
