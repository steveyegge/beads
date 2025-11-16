#!/bin/bash
# Fixed heavy write workload benchmark
# Uses --json output for reliable ID parsing across all ID formats

set -e

BD_BINARY="${1:-bd}"
ISSUES_TO_CREATE=100
UPDATE_CYCLES=3
TMPDIR=$(mktemp -d -t bd-writes-XXXXXX)

echo "Benchmarking heavy write workload with $BD_BINARY"
echo "=========================================="
echo "Issues to create: $ISSUES_TO_CREATE"
echo "Update cycles: $UPDATE_CYCLES"
echo "Test directory: $TMPDIR"
echo "BD Version: $($BD_BINARY --version 2>&1 || echo 'unknown')"
echo ""

cd "$TMPDIR"

# Initialize database
export BEADS_AUTO_START_DAEMON=false
$BD_BINARY init --prefix test > /dev/null 2>&1

# Phase 1: Rapid creation
echo "Phase 1: Creating $ISSUES_TO_CREATE issues..."
start_create=$(python3 -c 'import time; print(time.time())')

issue_ids=()
for i in $(seq 1 $ISSUES_TO_CREATE); do
    # Use --json to reliably extract ID regardless of format (hex, base-36, etc.)
    output=$($BD_BINARY create "Issue $i" --priority $((i % 4 + 1)) --json 2>&1)
    id=$(echo "$output" | python3 -c "import sys, json; print(json.load(sys.stdin)['id'])" 2>/dev/null || echo "")

    if [ -n "$id" ]; then
        issue_ids+=("$id")
    fi

    # Progress indicator every 20 issues
    if [ $((i % 20)) -eq 0 ]; then
        echo "  Created $i/$ISSUES_TO_CREATE issues..."
    fi
done

end_create=$(python3 -c 'import time; print(time.time())')
elapsed_create=$(python3 -c "print(int(($end_create - $start_create) * 1000))")

echo "✓ Creation complete: ${elapsed_create}ms"
throughput_create=$(python3 -c "print(f'{$ISSUES_TO_CREATE / ($elapsed_create / 1000):.1f}')")
echo "  Throughput: ${throughput_create} issues/sec"

# Phase 2: Rapid updates
echo ""
echo "Phase 2: Running $UPDATE_CYCLES update cycles..."
start_update=$(python3 -c 'import time; print(time.time())')

total_updates=0
for cycle in $(seq 1 $UPDATE_CYCLES); do
    echo "  Cycle $cycle/$UPDATE_CYCLES..."
    for id in "${issue_ids[@]}"; do
        $BD_BINARY update "$id" --status open > /dev/null 2>&1
        ((total_updates++))
    done
done

end_update=$(python3 -c 'import time; print(time.time())')
elapsed_update=$(python3 -c "print(int(($end_update - $start_update) * 1000))")

echo "✓ Updates complete: ${elapsed_update}ms"
echo "  Total updates: $total_updates"
throughput_update=$(python3 -c "print(f'{$total_updates / ($elapsed_update / 1000):.1f}')")
echo "  Throughput: ${throughput_update} updates/sec"

# Phase 3: Mixed operations
echo ""
echo "Phase 3: Mixed read/write operations..."
start_mixed=$(python3 -c 'import time; print(time.time())')

mixed_ops=0
for i in $(seq 1 50); do
    # List operations
    $BD_BINARY list --limit 10 > /dev/null 2>&1
    ((mixed_ops++))

    # Update random issue
    idx=$((RANDOM % ${#issue_ids[@]}))
    id="${issue_ids[$idx]}"
    $BD_BINARY update "$id" --priority $((RANDOM % 4 + 1)) > /dev/null 2>&1
    ((mixed_ops++))

    # Show operations
    $BD_BINARY show "$id" > /dev/null 2>&1
    ((mixed_ops++))
done

end_mixed=$(python3 -c 'import time; print(time.time())')
elapsed_mixed=$(python3 -c "print(int(($end_mixed - $start_mixed) * 1000))")

echo "✓ Mixed operations complete: ${elapsed_mixed}ms"
echo "  Operations: $mixed_ops"
throughput_mixed=$(python3 -c "print(f'{$mixed_ops / ($elapsed_mixed / 1000):.1f}')")
echo "  Throughput: ${throughput_mixed} ops/sec"

# Summary
echo ""
echo "=========================================="
echo "RESULTS"
echo "=========================================="
total_elapsed=$((elapsed_create + elapsed_update + elapsed_mixed))
total_ops=$((ISSUES_TO_CREATE + total_updates + mixed_ops))

echo "Total time: ${total_elapsed}ms"
echo "Total operations: $total_ops"
throughput_overall=$(python3 -c "print(f'{$total_ops / ($total_elapsed / 1000):.1f}')")
echo "Overall throughput: ${throughput_overall} ops/sec"

# Verify integrity using JSON
echo ""
echo "Database integrity check:"
issue_count=$($BD_BINARY list --limit 999 --json 2>/dev/null | python3 -c "import sys, json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")
echo "Issues in database: $issue_count (expected: $ISSUES_TO_CREATE)"

if [ "$issue_count" -eq "$ISSUES_TO_CREATE" ]; then
    echo "✓ Database integrity verified"
    exit_code=0
else
    echo "✗ Database corruption detected!"
    exit_code=1
fi

# Cleanup
cd - > /dev/null 2>&1
rm -rf "$TMPDIR"

exit $exit_code
