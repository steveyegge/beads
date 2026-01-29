#!/bin/bash
# install-bd-daemon.sh - Install bd daemon as a systemd user service
#
# This script:
# 1. Installs the service unit file
# 2. Enables lingering (so daemon runs after logout)
# 3. Enables and starts the daemon for a specific workspace
#
# Usage:
#   ./install-bd-daemon.sh /path/to/workspace
#   ./install-bd-daemon.sh  # Uses current directory

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get workspace path (default to current directory)
WORKSPACE="${1:-$(pwd)}"

# Resolve to absolute path
WORKSPACE=$(realpath "$WORKSPACE")

# Verify workspace has .beads directory
if [ ! -d "$WORKSPACE/.beads" ]; then
    echo -e "${RED}Error: $WORKSPACE/.beads does not exist${NC}"
    echo "Run 'bd init' in the workspace first"
    exit 1
fi

# Find bd binary
BD_PATH=$(which bd 2>/dev/null || echo "/usr/local/bin/bd")
if [ ! -x "$BD_PATH" ]; then
    echo -e "${RED}Error: bd binary not found${NC}"
    exit 1
fi

echo -e "${GREEN}Installing bd daemon service for:${NC} $WORKSPACE"

# Step 1: Create systemd user directory
USER_SYSTEMD_DIR="$HOME/.config/systemd/user"
mkdir -p "$USER_SYSTEMD_DIR"

# Step 2: Copy service file (if not already present)
SERVICE_FILE="$USER_SYSTEMD_DIR/bd-daemon@.service"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ ! -f "$SERVICE_FILE" ]; then
    echo "Installing service template..."
    cp "$SCRIPT_DIR/bd-daemon@.service" "$SERVICE_FILE"

    # Update bd path in service file
    sed -i "s|/usr/local/bin/bd|$BD_PATH|g" "$SERVICE_FILE"
else
    echo "Service template already installed"
fi

# Step 3: Enable lingering (so daemon survives logout)
echo "Enabling user lingering..."
if ! loginctl show-user "$USER" 2>/dev/null | grep -q "Linger=yes"; then
    loginctl enable-linger
    echo -e "${GREEN}Lingering enabled${NC}"
else
    echo "Lingering already enabled"
fi

# Step 4: Reload systemd to pick up new unit
echo "Reloading systemd user daemon..."
systemctl --user daemon-reload

# Step 5: Generate unit name from workspace path
UNIT_NAME=$(systemd-escape --path "$WORKSPACE")
FULL_SERVICE="bd-daemon@${UNIT_NAME}.service"

echo -e "Service name: ${YELLOW}$FULL_SERVICE${NC}"

# Step 6: Stop any existing daemon in this workspace
echo "Stopping any existing bd daemon..."
cd "$WORKSPACE" && $BD_PATH daemon stop 2>/dev/null || true

# Step 7: Enable and start the service
echo "Enabling service..."
systemctl --user enable "$FULL_SERVICE"

echo "Starting service..."
systemctl --user start "$FULL_SERVICE"

# Step 8: Wait and verify
sleep 2
if systemctl --user is-active --quiet "$FULL_SERVICE"; then
    echo -e "${GREEN}bd daemon started successfully!${NC}"
    echo ""
    echo "Useful commands:"
    echo "  systemctl --user status $FULL_SERVICE  # Check status"
    echo "  journalctl --user -u $FULL_SERVICE -f  # Follow logs"
    echo "  systemctl --user restart $FULL_SERVICE # Restart after upgrade"
    echo "  systemctl --user stop $FULL_SERVICE    # Stop daemon"
else
    echo -e "${RED}Warning: Service may not have started correctly${NC}"
    echo "Check logs with: journalctl --user -u $FULL_SERVICE"
    systemctl --user status "$FULL_SERVICE" || true
fi
