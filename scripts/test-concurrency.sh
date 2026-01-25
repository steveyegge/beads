#!/bin/bash
# Concurrency Stress Test for Beads
# Tests concurrent writes to detect:
# - SQLite lock errors
# - JSONL corruption
# - Data loss / conflicts
#
# Usage: ./scripts/test-concurrency.sh [--parallel N] [--iterations N] [--db PATH]

set -e

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

# Configuration
PARALLEL=${PARALLEL:-5}
ITERATIONS=${ITERATIONS:-10}
BD_CMD="${BD_CMD:-$REPO_ROOT/bd}"
TEST_DB=""
USE_TEMP_DB=true
CLEANUP=true

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --parallel) PARALLEL="$2"; shift 2 ;;
        --iterations) ITERATIONS="$2"; shift 2 ;;
        --db) TEST_DB="$2"; USE_TEMP_DB=false; shift 2 ;;
        --no-cleanup) CLEANUP=false; shift ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Setup test environment
setup_test_env() {
    if [ "$USE_TEMP_DB" = true ]; then
        TEST_DIR=$(mktemp -d)
        TEST_DB="$TEST_DIR/test.db"
        TEST_BEADS_DIR="$TEST_DIR/.beads"
        mkdir -p "$TEST_BEADS_DIR"

        echo -e "${BLUE}Setting up test environment...${NC}"
        echo "Test directory: $TEST_DIR"

        # Initialize a fresh database
        pushd "$TEST_DIR" > /dev/null
        git init -q
        git config user.email "test@test.com"
        git config user.name "Test User"
        $BD_CMD init -p "test" -q 2>/dev/null || true
        popd > /dev/null

        echo -e "${GREEN}Test environment ready${NC}"
    else
        TEST_DIR=$(dirname "$TEST_DB")
        TEST_BEADS_DIR=$(dirname "$TEST_DB")
    fi
}

# Cleanup test environment
cleanup_test_env() {
    if [ "$USE_TEMP_DB" = true ] && [ "$CLEANUP" = true ] && [ -n "$TEST_DIR" ]; then
        echo -e "${BLUE}Cleaning up test environment...${NC}"
        rm -rf "$TEST_DIR"
    fi
}

trap cleanup_test_env EXIT

