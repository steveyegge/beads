#!/bin/bash
# Dolt Performance Benchmark Script
# Usage: ./scripts/dolt-benchmark.sh [options]
#
# Options:
#   -i N    Number of iterations (default: 5)
#   -j      Output JSON format (for CI consumption)
#   -c FILE Compare with previous results file
#   -g      Run Go benchmarks instead of bd doctor
#   -q      Quick mode (fewer iterations, shorter benchtime)
#   -h      Show help
#
# Runs systematic performance benchmarks for Dolt backend.

set -e

# Defaults
ITERATIONS=5
JSON_OUTPUT=false
COMPARE_FILE=""
GO_BENCH=false
QUICK_MODE=false

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

usage() {
    echo "Dolt Performance Benchmark Script"
    echo ""
    echo "Usage: $0 [options]"
    echo ""
    echo "Options:"
    echo "  -i N    Number of iterations (default: 5)"
    echo "  -j      Output JSON format (for CI consumption)"
    echo "  -c FILE Compare with previous results file"
    echo "  -g      Run Go benchmarks instead of bd doctor"
    echo "  -q      Quick mode (fewer iterations, shorter benchtime)"
    echo "  -h      Show help"
    echo ""
    echo "Examples:"
    echo "  $0                    # Run standard benchmarks"
    echo "  $0 -j > results.json  # Output JSON for CI"
    echo "  $0 -c old.json        # Compare with previous run"
    echo "  $0 -g                 # Run Go benchmarks"
    echo "  $0 -q                 # Quick benchmarks"
}

while getopts "i:jc:gqh" opt; do
    case $opt in
        i) ITERATIONS=$OPTARG ;;
        j) JSON_OUTPUT=true ;;
        c) COMPARE_FILE=$OPTARG ;;
        g) GO_BENCH=true ;;
        q) QUICK_MODE=true; ITERATIONS=2 ;;
        h) usage; exit 0 ;;
        *) usage; exit 1 ;;
    esac
done

RESULTS_DIR="docs/reports/benchmark-results"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

# Create results directory
mkdir -p "${RESULTS_DIR}"

# ============================================================================
# Go Benchmarks Mode
# ============================================================================
if $GO_BENCH; then
    if ! command -v dolt >/dev/null 2>&1; then
        echo -e "${RED}Error: Dolt not installed${NC}" >&2
        exit 1
    fi

    BENCHTIME="1s"
    if $QUICK_MODE; then
        BENCHTIME="100ms"
    fi

    if $JSON_OUTPUT; then
        # Run benchmarks and convert to JSON
        echo "{"
        echo '  "timestamp": "'$(date -Iseconds)'",'
        echo '  "type": "go-benchmarks",'
        echo '  "benchtime": "'$BENCHTIME'",'
        echo '  "results": ['

        go test -bench=. -benchmem -benchtime=$BENCHTIME -run='^$' ./internal/storage/dolt/ 2>/dev/null | \
            grep -E '^Benchmark' | \
            awk 'BEGIN{first=1} {
                if (!first) print ","
                first=0
                name=$1
                gsub(/^Benchmark/, "", name)
                gsub(/-[0-9]+$/, "", name)
                ns_op=$3
                allocs=$5
                bytes=$7
                printf "    {\"name\": \"%s\", \"ns_op\": %s, \"allocs_op\": %s, \"bytes_op\": %s}", name, ns_op, allocs, bytes
            }'

        echo ""
        echo "  ]"
        echo "}"
    else
        echo -e "${GREEN}Running Go benchmarks...${NC}"
        echo ""
        go test -bench=. -benchmem -benchtime=$BENCHTIME -run='^$' ./internal/storage/dolt/
    fi
    exit 0
fi

# ============================================================================
# bd doctor Benchmarks Mode
# ============================================================================

# Check if bd doctor --perf-dolt is available
if ! bd doctor --help 2>&1 | grep -q "perf-dolt"; then
    echo -e "${RED}Error: bd doctor --perf-dolt not available. Update bd.${NC}" >&2
    exit 1
fi

# Arrays to store results
declare -a conn_times ready_times list_times show_times complex_times

run_iteration() {
    local output
    output=$(bd doctor --perf-dolt 2>&1)

    # Parse metrics
    local conn=$(echo "$output" | grep -oP 'Connection/Bootstrap:\s+\K\d+' || echo "0")
    local ready=$(echo "$output" | grep -oP 'bd ready.*:\s+\K\d+' || echo "0")
    local list=$(echo "$output" | grep -oP 'bd list.*:\s+\K\d+' || echo "0")
    local show=$(echo "$output" | grep -oP 'bd show.*:\s+\K\d+' || echo "0")
    local complex=$(echo "$output" | grep -oP 'Complex.*:\s+\K\d+' || echo "0")

    conn_times+=($conn)
    ready_times+=($ready)
    list_times+=($list)
    show_times+=($show)
    complex_times+=($complex)
}

