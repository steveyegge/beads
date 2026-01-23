#!/bin/bash
# Dolt Performance Benchmark Script
# Usage: ./scripts/dolt-benchmark.sh [iterations]
#
# Runs systematic performance benchmarks for Dolt backend.
# Requires Dolt backend to be configured.

set -e

ITERATIONS=${1:-5}
RESULTS_DIR="docs/reports/benchmark-results"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
RESULTS_FILE="${RESULTS_DIR}/benchmark-${TIMESTAMP}.md"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=== Dolt Performance Benchmark ==="
echo "Iterations: ${ITERATIONS}"
echo "Timestamp: ${TIMESTAMP}"
echo ""

# Check if Dolt backend is configured
if ! bd doctor 2>&1 | grep -q "Backend: dolt"; then
    echo -e "${RED}Error: Not a Dolt backend. Configure with 'bd init --backend dolt'${NC}"
    exit 1
fi

# Create results directory
mkdir -p "${RESULTS_DIR}"

# Start results file
cat > "${RESULTS_FILE}" << EOF
# Dolt Performance Benchmark Results

**Date**: $(date -Iseconds)
**Host**: $(hostname)
**OS**: $(uname -s) $(uname -r)
**CPU**: $(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2 | xargs || echo "Unknown")
**RAM**: $(free -h 2>/dev/null | awk '/^Mem:/{print $2}' || echo "Unknown")

## Baseline Measurements (Embedded Mode)

| Iteration | Connection | Ready-work | List-open | Show-issue | Complex |
|-----------|------------|------------|-----------|------------|---------|
EOF

echo -e "${GREEN}Running embedded mode benchmarks...${NC}"

# Arrays to store results for averaging
declare -a conn_times ready_times list_times show_times complex_times

for i in $(seq 1 $ITERATIONS); do
    echo "  Iteration $i/$ITERATIONS..."

    # Run diagnostics and capture output
    output=$(bd doctor --perf-dolt 2>&1)

    # Parse metrics (adjust patterns based on actual output)
    conn=$(echo "$output" | grep -oP 'Connection/Bootstrap:\s+\K\d+' || echo "0")
    ready=$(echo "$output" | grep -oP 'bd ready.*:\s+\K\d+' || echo "0")
    list=$(echo "$output" | grep -oP 'bd list.*:\s+\K\d+' || echo "0")
    show=$(echo "$output" | grep -oP 'bd show.*:\s+\K\d+' || echo "0")
    complex=$(echo "$output" | grep -oP 'Complex.*:\s+\K\d+' || echo "0")

    # Store for averaging
    conn_times+=($conn)
    ready_times+=($ready)
    list_times+=($list)
    show_times+=($show)
    complex_times+=($complex)

    # Write to results file
    echo "| $i | ${conn}ms | ${ready}ms | ${list}ms | ${show}ms | ${complex}ms |" >> "${RESULTS_FILE}"

    # Brief pause between iterations
    sleep 1
done

