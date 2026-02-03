# Wobble Skill Dependents Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Parse `depends_on` YAML front‑matter in skill files and persist dependents into wobble store so `bd cascade` reflects real metadata.

**Architecture:** Read skill files from the scan directory, parse front‑matter block at top (`---` … `---`), extract `depends_on` (inline list or multi-line), and attach dependents to wobble skills before persistence. Keep a lightweight parser (no YAML deps). Preserve existing dependents if no metadata found.

**Tech Stack:** Go CLI (bd), file parsing, wobble store.

---

### Task 1: Parse skill front‑matter dependents

**Files:**
- Modify: `cmd/bd/wobble_store.go`
- Test: `cmd/bd/wobble_store_test.go`

**Step 1: Write failing test**
```go
func TestParseSkillDependentsFrontMatter(t *testing.T) {
  dir := t.TempDir()
  skillDir := filepath.Join(dir, "beads")
  if err := os.MkdirAll(skillDir, 0755); err != nil { t.Fatal(err) }
  content := "---\nname: beads\ndepends_on:\n  - spec-tracker\n  - pacman\n---\n"
  if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil { t.Fatal(err) }

  deps, err := parseSkillDependents(dir, "beads")
  require.NoError(t, err)
  require.Equal(t, []string{"pacman", "spec-tracker"}, deps)
}
```

**Step 2: Run test to verify it fails**
Run: `go test ./cmd/bd -run TestParseSkillDependentsFrontMatter`
Expected: FAIL (missing parser)

**Step 3: Write minimal implementation**
```go
func parseSkillDependents(skillsDir, skillName string) ([]string, error) {
  // resolve SKILL.md (dir or file), read front matter only
  // parse depends_on: [a,b] or list lines "- a"
  // return sorted unique list
}
```

**Step 4: Run test to verify it passes**
Run: `go test ./cmd/bd -run TestParseSkillDependentsFrontMatter`
Expected: PASS

**Step 5: Commit**
```bash
git add cmd/bd/wobble_store.go cmd/bd/wobble_store_test.go
git commit -m "feat: parse skill dependents from front matter"
```

---

### Task 2: Attach dependents during wobble scans

**Files:**
- Modify: `cmd/bd/wobble_store.go`
- Modify: `cmd/bd/wobble.go`
- Test: `cmd/bd/wobble_store_test.go`

**Step 1: Write failing test**
```go
func TestWobbleSkillsFromSummaryIncludesDependents(t *testing.T) {
  dir := t.TempDir()
  skillDir := filepath.Join(dir, "beads")
  os.MkdirAll(skillDir, 0755)
  os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndepends_on: [spec-tracker]\n---\n"), 0644)

  results := []wobble.SkillSummary{{Name: "beads", StructuralRisk: 0.1}}
  skills := wobbleSkillsFromSummary(results, dir)
  require.Equal(t, []string{"spec-tracker"}, skills[0].Dependents)
}
```

**Step 2: Run test to verify it fails**
Run: `go test ./cmd/bd -run TestWobbleSkillsFromSummaryIncludesDependents`
Expected: FAIL

**Step 3: Write minimal implementation**
```go
// update wobbleSkillsFromSummary/ScanResult/RealResults to accept skillsDir
// call parseSkillDependents and set Dependents if found
```

**Step 4: Run test to verify it passes**
Run: `go test ./cmd/bd -run TestWobbleSkillsFromSummaryIncludesDependents`
Expected: PASS

**Step 5: Commit**
```bash
git add cmd/bd/wobble_store.go cmd/bd/wobble.go cmd/bd/wobble_store_test.go
git commit -m "feat: attach wobble dependents from skill metadata"
```

---

### Task 3: Verification

**Step 1: Run focused tests**
Run: `go test ./cmd/bd -run "TestParseSkillDependentsFrontMatter|TestWobbleSkillsFromSummaryIncludesDependents"`
Expected: PASS

**Step 2: Run existing wobble drift tests**
Run: `go test ./cmd/bd -run "TestWriteWobbleStore|TestDriftCommand_JSON|TestPacmanLeaderboardSkillsFixed|TestCascadeCommand"`
Expected: PASS
