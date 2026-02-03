# Workspace Hub Scan Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Finish `bd workspace scan` to match the workspace-hub integration spec (preview/apply/update, triage routing, optional bead creation).

**Architecture:** Keep scanning and note generation in `cmd/bd/workspace.go`. Add deterministic status detection based on note timestamps and a small “report” writer under `workspace-hub/reports/`. Route notes to inbox/triage/active based on decision rules and bead presence. Optional bead creation uses existing bd create APIs in the target repo.

**Tech Stack:** Go (cobra CLI), filesystem I/O, existing bd storage APIs.

---

### Task 1: Add tests for scan classification and note routing

**Files:**
- Create: `cmd/bd/workspace_test.go`

**Step 1: Write the failing test**

```go
func TestWorkspaceScanRoutesAndStatuses(t *testing.T) {
    // Build a temp workspace with two projects and a workspace-hub
    // Project A has specs/alpha.md (new)
    // Project B has docs/readme.md with an existing hub note (updated)
    // Expect: status new/updated and routing to inbox/triage based on rules
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestWorkspaceScanRoutesAndStatuses -v`
Expected: FAIL with missing functions or wrong statuses.

**Step 3: Write minimal implementation**

- Add helpers in `cmd/bd/workspace.go`:
  - `classifyItemStatus(srcPath, notePath) (status string)`
  - `decideHubBucket(item WorkspaceScanItem) string`

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestWorkspaceScanRoutesAndStatuses -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/bd/workspace_test.go cmd/bd/workspace.go
git commit -m "test: add workspace scan routing coverage"
```

---

### Task 2: Implement “updated” detection and bucket routing

**Files:**
- Modify: `cmd/bd/workspace.go`

**Step 1: Write the failing test**

```go
func TestWorkspaceScanDetectsUpdatedNotes(t *testing.T) {
    // note mtime older than source file -> status = updated
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestWorkspaceScanDetectsUpdatedNotes -v`
Expected: FAIL (status returns unchanged).

**Step 3: Write minimal implementation**

- In `classifyItemStatus`, compare `os.Stat(src).ModTime()` to note’s `ModTime()`.
- `updated` if src is newer, `unchanged` if note newer or equal.
- Route to buckets:
  - `active` if bead exists (future-proofed placeholder)
  - `triage` for docs/blogs, `inbox` for specs/readme by default

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestWorkspaceScanDetectsUpdatedNotes -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/bd/workspace.go cmd/bd/workspace_test.go
git commit -m "feat: classify updated items and route hub notes"
```

---

### Task 3: Write report output to workspace-hub/reports

**Files:**
- Modify: `cmd/bd/workspace.go`

**Step 1: Write the failing test**

```go
func TestWorkspaceScanWritesReport(t *testing.T) {
    // After apply, expect a report file under reports/ with a summary header
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestWorkspaceScanWritesReport -v`
Expected: FAIL (no report file).

**Step 3: Write minimal implementation**

- Add `writeScanReport(result)` to create `workspace-hub/reports/scan_content_YYYY-MM-DD_HHMMSS.md`.
- Include summary counts and a list of items.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestWorkspaceScanWritesReport -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/bd/workspace.go cmd/bd/workspace_test.go
git commit -m "feat: write workspace scan report"
```

---

### Task 4: Implement optional bead creation

**Files:**
- Modify: `cmd/bd/workspace.go`

**Step 1: Write the failing test**

```go
func TestWorkspaceScanCreateBeadsFlag(t *testing.T) {
    // With --create-beads, ensure bead creation stub is called for items
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestWorkspaceScanCreateBeadsFlag -v`
Expected: FAIL.

**Step 3: Write minimal implementation**

- Create a helper that shells out to `bd create` in the target repo directory, or calls existing internal creation if available.
- Only run when `--apply --create-beads`.
- Update hub note to include bead ID.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestWorkspaceScanCreateBeadsFlag -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/bd/workspace.go cmd/bd/workspace_test.go
git commit -m "feat: create beads from workspace scan"
```

---

### Task 5: Update docs and usage examples

**Files:**
- Modify: `specs/ideas/WORKSPACE_HUB_INTEGRATION_SPEC.md`
- Modify: `README.md` (if the command is promoted)

**Step 1: Update spec with final behavior and defaults**

**Step 2: Run targeted tests**

Run: `go test ./cmd/bd -run TestWorkspaceScan -v`
Expected: PASS.

**Step 3: Commit**

```bash
git add specs/ideas/WORKSPACE_HUB_INTEGRATION_SPEC.md README.md
git commit -m "docs: finalize workspace scan behavior"
```

---

### Task 6: Full test pass

**Step 1: Run full tests**

Run: `go test ./...`
Expected: PASS.

**Step 2: Commit final cleanup (if needed)**

```bash
git add -A
git commit -m "chore: finalize workspace scan"
```
