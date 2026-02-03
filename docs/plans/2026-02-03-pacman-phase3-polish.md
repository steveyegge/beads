# Pacman Phase 3 Polish Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Pacman Phase 3 polish: achievements, improved ASCII art, and `bd pacman --global` improvements.

**Architecture:** Extend pacman state aggregation to compute achievements and render them in both single-project and global views. Add a small achievement engine using existing scoreboard/agents data. Keep storage file-based under `.beads/` and reuse existing pacman rendering helpers.

**Tech Stack:** Go (cobra CLI), file-based JSON under `.beads/`, existing pacman code in `cmd/bd/pacman.go`.

---

### Task 1: Achievements data model + tests

**Files:**
- Modify: `cmd/bd/pacman.go`
- Test: `cmd/bd/pacman_test.go` (new)

**Step 1: Write failing test**

```go
func TestPacmanAchievements(t *testing.T) {
    tmpDir := t.TempDir()
    // create scoreboard.json with dots and a pause file
    // create issues to simulate blocked/unblocked
    // call computeAchievements(...) and assert awards
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestPacmanAchievements`
Expected: FAIL (function not implemented)

**Step 3: Write minimal implementation**

Add `computeAchievements(...)` that returns:
- first-blood (first dot closed)
- streak-5 (5 closes in a day)
- ghost-buster (closed blocked issue)
- assist-master (closed issue that unblocks others)
- comeback (close after pause lifted)

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestPacmanAchievements`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/bd/pacman.go cmd/bd/pacman_test.go
git commit -m "feat(pacman): add achievements"
```

---

### Task 2: Render achievements in pacman output

**Files:**
- Modify: `cmd/bd/pacman.go`
- Test: `cmd/bd/pacman_test.go`

**Step 1: Write failing test**

```go
func TestPacmanRenderIncludesAchievements(t *testing.T) {
    // build pacmanState with achievements
    // render and assert strings include achievements
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestPacmanRenderIncludesAchievements`
Expected: FAIL (no achievements in output)

**Step 3: Write minimal implementation**

Render achievements after leaderboard with a short list.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestPacmanRenderIncludesAchievements`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/bd/pacman.go cmd/bd/pacman_test.go
git commit -m "feat(pacman): render achievements"
```

---

### Task 3: ASCII art polish

**Files:**
- Modify: `cmd/bd/pacman.go`
- Test: `cmd/bd/pacman_test.go`

**Step 1: Write failing test**

```go
func TestPacmanMazeRendersGhosts(t *testing.T) {
    // build pacmanState with blockers
    // render and assert ghost count > 0
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestPacmanMazeRendersGhosts`
Expected: FAIL if ghosts not shown

**Step 3: Write minimal implementation**

Adjust `renderPacmanArt` to include ghost count and keep width stable.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestPacmanMazeRendersGhosts`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/bd/pacman.go cmd/bd/pacman_test.go
git commit -m "feat(pacman): polish ASCII art"
```

---

### Task 4: `bd pacman --global` improvements

**Files:**
- Modify: `cmd/bd/pacman.go`
- Test: `cmd/bd/pacman_test.go`

**Step 1: Write failing test**

```go
func TestPacmanGlobalAggregatesScores(t *testing.T) {
    // create two temp repos with .beads/scoreboard.json
    // run global aggregation and verify combined output
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bd -run TestPacmanGlobalAggregatesScores`
Expected: FAIL

**Step 3: Write minimal implementation**

Improve global aggregation to:
- include achievements per agent
- skip repos without .beads
- show top N leaderboard across workspace

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bd -run TestPacmanGlobalAggregatesScores`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/bd/pacman.go cmd/bd/pacman_test.go
git commit -m "feat(pacman): improve global view"
```

---

### Task 5: Documentation

**Files:**
- Modify: `README.md`

**Step 1: Add achievements + global notes**

Add a short section describing achievements and global view.

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: document pacman achievements"
```

---

### Task 6: Tests

Run targeted tests:

```bash
go test ./cmd/bd -run TestPacmanAchievements
Go test ./cmd/bd -run TestPacmanRenderIncludesAchievements
Go test ./cmd/bd -run TestPacmanMazeRendersGhosts
Go test ./cmd/bd -run TestPacmanGlobalAggregatesScores
```

Full suite is expected to fail at `internal/hooks` (existing baseline failure).
