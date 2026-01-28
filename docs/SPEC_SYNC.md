# Spec Sync - Phase 2 Implementation Spec

**Status:** Ready to implement
**Depends on:** Phase 1 (SpecID) — ✅ Complete
**Epic:** bd-3x5

## Summary

Phase 1 added `spec_id` field to beads. Phase 2 builds a **spec registry** that tracks spec files in the repo, detects changes, and surfaces when linked beads need attention.

## Goal

When a spec file changes, beads linked to it should be flagged so agents/humans know to review them.

## Architecture

```
specs/**/*.md (files)
       ↓ bd spec scan
spec_registry (SQLite table)
       ↓ hash comparison
issues.spec_changed_at (timestamp)
       ↓ bd list --spec-changed
surfaced beads needing review
```

## Spec ID Linkage Rules

A `spec_id` is treated as a **scannable file path** only when:
1. It does NOT contain `://` (not a URL)
2. It does NOT start with `SPEC-` or similar ID prefixes
3. It is repo-relative (no leading `/`)

**Examples:**
| spec_id | Type | Scanned? |
|---------|------|----------|
| `specs/auth/login.md` | File path | Yes |
| `docs/design/api.md` | File path | Yes |
| `https://docs.example.com/spec` | URL | No |
| `SPEC-001` | Identifier | No |
| `/absolute/path.md` | Absolute path | No (warn) |

Non-file spec_ids are never flagged as "missing" and never trigger change detection.

## Repo Root Resolution

The scanner resolves repo root by:
1. Look for `.beads/` directory (walk up from cwd)
2. Fall back to `git rev-parse --show-toplevel`
3. Error if neither found

All spec_ids are normalized to repo-relative paths with forward slashes.

## Implementation Tasks

### Task 1: Spec Registry Table + Migration

**File:** `internal/storage/sqlite/migrations/042_spec_registry.go`

```sql
CREATE TABLE IF NOT EXISTS spec_registry (
    spec_id TEXT PRIMARY KEY,           -- normalized path (specs/auth/login.md)
    path TEXT NOT NULL,                  -- same as spec_id for now
    title TEXT DEFAULT '',               -- first H1 from file
    sha256 TEXT DEFAULT '',              -- content hash
    mtime DATETIME,                      -- last modified time (nullable)
    discovered_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_scanned_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    missing_at DATETIME                  -- soft delete: when file disappeared (nullable)
);

CREATE INDEX IF NOT EXISTS idx_spec_registry_path ON spec_registry(path);
CREATE INDEX IF NOT EXISTS idx_spec_registry_missing ON spec_registry(missing_at);
```

**Why:** Registry is source of truth for known specs. Hash enables change detection. DATETIME matches existing schema style. `missing_at` enables soft-delete without migration churn later.

---

### Task 2: Add spec_changed_at to Issues

**File:** `internal/storage/sqlite/migrations/043_spec_changed_at.go`

```sql
ALTER TABLE issues ADD COLUMN spec_changed_at DATETIME;
CREATE INDEX IF NOT EXISTS idx_issues_spec_changed_at ON issues(spec_changed_at);
```

**Semantics:**
- `NULL` = no change pending (default, unset)
- `DATETIME` = spec changed on this date, review needed
- Filter: `WHERE spec_changed_at IS NOT NULL`

**When spec_changed_at clears:**
1. `bd update <id> --ack-spec` — explicit acknowledgment
2. `bd update <id> --spec-id <new>` — changing spec_id clears it
3. `bd close <id>` — closing clears it (implicit ack)

**Files to update:**
- `internal/types/types.go` — add `SpecChangedAt *time.Time` (pointer for nullable)
- `internal/storage/sqlite/transaction.go` — add to scanIssueRow (use sql.NullTime)
- `internal/storage/sqlite/queries.go` — add to allowedUpdateFields
- `internal/storage/sqlite/issues.go` — add to INSERT
- `internal/storage/sqlite/schema.go` — add to schema probe/invariants
- `internal/storage/dolt/*` — mirror changes
- `internal/storage/memory/*` — mirror changes

