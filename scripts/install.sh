#!/usr/bin/env bash
set -euo pipefail

# ─────────────────────────────────────────────
# VPSmon - Quick Install Script (Run on VPS)
# Usage:
# curl -sL https://raw.githubusercontent.com/leodeim/vpsmon/main/scripts/install.sh | sudo bash
# ─────────────────────────────────────────────

REPO="leodeim/vpsmon"
APP_NAME="vpsmon"
REMOTE_DIR="/opt/vpsmon"
SERVICE_USER="vpsmon"
SERVICE_GROUP="vpsmon"

if [ "$EUID" -ne 0 ]; then
  echo "Please run this script as root or with sudo."
  exit 1
fi

echo "═══════════════════════════════════════"
echo "  VPSmon - VPS Installer"
echo "═══════════════════════════════════════"
echo ""

# Determine architecture
ARCH=$(uname -m)
case $ARCH in
    x86_64) GOARCH="amd64" ;;
    aarch64) GOARCH="arm64" ;;
    armv8l) GOARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "==> Fetching latest release info for ${REPO}..."
LATEST_RELEASE_URL=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"browser_download_url":' | grep "linux-${GOARCH}" | cut -d '"' -f 4 | head -n 1 || true)

if [ -z "$LATEST_RELEASE_URL" ]; then
    echo "Error: Could not find a binary for linux-${GOARCH} in the latest release."
    echo "Make sure you have created a release on GitHub with attached binaries."
    exit 1
fi

echo "── Configuration ──────────────────────"
read -rp "Monitor port [8088]: " MON_PORT </dev/tty
MON_PORT="${MON_PORT:-8088}"

choose_skin() {
    local selected=0 key arrow

    render_skin_picker() {
        printf '\033[2KDashboard skin\n' >/dev/tty
        printf '\033[2K  Use Up/Down arrows, then press Enter.\n' >/dev/tty
        if [ "$selected" -eq 0 ]; then
            printf '\033[2K  > Terminal — compact TUI-inspired view\n' >/dev/tty
            printf '\033[2K    Modern   — visual dashboard view\n' >/dev/tty
        else
            printf '\033[2K    Terminal — compact TUI-inspired view\n' >/dev/tty
            printf '\033[2K  > Modern   — visual dashboard view\n' >/dev/tty
        fi
    }

    render_skin_picker
    while true; do
        IFS= read -rsn1 key </dev/tty
        case "$key" in
            '')
                if [ "$selected" -eq 0 ]; then
                    MON_SKIN="terminal"
                else
                    MON_SKIN="modern"
                fi
                printf '\n' >/dev/tty
                return
                ;;
            $'\e')
                IFS= read -rsn2 arrow </dev/tty || true
                case "$arrow" in
                    '[A'|'[B') selected=$((1 - selected)) ;;
                esac
                ;;
            k|K) selected=0 ;;
            j|J) selected=1 ;;
        esac
        printf '\033[4A' >/dev/tty
        render_skin_picker
    done
}

choose_skin

while true; do
    read -rp "Disable built-in authentication? [y/N]: " MON_NO_AUTH_INPUT </dev/tty
    case "${MON_NO_AUTH_INPUT,,}" in
        ""|n|no)
            MON_NO_AUTH="false"
            break
            ;;
        y|yes)
            MON_NO_AUTH="true"
            echo "WARNING: Anyone who can reach this port will have full dashboard access." >/dev/tty
            break
            ;;
        *) echo "Choose yes or no." >/dev/tty ;;
    esac
done

if [ "$MON_NO_AUTH" = "false" ]; then
    read -rp "Monitor username [admin]: " MON_USER </dev/tty
    MON_USER="${MON_USER:-admin}"

    while true; do
        read -rsp "Monitor password: " MON_PASS </dev/tty
        echo "" >/dev/tty
        if [ -z "$MON_PASS" ]; then
            echo "Password cannot be empty. Try again." >/dev/tty
            continue
        fi
        read -rsp "Confirm password: " MON_PASS2 </dev/tty
        echo "" >/dev/tty
        if [ "$MON_PASS" != "$MON_PASS2" ]; then
            echo "Passwords do not match. Try again." >/dev/tty
            continue
        fi
        break
    done
