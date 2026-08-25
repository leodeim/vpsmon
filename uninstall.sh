#!/usr/bin/env bash
set -euo pipefail

# ─────────────────────────────────────────────
# VPSmon - Uninstall Script
# Usage:
# curl -sO https://raw.githubusercontent.com/leodeim/vpsmon/main/uninstall.sh
# sudo bash uninstall.sh
# ─────────────────────────────────────────────

APP_NAME="vpsmon"
REMOTE_DIR="/opt/vpsmon"
SERVICE_USER="vpsmon"
SERVICE_GROUP="vpsmon"

if [ "$EUID" -ne 0 ]; then
  echo "Please run this script as root or with sudo."
  exit 1
fi

echo "═══════════════════════════════════════"
echo "  VPSmon - Uninstaller"
echo "═══════════════════════════════════════"
echo ""

read -rp "Are you sure you want to completely remove VPSmon? [y/N] " CONFIRM </dev/tty
CONFIRM="${CONFIRM:-N}"
if [[ ! "$CONFIRM" =~ ^[Yy]$ ]]; then
    echo "Aborted."
    exit 0
fi

echo ""
echo "==> Stopping and disabling service..."
if systemctl is-active --quiet "${APP_NAME}" 2>/dev/null; then
    systemctl stop "${APP_NAME}" || true
fi
if systemctl is-enabled --quiet "${APP_NAME}" 2>/dev/null; then
    systemctl disable "${APP_NAME}" || true
fi

echo "==> Removing systemd service file..."
if [ -f "/etc/systemd/system/${APP_NAME}.service" ]; then
    rm -f "/etc/systemd/system/${APP_NAME}.service"
    systemctl daemon-reload
fi

echo "==> Removing application files..."
if [ -d "${REMOTE_DIR}" ]; then
    rm -rf "${REMOTE_DIR}"
fi

echo "==> Removing user and group..."
if id "${SERVICE_USER}" > /dev/null 2>&1; then
    userdel "${SERVICE_USER}" 2>/dev/null || true
fi
if getent group "${SERVICE_GROUP}" > /dev/null 2>&1; then
    groupdel "${SERVICE_GROUP}" 2>/dev/null || true
fi

echo ""
echo "═══════════════════════════════════════"
echo "  VPSmon has been successfully removed."
echo "═══════════════════════════════════════"
