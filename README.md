<p align="center">
  <img src="misc/logo.png" width="200" alt="vpsmon Logo">
</p>

# vpsmon

Lightweight system monitor for Linux VPS. Single Go binary, ~5MB RAM usage.

<p align="center">
  <img src="misc/screenshot.png" width="800" alt="vpsmon Screenshot">
</p>

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
| `MONITOR_PASS_HASH` | (hash of `changeme`) | Web UI password (bcrypt hash) |

## Password Hashing

For security, the password is not stored in plaintext. You must provide a bcrypt hash via the `MONITOR_PASS_HASH` environment variable. The `deploy.sh` script handles this automatically during setup.

If you need to generate a hash manually, you can run the binary with the `-hash` flag:

```bash
./vpsmon -hash "my_secure_password"
```
