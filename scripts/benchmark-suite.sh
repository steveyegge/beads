#!/usr/bin/env bash
# Comprehensive Benchmark Suite for Beads
# Usage: ./scripts/benchmark-suite.sh [--synthetic|--real|--both] [--output FILE]
#
# Measures:
# - Latency (P50, P95, P99, mean, stddev)
# - Throughput (ops/sec)
# - Daemon vs Direct mode comparison

set -e

# Configuration
ITERATIONS=${ITERATIONS:-20}
SYNTHETIC_DB="/tmp/beads-bench-cache/large.db"
REAL_DB=".beads/beads.db"
BD_CMD="${BD_CMD:-./bd}"
OUTPUT_FILE=""
MODE="both"  # synthetic, real, both
RESULTS_FILE=$(mktemp)

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --synthetic) MODE="synthetic"; shift ;;
        --real) MODE="real"; shift ;;
        --both) MODE="both"; shift ;;
        --output) OUTPUT_FILE="$2"; shift 2 ;;
        --iterations) ITERATIONS="$2"; shift 2 ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Store result in temp file
store_result() {
    local key="$1"
    local value="$2"
    echo "$key=$value" >> "$RESULTS_FILE"
}

# Get system info as JSON
get_system_info() {
    python3 << 'PYTHON'
import subprocess
import json
import platform
import os

def run_cmd(cmd):
    try:
        return subprocess.check_output(cmd, shell=True, stderr=subprocess.DEVNULL).decode().strip()
    except:
        return "unknown"

info = {
    "timestamp": run_cmd("date -u +%Y-%m-%dT%H:%M:%SZ"),
    "hostname": platform.node(),
    "os": platform.system(),
    "os_version": platform.release(),
    "arch": platform.machine(),
    "cpu": run_cmd("sysctl -n machdep.cpu.brand_string") if platform.system() == "Darwin" else run_cmd("cat /proc/cpuinfo | grep 'model name' | head -1 | cut -d: -f2"),
    "cpu_cores": os.cpu_count() or 0,
    "bd_version": run_cmd("./bd version 2>/dev/null | head -1")
}

# Get memory
try:
    if platform.system() == "Darwin":
        mem = int(run_cmd("sysctl -n hw.memsize"))
        info["memory_gb"] = round(mem / (1024**3), 1)
    else:
        info["memory_gb"] = 0
except:
    info["memory_gb"] = 0

print(json.dumps(info))
PYTHON
}

# Run a benchmark and collect timing data
run_benchmark() {
    local name="$1"
    local cmd="$2"
    local iterations="${3:-$ITERATIONS}"

    echo -e "${BLUE}Benchmarking:${NC} $name"
    echo -e "${YELLOW}Command:${NC} $cmd"
    echo -e "${YELLOW}Iterations:${NC} $iterations"

    # Use Python for reliable timing and stats
    python3 << PYTHON
import subprocess
import time
import json
import statistics

name = "$name"
cmd = "$cmd"
iterations = $iterations
times = []

# Warmup (2 runs)
for _ in range(2):
    try:
        subprocess.run(cmd, shell=True, capture_output=True, timeout=30)
    except:
        pass

# Timed runs
for i in range(iterations):
    start = time.time()
    try:
        result = subprocess.run(cmd, shell=True, capture_output=True, timeout=30)
        if result.returncode == 0:
            elapsed = (time.time() - start) * 1000
            times.append(elapsed)
            print(f"  [{i+1:2d}/{iterations}] {elapsed:8.2f} ms")
        else:
            print(f"  [{i+1:2d}/{iterations}] FAILED")
    except Exception as e:
        print(f"  [{i+1:2d}/{iterations}] ERROR: {e}")

# Calculate stats
if times:
    times_sorted = sorted(times)
    n = len(times_sorted)

    def percentile(data, p):
        k = (len(data) - 1) * p / 100
        f = int(k)
        c = min(f + 1, len(data) - 1)
        return data[f] + (k - f) * (data[c] - data[f])

    stats = {
        "count": n,
        "mean": round(statistics.mean(times), 2),
        "stddev": round(statistics.stdev(times), 2) if n > 1 else 0,
        "min": round(min(times), 2),
        "max": round(max(times), 2),
        "p50": round(percentile(times_sorted, 50), 2),
        "p95": round(percentile(times_sorted, 95), 2),
        "p99": round(percentile(times_sorted, 99), 2)
    }
    print(f"\033[0;32mResults:\033[0m {json.dumps(stats)}")

    # Write to results file
    with open("$RESULTS_FILE", "a") as f:
        f.write(f"{name}={json.dumps(stats)}\n")
else:
    print("\033[0;31mNo successful runs\033[0m")
    with open("$RESULTS_FILE", "a") as f:
        f.write(f'{name}={{"error": "all runs failed"}}\n')
PYTHON
    echo ""
}

