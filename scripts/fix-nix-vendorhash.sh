#!/bin/bash
# Fix Nix vendorHash after go.mod/go.sum changes
#
# When Go dependencies change, the Nix vendorHash in default.nix needs to be recalculated.
# This script helps extract the correct hash from Nix build output.
#
# Background:
# - vendorHash is a SHA256 of the vendor/ directory used in Nix builds
# - When go.mod/go.sum change, the vendor/ changes and hash becomes stale
# - Nix will report the mismatch: "hash mismatch ... got: sha256-..."
#
# Usage:
#   1. Run a Nix build and capture the error output:
#      nix build .# 2>&1 | tee nix-build.log
#   
#   2. Extract the correct hash from the error:
#      grep "got: " nix-build.log | sed 's/.*got: //'
#   
#   3. Update default.nix with the new hash
#   
# Or use this script:
#   ./scripts/fix-nix-vendorhash.sh <NEW_HASH>

set -e

if [ $# -ne 1 ]; then
    echo "Usage: $0 <new_vendorhash>"
    echo ""
    echo "Example:"
    echo "  $0 'sha256-abc123def456...'"
    echo ""
    echo "To find the correct hash:"
    echo "  1. Run: nix build .# 2>&1 | tee build.log"
    echo "  2. Look for line containing 'got: sha256-...'"
    echo "  3. Copy that value and run this script"
    exit 1
fi

NEW_HASH="$1"

# Validate hash format
if ! echo "$NEW_HASH" | grep -q "^sha256-"; then
    echo "Error: Hash must start with 'sha256-'"
    echo "Got: $NEW_HASH"
    exit 1
fi

# Update default.nix
sed -i "s|vendorHash = \"sha256-[^\"]*\"|vendorHash = \"$NEW_HASH\"|" default.nix

echo "✅ Updated vendorHash in default.nix"
echo "New hash: $NEW_HASH"
echo ""
echo "Next steps:"
echo "  1. Verify: nix build .#"
echo "  2. Commit: git add default.nix && git commit -m \"chore(nix): update vendorHash\""
echo "  3. Push: git push"
