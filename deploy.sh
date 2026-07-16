#!/usr/bin/env bash
set -euo pipefail

# ─────────────────────────────────────────────
# VPSmon - Full Deploy Script
# Builds locally, uploads to VPS, installs as
# a systemd service. Run from project root.
# ─────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
APP_NAME="vpsmon"
BINARY="vpsmon"
REMOTE_DIR="/opt/vpsmon"
SERVICE_USER="vpsmon"
SERVICE_GROUP="vpsmon"

# ─── Gather input ────────────────────────────

echo "═══════════════════════════════════════"
echo "  VPSmon - Deploy"
echo "═══════════════════════════════════════"
echo ""

read -rp "VPS IP or hostname: " VPS_HOST
read -rp "SSH user [root]: " SSH_USER
SSH_USER="${SSH_USER:-root}"
read -rp "SSH port [22]: " SSH_PORT
SSH_PORT="${SSH_PORT:-22}"

echo ""
echo "── Login credentials for the web UI ──"
read -rp "Monitor username [admin]: " MON_USER
MON_USER="${MON_USER:-admin}"
while true; do
    read -rsp "Monitor password: " MON_PASS
    echo ""
    if [ -z "$MON_PASS" ]; then
        echo "Password cannot be empty. Try again."
        continue
    fi
    read -rsp "Confirm password: " MON_PASS2
    echo ""
    if [ "$MON_PASS" != "$MON_PASS2" ]; then
        echo "Passwords do not match. Try again."
        continue
    fi
    break
done

echo ""
echo "── Summary ────────────────────────────"
echo "  VPS:        ${SSH_USER}@${VPS_HOST}:${SSH_PORT}"
echo "  Web login:  ${MON_USER} / ****"
echo "  Port:       8088"
echo "────────────────────────────────────────"
read -rp "Proceed? [Y/n] " CONFIRM
CONFIRM="${CONFIRM:-Y}"
if [[ ! "$CONFIRM" =~ ^[Yy]$ ]]; then
    echo "Aborted."
    exit 0
fi

SSH_CMD="ssh -o StrictHostKeyChecking=accept-new -p ${SSH_PORT} ${SSH_USER}@${VPS_HOST}"
SCP_CMD="scp -o StrictHostKeyChecking=accept-new -P ${SSH_PORT}"

# ─── Step 1: Build ───────────────────────────

echo ""
echo "==> [1/4] Building binary for linux/amd64..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o "${SCRIPT_DIR}/${BINARY}" "${SCRIPT_DIR}"
echo "    Built: ${BINARY} ($(du -h "${SCRIPT_DIR}/${BINARY}" | cut -f1))"

# ─── Step 2: Upload ─────────────────────────

echo ""
echo "==> [2/4] Uploading binary to VPS..."
${SSH_CMD} "mkdir -p ${REMOTE_DIR}"
# Upload to a .tmp file first and move it to avoid "Text file busy" errors when updating
${SCP_CMD} "${SCRIPT_DIR}/${BINARY}" "${SSH_USER}@${VPS_HOST}:${REMOTE_DIR}/${APP_NAME}.tmp"
${SSH_CMD} "mv -f ${REMOTE_DIR}/${APP_NAME}.tmp ${REMOTE_DIR}/${APP_NAME}"
echo "    Uploaded to ${VPS_HOST}:${REMOTE_DIR}/${APP_NAME}"

# ─── Step 3: Setup on VPS ────────────────────

echo ""
echo "==> [3/4] Setting up service on VPS..."

${SSH_CMD} bash -s <<REMOTE_SCRIPT
set -euo pipefail

# Create system user/group
if ! getent group "${SERVICE_GROUP}" > /dev/null 2>&1; then
    groupadd --system "${SERVICE_GROUP}"
    echo "    Created group: ${SERVICE_GROUP}"
fi
if ! id "${SERVICE_USER}" > /dev/null 2>&1; then
    adduser --system --no-create-home --ingroup "${SERVICE_GROUP}" --shell /usr/sbin/nologin "${SERVICE_USER}" 2>/dev/null
    echo "    Created user: ${SERVICE_USER}"
fi

# Set permissions on binary
chmod 755 "${REMOTE_DIR}/${APP_NAME}"
chown "${SERVICE_USER}:${SERVICE_GROUP}" "${REMOTE_DIR}/${APP_NAME}"

# Create env file with credentials (readable only by service user)
PASS_HASH=\$("${REMOTE_DIR}/${APP_NAME}" -hash "${MON_PASS}")
cat > "${REMOTE_DIR}/.env" <<ENVEOF
MONITOR_ADDR=:8088
MONITOR_USER=${MON_USER}
MONITOR_PASS_HASH=\${PASS_HASH}
ENVEOF
chmod 600 "${REMOTE_DIR}/.env"
chown "${SERVICE_USER}:${SERVICE_GROUP}" "${REMOTE_DIR}/.env"
echo "    Created env file"

# Create systemd service
cat > "/etc/systemd/system/${APP_NAME}.service" <<SVCEOF
[Unit]
Description=VPSmon
After=network.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_GROUP}
WorkingDirectory=${REMOTE_DIR}
EnvironmentFile=${REMOTE_DIR}/.env
ExecStart=${REMOTE_DIR}/${APP_NAME}
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadOnlyPaths=/proc /sys

[Install]
WantedBy=multi-user.target
SVCEOF

# Enable and start
systemctl daemon-reload
systemctl enable "${APP_NAME}" --quiet
systemctl restart "${APP_NAME}"
echo "    Service enabled and started"

# Reload Caddy if installed
if systemctl is-active --quiet caddy 2>/dev/null; then
    systemctl reload caddy
    echo "    Caddy reloaded"
fi
REMOTE_SCRIPT

# ─── Step 4: Verify ─────────────────────────

echo ""
echo "==> [4/4] Verifying..."
sleep 2

STATUS=$(${SSH_CMD} "systemctl is-active ${APP_NAME} 2>/dev/null" || true)
if [ "$STATUS" = "active" ]; then
    echo "    Service is running!"
else
    echo "    WARNING: Service is not running (status: ${STATUS})"
    echo "    Check logs: ssh ${SSH_USER}@${VPS_HOST} -p ${SSH_PORT} 'journalctl -u ${APP_NAME} -n 20'"
    exit 1
fi

echo ""
echo "═══════════════════════════════════════"
echo "  Deployed successfully!"
echo ""
echo "  URL:     http://${VPS_HOST}:8088"
echo "  Login:   ${MON_USER}"
echo ""
echo "  Useful commands (run on VPS):"
echo "    systemctl status ${APP_NAME}"
echo "    journalctl -u ${APP_NAME} -f"
echo "    systemctl restart ${APP_NAME}"
echo "═══════════════════════════════════════"
