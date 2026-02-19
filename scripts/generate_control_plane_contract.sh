#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
AGENTS_PATH="$ROOT_DIR/AGENTS.md"
OUT_PATH="$ROOT_DIR/docs/CONTROL_PLANE_CONTRACT.md"

if [[ ! -f "$AGENTS_PATH" ]]; then
  echo "error: AGENTS.md not found at $AGENTS_PATH" >&2
  exit 1
fi

SECTION_CONTENT="$(
  awk '
    /^## Control-Plane Contract \(Inlined\)/ {capture=1; next}
    /^## Cold Start/ {capture=0}
    capture {print}
  ' "$AGENTS_PATH" | perl -0777 -pe 's/\A\s+|\s+\z//g'
)"

if [[ -z "${SECTION_CONTENT//[[:space:]]/}" ]]; then
  echo "error: failed to extract Control-Plane Contract section from AGENTS.md" >&2
  exit 1
fi

cat >"$OUT_PATH" <<EOF
# Control-Plane Contract (Generated)

> Source: \`AGENTS.md\` section \`Control-Plane Contract (Inlined)\`.
> Regenerate with \`./scripts/generate_control_plane_contract.sh\`.

$SECTION_CONTENT
EOF

echo "updated $OUT_PATH"