calc_avg() {
    local arr=("$@")
    local sum=0
    local count=${#arr[@]}
    if [ $count -eq 0 ]; then
        echo "0"
        return
    fi
    for val in "${arr[@]}"; do
        sum=$((sum + val))
    done
    echo $((sum / count))
}

calc_stddev() {
    local arr=("$@")
    local count=${#arr[@]}
    if [ $count -lt 2 ]; then
        echo "0"
        return
    fi
    local avg=$(calc_avg "${arr[@]}")
    local sum_sq=0
    for val in "${arr[@]}"; do
        local diff=$((val - avg))
        sum_sq=$((sum_sq + diff * diff))
    done
    echo $(echo "scale=0; sqrt($sum_sq / ($count - 1))" | bc 2>/dev/null || echo "0")
}

# Run benchmarks
if ! $JSON_OUTPUT; then
    echo -e "${GREEN}=== Dolt Performance Benchmark ===${NC}"
    echo "Iterations: ${ITERATIONS}"
    echo "Timestamp: ${TIMESTAMP}"
    echo ""
fi

for i in $(seq 1 $ITERATIONS); do
    if ! $JSON_OUTPUT; then
        echo -e "  ${BLUE}Iteration $i/$ITERATIONS...${NC}"
    fi
    run_iteration
    sleep 1
done

# Calculate statistics
avg_conn=$(calc_avg "${conn_times[@]}")
avg_ready=$(calc_avg "${ready_times[@]}")
avg_list=$(calc_avg "${list_times[@]}")
avg_show=$(calc_avg "${show_times[@]}")
avg_complex=$(calc_avg "${complex_times[@]}")

stddev_conn=$(calc_stddev "${conn_times[@]}")
stddev_ready=$(calc_stddev "${ready_times[@]}")

# ============================================================================
# Output Results
# ============================================================================

if $JSON_OUTPUT; then
    cat << EOF
{
  "timestamp": "$(date -Iseconds)",
  "type": "bd-doctor",
  "iterations": $ITERATIONS,
  "host": "$(hostname)",
  "results": {
    "connection": {"avg": $avg_conn, "stddev": $stddev_conn, "unit": "ms"},
    "ready_work": {"avg": $avg_ready, "stddev": $stddev_ready, "unit": "ms"},
    "list_open": {"avg": $avg_list, "unit": "ms"},
    "show_issue": {"avg": $avg_show, "unit": "ms"},
    "complex_query": {"avg": $avg_complex, "unit": "ms"}
  },
  "raw": {
    "connection": [$(IFS=,; echo "${conn_times[*]}")],
    "ready_work": [$(IFS=,; echo "${ready_times[*]}")],
    "list_open": [$(IFS=,; echo "${list_times[*]}")],
    "show_issue": [$(IFS=,; echo "${show_times[*]}")],
    "complex_query": [$(IFS=,; echo "${complex_times[*]}")]
  }
}
EOF
else
    echo ""
    echo -e "${GREEN}Results:${NC}"
    echo "  Connection/Bootstrap: ${avg_conn}ms (±${stddev_conn})"
    echo "  Ready-work query:     ${avg_ready}ms (±${stddev_ready})"
    echo "  List open issues:     ${avg_list}ms"
    echo "  Show single issue:    ${avg_show}ms"
    echo "  Complex query:        ${avg_complex}ms"

    # Recommendations
    echo ""
    echo -e "${YELLOW}Recommendations:${NC}"
    if [ "$avg_conn" -gt 500 ]; then
        echo "  - High bootstrap time (${avg_conn}ms): Consider server mode"
    fi
    if [ "$avg_ready" -gt 200 ]; then
        echo "  - Slow ready-work (${avg_ready}ms): Check indexes"
    fi
    if [ "$avg_conn" -le 500 ] && [ "$avg_ready" -le 200 ]; then
        echo "  - Performance looks healthy"
    fi
fi

# ============================================================================
# Comparison Mode
# ============================================================================

if [ -n "$COMPARE_FILE" ] && [ -f "$COMPARE_FILE" ]; then
    if ! $JSON_OUTPUT; then
        echo ""
        echo -e "${BLUE}=== Comparison with $COMPARE_FILE ===${NC}"
    fi

    # Parse old results
    old_conn=$(jq -r '.results.connection.avg // 0' "$COMPARE_FILE" 2>/dev/null || echo "0")
    old_ready=$(jq -r '.results.ready_work.avg // 0' "$COMPARE_FILE" 2>/dev/null || echo "0")

    if [ "$old_conn" != "0" ] && [ "$old_ready" != "0" ]; then
        conn_change=$(echo "scale=1; (($avg_conn - $old_conn) / $old_conn) * 100" | bc 2>/dev/null || echo "N/A")
        ready_change=$(echo "scale=1; (($avg_ready - $old_ready) / $old_ready) * 100" | bc 2>/dev/null || echo "N/A")

        if $JSON_OUTPUT; then
            echo ""
            echo "Comparison:"
            echo "  Connection: ${old_conn}ms -> ${avg_conn}ms (${conn_change}%)"
            echo "  Ready-work: ${old_ready}ms -> ${avg_ready}ms (${ready_change}%)"
        else
            echo "  Connection: ${old_conn}ms -> ${avg_conn}ms (${conn_change}%)"
            echo "  Ready-work: ${old_ready}ms -> ${avg_ready}ms (${ready_change}%)"

            # Check for regression
            if [ "$(echo "$conn_change > 20" | bc 2>/dev/null)" = "1" ]; then
                echo -e "  ${RED}WARNING: Connection time regression >20%${NC}"
            fi
            if [ "$(echo "$ready_change > 20" | bc 2>/dev/null)" = "1" ]; then
                echo -e "  ${RED}WARNING: Ready-work regression >20%${NC}"
            fi
        fi
    else
        echo "  Could not parse comparison file"
    fi
fi

# Save results for future comparison
if ! $JSON_OUTPUT; then
    RESULTS_JSON="${RESULTS_DIR}/benchmark-${TIMESTAMP}.json"
    $0 -j -i $ITERATIONS > "$RESULTS_JSON" 2>/dev/null || true
    echo ""
    echo -e "${GREEN}Results saved to: ${RESULTS_JSON}${NC}"
fi
