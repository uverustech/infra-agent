# Infra Agent Documentation

The `infra-agent` is a core component of the uverustech infrastructure management system. It runs on every node in the cluster, providing system setup, management, monitoring, and log streaming capabilities.

## Overview

The agent performs two primary roles:
1.  **Provisioning & Setup**: Bootstrapping new nodes via the `setup` command.
2.  **Continuous Management**: Running as a background service to monitor system health, stream logs, and keep gateway configurations in sync.

---

## Background Activities

When started without a subcommand (or via its systemd service), the agent enters a long-running execution loop performing the following background tasks:

### 1. Log Streaming
The agent monitors the system journal via `journalctl -f -o json`.
- **Action**: It parses journal entries, extracts priority levels, units, and messages.
- **Delivery**: Logs are streamed in real-time to the Control Plane via a WebSocket connection (`/api/logs/stream`).
- **Resilience**: Automatically attempts to reconnect if the WebSocket connection is lost.

### 2. Heartbeat & Metrics
Every 10 seconds, the agent sends a heartbeat to the Control Plane.
- **Payload**: Includes Node ID, version, Git SHA (for gateway nodes), and health metrics.
- **Metrics Collected**:
    - **Disk Usage**: Percent used on the root partition.
    - **Memory Usage**: Percent used (based on `/proc/meminfo`).
    - **CPU Load**: 1-minute load average.
    - **Uptime**: System uptime since last boot.
- **Health Reporting**: If disk, memory, or CPU usage exceed critical thresholds (90%, 95%, and 98/load respectively), the node marks itself as unhealthy in the heartbeat.

### 3. Gateway Management (Gateway Nodes Only)
For nodes identified as `gateway`:
- **Sync**: It optionally performs a `git pull` on `/etc/caddy` to keep configurations up to date.
- **Reload**: It validates and reloads the Caddy server whenever changes are detected.
- **Auto-Pull**: If `auto-pull` is enabled in the configuration, these checks happen automatically every 10 seconds.

### 4. Self-Updates
The agent checks for new versions during each heartbeat. If a newer version is available on the Control Plane, it automatically downloads and replaces itself.

---

## Command Reference

### `setup`
Runs the system-level setup tasks required to bootstrap a node. Running `setup` without subcommands executes all steps.

- **`setup ssh`**: Installs administrative SSH public keys (`uvr-ops` and `uvr-root`).
- **`setup hostname`**: Configures system static and pretty hostnames based on the configured `node-id`.
- **`setup hardening`**: Runs the full system hardening sequence (see below).
- **`setup packages`**: *Placeholder* - Installs system packages.
- **`setup timezone`**: *Placeholder* - Configures system timezone.

---

### `setup hardening`
Secures the node by applying a sequence of hardening steps.

#### Actions (Run in sequence when using `setup hardening`):
1.  **`ensure-openssh-server`**: Ensures OpenSSH server is installed, enabled, and running.
2.  **`setup-ssh-keys`**: Adds administrative public keys to `~/.ssh/authorized_keys` with correct permissions.
3.  **`disable-ssh-password-and-pam`**: Enforces key-only authentication and disables password/PAM in `/etc/ssh/sshd_config.d/99-infra-hardening.conf`.
4.  **`install-and-configure-ufw`**: Sets up a deny-by-default firewall, allowing only OpenSSH by default.
5.  **`install-and-configure-fail2ban`**: Installs Fail2Ban with an aggressive SSH jail to block brute-force attempts.
6.  **`verify-hardening-status`**: Performs a final check and reports the hardening status (UFW, SSH settings, Fail2Ban).

#### Subcommands (Run specific steps):
- **`setup hardening ssh`**: Only runs Step 3 (Disable password/pam).
- **`setup hardening ufw`**: Only runs Step 4 (Configure UFW).
- **`setup hardening fail2ban`**: Only runs Step 5 (Configure Fail2Ban).
- **`setup hardening status`**: Only runs Step 6 (Verify status).

All setup actions (except those with `--yes`) will prompt for confirmation.

### `config`
Manages the agent's persistent configuration.
- **`config set [key] [value]`**: Saves a configuration value to `/etc/infra-agent/infra-agent.yaml`.
- **`config get [key]`**: Displays a specific configuration value.
- **Precedence**: Configuration follows this order: Command-line Flag > Environment Variable > Config File > Default Value.

### `gateway`
Commands specific to gateway nodes running Caddy.
- **`gateway pull`**: Manually triggers a `git pull` in the `/etc/caddy` directory.
- **`gateway reload`**: Validates the Caddyfile and reloads the Caddy service.
- **`gateway status`**: Displays current Git SHAs and detects "drift" (when local config lags behind the remote repository).

### `update`
Manually triggers a check for updates and performs a self-update if a newer version is available.

### `version`
Displays the current version of the `infra-agent`.

---

## Configuration Keys

| Key | Environment Variable | Default | Description |
| :--- | :--- | :--- | :--- |
| `node-id` | `INFRA_NODE_ID` | (required) | Unique identifier for the node. |
| `node-type` | `INFRA_NODE_TYPE` | `server` | Either `gateway` or `server`. |
| `control-url` | `INFRA_CONTROL_URL` | `https://command.uvrs.xyz` | URL of the Control Plane. |
| `github-token` | `INFRA_GITHUB_TOKEN` | - | Token used to fetch private SSH keys and configs. |
| `ssh-key-url` | `INFRA_SSH_KEY_URL` | (internal) | URL to the public SSH keys file. |
| `auto-pull` | `INFRA_AUTO_PULL` | `true` | Whether to automatically sync git configs on gateways. |
| `verbose` | `INFRA_VERBOSE` | `false` | Enable debug logging. |

## Utilities

The agent includes several internal utility functions located in `internal/agent` and `internal/config`:
- **Metric Helpers**: Read from `/proc/meminfo`, `/proc/loadavg`, and `/proc/uptime` to provide OS-agnostic (Linux) system stats.
- **Secret Masking**: Automatically masks tokens and sensitive strings when displaying configuration.