# Run concurrent creates
test_concurrent_creates() {
    local parallel=$1
    local iterations=$2

    echo -e "\n${YELLOW}========================================${NC}"
    echo -e "${YELLOW}Concurrent Create Test${NC}"
    echo -e "${YELLOW}Parallel: $parallel, Iterations: $iterations${NC}"
    echo -e "${YELLOW}========================================${NC}\n"

    local results_dir=$(mktemp -d)
    local total_ops=$((parallel * iterations))
    local start_time=$(python3 -c "import time; print(time.time())")

    echo -e "${BLUE}Spawning $parallel parallel workers, $iterations iterations each...${NC}"

    # Spawn parallel workers
    for worker in $(seq 1 $parallel); do
        (
            cd "$TEST_DIR"
            for i in $(seq 1 $iterations); do
                title="Concurrent test issue W${worker}-I${i}"
                if "$BD_CMD" --no-daemon create --title "$title" --type task -q 2>"$results_dir/err_${worker}_${i}.txt"; then
                    echo "success" > "$results_dir/result_${worker}_${i}.txt"
                else
                    echo "failed" > "$results_dir/result_${worker}_${i}.txt"
                fi
            done
        ) &
    done

    # Wait for all workers
    wait

    local end_time=$(python3 -c "import time; print(time.time())")
    local duration=$(python3 -c "print(round($end_time - $start_time, 2))")

    # Count results
    local success_count=$(find "$results_dir" -name "result_*.txt" -exec cat {} \; | grep -c "success" || echo 0)
    local failed_count=$(find "$results_dir" -name "result_*.txt" -exec cat {} \; | grep -c "failed" || echo 0)

    # Check for lock errors
    local lock_errors=$(find "$results_dir" -name "err_*.txt" -exec cat {} \; | grep -c "database is locked\|SQLITE_BUSY" || echo 0)

    # Analyze error types
    echo -e "\n${BLUE}Error Analysis:${NC}"
    local error_types=$(find "$results_dir" -name "err_*.txt" -exec cat {} \; | sort | uniq -c | sort -rn | head -10)
    if [ -n "$error_types" ]; then
        echo "$error_types"
    else
        echo "No errors"
    fi

    # Cleanup results dir
    rm -rf "$results_dir"

    # Verify database integrity
    echo -e "\n${BLUE}Database Integrity Check:${NC}"
    cd "$TEST_DIR"
    local db_issue_count=$($BD_CMD --no-daemon stats --json 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('summary',{}).get('total_issues',0))" 2>/dev/null || echo "error")

    # Calculate metrics
    local ops_per_sec=$(python3 -c "print(round($total_ops / $duration, 2))")
    local success_rate=$(python3 -c "print(round($success_count / $total_ops * 100, 1))")
    local conflict_rate=$(python3 -c "print(round($failed_count / $total_ops * 100, 1))")

    # Print results
    echo -e "\n${GREEN}========================================${NC}"
    echo -e "${GREEN}Results${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo -e "Total operations:    $total_ops"
    echo -e "Successful:          ${GREEN}$success_count${NC}"
    echo -e "Failed:              ${RED}$failed_count${NC}"
    echo -e "Lock errors:         ${YELLOW}$lock_errors${NC}"
    echo -e "Duration:            ${duration}s"
    echo -e "Throughput:          ${ops_per_sec} ops/sec"
    echo -e "Success rate:        ${success_rate}%"
    echo -e "Conflict rate:       ${conflict_rate}%"
    echo -e "Issues in DB:        $db_issue_count"
    echo ""

    # Return JSON result
    echo "{\"parallel\": $parallel, \"iterations\": $iterations, \"total_ops\": $total_ops, \"success\": $success_count, \"failed\": $failed_count, \"lock_errors\": $lock_errors, \"duration_sec\": $duration, \"ops_per_sec\": $ops_per_sec, \"success_rate\": $success_rate, \"conflict_rate\": $conflict_rate, \"issues_in_db\": $db_issue_count}"
}

# Test concurrent reads
test_concurrent_reads() {
    local parallel=$1
    local iterations=$2

    echo -e "\n${YELLOW}========================================${NC}"
    echo -e "${YELLOW}Concurrent Read Test${NC}"
    echo -e "${YELLOW}Parallel: $parallel, Iterations: $iterations${NC}"
    echo -e "${YELLOW}========================================${NC}\n"

    local results_dir=$(mktemp -d)
    local total_ops=$((parallel * iterations))
    local start_time=$(python3 -c "import time; print(time.time())")

    # Spawn parallel readers
    for worker in $(seq 1 $parallel); do
        (
            cd "$TEST_DIR"
            for i in $(seq 1 $iterations); do
                if $BD_CMD --no-daemon ready --limit 10 --json 2>"$results_dir/err_${worker}_${i}.txt" > /dev/null; then
                    echo "success" > "$results_dir/result_${worker}_${i}.txt"
                else
                    echo "failed" > "$results_dir/result_${worker}_${i}.txt"
                fi
            done
        ) &
    done

    wait

    local end_time=$(python3 -c "import time; print(time.time())")
    local duration=$(python3 -c "print(round($end_time - $start_time, 2))")

    local success_count=$(find "$results_dir" -name "result_*.txt" -exec cat {} \; | grep -c "success" || echo 0)
    local failed_count=$(find "$results_dir" -name "result_*.txt" -exec cat {} \; | grep -c "failed" || echo 0)
    local ops_per_sec=$(python3 -c "print(round($total_ops / $duration, 2))")

    rm -rf "$results_dir"

    echo -e "${GREEN}Results:${NC}"
    echo -e "Total reads:    $total_ops"
    echo -e "Successful:     ${GREEN}$success_count${NC}"
    echo -e "Failed:         ${RED}$failed_count${NC}"
    echo -e "Duration:       ${duration}s"
    echo -e "Throughput:     ${ops_per_sec} reads/sec"
    echo ""
}

