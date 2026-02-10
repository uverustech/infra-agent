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

## CLI Usage

The refactored `infra-agent` uses Cobra for a clean CLI experience.

### Basic Commands
```bash
# Show version
infra-agent version

# Start the agent (default action)
infra-agent

# Manage configuration
infra-agent config set node-id my-node-1
infra-agent config get node-id
```

### Gateway Actions
```bash
# Pull latest Caddy config
infra-agent gateway pull

# Validate and reload Caddy
infra-agent gateway reload
```

### System Setup
```bash
# Run all setup steps (SSH, Hardening, Packages, Timezone)
sudo infra-agent setup --yes --verbose

# Run specific setup steps
sudo infra-agent setup ssh
sudo infra-agent setup hardening
```

## Configuration

Configuration can be managed via flags, environment variables (prefix `INFRA_`), or a config file (`infra-agent.yaml`).

| Key | Flag | Env Var | Default |
|-----|------|---------|---------|
| `node-id` | `-i`, `--node-id` | `INFRA_NODE_ID` | (none) |
| `node-type` | `-t`, `--node-type` | `INFRA_NODE_TYPE` | `server` |
| `control-url` | - | `INFRA_CONTROL_URL` | `https://control.uvrs.xyz` |
| `github-token` | - | `INFRA_GITHUB_TOKEN` | (none) |

## Build and Developer tools

- `scripts/bump-version.go`: Auto-bumps version based on commit message and tags the release.
- `.github/workflows/release.yml`: CI/CD pipeline for building and releasing binaries.
