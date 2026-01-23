#!/bin/bash
# Clean benchmark using 10K synthetic database
set -e

DB="/tmp/beads-bench-cache/large.db"
ITERATIONS=10

echo "=============================================="
echo "Clean Baseline Benchmark (10K synthetic DB)"
echo "=============================================="
echo "Database: $DB ($(ls -lh $DB | awk '{print $5}'))"
echo "Iterations: $ITERATIONS"
echo ""

time_cmd() {
    local label=$1
    shift
    local times=()

    echo "$label"
    echo "---"

    # Warmup
    "$@" > /dev/null 2>&1 || true

    for i in $(seq 1 $ITERATIONS); do
        start=$(python3 -c "import time; print(time.time())")
        "$@" > /dev/null 2>&1
        end=$(python3 -c "import time; print(time.time())")
        elapsed=$(python3 -c "print(f'{($end - $start) * 1000:.2f}')")
        times+=($elapsed)
        printf "  Run %2d: %8s ms\n" $i $elapsed
    done

    times_str=$(IFS=,; echo "${times[*]}")
    python3 << PYTHON
import statistics
times = [$times_str]
mean = statistics.mean(times)
stdev = statistics.stdev(times) if len(times) > 1 else 0
print(f"\n  Mean: {mean:.1f}ms  StdDev: {stdev:.1f}ms  Min: {min(times):.1f}ms  Max: {max(times):.1f}ms\n")
PYTHON
}

echo "=== Direct Mode (--no-daemon, 10K issues) ==="
time_cmd "bd ready --limit 10" ./bd --db $DB --no-daemon --no-auto-import ready --limit 10 --json

echo "=== Minimal CLI Overhead ==="
time_cmd "bd version" ./bd version

echo "=== Direct Mode Breakdown ==="
time_cmd "bd ready --limit 1" ./bd --db $DB --no-daemon --no-auto-import ready --limit 1 --json
time_cmd "bd ready --limit 100" ./bd --db $DB --no-daemon --no-auto-import ready --limit 100 --json
time_cmd "bd stats" ./bd --db $DB --no-daemon --no-auto-import stats --json

echo "=============================================="
echo "Summary"
echo "=============================================="
