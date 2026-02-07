#!/bin/bash
# check_dolt_imports.sh - Prevent direct dolt imports from cmd/bd/ (bd-ma0s.8)
#
# Validates that cmd/bd/ Go files do not import internal/storage/dolt
# except for the 5 legitimate exceptions that require direct access.
#
# Usage: scripts/check_dolt_imports.sh [--ci]
#   --ci: Exit with non-zero status on violations (for CI pipelines)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Legitimate exceptions that are allowed to import internal/storage/dolt.
# These files require direct Dolt access for daemon internals, initialization,
# migration, or diagnostics.
ALLOWED_FILES=(
  "cmd/bd/daemon_event_loop.go"
  "cmd/bd/dolt_server_cgo.go"
  "cmd/bd/init.go"
  "cmd/bd/migrate_dolt.go"
  "cmd/bd/doctor/federation.go"
)

ci_mode=false
if [[ "${1:-}" == "--ci" ]]; then
  ci_mode=true
fi

# Find all Go files in cmd/bd/ that import internal/storage/dolt
violations=()
while IFS= read -r file; do
  # Make path relative to repo root
  relpath="${file#"$REPO_ROOT/"}"

  # Check if this file is in the allowed list
  allowed=false
  for allowed_file in "${ALLOWED_FILES[@]}"; do
    if [[ "$relpath" == "$allowed_file" ]]; then
      allowed=true
      break
    fi
  done

  if [[ "$allowed" == "false" ]]; then
    violations+=("$relpath")
  fi
done < <(grep -rl 'internal/storage/dolt' "$REPO_ROOT/cmd/bd/" --include='*.go' 2>/dev/null || true)

if [[ ${#violations[@]} -eq 0 ]]; then
  echo "✓ No unauthorized dolt imports in cmd/bd/"
  echo "  Allowed exceptions: ${#ALLOWED_FILES[@]} files"
  exit 0
else
  echo "✗ Found unauthorized dolt imports in cmd/bd/:"
  for v in "${violations[@]}"; do
    echo "  - $v"
  done
  echo ""
  echo "cmd/bd/ commands should use daemon RPC instead of direct dolt access."
  echo "Allowed exceptions: ${ALLOWED_FILES[*]}"
  if $ci_mode; then
    exit 1
  fi
  exit 0
fi
