#!/usr/bin/env bash
# Manual integration tests for v0.59.0 deprecation warnings and v1.0.0 clean break.
#
# Run from outside the Gas Town tree (auto-start is disabled under crew/):
#   bash /path/to/beads/scripts/test-deprecation-manual.sh [bd-binary]
#
# If bd-binary is not specified, builds from source.

set -euo pipefail

BD="${1:-}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKDIR="$(mktemp -d /tmp/bd-deprecation-test.XXXXXX)"
PASS=0
FAIL=0

cleanup() {
  # Kill any dolt server we started
  if [[ -f "$WORKDIR/.beads/dolt-server.pid" ]]; then
    kill "$(cat "$WORKDIR/.beads/dolt-server.pid")" 2>/dev/null || true
  fi
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

if [[ -z "$BD" ]]; then
  echo "Building bd from source..."
  BD="$WORKDIR/bd"
  (cd "$REPO_ROOT" && go build -o "$BD" ./cmd/bd)
fi

VERSION=$("$BD" version 2>/dev/null | grep -oP '\d+\.\d+\.\d+' | head -1)
echo "Testing bd $VERSION"
echo "Workdir: $WORKDIR"
echo

pass() { echo "  PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL + 1)); }

# --- Setup: init a fresh workspace ---
cd "$WORKDIR"
git init -q
git config user.name "test"
git config user.email "test@test.test"
git commit --allow-empty -q -m "init"

export BEADS_NO_DAEMON=1
"$BD" init --prefix test -q 2>/dev/null

echo "=== Test 1: Clean config produces no deprecation warnings ==="
STDERR=$("$BD" list 2>&1 1>/dev/null || true)
if echo "$STDERR" | grep -qi "deprecated"; then
  fail "clean config should not produce deprecation warnings"
else
  pass "no deprecation warnings on clean config"
fi

echo "=== Test 2: dolt_mode:embedded triggers warning (v0.59.0 only) ==="
# Inject legacy field into metadata.json
METADATA=".beads/metadata.json"
python3 -c "
import json, sys
with open('$METADATA') as f: cfg = json.load(f)
cfg['dolt_mode'] = 'embedded'
with open('$METADATA', 'w') as f: json.dump(cfg, f, indent=2)
"
STDERR=$("$BD" list 2>&1 1>/dev/null || true)
if [[ "$VERSION" == 0.59.* ]]; then
  if echo "$STDERR" | grep -qi "deprecated\|embedded.*deprecated"; then
    pass "v0.59.0 shows deprecation warning for embedded mode"
  else
    fail "v0.59.0 should show deprecation warning for embedded mode"
  fi
elif [[ "$VERSION" == 1.* ]]; then
  # v1.0.0 removed the deprecation system — field is silently ignored
  if echo "$STDERR" | grep -qi "deprecated"; then
    fail "v1.0.0 should NOT show deprecation warnings (system removed)"
  else
    pass "v1.0.0 silently ignores dolt_mode:embedded"
  fi
fi

echo "=== Test 3: --json mode suppresses warnings ==="
JSON_OUT=$("$BD" list --json 2>/dev/null || true)
JSON_ERR=$("$BD" list --json 2>&1 1>/dev/null || true)
if echo "$JSON_ERR" | grep -qi "deprecated"; then
  fail "--json should suppress deprecation warnings on stderr"
else
  pass "--json suppresses warnings"
fi

echo "=== Test 4: bd doctor runs without error ==="
DOCTOR_OUT=$("$BD" doctor "$WORKDIR" 2>&1 || true)
if echo "$DOCTOR_OUT" | grep -qi "panic\|fatal\|segfault"; then
  fail "bd doctor crashed"
else
  pass "bd doctor runs cleanly"
fi
if [[ "$VERSION" == 0.59.* ]]; then
  if echo "$DOCTOR_OUT" | grep -qi "deprecated\|embedded.*deprecated"; then
    pass "bd doctor shows deprecation checks (v0.59.0)"
  else
    fail "bd doctor should show deprecation checks in v0.59.0"
  fi
fi

echo "=== Test 5: Old metadata.json with legacy fields loads without error ==="
python3 -c "
import json
with open('$METADATA') as f: cfg = json.load(f)
cfg['backend'] = 'sqlite'
cfg['dolt_mode'] = 'embedded'
with open('$METADATA', 'w') as f: json.dump(cfg, f, indent=2)
"
# bd list should not crash
if "$BD" list 2>/dev/null; then
  pass "old metadata.json with backend:sqlite + dolt_mode:embedded loads OK"
else
  fail "old metadata.json with legacy fields caused an error"
fi

echo "=== Test 6: bd init creates clean config ==="
NEWDIR="$(mktemp -d "$WORKDIR/fresh.XXXXXX")"
cd "$NEWDIR"
git init -q
git config user.name "test"
git config user.email "test@test.test"
git commit --allow-empty -q -m "init"
"$BD" init --prefix fresh -q 2>/dev/null
NEWMETA="$NEWDIR/.beads/metadata.json"
if grep -q '"backend"' "$NEWMETA" 2>/dev/null; then
  fail "bd init should not write backend field"
elif grep -q '"dolt_mode"' "$NEWMETA" 2>/dev/null; then
  fail "bd init should not write dolt_mode field"
else
  pass "bd init creates clean config without legacy fields"
fi

echo "=== Test 7: BEADS_DOLT_SERVER_MODE env var is harmless ==="
cd "$WORKDIR"
# Restore clean metadata
python3 -c "
import json
with open('$METADATA') as f: cfg = json.load(f)
cfg.pop('backend', None)
cfg.pop('dolt_mode', None)
with open('$METADATA', 'w') as f: json.dump(cfg, f, indent=2)
"
BEADS_DOLT_SERVER_MODE=1 "$BD" list 2>/dev/null
if [[ $? -eq 0 ]]; then
  pass "BEADS_DOLT_SERVER_MODE=1 does not cause errors"
else
  fail "BEADS_DOLT_SERVER_MODE=1 caused an error"
fi

echo
echo "==============================="
echo "Results: $PASS passed, $FAIL failed"
echo "==============================="
[[ $FAIL -eq 0 ]]
