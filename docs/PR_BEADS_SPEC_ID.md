# PR: Add spec_id field for linking issues to specification documents

**Target repo:** steveyegge/beads
**Branch name:** `feature/spec-id`
**Closes:** #976

---

## Summary

Adds first-class spec linking to beads, addressing the workflow question raised in #976.

Instead of manually embedding doc references in `--desc`, users can now use a dedicated `spec_id` field:

```bash
# Link issue to spec document
bd create "Implement auth flow" --spec-id "docs/plans/auth-design.md"

# Filter by spec
bd list --spec "docs/plans/"

# View shows linked spec
bd show bd-xxx
# Output includes: Spec: docs/plans/auth-design.md
```

This provides a structured foundation for spec-driven development workflows.

---

## Changes

### Types
- `internal/types/types.go` — Add `SpecID string` field to Issue struct

### Storage (SQLite)
- `internal/storage/sqlite/migrations/041_spec_id_column.go` — Migration to add column + index
- `internal/storage/sqlite/schema.go` — Add spec_id to base schema
- `internal/storage/sqlite/issues.go` — Add spec_id to INSERT statements
- `internal/storage/sqlite/transaction.go` — Add spec_id to scanIssueRow
- `internal/storage/sqlite/queries.go` — Add spec_id to allowedUpdateFields

### Storage (Dolt)
- `internal/storage/dolt/schema.go` — Mirror SQLite changes
- `internal/storage/dolt/issues.go` — Mirror SQLite changes
- `internal/storage/dolt/transaction.go` — Mirror SQLite changes
- `internal/storage/dolt/queries.go` — Mirror SQLite changes

### Storage (Memory)
- `internal/storage/memory/memory.go` — Add SpecID field handling

### CLI
- `cmd/bd/create.go` — Add `--spec-id` and `--spec` flags
- `cmd/bd/update.go` — Add `--spec-id` flag
- `cmd/bd/list.go` — Add `--spec` filter flag (supports prefix matching)
- `cmd/bd/show.go` — Display spec_id in output

### RPC
- `internal/rpc/protocol.go` — Add SpecID to ListFilter

### Tests
- `internal/storage/sqlite/migrations/041_spec_id_column_test.go` — Migration test

### Docs
- `docs/CLI_REFERENCE.md` — Document new flags

---

## Files to EXCLUDE (Phase 2 / Shadowbook)

These files are part of a separate "spec registry" feature and should NOT be in this PR:

- `internal/storage/sqlite/migrations/042_spec_registry.go`
- `internal/storage/sqlite/migrations/043_spec_changed_at.go`
- `internal/storage/sqlite/spec_registry.go`
- `internal/storage/dolt/spec_registry.go`
- `internal/storage/memory/spec_registry.go`
- `internal/spec/*` (entire package)
- `internal/rpc/server_spec.go`
- `cmd/bd/spec.go`
- `docs/SPEC_SYNC.md`
- Any references to `spec_changed_at`, `spec_registry`, `bd spec` commands

---

## Usage Examples

```bash
# Create with spec reference
bd create "Implement OAuth provider" --spec-id "docs/plans/auth-redesign.md"
bd create "Add rate limiting" --spec "specs/api/rate-limits.md"

# Update to add/change spec
bd update bd-xxx --spec-id "docs/plans/new-spec.md"
bd update bd-xxx --spec-id ""  # Clear spec reference

# Filter by spec (exact match)
bd list --spec "docs/plans/auth-redesign.md"

# Filter by spec prefix (all issues for auth specs)
bd list --spec "docs/plans/auth/"

# Show displays spec
bd show bd-xxx
# Output:
# ○ bd-xxx · Implement OAuth provider   [P2 · OPEN]
# Spec: docs/plans/auth-redesign.md
```

---

## Design Decisions

1. **Freeform spec_id** — No validation on format. Can be file path, URL, or identifier (e.g., `SPEC-001`). Flexibility over strictness.

2. **Single spec per issue** — Kept simple for MVP. Multiple specs can be tracked via related issues or description field.

3. **Prefix matching** — `--spec "docs/"` matches all specs under that path. Useful for filtering by category.

4. **Empty string clears** — `--spec-id ""` removes the spec reference.

---

## Testing

```bash
# Run migration test
go test ./internal/storage/sqlite/migrations/... -run TestMigrateSpecIDColumn

# Run full test suite
go test ./...
```

---

## Related

- Closes #976 (Best practices for using Beads alongside detailed planning docs)
- Foundation for potential future spec-tracking features
