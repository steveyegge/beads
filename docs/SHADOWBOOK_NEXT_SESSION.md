# Shadowbook - Next Session Tasks

**Status:** PR #1372 submitted to beads (Phase 1). Phase 2 implemented on main, needs fixes.
**Date:** 2026-01-28

## Current State (What's Already Implemented)

Phase 2 code is **merged on main** and working:

| Component | File | Status |
|-----------|------|--------|
| Scanner | `internal/spec/scanner.go` | ✓ Scan(), ExtractTitle(), hashFile() |
| Registry types | `internal/spec/types.go` | ✓ SpecRegistryEntry, ScannedSpec |
| Registry logic | `internal/spec/registry.go` | ✓ UpdateRegistry() |
| SQLite storage | `internal/storage/sqlite/spec_registry.go` | ✓ All CRUD + MarkSpecChangedBySpecIDs |
| CLI commands | `cmd/bd/spec.go` | ✓ scan, list, show, coverage |
| RPC layer | `internal/rpc/server_spec.go` | ✓ Daemon support |

**What works now:**
```bash
bd spec scan          # Scans specs/, updates registry
bd spec list          # Shows specs with bead counts
bd spec show <id>     # Shows spec + linked beads
bd spec coverage      # Coverage metrics
```

## What's Pending

- [x] Apply 5 code quality fixes (below) - **ALL DONE**
- [ ] Create shadowbook repo (or decide to keep as beads fork)
- [ ] Set up homebrew tap
- [ ] Test end-to-end

---

## Task 1: Code Quality Fixes

Code review identified 5 issues. Current implementation in `internal/storage/sqlite/spec_registry.go:248-299`.

### 1.1 Fix updated_at + events (HIGH)

**Problem:** `MarkSpecChangedBySpecIDs` only sets `spec_changed_at` without updating `updated_at` or creating audit events.

**File:** `internal/storage/sqlite/spec_registry.go` (line 269)

**Current code:**
```go
query := fmt.Sprintf(`UPDATE issues SET spec_changed_at = ? WHERE spec_id IN (%s)`, placeholders)
```

**Fix:**
```go
query := fmt.Sprintf(`UPDATE issues SET spec_changed_at = ?, updated_at = ? WHERE spec_id IN (%s)`, placeholders)
// args needs second timestamp
```

**Event decision:** Either:
- (a) Add `EventSpecChanged EventType = "spec_changed"` to `internal/types/types.go:844` and emit it, OR
- (b) Just update timestamps (simpler, events visible via updated_at change)

Recommend (b) for now - events can be added later if needed.

### 1.2 Implement IsScannableSpecID (HIGH)

**Problem:** No filtering for URLs, SPEC-xxx IDs, absolute paths. These shouldn't trigger spec scanning.

**Where to enforce:** Two places:
1. **`internal/spec/registry.go`** - in `UpdateRegistry()` when building the list of changed specs to mark
2. **`cmd/bd/spec.go`** or **issue validation** - when a spec_id is set on an issue (optional, defensive)

**Add to `internal/spec/scanner.go`:**
```go
// IsScannableSpecID returns true if spec_id refers to a local file path
// (vs URLs, external IDs like SPEC-001, or absolute paths).
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
    idPrefixes := []string{"SPEC-", "REQ-", "FEAT-", "US-", "JIRA-"}
    upper := strings.ToUpper(specID)
    for _, prefix := range idPrefixes {
        if strings.HasPrefix(upper, prefix) {
            return false // external ID
        }
    }
    return true
}
```

**Usage:** Call in `registry.go` UpdateRegistry() before adding to `changedSpecIDs`:
```go
if !IsScannableSpecID(specID) {
    continue
}
```

### 1.3 Robust repo root resolution (MEDIUM)

**Problem:** CLI direct mode uses `filepath.Dir(beadsDir)` which works, but caller sites should use a consistent helper.

**Current code** (`cmd/bd/spec.go:70-74`):
```go
beadsDir := beads.FindBeadsDir()
if beadsDir == "" {
    FatalErrorRespectJSON("no .beads directory found")
}
repoRoot := filepath.Dir(beadsDir)
```

**Fix:** This is actually fine. The `beads.FindBeadsDir()` already walks up looking for `.beads/`. The issue is consistency - consider extracting to a helper if used elsewhere.

**No code change needed** unless we want a dedicated `FindRepoRoot()` for clarity.

### 1.4 Add local-only warning (MEDIUM)

**Problem:** Users may expect spec_registry to sync via git.

**Fix in `cmd/bd/spec.go` (scan command output, line 91-92):**
```go
fmt.Printf("%s Scanned %d specs (added=%d updated=%d missing=%d marked=%d)\n",
    ui.RenderPass("✓"), result.Scanned, result.Added, result.Updated, result.Missing, result.MarkedBeads)
fmt.Println("● Note: Spec registry is local-only (not synced via git)")
```

Use `●` (allowed symbol) not `ℹ` (emoji).

**Also add to docs/CLI_REFERENCE.md** in the spec commands section.

### 1.5 Add missing tests (MEDIUM)

**Required tests:**