---

### Task 3: Storage Interface for Spec Registry

**File:** `internal/storage/storage.go` (extend existing)

```go
// SpecRegistryStore handles spec registry operations
// Implemented by sqlite, dolt, and memory backends
type SpecRegistryStore interface {
    // Registry CRUD
    UpsertSpec(ctx context.Context, spec SpecEntry) error
    GetSpec(ctx context.Context, specID string) (*SpecEntry, error)
    ListSpecs(ctx context.Context, opts SpecListOptions) ([]SpecEntry, error)
    MarkSpecMissing(ctx context.Context, specID string, missingAt time.Time) error

    // Bead linkage
    GetBeadsBySpecID(ctx context.Context, specID string) ([]Issue, error)
    MarkBeadsSpecChanged(ctx context.Context, specID string, changedAt time.Time) error
    ClearSpecChanged(ctx context.Context, issueID string) error
}

type SpecEntry struct {
    SpecID        string
    Path          string
    Title         string
    SHA256        string
    Mtime         *time.Time
    DiscoveredAt  time.Time
    LastScannedAt time.Time
    MissingAt     *time.Time  // nil = present, set = soft-deleted
}

type SpecListOptions struct {
    IncludeMissing bool   // include soft-deleted specs
    PathPrefix     string // filter by path prefix
}
```

**Implementation files:**
- `internal/storage/sqlite/spec_registry.go` — SQLite implementation
- `internal/storage/dolt/spec_registry.go` — Dolt implementation
- `internal/storage/memory/spec_registry.go` — Memory implementation

---

### Task 4: Spec Scanner Logic

**File:** `internal/spec/scanner.go` (new package)

```go
package spec

type ScannedSpec struct {
    SpecID   string    // normalized repo-relative path
    Path     string    // absolute path on disk
    Title    string    // first H1
    SHA256   string    // content hash
    Mtime    time.Time
}

// Scan walks dir for *.md files and returns specs
// repoRoot is used to compute repo-relative paths
func Scan(repoRoot, dir string) ([]ScannedSpec, error)

// FindRepoRoot locates repo root via .beads/ or git
func FindRepoRoot() (string, error)

// ExtractTitle reads first H1 from markdown
func ExtractTitle(path string) string

// IsScannableSpecID returns true if spec_id should be scanned as a file
// Returns false for URLs (contains ://), IDs (SPEC-xxx), absolute paths
func IsScannableSpecID(specID string) bool
```

**Behavior:**
- Walk `specs/` (or configured path) relative to repo root
- Normalize paths to forward slashes, repo-relative
- Hash file contents with sha256
- Extract first `# Heading` as title
- Skip non-scannable spec_ids (URLs, IDs)

---

### Task 5: Registry Update Logic

**File:** `internal/spec/registry.go`

```go
type ScanResult struct {
    Added     int
    Updated   int
    Unchanged int
    Missing   int      // specs in registry but not on disk
    ChangedSpecs []string  // spec_ids with hash changes
}

// UpdateRegistry syncs scanned specs to database
func UpdateRegistry(store SpecRegistryStore, specs []ScannedSpec) (*ScanResult, error)

// MarkChangedBeads sets spec_changed_at on issues whose spec hash changed
func MarkChangedBeads(store SpecRegistryStore, changedSpecIDs []string) (int, error)
```

