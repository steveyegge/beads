package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type specTriageTestEntry struct {
	Spec struct {
		SpecID    string `json:"spec_id"`
		GitStatus string `json:"git_status"`
	} `json:"spec"`
}

type specTriageTestResult struct {
	Entries []specTriageTestEntry `json:"entries"`
}

func TestSpecTriageFiltersIdeas(t *testing.T) {
	requireTestGuardDisabled(t)

	bdExe := buildBDForTest(t)
	ws := mkTmpDirInTmp(t, "bd-spec-triage-*")
	dbPath := filepath.Join(ws, ".beads", "beads.db")

	if err := runGit(ws, "init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "init", "--prefix", "test", "--quiet"); err != nil {
		t.Fatalf("bd init failed: %v", err)
	}

	ideasDir := filepath.Join(ws, "specs", "ideas")
	activeDir := filepath.Join(ws, "specs", "active")
	if err := os.MkdirAll(ideasDir, 0755); err != nil {
		t.Fatalf("mkdir ideas: %v", err)
	}
	if err := os.MkdirAll(activeDir, 0755); err != nil {
		t.Fatalf("mkdir active: %v", err)
	}

	writeSpec(t, filepath.Join(ideasDir, "tracked.md"))
	if err := runGit(ws, "add", "."); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
	if err := runGit(ws, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-m", "tracked"); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	modifiedPath := filepath.Join(ideasDir, "modified.md")
	writeSpec(t, modifiedPath)
	if err := runGit(ws, "add", modifiedPath); err != nil {
		t.Fatalf("git add modified failed: %v", err)
	}
	if err := runGit(ws, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-m", "modified"); err != nil {
		t.Fatalf("git commit modified failed: %v", err)
	}
	if err := os.WriteFile(modifiedPath, []byte("# modified\n\nupdate"), 0644); err != nil {
		t.Fatalf("rewrite modified: %v", err)
	}

	writeSpec(t, filepath.Join(ideasDir, "untracked.md"))
	writeSpec(t, filepath.Join(activeDir, "ignore.md"))

	now := time.Now().UTC().Truncate(time.Second)
	for _, name := range []string{"tracked.md", "modified.md", "untracked.md"} {
		path := filepath.Join(ideasDir, name)
		_ = os.Chtimes(path, now, now)
	}

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "scan", "--path", "specs"); err != nil {
		t.Fatalf("bd spec scan failed: %v", err)
	}

	out, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "triage", "--json", "--sort", "status", "--limit", "10")
	if err != nil {
		t.Fatalf("bd spec triage failed: %v\n%s", err, out)
	}

	var result specTriageTestResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(result.Entries))
	}
	if result.Entries[0].Spec.GitStatus != "untracked" {
		t.Fatalf("first status = %q, want untracked", result.Entries[0].Spec.GitStatus)
	}
}

func writeSpec(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("# "+filepath.Base(path)), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v failed: %v\n%s", args, err, string(out))
	}
	return nil
}
