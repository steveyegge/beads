# Spec Alignment + Pacman Phase 2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a spec↔bead↔code alignment report and complete Pacman Phase 2 with an assign command.

**Architecture:** Add a lightweight `bd spec align` command that reads spec registry counts and runs file-based code checks for known specs (starting with Pacman). Add an `bd assign` command that updates assignee via existing storage paths. Use TDD with focused unit/CLI tests.

**Tech Stack:** Go (cobra CLI), SQLite test helpers, existing spec registry storage.

---

### Task 1: Add tests for `bd assign`

**Files:**
- Create: `cmd/bd/assign_test.go`

**Step 1: Write failing test**

```go
func TestAssignCommandSetsAssignee(t *testing.T) {
    if testing.Short() { t.Skip("skipping slow CLI test in short mode") }
    tmpDir := setupCLITestDB(t)
    out := runBDInProcess(t, tmpDir, "create", "Task", "-p", "2", "--json")
    // parse issue id from JSON
    // run: bd assign <id> --to=alice
    // run: bd show <id> --json
    // assert assignee == "alice"
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestAssignCommandSetsAssignee`
Expected: FAIL (unknown command "assign" or assignee not set)

**Step 3: Write minimal implementation**

Add `cmd/bd/assign.go` with a cobra command that updates issue assignee via store.UpdateIssue.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestAssignCommandSetsAssignee`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/bd/assign.go cmd/bd/assign_test.go
# commit after completing all tasks
```

---

### Task 2: Add tests for spec alignment report code checks

**Files:**
- Create: `cmd/bd/spec_align_test.go`

**Step 1: Write failing test**

```go
func TestRunCodeChecks(t *testing.T) {
    tmpDir := t.TempDir()
    path := filepath.Join(tmpDir, "cmd", "bd", "pacman.go")
    os.MkdirAll(filepath.Dir(path), 0o755)
    os.WriteFile(path, []byte("assignCmd"), 0o644)
    checks := []codeCheck{{ID: "assign", File: "cmd/bd/pacman.go", Contains: "assignCmd"}}
    results := runCodeChecks(tmpDir, checks)
    if !results[0].Passed { t.Fatalf("expected pass") }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestRunCodeChecks`
Expected: FAIL (runCodeChecks undefined)

**Step 3: Write minimal implementation**

Implement `runCodeChecks` and `codeCheck` in `cmd/bd/spec_align.go`.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestRunCodeChecks`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/bd/spec_align.go cmd/bd/spec_align_test.go
# commit after completing all tasks
```

---

### Task 3: Implement `bd spec align` command

**Files:**
- Create: `cmd/bd/spec_align.go`
- Modify: `cmd/bd/spec.go`

**Step 1: Write failing test**

```go
func TestSpecAlignReportIncludesBeadCounts(t *testing.T) {
    if testing.Short() { t.Skip("skipping slow CLI test in short mode") }
    tmpDir := setupCLITestDB(t)
    os.MkdirAll(filepath.Join(tmpDir, "specs", "active"), 0o755)
    specPath := filepath.Join(tmpDir, "specs", "active", "PACMAN_MODE_SPEC.md")
    os.WriteFile(specPath, []byte("# Pacman"), 0o644)
    runBDInProcess(t, tmpDir, "spec", "scan")
    out := runBDInProcess(t, tmpDir, "spec", "align", "--json")
    if !strings.Contains(out, "PACMAN_MODE_SPEC.md") { t.Fatalf("expected spec in output") }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestSpecAlignReportIncludesBeadCounts`
Expected: FAIL (unknown command "align")

**Step 3: Write minimal implementation**

`bd spec align` reads `ListSpecRegistryWithCounts`, attaches code check results for known specs, and prints JSON or a simple list.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestSpecAlignReportIncludesBeadCounts`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/bd/spec_align.go cmd/bd/spec_align_test.go cmd/bd/spec.go
# commit after completing all tasks
```

---

### Task 4: Update Pacman spec to reflect Phase 2 completion

**Files:**
- Modify: `specs/active/PACMAN_MODE_SPEC.md`

**Step 1: Write the change**

Mark Phase 2 items as complete and keep Phase 3 status as-is.

**Step 2: Commit**

```bash
git add specs/active/PACMAN_MODE_SPEC.md
# commit after completing all tasks
```

---

### Task 5: Run tests

Run targeted tests:

```bash
go test ./cmd/bd -run TestAssignCommandSetsAssignee
Go test ./cmd/bd -run TestRunCodeChecks
Go test ./cmd/bd -run TestSpecAlignReportIncludesBeadCounts
```

(Optional) full suite (known baseline failure in internal/hooks may persist).

---

### Task 6: Close specs (if requested)

After validation, run `bd spec mark-done` for the spec(s) you want closed.

---

### Task 7: Documentation

If CLI changed, update `README.md` with the new `bd spec align` command.
