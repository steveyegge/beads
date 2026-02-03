# Spec Radar Sync Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add spec-radar commands and spec↔beads sync to the Shadowbook CLI, with tests and reports.

**Architecture:** Extend spec scanning to capture git status, persist it in the spec registry, and surface it in list/show. Add new `bd spec` subcommands for staleness, ideas triage, duplicates, delta, report, and sync. Store delta snapshots in `.beads/spec_scan_cache.json`, and apply registry updates with explicit confirmation.

**Tech Stack:** Go 1.24+, Cobra CLI, SQLite/Dolt/memory storage, internal/spec helpers.

---

### Task 1: Add git status to spec scan and registry

**Files:**
- Modify: `internal/spec/types.go`
- Modify: `internal/spec/scanner.go`
- Modify: `internal/spec/registry.go`
- Modify: `internal/storage/sqlite/spec_registry.go`
- Modify: `internal/storage/dolt/spec_registry.go`
- Modify: `internal/storage/memory/spec_registry.go`
- Modify: `internal/storage/sqlite/migrations/042_spec_registry.go`
- Modify: `internal/spec/registry_test.go`
- Modify: `internal/storage/sqlite/spec_registry_test.go`

**Step 1: Write the failing test**

Add a registry test that expects `GitStatus` to persist.

```go
func TestRegistryTracksGitStatus(t *testing.T) {
	ctx := context.Background()
	m := &mockStore{}
	now := time.Now().UTC().Truncate(time.Second)
	scanned := []ScannedSpec{{
		SpecID: "specs/a.md",
		Path:   "specs/a.md",
		Title:  "A",
		SHA256: "hash",
		Mtime:  now,
		GitStatus: "modified",
	}}
	_, err := UpdateRegistry(ctx, m, scanned, now)
	if err != nil {
		t.Fatalf("UpdateRegistry failed: %v", err)
	}
	if got := m.entries[0].GitStatus; got != "modified" {
		t.Fatalf("GitStatus = %q", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/spec -run TestRegistryTracksGitStatus -v`
Expected: FAIL (field missing)

**Step 3: Write minimal implementation**

- Add `GitStatus` to `ScannedSpec` and `SpecRegistryEntry`.
- In `Scan`, compute git status via `git status --porcelain -- <path>`, map to `tracked|untracked|modified`.
- Pass `GitStatus` through `UpdateRegistry` upserts.
- Add `git_status` column with default `tracked` in SQLite migration and include it in storage adapters.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/spec -run TestRegistryTracksGitStatus -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/spec/types.go internal/spec/scanner.go internal/spec/registry.go \
  internal/storage/sqlite/spec_registry.go internal/storage/dolt/spec_registry.go \
  internal/storage/memory/spec_registry.go internal/storage/sqlite/migrations/042_spec_registry.go \
  internal/spec/registry_test.go internal/storage/sqlite/spec_registry_test.go

git commit -m "feat(spec): track git status in registry"
```

---

### Task 2: Add `bd spec stale`

**Files:**
- Create: `cmd/bd/spec_stale.go`
- Modify: `cmd/bd/spec.go`
- Modify: `cmd/bd/spec_stale_test.go`

**Step 1: Write the failing test**

Add a CLI test that seeds registry entries with controlled mtimes and asserts bucket counts.

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestSpecStaleBuckets -v`
Expected: FAIL (command missing)

**Step 3: Write minimal implementation**

- Add `spec stale` cobra command under `specCmd`.
- Load registry entries, compute `age_days` from `mtime` (or `last_scanned_at`).
- Group into buckets and render counts.
- Add `--limit` to cap per-bucket listing.
- Support JSON output.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestSpecStaleBuckets -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/bd/spec_stale.go cmd/bd/spec.go cmd/bd/spec_stale_test.go

git commit -m "feat(spec): add staleness buckets command"
```

---

### Task 3: Add `bd spec triage`

**Files:**
- Create: `cmd/bd/spec_triage.go`
- Modify: `cmd/bd/spec.go`
- Modify: `cmd/bd/spec_triage_test.go`

**Step 1: Write the failing test**

Add a CLI test that includes ideas and non-ideas specs and asserts filtering and sort order.

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestSpecTriage -v`
Expected: FAIL

**Step 3: Write minimal implementation**

- Filter to `specs/ideas/` entries.
- Show path, git status, age, bead count.
- Add `--sort age|status` and `--limit`.
- Support JSON output.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestSpecTriage -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/bd/spec_triage.go cmd/bd/spec.go cmd/bd/spec_triage_test.go

