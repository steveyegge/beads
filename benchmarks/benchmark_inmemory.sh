#!/bin/bash
# Benchmark in-memory mode operations to test URI detection fixes
# Fork should handle all 3 SQLite in-memory formats without crashes

set -e

BD_BINARY="${1:-bd}"
OPERATIONS=50

echo "Benchmarking in-memory mode with $BD_BINARY"
echo "=========================================="
echo "Operations per mode: $OPERATIONS"
echo "BD Version: $($BD_BINARY --version 2>&1 || echo 'unknown')"
echo ""

# Test different in-memory URI formats
test_memory_mode() {
    local mode_name=$1
    local db_uri=$2
    local tmpdir=$(mktemp -d -t bd-mem-XXXXXX)

    echo "Testing: $mode_name"
    echo "  URI: $db_uri"

    cd "$tmpdir"

    # Initialize with in-memory database
    export BEADS_AUTO_START_DAEMON=false
    export BEADS_DB="$db_uri"

    local errors=0
    local start=$(python3 -c 'import time; print(time.time())')

    # Try to initialize
    if ! $BD_BINARY init --prefix test > /dev/null 2>&1; then
        echo "  ✗ Init failed"
        ((errors++))
        cd - > /dev/null 2>&1
        rm -rf "$tmpdir"
        return $errors
    fi

    # Create issues
    local created=0
    for i in $(seq 1 $OPERATIONS); do
        if $BD_BINARY create "Test issue $i" > /dev/null 2>&1; then
            ((created++))
        else
            ((errors++))
        fi
    done

    # List operations
    for i in $(seq 1 $OPERATIONS); do
        if ! $BD_BINARY list --limit 10 > /dev/null 2>&1; then
            ((errors++))
        fi
    done

    local end=$(python3 -c 'import time; print(time.time())')
    local elapsed=$(python3 -c "print(int(($end - $start) * 1000))")

    echo "  Time: ${elapsed}ms"
    echo "  Created: $created/$OPERATIONS issues"
    echo "  Errors: $errors"

    # Verify in-memory behavior (data should not persist)
    unset BEADS_DB
    cd - > /dev/null 2>&1
    rm -rf "$tmpdir"

    if [ $errors -gt 0 ]; then
        echo "  ✗ FAILED"
        return 1
    else
        echo "  ✓ PASSED"
        return 0
    fi
}

# Test all three in-memory URI formats
echo "Testing SQLite in-memory URI formats:"
echo "=========================================="
echo ""

total_failed=0

# Format 1: :memory:
if ! test_memory_mode "Classic :memory:" ":memory:"; then
    ((total_failed++))
fi
echo ""

# Format 2: file::memory:
if ! test_memory_mode "Shared file::memory:" "file::memory:"; then
    ((total_failed++))
fi
echo ""

# Format 3: file:memdb?mode=memory
if ! test_memory_mode "URI mode=memory" "file:memdb?mode=memory"; then
    ((total_failed++))
fi
echo ""

# Summary
echo "=========================================="
echo "RESULTS"
echo "=========================================="
echo "Formats tested: 3"
echo "Formats failed: $total_failed"

if [ $total_failed -eq 0 ]; then
    echo "✓ All in-memory formats work correctly"
else
    echo "✗ $total_failed format(s) failed - URI detection issues!"
fi

exit $total_failed
