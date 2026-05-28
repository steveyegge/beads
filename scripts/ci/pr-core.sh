#!/usr/bin/env bash
# Required fast PR Go test contract.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# shellcheck source=../.buildflags
source "$REPO_ROOT/.buildflags"
# shellcheck source=lib/timing.sh
source "$REPO_ROOT/scripts/ci/lib/timing.sh"

cd "$REPO_ROOT"

ci_time "pr-core go test" -- \
    go test -race -short -skip '^TestEmbedded' ./...
