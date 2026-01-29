#!/bin/bash
# uninstall-bd-daemon.sh - Remove bd daemon systemd user service for a workspace
#
# Usage:
#   ./uninstall-bd-daemon.sh /path/to/workspace
#   ./uninstall-bd-daemon.sh  # Uses current directory

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Get workspace path
WORKSPACE="${1:-$(pwd)}"
WORKSPACE=$(realpath "$WORKSPACE")

# Generate unit name
UNIT_NAME=$(systemd-escape --path "$WORKSPACE")
FULL_SERVICE="bd-daemon@${UNIT_NAME}.service"

echo -e "${YELLOW}Uninstalling:${NC} $FULL_SERVICE"

# Stop service if running
if systemctl --user is-active --quiet "$FULL_SERVICE" 2>/dev/null; then
    echo "Stopping service..."
    systemctl --user stop "$FULL_SERVICE"
fi

# Disable service
if systemctl --user is-enabled --quiet "$FULL_SERVICE" 2>/dev/null; then
    echo "Disabling service..."
    systemctl --user disable "$FULL_SERVICE"
fi

echo -e "${GREEN}Service uninstalled${NC}"
echo ""
echo "Note: The template unit file was not removed."
echo "To remove completely: rm ~/.config/systemd/user/bd-daemon@.service"
