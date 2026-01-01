#!/bin/bash
#
# test-version-check.sh - Test suite for bd-version-check.sh
#
# Run from repo root: bash examples/startup-hooks/test-version-check.sh
#
# Tests:
# 1. Script sources correctly
# 2. Handles missing jq gracefully
# 3. Detects version changes (requires jq)
# 4. Skips notification on first run
# 5. Updates metadata.json correctly
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

cd "$REPO_ROOT"

echo "═══════════════════════════════════════════════════════════════"
echo "Testing bd-version-check.sh"
echo "═══════════════════════════════════════════════════════════════"
echo ""

# Check prerequisites
echo "Checking prerequisites..."
echo -n "  bd installed: "
if command -v bd &> /dev/null; then
    echo "✓ ($(bd --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1))"
else
    echo "✗ (REQUIRED)"
    exit 1
fi

echo -n "  jq installed: "
if command -v jq &> /dev/null; then
    echo "✓ ($(jq --version))"
    HAS_JQ=true
else
    echo "✗ (optional - some tests will be skipped)"
    HAS_JQ=false
fi

echo -n "  .beads/ exists: "
if [ -d ".beads" ]; then
    echo "✓"
else
    echo "✗ (run 'bd init' first)"
    exit 1
fi

echo ""

# Test 1: Script sources without error
echo "Test 1: Script sources without error"
if source examples/startup-hooks/bd-version-check.sh 2>/dev/null; then
    echo "  ✓ Script sourced successfully"
else
    echo "  ✗ Script failed to source"
    exit 1
fi

# Test 2: Metadata file exists or is created
echo ""
echo "Test 2: Metadata file exists"
if [ -f ".beads/metadata.json" ]; then
    echo "  ✓ .beads/metadata.json exists"
    echo "  Content: $(cat .beads/metadata.json)"
else
    echo "  ✗ .beads/metadata.json not found"
fi

if [ "$HAS_JQ" = "true" ]; then
    # Test 3: Simulate version upgrade
    echo ""
    echo "Test 3: Simulate version upgrade"

    # Backup current metadata
    cp .beads/metadata.json .beads/metadata.json.bak

    # Set old version
    jq '.last_bd_version = "0.1.0"' .beads/metadata.json > .beads/metadata.json.tmp
    mv .beads/metadata.json.tmp .beads/metadata.json

    echo "  Set last_bd_version to 0.1.0"
    echo "  Running version check..."
    echo ""

    # Run the script and capture output
    OUTPUT=$(source examples/startup-hooks/bd-version-check.sh 2>&1)

    if echo "$OUTPUT" | grep -q "bd upgraded"; then
        echo "  ✓ Upgrade notification displayed"
    else
        echo "  ✗ No upgrade notification (expected 'bd upgraded')"
    fi

    # Check if version was updated
    NEW_VERSION=$(jq -r '.last_bd_version' .beads/metadata.json)
    CURRENT_VERSION=$(bd --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)

    if [ "$NEW_VERSION" = "$CURRENT_VERSION" ]; then
        echo "  ✓ Metadata updated to $NEW_VERSION"
    else
        echo "  ✗ Metadata not updated (got: $NEW_VERSION, expected: $CURRENT_VERSION)"
    fi

    # Restore original metadata
    mv .beads/metadata.json.bak .beads/metadata.json
    echo "  Restored original metadata"

    # Test 4: First run (unknown version)
    echo ""
    echo "Test 4: First run simulation"

    # Backup and remove last_bd_version
    cp .beads/metadata.json .beads/metadata.json.bak
    jq 'del(.last_bd_version)' .beads/metadata.json > .beads/metadata.json.tmp
    mv .beads/metadata.json.tmp .beads/metadata.json

    echo "  Removed last_bd_version from metadata"

    OUTPUT=$(source examples/startup-hooks/bd-version-check.sh 2>&1)

    if echo "$OUTPUT" | grep -q "bd upgraded"; then
        echo "  ✗ Showed upgrade notification on first run (should skip)"
    else
        echo "  ✓ No notification on first run (correct)"
    fi

    # Restore
    mv .beads/metadata.json.bak .beads/metadata.json
    echo "  Restored original metadata"

else
    echo ""
    echo "Tests 3-4: Skipped (requires jq)"
    echo "  Install jq to run full test suite:"
    echo "    macOS: brew install jq"
    echo "    Ubuntu: apt-get install jq"
    echo "    Windows: winget install jqlang.jq"
fi

# Test 5: bd prime still runs
echo ""
echo "Test 5: bd prime execution"
if bd prime 2>/dev/null | head -1 | grep -q "Beads"; then
    echo "  ✓ bd prime outputs workflow context"
else
    echo "  ✗ bd prime failed"
fi

echo ""
echo "═══════════════════════════════════════════════════════════════"
echo "Testing complete"
echo "═══════════════════════════════════════════════════════════════"