**Behavior:**
1. Compare scanned specs to registry (by spec_id)
2. Insert new specs (discovered_at = now)
3. Update changed specs (different sha256, last_scanned_at = now)
4. Mark missing specs (missing_at = now, don't delete)
5. Clear missing_at if spec reappears
6. For changed specs, find issues with matching spec_id
7. Set `spec_changed_at = now` on those issues (only if currently NULL or older)

---

### Task 6: CLI Commands

#### 6.1 `bd spec scan`

**File:** `cmd/bd/spec_scan.go`

```bash
bd spec scan                    # scan specs/ by default
bd spec scan --path docs/specs  # custom path
bd spec scan --json             # output JSON
```

**Output:**
```
Scanned 42 specs
  Added: 3
  Updated: 2 (hash changed)
  Unchanged: 37

Beads marked for review: 2
  bd-a1b2: Implement login flow (specs/auth/login.md)
  bd-c3d4: Fix signup bug (specs/auth/signup.md)
```

#### 6.2 `bd spec list`

**File:** `cmd/bd/spec_list.go`

```bash
bd spec list              # all specs in registry
bd spec list --json       # JSON output
```

**Output:**
```
specs/auth/login.md      Login Flow           3 beads
specs/auth/signup.md     User Signup          1 bead
specs/api/endpoints.md   API Endpoints        0 beads
```

#### 6.3 `bd spec show <spec-id>`

**File:** `cmd/bd/spec_show.go`

```bash
bd spec show specs/auth/login.md
```

**Output:**
```
Spec: specs/auth/login.md
Title: Login Flow
Last scanned: 2026-01-28 14:30
Hash: a1b2c3d4...

Linked beads:
  ○ bd-a1b2 [P1] Implement login flow
  ✓ bd-e5f6 [P2] Add remember me checkbox
  ◐ bd-g7h8 [P1] Fix session timeout [SPEC CHANGED]
```

Note: Use `◐` (half-filled) or append `[SPEC CHANGED]` tag. No emoji icons.

#### 6.4 `bd spec coverage`

**File:** `cmd/bd/spec_coverage.go`

```bash
bd spec coverage
```

**Output:**
```
Spec Coverage: 12/42 specs have linked beads (29%)

With beads (12):
  specs/auth/login.md (3 beads)
  specs/auth/signup.md (1 bead)
  ...

Without beads (30):
  specs/api/webhooks.md
  specs/billing/invoices.md
  ...
```

#### 6.5 `bd list --spec-changed`

**File:** `cmd/bd/list.go` (modify existing)

```bash
bd list --spec-changed                    # all beads with changed specs
bd list --spec-changed --spec "specs/auth/"  # filter by spec prefix
```

**Filter logic:** `WHERE spec_changed_at IS NOT NULL`

---

### Task 7: Show spec_changed_at in bd show

**File:** `cmd/bd/show.go` (modify existing)

When `spec_changed_at IS NOT NULL`, display:

```
○ bd-a1b2 · Implement login flow   [P1 · OPEN]
Spec: specs/auth/login.md
● SPEC CHANGED on 2026-01-28 — review may be needed
```

Use `●` (filled circle) prefix for the warning line. No emoji icons.

**Clearing the flag:**
```bash
bd update bd-a1b2 --ack-spec    # acknowledge spec change, clears flag
```

---

### Task 8: RPC Support

**Files:**
- `internal/rpc/protocol.go` — add SpecChangedAt to filters and spec commands
- `internal/rpc/server_spec.go` (new) — handlers for spec commands

**Protocol additions:**

```go
// List filter extension
type ListFilter struct {
    // ... existing fields ...
    SpecChanged *bool  // filter by spec_changed_at IS NOT NULL
}

// New RPC methods
type SpecScanRequest struct {
    Path string `json:"path"` // optional, defaults to "specs"
}

type SpecScanResponse struct {
    Added        int      `json:"added"`
    Updated      int      `json:"updated"`
    Unchanged    int      `json:"unchanged"`
    Missing      int      `json:"missing"`
    BeadsMarked  int      `json:"beads_marked"`
    ChangedSpecs []string `json:"changed_specs"`
}

type SpecListRequest struct {
    IncludeMissing bool   `json:"include_missing"`
    PathPrefix     string `json:"path_prefix"`
}

type SpecListResponse struct {
    Specs []SpecEntry `json:"specs"`
}

type SpecShowRequest struct {
    SpecID string `json:"spec_id"`
}

type SpecShowResponse struct {
    Spec   *SpecEntry `json:"spec"`
    Beads  []Issue    `json:"beads"`
}
```

**Behavior:**
- Spec commands work via daemon RPC (preferred) or direct DB access (fallback)
- `bd spec scan` requires daemon for background operation, falls back to direct if no daemon
- All spec commands support `--json` flag

---

## File Changes Summary

| File | Change |
|------|--------|
| **Migrations** | |
| `internal/storage/sqlite/migrations/042_spec_registry.go` | New |
| `internal/storage/sqlite/migrations/043_spec_changed_at.go` | New |
| **Storage Interface** | |
| `internal/storage/storage.go` | Add SpecRegistryStore interface |
| `internal/types/types.go` | Add SpecChangedAt *time.Time, SpecEntry type |
| **SQLite Backend** | |
| `internal/storage/sqlite/schema.go` | Add spec_registry table + schema probe |
| `internal/storage/sqlite/spec_registry.go` | New - SpecRegistryStore impl |
| `internal/storage/sqlite/transaction.go` | Add spec_changed_at to scanIssueRow |
| `internal/storage/sqlite/queries.go` | Add spec_changed_at to allowedUpdateFields |
| `internal/storage/sqlite/issues.go` | Add spec_changed_at to INSERT |
| `internal/storage/sqlite/ready.go` | Add spec_registry to schema validation |
| **Other Backends** | |
| `internal/storage/dolt/spec_registry.go` | New - mirror sqlite |
| `internal/storage/dolt/transaction.go` | Add spec_changed_at |
| `internal/storage/memory/spec_registry.go` | New - mirror sqlite |
| `internal/storage/memory/memory.go` | Add spec_changed_at |
| **Spec Package** | |
| `internal/spec/scanner.go` | New |
| `internal/spec/registry.go` | New |
| `internal/spec/scanner_test.go` | New |
| `internal/spec/registry_test.go` | New |
| **CLI** | |
| `cmd/bd/spec.go` | New (parent command) |
| `cmd/bd/spec_scan.go` | New |
| `cmd/bd/spec_list.go` | New |
| `cmd/bd/spec_show.go` | New |
| `cmd/bd/spec_coverage.go` | New |
| `cmd/bd/list.go` | Add --spec-changed flag |
| `cmd/bd/show.go` | Show spec_changed_at warning |
| `cmd/bd/update.go` | Add --ack-spec flag |
| **RPC** | |
| `internal/rpc/protocol.go` | Add SpecChanged filter, spec request/response types |
| `internal/rpc/server_spec.go` | New |

---

## Testing

### Unit Tests

```go
// scanner_test.go
func TestScanSpecs(t *testing.T)
func TestExtractTitle(t *testing.T)
func TestNormalizePath(t *testing.T)

// registry_test.go
func TestUpdateRegistry_NewSpecs(t *testing.T)
func TestUpdateRegistry_ChangedSpecs(t *testing.T)
func TestMarkChangedBeads(t *testing.T)

// migrations
func TestSpecRegistryMigration(t *testing.T)
func TestSpecChangedAtMigration(t *testing.T)
```

### Integration Tests

```bash
# Setup test specs
mkdir -p specs/auth
echo "# Login Flow" > specs/auth/login.md

# Scan
bd spec scan
bd spec list | grep "login.md"

# Create linked bead
bd create "Implement login" --spec-id "specs/auth/login.md"

# Modify spec and rescan
echo "# Login Flow v2" > specs/auth/login.md
bd spec scan

# Verify bead flagged
bd list --spec-changed | grep "Implement login"
bd show bd-xxxx | grep "Spec changed"
```

---

## Success Criteria

- [ ] SpecRegistryStore interface defined and implemented (sqlite/dolt/memory)
- [ ] Migrations 042 + 043 apply cleanly to existing DBs
- [ ] Schema probe validates spec_registry table
- [ ] `bd spec scan` discovers and hashes spec files
- [ ] Registry persists in SQLite with soft-delete support
- [ ] Non-file spec_ids (URLs, IDs) are skipped, not flagged as missing
- [ ] Changed specs trigger `spec_changed_at` on linked beads
- [ ] `bd list --spec-changed` filters correctly (IS NOT NULL)
- [ ] `bd show` displays spec change warning with `●` prefix
- [ ] `bd update --ack-spec` clears spec_changed_at
- [ ] `bd spec list/show/coverage` work via RPC and direct
- [ ] All commands support `--json` flag
- [ ] All existing tests pass
- [ ] New unit tests for scanner, registry, migrations

---

## Implementation Order

1. **Storage interface** — Add SpecRegistryStore to storage.go
2. **Migration 042** — spec_registry table
3. **Migration 043** — spec_changed_at column + schema probe updates
4. **SQLite backend** — Implement SpecRegistryStore for sqlite
5. **Other backends** — Mirror to dolt + memory
6. **Scanner** — file discovery + hashing + repo root resolution
7. **Registry** — update logic + bead marking
8. **CLI: spec scan** — wire it up with RPC
9. **CLI: spec list/show/coverage** — views
10. **CLI: list --spec-changed** — filter
11. **CLI: update --ack-spec** — clear flag
12. **Show warning** — display in bd show
13. **Tests** — unit + integration for each layer

---

## Open Questions (Resolved)

1. **Soft delete vs hard delete for missing specs?**
   ✅ Resolved: `missing_at` column included in schema. Soft delete preserves history.

2. **Auto-scan on daemon start?**
   ✅ Resolved: No for MVP. Manual `bd spec scan` only.

3. **Clear spec_changed_at flag?**
   ✅ Resolved: Three ways to clear:
   - `bd update <id> --ack-spec` — explicit acknowledgment
   - `bd update <id> --spec-id <new>` — changing spec_id
   - `bd close <id>` — closing the bead

4. **Time format (INTEGER vs DATETIME)?**
   ✅ Resolved: Use DATETIME (nullable) to match existing schema style.

5. **Non-file spec_ids (URLs, IDs)?**
   ✅ Resolved: Only treat spec_id as scannable if it's a repo-relative path without `://`.

---

## Phase 3: Fixes Required

Code review identified gaps that must be addressed before production use.

### Fix 1: spec_changed_at must update updated_at + emit event (HIGH)

**Problem:** `MarkSpecChangedBySpecIDs` only sets `spec_changed_at` without updating `updated_at` or creating an audit event. This means:
- Activity feeds miss spec change signals
- JSONL exports have stale `updated_at`
- No audit trail for spec drift detection

**Files:**
- `internal/storage/sqlite/spec_registry.go`
- `internal/storage/dolt/spec_registry.go`
- `internal/storage/memory/spec_registry.go`

**Fix:**
```sql
UPDATE issues
SET spec_changed_at = ?, updated_at = ?
WHERE spec_id IN (...)
```

Plus emit event:
```go
event := types.Event{
    IssueID:   issue.ID,
    Type:      "spec_changed",
    Actor:     "system",
    Timestamp: now,
    Data:      map[string]string{"spec_id": specID, "reason": "spec content hash changed"},
}
```

---

### Fix 2: Implement IsScannableSpecID filter (HIGH)

**Problem:** The spec defines linkage rules (skip URLs, SPEC-xxx IDs, absolute paths) but no code enforces them. All spec_ids get flagged regardless of type.

**File:** `internal/spec/scanner.go` (add new function)

**Implementation:**
```go
// IsScannableSpecID returns true if spec_id should be scanned as a file path.
// Returns false for:
//   - URLs (contains "://")
//   - Identifier patterns (starts with "SPEC-", "REQ-", etc.)
//   - Absolute paths (starts with "/")
//   - Empty strings
func IsScannableSpecID(specID string) bool {
    if specID == "" {
        return false
    }
    if strings.Contains(specID, "://") {
        return false // URL
    }
    if strings.HasPrefix(specID, "/") {
        return false // absolute path
    }
    // Common ID prefixes
    idPrefixes := []string{"SPEC-", "REQ-", "FEAT-", "US-", "STORY-"}
    upper := strings.ToUpper(specID)
    for _, prefix := range idPrefixes {
        if strings.HasPrefix(upper, prefix) {
            return false
        }
    }
    return true
}
```

**Usage:** Call in `MarkSpecChangedBySpecIDs` before marking issues:
```go
// Filter to only scannable spec_ids
var scannableIDs []string
for _, id := range specIDs {
    if IsScannableSpecID(id) {
        scannableIDs = append(scannableIDs, id)
    }
}
```

---

### Fix 3: Robust repo root resolution (MEDIUM)

**Problem:** Current code assumes repo root = parent of `.beads/`. Fails if `.beads/` is redirected.

**File:** `internal/spec/scanner.go`

**Implementation:**
```go
// FindRepoRoot locates the repository root directory.
// Priority:
//   1. Parent of .beads/ directory (walk up from cwd)
//   2. git rev-parse --show-toplevel
//   3. Error if neither found
func FindRepoRoot() (string, error) {
    // Try .beads/ first
    cwd, err := os.Getwd()
    if err != nil {
        return "", err
    }

    dir := cwd
    for {
        if _, err := os.Stat(filepath.Join(dir, ".beads")); err == nil {
            return dir, nil
        }
        parent := filepath.Dir(dir)
        if parent == dir {
            break
        }
        dir = parent
    }

    // Fallback to git
    cmd := exec.Command("git", "rev-parse", "--show-toplevel")
    out, err := cmd.Output()
    if err == nil {
        return strings.TrimSpace(string(out)), nil
    }

    return "", fmt.Errorf("cannot find repo root: no .beads/ directory and not a git repo")
}
```

---

### Fix 4: Add local-only warning for spec_registry (MEDIUM)

**Problem:** `spec_registry` table is NOT synced via JSONL/git. Teams may expect it to sync.

**Fix options:**

**Option A: CLI warning**
```
$ bd spec scan
✓ Scanned 42 specs (added=3 updated=2 missing=0 marked=5)
ℹ Note: Spec registry is local to this machine (not synced via git)
```

**Option B: Doc callout** in CLI_REFERENCE.md:
```markdown
> **Note:** The spec registry (`spec_registry` table) is local-only and not
> synced across machines via JSONL/git. Each developer's registry reflects
> their local spec scans. The `spec_changed_at` flag on issues IS synced.
```

**Recommendation:** Both. Warning on first scan, doc callout.

---

### Fix 5: Add missing tests (MEDIUM)

**Required test coverage:**

| Test | File | Coverage |
|------|------|----------|
| `TestIsScannableSpecID` | `scanner_test.go` | URL, ID, path, valid cases |
| `TestScanExtractsTitle` | `scanner_test.go` | H1 extraction from markdown |
| `TestScanComputesHash` | `scanner_test.go` | SHA256 consistency |
| `TestRegistryUpdate_Add` | `registry_test.go` | New specs added |
| `TestRegistryUpdate_Change` | `registry_test.go` | Hash change detected |
| `TestRegistryUpdate_Missing` | `registry_test.go` | Soft delete on missing |
| `TestMarkSpecChanged_UpdatesTimestamp` | `spec_registry_test.go` | updated_at set |
| `TestMarkSpecChanged_CreatesEvent` | `spec_registry_test.go` | Audit event emitted |
| `TestMarkSpecChanged_FiltersNonScannable` | `spec_registry_test.go` | URLs/IDs skipped |

---

## Implementation Order (Updated)

1. ~~Storage interface~~ ✅
2. ~~Migration 042~~ ✅
3. ~~Migration 043~~ ✅
4. ~~SQLite backend~~ ✅
5. ~~Other backends~~ ✅
6. ~~Scanner~~ ✅
7. ~~Registry~~ ✅
8. ~~CLI commands~~ ✅
9. **Fix 1: updated_at + events** ← Next
10. **Fix 2: IsScannableSpecID**
11. **Fix 3: Repo root resolution**
12. **Fix 4: Local-only warning**
13. **Fix 5: Tests**
14. Production ready
