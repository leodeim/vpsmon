<p align="center">
  <img src="misc/logo.png" width="400" alt="vpsmon Logo">
</p>

<p align="center">
  <a href="https://github.com/leodeim/vpsmon/actions/workflows/build.yml"><img src="https://github.com/leodeim/vpsmon/actions/workflows/build.yml/badge.svg" alt="Build and Release"></a>
  <a href="https://github.com/leodeim/vpsmon/releases/latest"><img src="https://img.shields.io/github/v/release/leodeim/vpsmon" alt="Latest Release"></a>
  <a href="https://github.com/leodeim/vpsmon/releases"><img src="https://img.shields.io/github/downloads/leodeim/vpsmon/total" alt="Downloads"></a>
  <a href="https://github.com/leodeim/vpsmon/blob/main/LICENSE"><img src="https://img.shields.io/github/license/leodeim/vpsmon" alt="License"></a>
</p>

## Features

- **Live System Metrics:** CPU, Memory, Swap, and Load Average
- **Docker Integration:** Automatically detects and displays live CPU/RAM usage for running containers
- **Process Monitoring:** Shows the top 5 processes by CPU and Memory usage
- **Disk & Network:** Tracks used/free space across all mounts and live network Rx/Tx speeds
- **Optional GPU Monitoring:** Shows NVIDIA (`nvidia-smi`) or AMD ROCm (`amd-smi`) GPU utilization, VRAM, and temperature when available
- **Built-in Security:** Password-protected Web UI (bcrypt) with login rate-limiting
- **Ultra Lightweight:** Single Go binary with zero dependencies and ~5MB RAM footprint

<p align="center">
  <img src="misc/screenshot.png" width="800" alt="vpsmon Screenshot">
</p>

## Installation

```bash
curl -sL https://raw.githubusercontent.com/leodeim/vpsmon/main/scripts/install.sh | sudo bash
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
