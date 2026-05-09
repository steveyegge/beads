#!/bin/bash
# Check that the versioned Docusaurus docs match the current bd release version.

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

if [ ! -f "cmd/bd/version.go" ] || [ ! -f "website/docusaurus.config.ts" ]; then
    echo -e "${RED}Error: must run from repository root${NC}"
    exit 1
fi

CANONICAL=$(grep 'Version = ' cmd/bd/version.go | sed 's/.*"\(.*\)".*/\1/')
if [ -z "$CANONICAL" ]; then
    echo -e "${RED}Could not read version from cmd/bd/version.go${NC}"
    exit 1
fi

MISMATCH=0

check_equal() {
    local description=$1
    local actual=$2
    local expected=$3

    if [ "$actual" != "$expected" ]; then
        echo -e "${RED}❌ $description: $actual (expected $expected)${NC}"
        MISMATCH=1
    else
        echo -e "${GREEN}✓ $description: $actual${NC}"
    fi
}

check_exists() {
    local description=$1
    local path=$2

    if [ ! -e "$path" ]; then
        echo -e "${RED}❌ Missing $description: $path${NC}"
        MISMATCH=1
    else
        echo -e "${GREEN}✓ $description: $path${NC}"
    fi
}

echo "Canonical version (from version.go): $CANONICAL"
echo ""

LATEST_DOCS_VERSION=$(grep -oE '"[0-9]+\.[0-9]+\.[0-9]+"' website/versions.json | head -1 | tr -d '"' || true)
LAST_VERSION=$(grep -oE "lastVersion: '[0-9]+\.[0-9]+\.[0-9]+'" website/docusaurus.config.ts | head -1 | sed "s/.*'\([^']*\)'.*/\1/" || true)
LLMS_VERSION=$(grep -oE 'version: [0-9]+\.[0-9]+\.[0-9]+' website/static/llms-full.txt | head -1 | sed 's/version: //' || true)
CLI_REF_LABEL=$(grep -oE 'Reference for bd v[0-9]+\.[0-9]+\.[0-9]+' "website/versioned_docs/version-$CANONICAL/cli-reference/index.md" 2>/dev/null | head -1 | sed 's/Reference for bd v//' || true)

check_equal "website/versions.json latest docs version" "$LATEST_DOCS_VERSION" "$CANONICAL"
check_equal "website/docusaurus.config.ts lastVersion" "$LAST_VERSION" "$CANONICAL"
check_equal "website/static/llms-full.txt source version" "$LLMS_VERSION" "$CANONICAL"
check_equal "versioned CLI reference label" "$CLI_REF_LABEL" "$CANONICAL"

check_exists "versioned docs snapshot" "website/versioned_docs/version-$CANONICAL"
check_exists "versioned sidebar snapshot" "website/versioned_sidebars/version-$CANONICAL-sidebars.json"

echo ""

if [ "$MISMATCH" -ne 0 ]; then
    echo -e "${RED}Docs version mismatch detected.${NC}"
    echo ""
    echo "To prepare release docs for $CANONICAL:"
    echo "  cd website"
    echo "  npm ci"
    echo "  npx docusaurus docs:version $CANONICAL"
    echo "  cd .."
    echo "  # Ensure website/docusaurus.config.ts lastVersion is '$CANONICAL'"
    echo "  ./scripts/generate-cli-docs.sh ./bd"
    echo "  ./scripts/generate-llms-full.sh"
    echo "  ./scripts/check-docs-version.sh"
    exit 1
fi

echo -e "${GREEN}✓ Docs version matches: $CANONICAL${NC}"
