#!/usr/bin/env bash
set -e && cd "$(dirname "$0")/.."

# Update the vendorHash in default.nix after Go dependency changes
# Usage: ./scripts/update-nix-hash.sh

echo "Getting correct vendorHash from nix..."

# Set pkgs.lib.fakeHash to force nix to output the correct one
printf ',s|vendorHash = ".*";|vendorHash = pkgs.lib.fakeHash;|\nw\n' | ed -s default.nix

# Extract correct hash from nix build error
CORRECT_HASH=$(nix build --no-link 2>&1 | grep "got:" | awk '{print $2}')

if [ -z "$CORRECT_HASH" ]; then
    echo "Error: Could not get hash from nix build"
    git restore default.nix
    exit 1
fi

# Update with correct hash
printf ',s|vendorHash = pkgs\.lib\.fakeHash;|vendorHash = "%s";|\nw\n' "$CORRECT_HASH" | ed -s default.nix

echo "✓ Updated vendorHash to: $CORRECT_HASH"
