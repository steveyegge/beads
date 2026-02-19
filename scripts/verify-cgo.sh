#!/usr/bin/env bash
#
# verify-cgo.sh - Fail if a built binary reports CGO_ENABLED=0.
# Usage: ./scripts/verify-cgo.sh <path-to-binary>

set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 <path-to-binary>" >&2
    exit 2
fi

binary_path="$1"

if [[ ! -f "$binary_path" ]]; then
    echo "ERROR: binary not found: $binary_path" >&2
    exit 1
fi

if ! command -v strings >/dev/null 2>&1; then
    echo "ERROR: 'strings' is required to verify CGO metadata" >&2
    exit 1
fi

if strings "$binary_path" | awk '/^build[[:space:]]+CGO_ENABLED=0$/ { found=1 } END { exit(found?0:1) }'; then
    echo "ERROR: $binary_path was built without CGO support (found CGO_ENABLED=0)" >&2
    exit 1
fi

echo "OK: $binary_path has CGO support"
