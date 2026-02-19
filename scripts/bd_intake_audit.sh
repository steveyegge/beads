#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: bd_intake_audit.sh <epic-id> [extra intake-audit args...]

Runs the canonical intake contract audit and writes proof on success.

Environment:
  BD_BIN   Optional bd binary path (default: bd)
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

EPIC_ID="${1:-}"
if [[ -z "$EPIC_ID" ]]; then
  usage >&2
  exit 64
fi
shift || true

BD_BIN="${BD_BIN:-bd}"
if [[ "$BD_BIN" == */* ]]; then
  [[ -x "$BD_BIN" ]] || { echo "bd_intake_audit.sh: BD_BIN is not executable: $BD_BIN" >&2; exit 127; }
else
  command -v "$BD_BIN" >/dev/null 2>&1 || { echo "bd_intake_audit.sh: bd binary not found: $BD_BIN" >&2; exit 127; }
fi

exec "$BD_BIN" intake audit --epic "$EPIC_ID" --write-proof --json "$@"

