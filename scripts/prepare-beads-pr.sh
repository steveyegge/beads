#!/bin/bash
# prepare-beads-pr.sh
# Creates a clean branch with only Phase 1 (spec_id) changes for beads PR

set -e

echo "=== Preparing beads PR branch ==="

# 1. Ensure we're in the right place
if [ ! -d ".beads" ]; then
    echo "Error: Run from repo root (no .beads/ found)"
    exit 1
fi

# 2. Fetch upstream beads
echo "Fetching upstream beads..."
git remote add upstream https://github.com/steveyegge/beads.git 2>/dev/null || true
git fetch upstream

# 3. Create clean branch from upstream main
echo "Creating feature/spec-id branch from upstream/main..."
git checkout -b feature/spec-id upstream/main

# 4. List the Phase 1 files to cherry-pick changes for
PHASE1_FILES=(
    "internal/types/types.go"
    "internal/storage/sqlite/migrations/041_spec_id_column.go"
    "internal/storage/sqlite/migrations/041_spec_id_column_test.go"
    "internal/storage/sqlite/schema.go"
    "internal/storage/sqlite/issues.go"
    "internal/storage/sqlite/transaction.go"
    "internal/storage/sqlite/queries.go"
    "internal/storage/sqlite/dependencies.go"
    "internal/storage/sqlite/labels.go"
    "internal/storage/sqlite/ready.go"
    "internal/storage/sqlite/migrations.go"
    "internal/storage/dolt/schema.go"
    "internal/storage/dolt/issues.go"
    "internal/storage/dolt/transaction.go"
    "internal/storage/dolt/queries.go"
    "internal/storage/dolt/dependencies.go"
    "internal/storage/memory/memory.go"
    "internal/rpc/protocol.go"
    "internal/rpc/server_issues_epics.go"
    "internal/rpc/list_filters_test.go"
    "cmd/bd/create.go"
    "cmd/bd/update.go"
    "cmd/bd/list.go"
    "cmd/bd/show.go"
    "cmd/bd/show_test.go"
    "docs/CLI_REFERENCE.md"
)

echo ""
echo "=== Phase 1 files (spec_id only) ==="
printf '%s\n' "${PHASE1_FILES[@]}"

echo ""
echo "=== Files to EXCLUDE (Phase 2 / Shadowbook) ==="
echo "- internal/storage/sqlite/migrations/042_spec_registry.go"
echo "- internal/storage/sqlite/migrations/043_spec_changed_at.go"
echo "- internal/storage/*/spec_registry.go"
echo "- internal/spec/* (entire package)"
echo "- internal/rpc/server_spec.go"
echo "- cmd/bd/spec.go"
echo "- docs/SPEC_SYNC.md"
echo "- docs/SPEC_ID.md (optional - can include if cleaned up)"

echo ""
echo "=== Next Steps ==="
echo "1. Cherry-pick or manually copy Phase 1 changes from main branch"
echo "2. Remove any spec_changed_at or spec_registry references"
echo "3. Run tests: go test ./..."
echo "4. Commit: git commit -m 'feat: add spec_id field for linking issues to specs'"
echo "5. Push: git push origin feature/spec-id"
echo "6. Create PR: gh pr create --repo steveyegge/beads"
echo ""
echo "For manual extraction, use:"
echo "  git checkout main -- <file>"
echo "  # Then edit to remove Phase 2 code"
echo ""
