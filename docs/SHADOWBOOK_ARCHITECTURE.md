# Shadowbook Architecture

Shadowbook extends Beads with spec tracking, drift detection, and compaction.

---

## Components

- **Spec Scanner**: walks `specs/` and hashes files
- **Spec Registry**: SQLite table of spec metadata
- **Drift Marker**: updates linked issues when spec hash changes
- **Auto‑Link**: suggests spec links based on title similarity
- **Compaction**: archives specs with summaries

---

## Data Model

### Issue Fields (Shadowbook additions)
- `spec_id` (string)
- `spec_changed_at` (timestamp)

### Spec Registry Fields
- `spec_id` (primary key)
- `path`
- `title`
- `sha256`
- `mtime`
- `discovered_at`
- `last_scanned_at`
- `missing_at`
- `lifecycle`
- `completed_at`
- `summary`
- `summary_tokens`
- `archived_at`

---

## Drift Detection Flow

1) `bd spec scan` computes hashes and updates the registry.
2) If a hash changes, linked issues get `spec_changed_at`.
3) `bd list --spec-changed` shows drifted issues.
4) `bd update --ack-spec` clears the warning.

---

## Auto‑Link Flow

- `bd spec suggest <id>` scores spec matches by title/filename tokens.
- `bd spec link --auto` previews matches; `--confirm` applies links.
- Optional table format for review: `--format table`.

---

## Compaction Flow

- `bd spec compact` stores a summary and lifecycle metadata.
- `bd close --compact-spec` auto‑compacts when the last linked issue closes.
- Summaries are deterministic and derived from spec highlights + closed bead titles.

---

## Key Code Paths

- Spec scan/update: `internal/spec/scanner.go`, `internal/spec/registry.go`
- Registry storage: `internal/storage/*/spec_registry.go`
- CLI: `cmd/bd/spec.go`, `cmd/bd/spec_match.go`, `cmd/bd/auto_compact.go`
- RPC handlers: `internal/rpc/server_spec.go`
