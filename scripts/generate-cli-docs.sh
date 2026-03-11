#!/bin/bash
# Generate CLI documentation from live bd command tree
# This script generates markdown files for each command with Docusaurus frontmatter

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
BD="$PROJECT_ROOT/bd"
CLI_DOCS_DIR="$PROJECT_ROOT/website/docs/cli-reference"
COMBINED_DOCS="$PROJECT_ROOT/docs/CLI_REFERENCE.md"

# Ensure output directory exists
mkdir -p "$CLI_DOCS_DIR"

# Check if bd binary exists, build if needed
if [ ! -f "$BD" ]; then
    echo "Building bd..."
    cd "$PROJECT_ROOT"
    CGO_ENABLED=0 go build -o bd ./cmd/bd/
fi

echo "Generating CLI documentation..."

# Generate individual command docs
echo "Generating individual command docs to $CLI_DOCS_DIR..."
for cmd in $($BD help --list); do
    echo "  - $cmd"
    $BD help --doc "$cmd" > "$CLI_DOCS_DIR/$cmd.md"
done

# Also generate the combined reference
echo "Generating combined reference to $COMBINED_DOCS..."
$BD help --all > "$COMBINED_DOCS"

echo ""
echo "Done!"
echo "  - Individual docs: $CLI_DOCS_DIR/"
echo "  - Combined reference: $COMBINED_DOCS"
