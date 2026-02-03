# Wobble Drift Full Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Persist wobble scans, add drift/cascade commands, and show skills-fixed in pacman leaderboard.

**Architecture:** Store wobble snapshot + history in `.beads/wobble/skills.json` and `.beads/wobble/history.json`. Read these for drift/cascade/leaderboard outputs; compute skills-fixed from the last two scans per actor (unstable/wobbly → stable).

**Tech Stack:** Go CLI (bd), JSON store, existing wobble scanning.

---

### Task 1: Wobble store (snapshot + history)

**Files:**
- Create: `cmd/bd/wobble_store.go`
- Modify: `cmd/bd/wobble.go`
- Test: `cmd/bd/wobble_store_test.go`

**Step 1: Write failing test**
```go
func TestWriteWobbleStore(t *testing.T) {
  dir := t.TempDir()
  historyPath := filepath.Join(dir, "history.json")
  skillsPath := filepath.Join(dir, "skills.json")

  store := wobbleStore{
    Version: 1,
    GeneratedAt: time.Date(2026, 2, 3, 8, 15, 0, 0, time.UTC),
    Skills: []wobbleSkill{{
      ID: "beads",
      Verdict: "stable",
      ChangeState: "stable",
      Signals: []string{"ok"},
      Dependents: []string{"spec-tracker"},
    }},
  }
  entry := wobbleHistoryEntry{
    Actor: "claude",
    CreatedAt: store.GeneratedAt,
    Stable: 1,
    Wobbly: 0,
    Unstable: 0,
    Skills: []string{"beads"},
    WobblySkills: nil,
    UnstableSkills: nil,
  }

  require.NoError(t, writeWobbleStore(skillsPath, historyPath, store, entry))

  loaded, history, err := loadWobbleStore(skillsPath, historyPath)
  require.NoError(t, err)
  require.Equal(t, 1, loaded.Version)
  require.Len(t, loaded.Skills, 1)
  require.Equal(t, "beads", loaded.Skills[0].ID)
  require.Equal(t, []string{"spec-tracker"}, loaded.Skills[0].Dependents)
  require.Len(t, history, 1)
  require.Equal(t, "claude", history[0].Actor)
}
```

**Step 2: Run test to verify it fails**
Run: `go test ./cmd/bd -run TestWriteWobbleStore`
Expected: FAIL (missing types/functions)

**Step 3: Write minimal implementation**
```go
type wobbleStore struct { Version int; GeneratedAt time.Time; Skills []wobbleSkill }
type wobbleSkill struct { ID string; Verdict string; ChangeState string; Signals []string; Dependents []string }
type wobbleHistoryEntry struct { Actor string; CreatedAt time.Time; Stable, Wobbly, Unstable int; Skills, WobblySkills, UnstableSkills []string }

func writeWobbleStore(skillsPath, historyPath string, store wobbleStore, entry wobbleHistoryEntry) error {
  // create dir, write skills json
  // append entry to existing history (or start new)
}

func loadWobbleStore(skillsPath, historyPath string) (wobbleStore, []wobbleHistoryEntry, error) {
  // read if exists, else empty
}
```

**Step 4: Run test to verify it passes**
Run: `go test ./cmd/bd -run TestWriteWobbleStore`
Expected: PASS

**Step 5: Commit**
```bash
git add cmd/bd/wobble_store.go cmd/bd/wobble.go cmd/bd/wobble_store_test.go
git commit -m "feat: persist wobble scan store"
```

---

### Task 2: Drift dashboard command

**Files:**
- Create: `cmd/bd/drift.go`
- Test: `cmd/bd/drift_test.go`
- Modify: `cmd/bd/commands.go` (register)

**Step 1: Write failing test**
```go
func TestDriftCommand_JSON(t *testing.T) {
  // create temp wobble store + minimal bead/spec fixtures
  // run `bd drift --json`
  // assert json fields: last_scan_at, unstable, wobbly, skills_fixed, specs_without_beads, beads_without_specs
}
```

**Step 2: Run test to verify it fails**
Run: `go test ./cmd/bd -run TestDriftCommand_JSON`
Expected: FAIL (command missing)

**Step 3: Write minimal implementation**
```go
// define driftSummary struct with json tags
// read wobble store, compute skills_fixed from last two scans per actor
// compute spec/bead drift using existing spec alignment helpers
// render text + json
```

**Step 4: Run test to verify it passes**
Run: `go test ./cmd/bd -run TestDriftCommand_JSON`
Expected: PASS

**Step 5: Commit**
```bash
git add cmd/bd/drift.go cmd/bd/drift_test.go cmd/bd/commands.go
git commit -m "feat: add wobble drift dashboard"
```

---

### Task 3: Pacman leaderboard skills fixed

**Files:**
- Modify: `cmd/bd/pacman.go`
- Test: `cmd/bd/pacman_test.go`

**Step 1: Write failing test**
```go
func TestPacmanLeaderboardSkillsFixed(t *testing.T) {
  // use wobble history fixture with two scans for actor
  // expect header includes "skills fixed" and row count matches
}
```

**Step 2: Run test to verify it fails**
Run: `go test ./cmd/bd -run TestPacmanLeaderboardSkillsFixed`
Expected: FAIL

**Step 3: Write minimal implementation**
```go
// load wobble history, compute skills_fixed per actor
// render in global leaderboard and per-actor rows
```

**Step 4: Run test to verify it passes**
Run: `go test ./cmd/bd -run TestPacmanLeaderboardSkillsFixed`
Expected: PASS

**Step 5: Commit**
```bash
git add cmd/bd/pacman.go cmd/bd/pacman_test.go
git commit -m "feat: show skills-fixed in pacman leaderboard"
```

---

### Task 4: Cascade command

**Files:**
- Create: `cmd/bd/cascade.go`
- Test: `cmd/bd/cascade_test.go`
- Modify: `cmd/bd/commands.go`

**Step 1: Write failing test**
```go
func TestCascadeCommand(t *testing.T) {
  // wobble store with dependents
  // run `bd cascade beads`
  // assert dependents list printed
}
```

**Step 2: Run test to verify it fails**
Run: `go test ./cmd/bd -run TestCascadeCommand`
Expected: FAIL

**Step 3: Write minimal implementation**
```go
// read wobble store; if missing or no dependents, print "none"
```

**Step 4: Run test to verify it passes**
Run: `go test ./cmd/bd -run TestCascadeCommand`
Expected: PASS

**Step 5: Commit**
```bash
git add cmd/bd/cascade.go cmd/bd/cascade_test.go cmd/bd/commands.go
git commit -m "feat: add wobble cascade command"
```

---

### Task 5: README updates

**Files:**
- Modify: `README.md`

**Step 1: Write failing test**
N/A

**Step 2: Update docs**
- Document `bd drift` and `bd cascade`
- Note wobble store location
- Mention skills-fixed in pacman leaderboard

**Step 3: Commit**
```bash
git add README.md
git commit -m "docs: document wobble drift and cascade"
```

---

### Task 6: Verification

**Step 1: Run focused tests**
Run: `go test ./cmd/bd -run "TestWriteWobbleStore|TestDriftCommand_JSON|TestPacmanLeaderboardSkillsFixed|TestCascadeCommand"`
Expected: PASS (baseline failures outside cmd/bd allowed)

**Step 2: Run full tests (optional)**
Run: `go test ./...`
Expected: currently FAIL at `internal/hooks TestRun_Async` (known)
