#!/bin/bash
# Benchmark script for bd ready command
# Usage: ./scripts/benchmark-ready.sh [iterations] [mode]
# mode: "daemon" (default), "direct" (--no-daemon), or "both"

set -e

ITERATIONS=${1:-10}
MODE=${2:-both}
BD_CMD="${BD_CMD:-bd}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=============================================="
echo "bd ready Performance Benchmark"
echo "=============================================="
echo "Binary: $BD_CMD"
echo "Version: $($BD_CMD version 2>/dev/null | head -1)"
echo "Iterations: $ITERATIONS"
echo "Mode: $MODE"
echo ""

# Get database stats
BEADS_DIR="${BEADS_DIR:-.beads}"
if [[ -f "$BEADS_DIR/issues.jsonl" ]]; then
    ISSUE_COUNT=$(wc -l < "$BEADS_DIR/issues.jsonl" | tr -d ' ')
    echo "Issues in JSONL: $ISSUE_COUNT"
fi
if [[ -f "$BEADS_DIR/beads.db" ]]; then
    DB_SIZE=$(ls -lh "$BEADS_DIR/beads.db" | awk '{print $5}')
    echo "Database size: $DB_SIZE"
fi
echo ""

# Function to run benchmark and calculate stats
run_benchmark() {
    local label=$1
    local cmd=$2
    local times=()

    echo "Running: $label"
    echo "Command: $cmd"
    echo "---"

    # Warmup run (not counted)
    eval "$cmd" > /dev/null 2>&1 || true

    for i in $(seq 1 $ITERATIONS); do
        # Use GNU time for precise measurement (macOS: gtime, Linux: /usr/bin/time)
        if command -v gtime &> /dev/null; then
            TIME_CMD="gtime -f %e"
        else
            TIME_CMD="/usr/bin/time -p"
        fi

        # Capture real time
        start=$(python3 -c "import time; print(time.time())")
        eval "$cmd" > /dev/null 2>&1
        end=$(python3 -c "import time; print(time.time())")

        elapsed=$(python3 -c "print(f'{($end - $start) * 1000:.2f}')")
        times+=($elapsed)
        printf "  Run %2d: %8s ms\n" $i $elapsed
    done

    # Calculate statistics using Python
    # Convert bash array to comma-separated string
    times_str=$(IFS=,; echo "${times[*]}")
    python3 << PYTHON
import statistics
times = [$times_str]
times = [float(t) for t in times]

mean = statistics.mean(times)
stdev = statistics.stdev(times) if len(times) > 1 else 0
min_t = min(times)
max_t = max(times)
median = statistics.median(times)

print()
print(f"  {'─' * 30}")
print(f"  Mean:   {mean:8.2f} ms")
print(f"  Median: {median:8.2f} ms")
print(f"  Min:    {min_t:8.2f} ms")
print(f"  Max:    {max_t:8.2f} ms")
print(f"  StdDev: {stdev:8.2f} ms")
print()
PYTHON
}

# Run benchmarks based on mode
if [[ "$MODE" == "daemon" || "$MODE" == "both" ]]; then
    echo -e "${GREEN}=== Daemon Mode ===${NC}"
    run_benchmark "bd ready (daemon)" "$BD_CMD ready --limit 10 --json"
fi

if [[ "$MODE" == "direct" || "$MODE" == "both" ]]; then
    echo -e "${YELLOW}=== Direct Mode (--no-daemon) ===${NC}"
    run_benchmark "bd ready (direct)" "$BD_CMD --no-daemon ready --limit 10 --json"
fi

# Additional timing breakdown
echo -e "${GREEN}=== Timing Breakdown ===${NC}"
echo "Measuring individual components..."
echo ""

# Time just version (minimal overhead)
echo "1. Minimal overhead (bd version):"
for i in 1 2 3; do
    start=$(python3 -c "import time; print(time.time())")
    $BD_CMD version > /dev/null 2>&1
    end=$(python3 -c "import time; print(time.time())")
    elapsed=$(python3 -c "print(f'{($end - $start) * 1000:.2f}')")
    echo "   Run $i: ${elapsed}ms"
done
echo ""

# Time ready with --json vs human output
echo "2. JSON vs Human output:"
start=$(python3 -c "import time; print(time.time())")
$BD_CMD --no-daemon ready --limit 10 --json > /dev/null 2>&1
end=$(python3 -c "import time; print(time.time())")
json_time=$(python3 -c "print(f'{($end - $start) * 1000:.2f}')")
echo "   JSON output:  ${json_time}ms"

start=$(python3 -c "import time; print(time.time())")
$BD_CMD --no-daemon ready --limit 10 > /dev/null 2>&1
end=$(python3 -c "import time; print(time.time())")
human_time=$(python3 -c "print(f'{($end - $start) * 1000:.2f}')")
echo "   Human output: ${human_time}ms"
echo ""

# Time with different limits
echo "3. Scaling with limit:"
for limit in 1 10 50 100; do
    start=$(python3 -c "import time; print(time.time())")
    $BD_CMD --no-daemon ready --limit $limit --json > /dev/null 2>&1
    end=$(python3 -c "import time; print(time.time())")
    elapsed=$(python3 -c "print(f'{($end - $start) * 1000:.2f}')")
    echo "   --limit $limit: ${elapsed}ms"
done
echo ""

echo "=============================================="
echo "Benchmark complete"
echo "=============================================="
