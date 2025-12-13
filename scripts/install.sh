#!/usr/bin/env bash
# Install bd with full version information (commit and branch)
#
# Usage:
#   ./scripts/install.sh         # Install to GOPATH/bin
#   ./scripts/install.sh /usr/local/bin  # Install to custom location

set -e

INSTALL_DIR="${1:-$(go env GOPATH)/bin}"

# Extract git information
COMMIT=$(git rev-parse HEAD 2>/dev/null || echo "")
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")

echo "Installing bd to $INSTALL_DIR..."
echo "  Commit: ${COMMIT:0:12}"
echo "  Branch: $BRANCH"

go install -ldflags="-X main.Commit=$COMMIT -X main.Branch=$BRANCH" ./cmd/bd

echo "✓ bd installed successfully"
echo ""
"$INSTALL_DIR"/bd version
