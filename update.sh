#!/usr/bin/env bash
set -euo pipefail

# ─────────────────────────────────────────────
# VPSmon - Update Script
# Usage:
# curl -sO https://raw.githubusercontent.com/leodeim/vpsmon/main/update.sh
# sudo bash update.sh
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
echo "  VPSmon - Updater"
echo "═══════════════════════════════════════"
echo ""

if [ ! -d "${REMOTE_DIR}" ]; then
    echo "Error: VPSmon does not appear to be installed in ${REMOTE_DIR}."
    echo "Please use the install.sh script instead."
    exit 1
fi

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
    exit 1
fi

echo "==> Stopping service..."
systemctl stop "${APP_NAME}" || true

echo "==> Downloading latest binary..."
curl -sL "$LATEST_RELEASE_URL" -o "${REMOTE_DIR}/${APP_NAME}.tmp"
mv -f "${REMOTE_DIR}/${APP_NAME}.tmp" "${REMOTE_DIR}/${APP_NAME}"
chmod 755 "${REMOTE_DIR}/${APP_NAME}"
chown "${SERVICE_USER}:${SERVICE_GROUP}" "${REMOTE_DIR}/${APP_NAME}"

echo "==> Starting service..."
systemctl start "${APP_NAME}"

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
echo "  VPSmon updated successfully!"
echo "═══════════════════════════════════════"
