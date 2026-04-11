# VPS Monitor

Lightweight system monitor for Linux VPS. Single Go binary, ~5MB RAM usage.

Monitors CPU, memory, swap, disk, network, uptime, and process count. Web UI with login, auto-refreshes every 5 seconds.

## Deploy

```bash
./deploy.sh
```

The script will ask for VPS connection details and web UI credentials, then build, upload, and install as a systemd service on port 8088.

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `MONITOR_ADDR` | `:8088` | Listen address |
| `MONITOR_USER` | `admin` | Web UI username |
| `MONITOR_PASS` | `changeme` | Web UI password |