# Measure throughput (ops per second)
measure_throughput() {
    local name="$1"
    local cmd="$2"
    local duration="${3:-10}"

    echo -e "${BLUE}Throughput Test:${NC} $name"
    echo -e "${YELLOW}Duration:${NC} ${duration}s"

    python3 << PYTHON
import subprocess
import time
import json

cmd = "$cmd"
duration = $duration
name = "throughput_$name"

count = 0
start = time.time()
end_time = start + duration

while time.time() < end_time:
    try:
        result = subprocess.run(cmd, shell=True, capture_output=True, timeout=10)
        if result.returncode == 0:
            count += 1
    except:
        pass

actual_duration = round(time.time() - start, 2)
ops_per_sec = round(count / actual_duration, 2) if actual_duration > 0 else 0

print(f"\033[0;32mResults:\033[0m {count} ops in {actual_duration}s = {ops_per_sec} ops/sec")

result = {"ops": count, "duration_sec": actual_duration, "ops_per_sec": ops_per_sec}
with open("$RESULTS_FILE", "a") as f:
    f.write(f"{name}={json.dumps(result)}\n")
PYTHON
    echo ""
}

# Run benchmarks for a specific database
benchmark_database() {
    local db_path="$1"
    local db_name="$2"
    local db_flags=""

    if [ "$db_path" != "default" ]; then
        db_flags="--db $db_path"
    fi

    echo -e "\n${GREEN}========================================${NC}"
    echo -e "${GREEN}Benchmarking: $db_name${NC}"
    echo -e "${GREEN}Database: $db_path${NC}"
    echo -e "${GREEN}========================================${NC}\n"

    # Check database exists and get size
    if [ -f "$db_path" ]; then
        local db_size=$(ls -lh "$db_path" | awk '{print $5}')
        echo -e "Database size: $db_size"

        # Get issue count
        local issue_count=$($BD_CMD $db_flags --no-daemon --no-auto-import stats --json 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('summary',{}).get('total_issues',0))" 2>/dev/null || echo "0")
        echo -e "Issue count: $issue_count"
        store_result "${db_name}_info" "{\"path\": \"$db_path\", \"size\": \"$db_size\", \"issues\": $issue_count}"
    else
        echo -e "${RED}Database not found: $db_path${NC}"
        return 1
    fi
    echo ""

    # Performance benchmarks - Direct Mode
    echo -e "${YELLOW}=== Direct Mode (--no-daemon) ===${NC}\n"

    run_benchmark "${db_name}_direct_ready_10" \
        "$BD_CMD $db_flags --no-daemon --no-auto-import ready --limit 10 --json"

    run_benchmark "${db_name}_direct_ready_100" \
        "$BD_CMD $db_flags --no-daemon --no-auto-import ready --limit 100 --json"

    run_benchmark "${db_name}_direct_stats" \
        "$BD_CMD $db_flags --no-daemon --no-auto-import stats --json"

    run_benchmark "${db_name}_direct_list_10" \
        "$BD_CMD $db_flags --no-daemon --no-auto-import list --limit 10 --json"

    # CLI overhead baseline
    run_benchmark "${db_name}_version" \
        "$BD_CMD version"

    # Throughput test (5 seconds)
    measure_throughput "${db_name}_direct_ready" \
        "$BD_CMD $db_flags --no-daemon --no-auto-import ready --limit 10 --json" 5
}

# Export results to JSON
export_results() {
    local sys_info=$(get_system_info)
    python3 << PYTHON
import json
import sys

# Read results file
results = {}
try:
    with open("$RESULTS_FILE", "r") as f:
        for line in f:
            line = line.strip()
            if "=" in line:
                key, value = line.split("=", 1)
                try:
                    results[key] = json.loads(value)
                except:
                    results[key] = value
except:
    pass

output = {
    "system": $sys_info,
    "config": {
        "iterations": $ITERATIONS,
        "mode": "$MODE"
    },
    "results": results
}

print(json.dumps(output, indent=2))
PYTHON
}

# Main execution
main() {
    echo -e "${GREEN}================================================${NC}"
    echo -e "${GREEN}    Beads Comprehensive Benchmark Suite${NC}"
    echo -e "${GREEN}================================================${NC}"
    echo ""
    echo "Mode: $MODE"
    echo "Iterations: $ITERATIONS"
    echo "BD Command: $BD_CMD"
    echo ""

    # Clear results file
    > "$RESULTS_FILE"

    # Run benchmarks based on mode
    if [ "$MODE" = "synthetic" ] || [ "$MODE" = "both" ]; then
        if [ -f "$SYNTHETIC_DB" ]; then
            benchmark_database "$SYNTHETIC_DB" "synthetic_10k"
        else
            echo -e "${RED}Synthetic database not found. Run 'make bench' first to generate it.${NC}"
        fi
    fi

    if [ "$MODE" = "real" ] || [ "$MODE" = "both" ]; then
        if [ -f "$REAL_DB" ]; then
            benchmark_database "$REAL_DB" "real"
        else
            echo -e "${YELLOW}Real database not found at $REAL_DB${NC}"
        fi
    fi

    # Export results
    echo -e "\n${GREEN}================================================${NC}"
    echo -e "${GREEN}    Results Summary${NC}"
    echo -e "${GREEN}================================================${NC}\n"

    if [ -n "$OUTPUT_FILE" ]; then
        export_results > "$OUTPUT_FILE"
        echo -e "Results saved to: ${GREEN}$OUTPUT_FILE${NC}"
        echo ""
        cat "$OUTPUT_FILE"
    else
        export_results
    fi

    # Cleanup
    rm -f "$RESULTS_FILE"
}

main
