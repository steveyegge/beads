#!/bin/bash
#
# session-start.sh - Combined session initialization for AI agents
#
# This script chains together:
# 1. bd-version-check.sh - Detect upgrades, show what's new, update hooks
# 2. bd prime - Inject workflow context for the session
#
# The script is designed to be called by Claude Code's SessionStart hook.
# It handles all edge cases gracefully (missing dependencies, not in beads project, etc.)
#

# Get the directory where this script lives
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Navigate to repo root (two levels up from .claude-plugin/hooks/)
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Change to repo root for relative paths to work
cd "$REPO_ROOT" 2>/dev/null || true

# Step 1: Run version check (handles its own edge cases)
# This script will:
# - Exit silently if not in beads project
# - Exit silently if bd not installed
# - Show upgrade notification if version changed
# - Auto-update git hooks if outdated
# - Persist version to metadata.json
if [ -f "examples/startup-hooks/bd-version-check.sh" ]; then
  source "examples/startup-hooks/bd-version-check.sh"
fi

# Step 2: Run bd prime for workflow context
# bd prime outputs:
# - Core workflow rules
# - Session close protocol
# - Command reference
bd prime 2>/dev/null || true

# Clean exit
exit 0
