# Shadowbook - Next Session Tasks

**Status:** PR #1372 submitted to beads (Phase 1). Phase 2 ready for shadowbook extraction.
**Date:** 2026-01-28

## Context

### What's Done
- [x] Phase 1 (spec_id field) PR submitted to steveyegge/beads (#1372)
- [x] Comment posted on issue #976
- [x] Fork created at github.com/anupamchugh/beads
- [x] SHADOWBOOK_README.md written
- [x] Phase 2 code exists on `main` branch (spec registry, scanning, change detection)

### What's Pending
- [ ] Fix Phase 2 code issues (5 fixes from code review)
- [ ] Create shadowbook repo
- [ ] Extract Phase 2 code to shadowbook
- [ ] Set up homebrew tap
- [ ] Test end-to-end

---

## Task 1: Fix Phase 2 Code Issues

The code review identified 5 issues that must be fixed before shadowbook release.

### 1.1 Fix updated_at + events (HIGH)

**Problem:** `MarkSpecChangedBySpecIDs` only sets `spec_changed_at` without updating `updated_at` or creating audit events.

**Files:**
- `internal/storage/sqlite/spec_registry.go`
- `internal/storage/dolt/spec_registry.go`
- `internal/storage/memory/spec_registry.go`

**Fix:**
```go
// In MarkSpecChangedBySpecIDs
query := `UPDATE issues
          SET spec_changed_at = ?, updated_at = ?
          WHERE spec_id IN (%s)`

// Also emit event for audit trail
event := types.Event{
    IssueID:   issueID,
    Type:      "spec_changed",
    Actor:     "system",
    Timestamp: now,
}
```

### 1.2 Implement IsScannableSpecID (HIGH)

**Problem:** No filtering for URLs, SPEC-xxx IDs, absolute paths.

**File:** `internal/spec/scanner.go`

**Add:**
```go
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
    idPrefixes := []string{"SPEC-", "REQ-", "FEAT-", "US-"}
    upper := strings.ToUpper(specID)
    for _, prefix := range idPrefixes {
        if strings.HasPrefix(upper, prefix) {
            return false
        }
    }
    return true
}
```

**Usage:** Call before marking beads in `MarkSpecChangedBySpecIDs`.

### 1.3 Robust repo root resolution (MEDIUM)

**Problem:** Assumes repo root = parent of `.beads/`.

**File:** `internal/spec/scanner.go`

**Add:**
```go
func FindRepoRoot() (string, error) {
    // Try .beads/ first
    cwd, _ := os.Getwd()
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
    return "", fmt.Errorf("cannot find repo root")
}
```

### 1.4 Add local-only warning (MEDIUM)

**Problem:** Users may expect spec_registry to sync via git.

**Fix in `cmd/bd/spec.go` (scan command):**
```go
fmt.Println("ℹ Note: Spec registry is local (not synced via git)")
```

**Also add to docs/CLI_REFERENCE.md.**

### 1.5 Add missing tests (MEDIUM)

**Required tests:**

| Test | File |
|------|------|
| `TestIsScannableSpecID` | `internal/spec/scanner_test.go` |
| `TestFindRepoRoot` | `internal/spec/scanner_test.go` |
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
# 1. Switch to main branch (has Phase 2 code)
git checkout main

# 2. Apply the 5 fixes
# - Fix 1.1: updated_at + events
# - Fix 1.2: IsScannableSpecID
# - Fix 1.3: FindRepoRoot
# - Fix 1.4: local-only warning
# - Fix 1.5: tests

# 3. Test locally
go build ./cmd/bd
./bd spec scan
./bd spec list

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
