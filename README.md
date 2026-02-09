# Uverus Gateway Agent (`infra-agent`)

Runs on every gateway node:  
`svr-gtw-nd1.uvrs.xyz`, `svr-gtw-nd2.uvrs.xyz`, `svr-gtw-nd3.uvrs.xyz`, …

### One-command install (Ubuntu 22.04 / 24.04)

```bash
# Auto-detects hostname (recommended)
curl -sSfL https://raw.githubusercontent.com/uverustech/infra-agent/main/setup.sh | bash
```

Or force a specific node name

```bash
NODE_ID=svr-gtw-nd1.uvrs.xyz curl -sSfL https://raw.githubusercontent.com/uverustech/infra-agent/main/setup.sh | bash
```

## What happens when you run it

- Installs Caddy + git

- Clones `github.com/uverustech/gtw-config` repo into `/etc/caddy`

- Downloads the pre-built infra-agent binary from GitHub Releases

- Starts systemd service that:
    - Auto-pulls config changes every 10 s
    - Validates + reloads Caddy atomically
    - Sends heartbeat + version + drift detection (ready for future dashboard)

- Exposes `/health` → returns "OK" (required for Bunny DNS)

## Health endpoint
```bash
http://[node].uvrs.xyz/health → 200 OK
```
