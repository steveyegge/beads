#!/bin/bash
# Stop hook: creates a decision point and blocks until the human responds.
# Uses SSE-based `bd watch --decision` instead of the old event bus handler.
#
# Install: add to ~/.claude/settings.json hooks.Stop
#   "hooks": { "Stop": [{ "type": "command", "command": "/path/to/stop-decision.sh" }] }
#
# Exit codes:
#   0 = allow stop
#   2 = block stop (human chose "continue")
#   other = allow stop (fail-open)

set -euo pipefail

# Create decision and capture ID
id=$(bd decision create \
  --prompt="Claude is ready to stop. Review and decide:" \
  --options='[{"id":"continue","short":"continue","label":"Continue working"},{"id":"stop","short":"stop","label":"Allow Claude to stop"}]' \
  --requested-by=stop-hook \
  --urgency=high \
  --json 2>/dev/null | jq -r .id)

if [ -z "$id" ] || [ "$id" = "null" ]; then
  # Creation failed — fail-open (allow stop)
  exit 0
fi

# Watch for response (blocks until human responds or timeout)
result=$(bd watch --decision="$id" --timeout=30m --json 2>/dev/null) || true
selected=$(echo "$result" | jq -r '.selected // empty' 2>/dev/null) || true

# "continue" = block stop, anything else = allow
if [ "$selected" = "continue" ]; then
  reason=$(echo "$result" | jq -r '.reason // "Human selected continue"' 2>/dev/null) || reason="Human selected continue"
  echo "{\"decision\":\"block\",\"reason\":\"$reason\"}" >&2
  exit 2
fi

exit 0
