#!/bin/bash
# Pre-push quality gates verification script
#
# Run this before pushing to catch issues early:
#   ./scripts/preflight.sh
#
# Exit codes:
#   0 = All checks passed
#   1 = At least one check failed
#
# This script verifies:
#   1. golangci-lint passes (0 errors)
#   2. go test passes (all tests)
#   3. nix flake check passes (if Nix available)

set -e

echo "🔍 Running pre-push verification checks..."
echo ""

FAILED=0

# Check 1: Linting
echo "Step 1: Checking linting (golangci-lint run)..."
if golangci-lint run ./...; then
    echo "✅ Linting passed"
else
    echo "❌ Linting failed"
    FAILED=1
fi
echo ""

# Check 2: Tests
echo "Step 2: Running tests (go test ./...)..."
if go test -short ./...; then
    echo "✅ Tests passed"
else
    echo "❌ Tests failed"
    FAILED=1
fi
echo ""

# Check 3: Nix build (optional)
if command -v nix &> /dev/null; then
    echo "Step 3: Checking Nix build (nix flake check)..."
    if nix flake check; then
        echo "✅ Nix build passed"
    else
        echo "❌ Nix build failed"
        FAILED=1
    fi
    echo ""
else
    echo "Step 3: Skipping Nix check (nix not available)"
    echo ""
fi

# Final result
if [ $FAILED -eq 0 ]; then
    echo "✅ All pre-push checks passed - safe to push!"
    exit 0
else
    echo "❌ Some checks failed - fix them before pushing"
    exit 1
fi
