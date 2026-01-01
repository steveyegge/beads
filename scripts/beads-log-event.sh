#!/bin/bash
# beads-log-event.sh - Log events to .beads/events.log
# Usage: ./scripts/beads-log-event.sh <event_code> [issue_id] [description]
#
# Event format: timestamp|event_code|issue_id|user|unix_timestamp|description

set -e

EVENT_CODE="${1:?Event code required}"
ISSUE_ID="${2:-none}"
DESCRIPTION="${3:-}"

# Ensure .beads directory exists
if [ ! -d ".beads" ]; then
    echo "Error: .beads directory not found" >&2
    exit 1
fi

# Get timestamp in ISO 8601 format
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
UNIX_TS=$(date +%s)

# Get user (use git config or fallback to unknown)
USER=$(git config user.name 2>/dev/null || echo "unknown")

# Escape pipe characters in description
DESCRIPTION_SAFE=$(echo "$DESCRIPTION" | tr '|' '-')

# Append to events log
echo "${TIMESTAMP}|${EVENT_CODE}|${ISSUE_ID}|${USER}|${UNIX_TS}|${DESCRIPTION_SAFE}" >> .beads/events.log

echo "Logged: ${EVENT_CODE} ${ISSUE_ID:+($ISSUE_ID)}"
