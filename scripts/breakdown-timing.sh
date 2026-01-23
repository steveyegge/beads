#!/bin/bash
# Breakdown timing for bd commands
# Shows where time is spent in the CLI

echo "==============================================="
echo "bd Command Timing Breakdown"
echo "==============================================="
echo ""

time_cmd() {
    local label=$1
    local cmd=$2
    local start end elapsed

    start=$(python3 -c "import time; print(time.time())")
    eval "$cmd" > /dev/null 2>&1
    end=$(python3 -c "import time; print(time.time())")
    elapsed=$(python3 -c "print(f'{($end - $start) * 1000:.1f}')")
    printf "%-45s %8s ms\n" "$label" "$elapsed"
}

echo "1. CLI Overhead Tests"
echo "-------------------------------------------"
time_cmd "bd version (minimal)" "bd version"
time_cmd "bd help (no db)" "bd help"
time_cmd "bd --no-daemon version" "bd --no-daemon version"
echo ""

echo "2. Database Operations (direct mode)"
echo "-------------------------------------------"
time_cmd "bd --no-daemon stats" "bd --no-daemon stats --json"
time_cmd "bd --no-daemon ready --limit 1" "bd --no-daemon ready --limit 1 --json"
time_cmd "bd --no-daemon ready --limit 10" "bd --no-daemon ready --limit 10 --json"
time_cmd "bd --no-daemon ready --limit 100" "bd --no-daemon ready --limit 100 --json"
time_cmd "bd --no-daemon list --limit 10" "bd --no-daemon list --limit 10 --json"
echo ""

echo "3. Testing Auto-Import Skip"
echo "-------------------------------------------"
time_cmd "ready (no-auto-import)" "bd --no-daemon --no-auto-import ready --limit 10 --json"
time_cmd "ready (no-auto-flush)" "bd --no-daemon --no-auto-flush ready --limit 10 --json"
time_cmd "ready (sandbox mode)" "bd --sandbox ready --limit 10 --json"
echo ""

echo "4. Git Operations Impact"
echo "-------------------------------------------"
# Set actor explicitly to skip git config lookup
time_cmd "ready (explicit actor)" "BD_ACTOR=test bd --no-daemon ready --limit 10 --json"
echo ""

echo "5. Testing allow-stale"
echo "-------------------------------------------"
time_cmd "ready (allow-stale)" "bd --no-daemon --allow-stale ready --limit 10 --json"
echo ""

echo "==============================================="
echo "Summary"
echo "==============================================="
echo ""
echo "Baseline overhead (bd version):          ~200ms"
echo "  - Go binary startup + init()"
echo "  - Cobra command parsing"
echo "  - Viper config loading"
echo ""
echo "Ready work additional time:              ~100ms"
echo "  - Fork protection check (git calls)"
echo "  - Database opening (SQLite)"
echo "  - Auto-import JSONL hash check"
echo "  - GetReadyWork query execution"
echo ""