git commit -m "feat(spec): add ideas triage command"
```

---

### Task 4: Add duplicate hints

**Files:**
- Create: `internal/spec/similarity.go`
- Create: `cmd/bd/spec_duplicates.go`
- Modify: `cmd/bd/spec.go`
- Modify: `internal/spec/similarity_test.go`
- Modify: `cmd/bd/spec_duplicates_test.go`

**Step 1: Write the failing test**

Create a similarity unit test with two near-duplicate titles and a low-similarity control.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/spec -run TestFindDuplicates -v`
Expected: FAIL

**Step 3: Write minimal implementation**

- Normalize title + summary, tokenize, compute Jaccard similarity.
- Return pairs above threshold (default 0.85).
- `bd spec duplicates` renders pairs with similarity.
- Support JSON output.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/spec -run TestFindDuplicates -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/spec/similarity.go internal/spec/similarity_test.go \
  cmd/bd/spec_duplicates.go cmd/bd/spec_duplicates_test.go cmd/bd/spec.go

git commit -m "feat(spec): add duplicate hints"
```

---

### Task 5: Add delta report

**Files:**
- Create: `internal/spec/delta.go`
- Create: `cmd/bd/spec_delta.go`
- Modify: `cmd/bd/spec.go`
- Modify: `internal/spec/delta_test.go`
- Modify: `cmd/bd/spec_delta_test.go`

**Step 1: Write the failing test**

Add a unit test for `ComputeDelta` with added/removed/changed specs.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/spec -run TestComputeDelta -v`
Expected: FAIL

**Step 3: Write minimal implementation**

- Compare by `SpecID` and detect changes in `Title`, `Lifecycle`, `SHA256`, `Mtime`.
- Load/store cache at `.beads/spec_scan_cache.json`.
- `bd spec delta` reports diff and writes the new cache.
- Support JSON output.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/spec -run TestComputeDelta -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/spec/delta.go internal/spec/delta_test.go \
  cmd/bd/spec_delta.go cmd/bd/spec_delta_test.go cmd/bd/spec.go

git commit -m "feat(spec): add delta report"
```

---

### Task 6: Add unified report

**Files:**
- Create: `cmd/bd/spec_report.go`
- Modify: `cmd/bd/spec.go`
- Modify: `cmd/bd/spec_report_test.go`

**Step 1: Write the failing test**

Create a CLI test that runs `bd spec report --format md --out <tmp>` and asserts the file exists.

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestSpecReport -v`
Expected: FAIL

**Step 3: Write minimal implementation**

- Compose summary, triage, staleness, duplicates, delta, and volatility sections.
- Output Markdown to `--out` or stdout.
- Support `--format json`.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestSpecReport -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/bd/spec_report.go cmd/bd/spec_report_test.go cmd/bd/spec.go

git commit -m "feat(spec): add report command"
```

---

### Task 7: Add `bd spec sync` with confirmation

**Files:**
- Create: `cmd/bd/spec_sync.go`
- Modify: `cmd/bd/spec.go`
- Modify: `cmd/bd/spec_sync_test.go`

**Step 1: Write the failing test**

Add a CLI test that seeds issues with a spec ID and verifies a dry-run suggestion.

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestSpecSyncDryRun -v`
Expected: FAIL

**Step 3: Write minimal implementation**

- Compute lifecycle per spec from open/closed beads.
- Default to preview; apply only with `--apply`.
- Require confirmation prompt unless `--yes` or `--json`.
- Update registry entries via `UpdateSpecRegistry`.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestSpecSyncDryRun -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/bd/spec_sync.go cmd/bd/spec_sync_test.go cmd/bd/spec.go

git commit -m "feat(spec): add spec sync command"
```

---

### Task 8: Documentation updates

**Files:**
- Modify: `README.md`

**Step 1: Write the failing test**

Not applicable.

**Step 2: Update docs**

Add new `bd spec` commands to the CLI table and note `bd spec sync` behavior and confirmation.

**Step 3: Commit**

```bash
git add README.md

git commit -m "docs: document spec radar commands"
```

---

### Task 9: Final verification

**Files:**
- None

**Step 1: Run targeted tests**

Run: `go test ./internal/spec -run TestRegistryTracksGitStatus|TestFindDuplicates|TestComputeDelta -v`
Expected: PASS

Run: `go test ./cmd/bd -run TestSpecStaleBuckets|TestSpecTriage|TestSpecDuplicates|TestSpecDelta|TestSpecReport|TestSpecSyncDryRun -v`
Expected: PASS

**Step 2: Note baseline failures**

Full `go test ./...` currently fails in `cmd/bd` and `internal/hooks` in this repo. Do not claim full green unless those are fixed.

**Step 3: Commit if needed**

No commit unless additional fixes were made.