fi

echo ""
echo "==> Setting up directories and users..."
mkdir -p "${REMOTE_DIR}"

if ! getent group "${SERVICE_GROUP}" > /dev/null 2>&1; then
    groupadd --system "${SERVICE_GROUP}"
    echo "    Created group: ${SERVICE_GROUP}"
fi
if ! id "${SERVICE_USER}" > /dev/null 2>&1; then
    adduser --system --no-create-home --ingroup "${SERVICE_GROUP}" --shell /usr/sbin/nologin "${SERVICE_USER}" 2>/dev/null
    echo "    Created user: ${SERVICE_USER}"
fi

echo "==> Downloading VPSmon..."
curl -sL "$LATEST_RELEASE_URL" -o "${REMOTE_DIR}/${APP_NAME}.tmp"
mv -f "${REMOTE_DIR}/${APP_NAME}.tmp" "${REMOTE_DIR}/${APP_NAME}"
chmod 755 "${REMOTE_DIR}/${APP_NAME}"
chown "${SERVICE_USER}:${SERVICE_GROUP}" "${REMOTE_DIR}/${APP_NAME}"

echo "==> Downloading update/uninstall scripts..."
curl -sL "https://raw.githubusercontent.com/${REPO}/main/scripts/update.sh" -o "${REMOTE_DIR}/update.sh" || true
curl -sL "https://raw.githubusercontent.com/${REPO}/main/scripts/uninstall.sh" -o "${REMOTE_DIR}/uninstall.sh" || true
chmod 750 "${REMOTE_DIR}/update.sh" "${REMOTE_DIR}/uninstall.sh"

if [ "$MON_NO_AUTH" = "false" ]; then
    echo "==> Generating credentials..."
    PASS_HASH=$("${REMOTE_DIR}/${APP_NAME}" -hash "${MON_PASS}")
fi

{
    echo "MONITOR_ADDR=:${MON_PORT}"
    echo "MONITOR_SKIN=${MON_SKIN}"
    echo "MONITOR_NO_AUTH=${MON_NO_AUTH}"
    if [ "$MON_NO_AUTH" = "false" ]; then
        echo "MONITOR_USER=${MON_USER}"
        echo "MONITOR_PASS_HASH=${PASS_HASH}"
    fi
} > "${REMOTE_DIR}/.env"
chmod 600 "${REMOTE_DIR}/.env"
chown "${SERVICE_USER}:${SERVICE_GROUP}" "${REMOTE_DIR}/.env"

echo "==> Installing systemd service..."
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

systemctl daemon-reload
systemctl enable "${APP_NAME}" --quiet
systemctl restart "${APP_NAME}"

echo "==> Verifying service..."
sleep 2
STATUS=$(systemctl is-active ${APP_NAME} 2>/dev/null || true)
if [ "$STATUS" = "active" ]; then
    echo "    Service is running!"
else
    echo "    WARNING: Service is not running (status: ${STATUS})"
    echo "    Check logs: journalctl -u ${APP_NAME} -n 20"
    exit 1
fi

echo ""
echo "═══════════════════════════════════════"
echo "  VPSmon installed successfully!"
echo ""
echo "  URL:     http://<your-vps-ip>:${MON_PORT}"
if [ "$MON_NO_AUTH" = "true" ]; then
    echo "  Login:   disabled"
else
    echo "  Login:   ${MON_USER}"
fi
echo ""
echo "  Useful commands:"
echo "    systemctl status ${APP_NAME}"
echo "    journalctl -u ${APP_NAME} -f"
echo "    systemctl restart ${APP_NAME}"
echo "    sudo bash ${REMOTE_DIR}/update.sh"
echo "    sudo bash ${REMOTE_DIR}/uninstall.sh"
echo "═══════════════════════════════════════"