| Test | File |
|------|------|
| `TestIsScannableSpecID` | `internal/spec/scanner_test.go` |
| `TestScanExtractsTitle` | `internal/spec/scanner_test.go` |
| `TestRegistryUpdate_Add` | `internal/spec/registry_test.go` |
| `TestRegistryUpdate_Change` | `internal/spec/registry_test.go` |
| `TestMarkSpecChanged_UpdatesTimestamp` | `internal/storage/sqlite/spec_registry_test.go` |

---

## Task 2: Create Shadowbook Repo

### 2.1 Create repo

```bash
gh repo create anupamchugh/shadowbook --public --description "Keep your specs and code in sync"
```

### 2.2 Structure

```
shadowbook/
├── cmd/
│   └── shadowbook/
│       └── main.go          # Or extend bd
├── internal/
│   ├── spec/
│   │   ├── scanner.go       # From beads
│   │   ├── registry.go      # From beads
│   │   └── store.go         # From beads
│   └── storage/
│       └── ...              # Subset of beads storage
├── docs/
│   ├── SPEC_SYNC.md
│   └── CLI_REFERENCE.md
├── README.md                 # From SHADOWBOOK_README.md
├── LICENSE
└── go.mod
```

### 2.3 Decision: Standalone vs Extension

**Option A: Standalone CLI**
- Separate `shadowbook` binary
- Uses beads as library dependency
- Commands: `shadowbook scan`, `shadowbook list`, etc.

**Option B: Beads Extension**
- Fork beads entirely
- Keep `bd` command with `bd spec` subcommands
- Market as "beads + spec intelligence"

**Recommendation:** Option B for now (simpler). Can extract to standalone later.

---

## Task 3: Homebrew Tap

### 3.1 Create tap repo

```bash
gh repo create anupamchugh/homebrew-shadowbook --public
```

### 3.2 Create formula

**File:** `Formula/shadowbook.rb`

```ruby
class Shadowbook < Formula
  desc "Keep your specs and code in sync - spec intelligence for beads"
  homepage "https://github.com/anupamchugh/shadowbook"
  url "https://github.com/anupamchugh/shadowbook/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "COMPUTE_AFTER_RELEASE"
  license "MIT"

  depends_on "go" => :build
  depends_on "beads"  # or bundle beads

  def install
    system "go", "build", "-o", bin/"shadowbook", "./cmd/shadowbook"
  end

  test do
    system "#{bin}/shadowbook", "version"
  end
end
```

### 3.3 Installation

```bash
brew tap anupamchugh/shadowbook
brew install shadowbook
```

---

## Task 4: End-to-End Testing

### 4.1 Test scenario

```bash
# Setup
mkdir test-project && cd test-project
git init
bd init
mkdir -p specs

# Create spec
echo "# Login Feature" > specs/login.md

# Scan
bd spec scan
bd spec list  # should show specs/login.md

# Link bead
bd create "Implement login" --spec-id "specs/login.md"

# Verify linkage
bd spec show specs/login.md  # should show linked bead
bd list --spec "specs/"      # should show the bead

# Change spec
echo "# Login Feature v2" > specs/login.md
bd spec scan  # should detect change

# Verify change detection
bd list --spec-changed  # should show the bead
bd show <id>            # should show SPEC CHANGED warning

# Acknowledge
bd update <id> --ack-spec
bd list --spec-changed  # should be empty

# Coverage
bd spec coverage
```

### 4.2 Edge cases to test

- [ ] URL spec_id (should not be scanned)
- [ ] SPEC-001 identifier (should not be scanned)
- [ ] Missing spec file (should soft-delete)
- [ ] Spec reappears (should clear missing_at)
- [ ] Multiple beads per spec
- [ ] Nested spec directories

---

## Task 5: Release Checklist

- [ ] All 5 fixes implemented
- [ ] Tests passing
- [ ] README updated with final install instructions
- [ ] Version tagged (v0.1.0)
- [ ] Homebrew formula created
- [ ] Release notes written

---

## Files Reference

| File | Purpose | Location |
|------|---------|----------|
| Phase 2 code | Spec registry, scanning | `main` branch |
| SHADOWBOOK_README.md | Marketing README | Repo root |
| docs/SPEC_SYNC.md | Technical spec | docs/ |
| docs/PR_BEADS_SPEC_ID.md | Beads PR reference | docs/ |

---

## Quick Start for Next Session

```bash
# 1. You're already on main with Phase 2 code working
git status

# 2. Apply the fixes (priority order)
# HIGH:
# - Fix 1.1: Add updated_at to MarkSpecChangedBySpecIDs query
# - Fix 1.2: Add IsScannableSpecID() and use in registry.go
# MEDIUM:
# - Fix 1.4: Add local-only warning with ● symbol
# - Fix 1.5: Add tests

# 3. Test locally
go build ./cmd/bd
./bd spec scan
./bd spec list
go test ./internal/spec/... ./internal/storage/sqlite/...

# 4. Create shadowbook repo (if going standalone)
# OR keep as beads fork

# 5. Set up homebrew tap

# 6. Release
```

---

## Success Criteria

- [ ] `bd spec scan` works with all fixes
- [ ] Non-scannable spec_ids are skipped
- [ ] Change detection updates `updated_at`
- [ ] Tests pass
- [ ] Installable via `brew install`
- [ ] README accurate and complete
