<p align="center">
  <img src="misc/logo.png" width="400" alt="vpsmon Logo">
</p>

# vpsmon

[![Build and Release](https://github.com/leodeim/vpsmon/actions/workflows/build.yml/badge.svg)](https://github.com/leodeim/vpsmon/actions/workflows/build.yml)
[![Latest Release](https://img.shields.io/github/v/release/leodeim/vpsmon)](https://github.com/leodeim/vpsmon/releases/latest)
[![Downloads](https://img.shields.io/github/downloads/leodeim/vpsmon/total)](https://github.com/leodeim/vpsmon/releases)
[![License](https://img.shields.io/github/license/leodeim/vpsmon)](https://github.com/leodeim/vpsmon/blob/main/LICENSE)

Lightweight system monitor for Linux VPS. Single Go binary, ~5MB RAM usage.

<p align="center">
  <img src="misc/screenshot.png" width="800" alt="vpsmon Screenshot">
</p>

Monitors CPU, memory, swap, disk, network, uptime, and process count. Web UI with login, auto-refreshes every 5 seconds.

## Installation

```bash
curl -sL https://raw.githubusercontent.com/leodeim/vpsmon/main/install.sh | sudo bash
```

## Useful Commands

Once installed on your VPS, you can manage the monitor using these commands:

- **Check status:** `sudo systemctl status vpsmon`
- **View live logs:** `sudo journalctl -u vpsmon -f`
- **Update to latest:** `sudo bash /opt/vpsmon/update.sh`
- **Restart service:** `sudo systemctl restart vpsmon`
- **Edit configuration:** `sudo nano /opt/vpsmon/.env` (Restart required after changing port or credentials)
- **Uninstall:** `sudo bash /opt/vpsmon/uninstall.sh`

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `MONITOR_ADDR` | `:8088` | Listen address |
| `MONITOR_USER` | `admin` | Web UI username |
| `MONITOR_PASS_HASH` | (hash of `changeme`) | Web UI password (bcrypt hash) |

## Reverse Proxy (HTTPS)

[Caddy](https://caddyserver.com/) is the easiest way to expose `vpsmon` with automatic HTTPS. Add the following to your `Caddyfile`:

```caddyfile
monitor.yourdomain.com {
    reverse_proxy 127.0.0.1:8088
}
```
