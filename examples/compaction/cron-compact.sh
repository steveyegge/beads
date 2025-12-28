#!/bin/bash
# Automated monthly compaction for cron
# Install: cp cron-compact.sh /etc/cron.monthly/bd-compact
#          chmod +x /etc/cron.monthly/bd-compact
#
# Or add to crontab:
#   0 2 1 * * /path/to/cron-compact.sh

# Configuration
REPO_PATH="${BD_REPO_PATH:-$HOME/your-project}"
LOG_FILE="${BD_LOG_FILE:-$HOME/.bd-compact.log}"
API_KEY="${ANTHROPIC_API_KEY}"

# Exit on error
set -e

# Logging helper
log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_FILE"
}

log "=== Starting BD Compaction ==="

# Check API key
if [ -z "$API_KEY" ]; then
  log "ERROR: ANTHROPIC_API_KEY not set"
  exit 1
fi

# Change to repo directory
if [ ! -d "$REPO_PATH" ]; then
  log "ERROR: Repository not found: $REPO_PATH"
  exit 1
fi

cd "$REPO_PATH"
log "Repository: $(pwd)"

# Check bd is installed
if ! command -v bd &> /dev/null; then
  log "ERROR: bd command not found"
  exit 1
fi

# Pull latest changes
log "Pulling latest changes..."
git pull origin main 2>&1 | tee -a "$LOG_FILE"

# Tier 1 compaction
log "Running Tier 1 compaction..."
TIER1_COUNT=$(bd admin compact --all --json 2>&1 | jq '. | length' || echo "0")
log "Compacted $TIER1_COUNT Tier 1 issues"

# Tier 2 compaction
log "Running Tier 2 compaction..."
TIER2_COUNT=$(bd admin compact --all --tier 2 --json 2>&1 | jq '. | length' || echo "0")
log "Compacted $TIER2_COUNT Tier 2 issues"

# Show statistics
log "Compaction statistics:"
bd admin compact --stats 2>&1 | tee -a "$LOG_FILE"

# Commit and push if changes exist
if git diff --quiet .beads/issues.jsonl issues.db 2>/dev/null; then
  log "No changes to commit"
else
  log "Committing compaction results..."
  git add .beads/issues.jsonl issues.db
  git commit -m "Automated compaction: $(date +%Y-%m-%d) - T1:$TIER1_COUNT T2:$TIER2_COUNT"
  git push origin main 2>&1 | tee -a "$LOG_FILE"
  log "Changes pushed to remote"
fi

log "=== Compaction Complete ==="
log "Total compacted: $((TIER1_COUNT + TIER2_COUNT)) issues"