# Test mixed read/write workload
test_mixed_workload() {
    local parallel=$1
    local iterations=$2

    echo -e "\n${YELLOW}========================================${NC}"
    echo -e "${YELLOW}Mixed Read/Write Workload Test${NC}"
    echo -e "${YELLOW}Parallel: $parallel, Iterations: $iterations${NC}"
    echo -e "${YELLOW}========================================${NC}\n"

    local results_dir=$(mktemp -d)
    local writers=$((parallel / 2))
    local readers=$((parallel - writers))
    local start_time=$(python3 -c "import time; print(time.time())")

    echo "Writers: $writers, Readers: $readers"

    # Spawn writers
    for worker in $(seq 1 $writers); do
        (
            cd "$TEST_DIR"
            for i in $(seq 1 $iterations); do
                if "$BD_CMD" --no-daemon create --title "Mixed W${worker}-I${i}" --type task -q 2>/dev/null; then
                    echo "write_success" > "$results_dir/result_w${worker}_${i}.txt"
                else
                    echo "write_failed" > "$results_dir/result_w${worker}_${i}.txt"
                fi
            done
        ) &
    done

    # Spawn readers
    for worker in $(seq 1 $readers); do
        (
            cd "$TEST_DIR"
            for i in $(seq 1 $iterations); do
                if $BD_CMD --no-daemon ready --limit 10 --json 2>/dev/null > /dev/null; then
                    echo "read_success" > "$results_dir/result_r${worker}_${i}.txt"
                else
                    echo "read_failed" > "$results_dir/result_r${worker}_${i}.txt"
                fi
            done
        ) &
    done

    wait

    local end_time=$(python3 -c "import time; print(time.time())")
    local duration=$(python3 -c "print(round($end_time - $start_time, 2))")

    local write_success=$(find "$results_dir" -name "result_*.txt" -exec cat {} \; | grep -c "write_success" || echo 0)
    local write_failed=$(find "$results_dir" -name "result_*.txt" -exec cat {} \; | grep -c "write_failed" || echo 0)
    local read_success=$(find "$results_dir" -name "result_*.txt" -exec cat {} \; | grep -c "read_success" || echo 0)
    local read_failed=$(find "$results_dir" -name "result_*.txt" -exec cat {} \; | grep -c "read_failed" || echo 0)

    rm -rf "$results_dir"

    echo -e "${GREEN}Results:${NC}"
    echo -e "Write success:  ${GREEN}$write_success${NC}"
    echo -e "Write failed:   ${RED}$write_failed${NC}"
    echo -e "Read success:   ${GREEN}$read_success${NC}"
    echo -e "Read failed:    ${RED}$read_failed${NC}"
    echo -e "Duration:       ${duration}s"
    echo ""
}

# Main
main() {
    echo -e "${GREEN}================================================${NC}"
    echo -e "${GREEN}    Beads Concurrency Stress Test${NC}"
    echo -e "${GREEN}================================================${NC}"
    echo ""
    echo "Parallel workers: $PARALLEL"
    echo "Iterations per worker: $ITERATIONS"
    echo ""

    setup_test_env

    # Run tests with increasing parallelism
    echo -e "\n${BLUE}Running concurrency tests...${NC}\n"

    # Test with configured parallelism
    test_concurrent_reads $PARALLEL $ITERATIONS
    test_concurrent_creates $PARALLEL $ITERATIONS
    test_mixed_workload $PARALLEL $ITERATIONS

    # Scaling tests
    echo -e "\n${YELLOW}========================================${NC}"
    echo -e "${YELLOW}Scaling Tests${NC}"
    echo -e "${YELLOW}========================================${NC}\n"

    for p in 5 10 20; do
        echo -e "\n${BLUE}--- Parallelism: $p ---${NC}"
        test_concurrent_creates $p 5
    done

    echo -e "\n${GREEN}================================================${NC}"
    echo -e "${GREEN}    Concurrency Tests Complete${NC}"
    echo -e "${GREEN}================================================${NC}"
}

main