# Calculate averages
calc_avg() {
    local arr=("$@")
    local sum=0
    for val in "${arr[@]}"; do
        sum=$((sum + val))
    done
    echo $((sum / ${#arr[@]}))
}

avg_conn=$(calc_avg "${conn_times[@]}")
avg_ready=$(calc_avg "${ready_times[@]}")
avg_list=$(calc_avg "${list_times[@]}")
avg_show=$(calc_avg "${show_times[@]}")
avg_complex=$(calc_avg "${complex_times[@]}")

cat >> "${RESULTS_FILE}" << EOF
| **Avg** | **${avg_conn}ms** | **${avg_ready}ms** | **${avg_list}ms** | **${avg_show}ms** | **${avg_complex}ms** |

EOF

# Check if server mode is available
if nc -z localhost 3306 2>/dev/null; then
    echo -e "${GREEN}Running server mode benchmarks...${NC}"

    cat >> "${RESULTS_FILE}" << EOF
## Server Mode Measurements

| Iteration | Connection | Ready-work | List-open | Show-issue | Complex |
|-----------|------------|------------|-----------|------------|---------|
EOF

    # Reset arrays
    conn_times=()
    ready_times=()
    list_times=()
    show_times=()
    complex_times=()

    # Set server mode env var
    export BEADS_DOLT_SERVER_MODE=1

    for i in $(seq 1 $ITERATIONS); do
        echo "  Iteration $i/$ITERATIONS..."

        output=$(bd doctor --perf-dolt 2>&1)

        conn=$(echo "$output" | grep -oP 'Connection/Bootstrap:\s+\K\d+' || echo "0")
        ready=$(echo "$output" | grep -oP 'bd ready.*:\s+\K\d+' || echo "0")
        list=$(echo "$output" | grep -oP 'bd list.*:\s+\K\d+' || echo "0")
        show=$(echo "$output" | grep -oP 'bd show.*:\s+\K\d+' || echo "0")
        complex=$(echo "$output" | grep -oP 'Complex.*:\s+\K\d+' || echo "0")

        conn_times+=($conn)
        ready_times+=($ready)
        list_times+=($list)
        show_times+=($show)
        complex_times+=($complex)

        echo "| $i | ${conn}ms | ${ready}ms | ${list}ms | ${show}ms | ${complex}ms |" >> "${RESULTS_FILE}"

        sleep 1
    done

    unset BEADS_DOLT_SERVER_MODE

    avg_conn_server=$(calc_avg "${conn_times[@]}")
    avg_ready_server=$(calc_avg "${ready_times[@]}")
    avg_list_server=$(calc_avg "${list_times[@]}")
    avg_show_server=$(calc_avg "${show_times[@]}")
    avg_complex_server=$(calc_avg "${complex_times[@]}")

    cat >> "${RESULTS_FILE}" << EOF
| **Avg** | **${avg_conn_server}ms** | **${avg_ready_server}ms** | **${avg_list_server}ms** | **${avg_show_server}ms** | **${avg_complex_server}ms** |

## Comparison Summary

| Metric | Embedded | Server | Speedup |
|--------|----------|--------|---------|
| Connection | ${avg_conn}ms | ${avg_conn_server}ms | $(echo "scale=1; $avg_conn / $avg_conn_server" | bc 2>/dev/null || echo "N/A")x |
| Ready-work | ${avg_ready}ms | ${avg_ready_server}ms | $(echo "scale=1; $avg_ready / $avg_ready_server" | bc 2>/dev/null || echo "N/A")x |
| List-open | ${avg_list}ms | ${avg_list_server}ms | $(echo "scale=1; $avg_list / $avg_list_server" | bc 2>/dev/null || echo "N/A")x |
| Show-issue | ${avg_show}ms | ${avg_show_server}ms | $(echo "scale=1; $avg_show / $avg_show_server" | bc 2>/dev/null || echo "N/A")x |
| Complex | ${avg_complex}ms | ${avg_complex_server}ms | $(echo "scale=1; $avg_complex / $avg_complex_server" | bc 2>/dev/null || echo "N/A")x |

EOF

else
    echo -e "${YELLOW}Server not running - skipping server mode benchmarks${NC}"
    echo -e "${YELLOW}To test server mode: dolt sql-server --data-dir .beads/dolt &${NC}"

    cat >> "${RESULTS_FILE}" << EOF
## Server Mode

Server not running during benchmark. To test server mode:
\`\`\`bash
dolt sql-server --data-dir .beads/dolt &
./scripts/dolt-benchmark.sh
\`\`\`

EOF
fi

# Add recommendations
cat >> "${RESULTS_FILE}" << EOF
## Recommendations

Based on benchmark results:

EOF

if [ "$avg_conn" -gt 500 ]; then
    echo "- **High bootstrap time in embedded mode** (${avg_conn}ms > 500ms): Consider using server mode" >> "${RESULTS_FILE}"
fi

if [ "$avg_ready" -gt 200 ]; then
    echo "- **Slow ready-work query** (${avg_ready}ms > 200ms): Review index on status column" >> "${RESULTS_FILE}"
fi

if [ "$avg_complex" -gt 500 ]; then
    echo "- **Slow complex queries** (${avg_complex}ms > 500ms): Review query patterns and indexes" >> "${RESULTS_FILE}"
fi

echo "" >> "${RESULTS_FILE}"
echo "---" >> "${RESULTS_FILE}"
echo "*Generated by dolt-benchmark.sh*" >> "${RESULTS_FILE}"

echo ""
echo -e "${GREEN}Benchmark complete!${NC}"
echo "Results saved to: ${RESULTS_FILE}"
