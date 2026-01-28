# Shadowbook Engineering Spec

**Owner:** Engineering Team
**Status:** Ready for implementation
**Date:** 2026-01-28

---

## Overview

Shadowbook is a spec-tracking extension for beads. It scans markdown spec files, tracks changes via SHA256 hashes, and marks linked beads when their specs change.

**Current state:** Core functionality implemented on `main` branch. Needs test fixes, repo setup, and distribution.

---

## Task 1: Fix Failing Tests

**Priority:** HIGH
**Estimate:** 1-2 hours

### 1.1 Fix `internal/spec/registry_test.go`

The mock store is missing interface methods. Add:

```go
func (m *mockStore) GetSpecRegistry(ctx context.Context, specID string) (*SpecRegistryEntry, error) {
    for _, e := range m.entries {
        if e.SpecID == specID {
            return &e, nil
        }
    }
    return nil, nil
}

func (m *mockStore) ListSpecRegistryWithCounts(ctx context.Context) ([]SpecRegistryCount, error) {
    var results []SpecRegistryCount
    for _, e := range m.entries {
        results = append(results, SpecRegistryCount{Spec: e})
    }
    return results, nil
}
```

### 1.2 Run and verify all tests pass

```bash
go test ./internal/spec/... -v
go test ./... 2>&1 | grep -E '(PASS|FAIL|ok)'
```

**Acceptance:** All spec package tests pass.

---

## Task 2: Create Shadowbook Repository

**Priority:** HIGH
**Estimate:** 2-3 hours

### Option A: Standalone Repo (Recommended for marketing)

```bash
gh repo create anupamchugh/shadowbook --public \
  --description "Keep your specs and code in sync"
```

**Structure:**
```
shadowbook/
├── cmd/shadowbook/main.go     # CLI entry point
├── internal/
│   ├── spec/                  # Copy from beads
│   │   ├── scanner.go
│   │   ├── registry.go
│   │   ├── store.go
│   │   └── types.go
│   └── storage/sqlite/        # Minimal subset
├── go.mod                     # github.com/anupamchugh/shadowbook
├── README.md                  # From SHADOWBOOK_README.md
└── LICENSE                    # MIT
```

**Dependencies:** Import beads as library OR copy minimal storage code.

### Option B: Keep as Beads Fork (Simpler)

Keep using `github.com/anupamchugh/beads` with `bd spec` commands.

**Decision criteria:**
- Standalone = better for marketing/adoption
- Fork = simpler maintenance, faster to ship

---

## Task 3: Create Homebrew Tap

**Priority:** MEDIUM
**Estimate:** 1-2 hours

### 3.1 Create tap repository

```bash
gh repo create anupamchugh/homebrew-shadowbook --public
```

### 3.2 Create formula

**File:** `Formula/shadowbook.rb`

```ruby
class Shadowbook < Formula
  desc "Keep your specs and code in sync"
  homepage "https://github.com/anupamchugh/shadowbook"
  url "https://github.com/anupamchugh/shadowbook/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "COMPUTE_AFTER_TAGGING"
  license "MIT"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w"), "./cmd/shadowbook"
  end

  test do
    system "#{bin}/shadowbook", "version"
  end
end
```

### 3.3 Release workflow

```bash
# Tag release
git tag v0.1.0
git push origin v0.1.0

# Compute SHA256
curl -sL https://github.com/anupamchugh/shadowbook/archive/refs/tags/v0.1.0.tar.gz | shasum -a 256

# Update formula with SHA256
# Push to homebrew-shadowbook repo
```

### 3.4 Installation command

```bash
brew tap anupamchugh/shadowbook
brew install shadowbook
```

---

## Task 4: End-to-End Test Script

**Priority:** HIGH
**Estimate:** 1 hour

Create `scripts/e2e-test.sh`:

```bash
#!/bin/bash
set -e

echo "=== Shadowbook E2E Test ==="

# Setup
TESTDIR=$(mktemp -d)
cd "$TESTDIR"
git init
bd init --prefix test

# Create spec
mkdir -p specs
echo "# Login Feature" > specs/login.md
echo "User can log in with email/password" >> specs/login.md

# Scan
bd spec scan
bd spec list | grep -q "specs/login.md" || { echo "FAIL: spec not found"; exit 1; }

# Create linked bead
bd create "Implement login" --spec-id "specs/login.md"
BEAD_ID=$(bd list --json | jq -r '.[0].id')

# Verify linkage
bd spec show specs/login.md | grep -q "$BEAD_ID" || { echo "FAIL: bead not linked"; exit 1; }

# Change spec
echo "# Login Feature v2" > specs/login.md
echo "Updated requirements" >> specs/login.md
bd spec scan

# Verify change detection
bd show "$BEAD_ID" | grep -qi "spec.*changed" || { echo "FAIL: change not detected"; exit 1; }

# Cleanup
rm -rf "$TESTDIR"
echo "=== All tests passed ==="
```

**Edge cases to add:**
- URL spec_id (should not scan)
- SPEC-001 identifier (should not scan)
- Missing spec file (should mark missing)
- Nested spec directories

---

## Task 5: Documentation

**Priority:** MEDIUM
**Estimate:** 1 hour

### 5.1 Update CLI_REFERENCE.md

Add to `docs/CLI_REFERENCE.md`:

```markdown
## Spec Commands

### bd spec scan [path]

Scan specs directory and update registry.

- Default path: `specs/`
- Detects new, changed, and missing specs
- Marks linked beads when their spec changes

**Note:** Spec registry is local-only (not synced via git).

### bd spec list

List all specs in registry with bead counts.

Flags:
- `--prefix` - Filter by spec ID prefix
- `--include-missing` - Include deleted specs

### bd spec show <spec_id>

Show spec details and linked beads.

### bd spec coverage

Show spec coverage metrics.
```

### 5.2 README for shadowbook repo

Copy and adapt `SHADOWBOOK_README.md` from repo root.

---

## Task 6: Release Checklist

**Priority:** HIGH

```
[ ] All tests passing (go test ./...)
[ ] E2E test script passes
[ ] Repo created (standalone or fork decision made)
[ ] README accurate
[ ] Version tagged (v0.1.0)
[ ] Homebrew formula created and tested
[ ] Release notes written
[ ] Announce in beads issue #976
```

---

## Files Reference

| File | Purpose |
|------|---------|
| `internal/spec/scanner.go` | Scan(), ExtractTitle(), IsScannableSpecID() |
| `internal/spec/registry.go` | UpdateRegistry() |
| `internal/spec/store.go` | SpecRegistryStore interface |
| `internal/spec/types.go` | Data types |
| `internal/storage/sqlite/spec_registry.go` | SQLite implementation |
| `cmd/bd/spec.go` | CLI commands |
| `SHADOWBOOK_README.md` | Marketing README |

---

## Quick Commands

```bash
# Build
go build ./cmd/bd

# Test
go test ./internal/spec/... -v
go test ./... 2>&1 | grep -E '(PASS|FAIL)'

# Manual test
./bd spec scan
./bd spec list
./bd spec show specs/example.md

# Run E2E
./scripts/e2e-test.sh
```

---

## Contact

Questions: Check `docs/SPEC_SYNC.md` for technical details or comment on beads issue #976.
