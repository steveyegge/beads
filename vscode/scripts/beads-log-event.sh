#!/bin/bash
# beads-log-event.sh - Central event logging utility for Beads-First applications
#
# Usage: beads-log-event.sh EVENT_CODE [ISSUE_ID] [DETAILS]
#
# Event codes follow the taxonomy defined in events/EVENT_TAXONOMY.md
#
# Examples:
#   beads-log-event.sh sk.bootup.activated
#   beads-log-event.sh bd.issue.create bd-0001 "InitApp epic created"
#   beads-log-event.sh hk.pre-commit.start none "hook triggered"

set -euo pipefail

# Arguments
EVENT_CODE="${1:?Event code required}"
ISSUE_ID="${2:-none}"
DETAILS="${3:-}"

# Environment
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
AGENT_ID="${BEADS_AGENT_ID:-${USER:-unknown}}"
SESSION_ID="${BEADS_SESSION_ID:-$(date +%s)}"

# Find project root (look for .beads directory)
find_project_root() {
    local dir="$PWD"
    while [[ "$dir" != "/" ]]; do
        if [[ -d "$dir/.beads" ]]; then
            echo "$dir"
            return 0
        fi
        dir="$(dirname "$dir")"
    done
    echo "$PWD"  # Fallback to current directory
}

PROJECT_ROOT=$(find_project_root)
LOG_DIR="$PROJECT_ROOT/.beads"
LOG_FILE="$LOG_DIR/events.log"

# Ensure log directory exists
mkdir -p "$LOG_DIR"

# Format: TIMESTAMP|EVENT_CODE|ISSUE_ID|AGENT_ID|SESSION_ID|DETAILS
LOG_ENTRY="${TIMESTAMP}|${EVENT_CODE}|${ISSUE_ID}|${AGENT_ID}|${SESSION_ID}|${DETAILS}"

# Append to log file
echo "$LOG_ENTRY" >> "$LOG_FILE"

# Echo for visibility (can be suppressed with BEADS_QUIET=1)
if [[ "${BEADS_QUIET:-0}" != "1" ]]; then
    echo "[BEADS EVENT] ${EVENT_CODE} | ${ISSUE_ID} | ${DETAILS}"
fi

# Return success
exit 0
