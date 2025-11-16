#!/bin/bash
# Fixed concurrent operations benchmark
# Uses --json output for reliable ID parsing across all ID formats (hex, base-36, etc.)

set -e

BD_BINARY="${1:-bd}"
WORKERS=10
OPERATIONS_PER_WORKER=20
TMPDIR=$(mktemp -d -t bd-concurrent-XXXXXX)

echo "Benchmarking concurrent operations with $BD_BINARY"
echo "=========================================="
echo "Workers: $WORKERS"
echo "Operations per worker: $OPERATIONS_PER_WORKER"
echo "Test directory: $TMPDIR"
echo "BD Version: $($BD_BINARY --version 2>&1 || echo 'unknown')"
echo ""

cd "$TMPDIR"

# Initialize database
export BEADS_AUTO_START_DAEMON=false
$BD_BINARY init --prefix test > /dev/null 2>&1

# Worker function that creates and updates issues
worker() {
    local worker_id=$1
    local errors=0
    local start=$(python3 -c 'import time; print(time.time())')

    for i in $(seq 1 $OPERATIONS_PER_WORKER); do
        # Create issue - use --json for reliable parsing
        if ! $BD_BINARY create "Worker $worker_id - Issue $i" --json > /dev/null 2>&1; then
            ((errors++))
        fi

        # List to trigger reads during writes
        if ! $BD_BINARY list --limit 5 --json > /dev/null 2>&1; then
            ((errors++))
        fi
    done

    local end=$(python3 -c 'import time; print(time.time())')
    local elapsed=$(python3 -c "print(int(($end - $start) * 1000))")

    echo "Worker $worker_id: ${elapsed}ms, errors: $errors"
    return $errors
}

# Launch workers in parallel
echo "Launching $WORKERS concurrent workers..."
start_total=$(python3 -c 'import time; print(time.time())')

pids=()
for worker_id in $(seq 1 $WORKERS); do
    worker $worker_id &
    pids+=($!)
done

# Wait for all workers and collect exit codes
total_errors=0
for pid in "${pids[@]}"; do
    if ! wait $pid; then
        ((total_errors++))
    fi
done

end_total=$(python3 -c 'import time; print(time.time())')
elapsed_total=$(python3 -c "print(int(($end_total - $start_total) * 1000))")

echo ""
echo "=========================================="
echo "RESULTS"
echo "=========================================="
echo "Total time: ${elapsed_total}ms"
echo "Total operations: $((WORKERS * OPERATIONS_PER_WORKER * 2))"
echo "Failed workers: $total_errors"

# Verify database integrity using JSON output
echo ""
echo "Database integrity check:"
issue_count=$($BD_BINARY list --limit 999 --json 2>/dev/null | python3 -c "import sys, json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
expected=$((WORKERS * OPERATIONS_PER_WORKER))
echo "Issues created: $issue_count (expected: $expected)"

if [ "$issue_count" -eq "$expected" ]; then
    echo "✓ All issues accounted for"
    exit_code=0
else
    echo "✗ Missing $((expected - issue_count)) issues - possible race condition!"
    exit_code=1
fi

# Cleanup
cd - > /dev/null 2>&1
rm -rf "$TMPDIR"

exit $exit_code
