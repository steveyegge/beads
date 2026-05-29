package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	internalbeads "github.com/steveyegge/beads/internal/beads"
	internalgit "github.com/steveyegge/beads/internal/git"
)

func TestNormalizeRemoteURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Dolt-native schemes — returned as-is
		{"dolthub://myorg/beads", "dolthub://myorg/beads"},
		{"file:///tmp/doltdb", "file:///tmp/doltdb"},
		{"aws://[dolt-table:us-east-1]/mydb", "aws://[dolt-table:us-east-1]/mydb"},
		{"gs://my-bucket/mydb", "gs://my-bucket/mydb"},
		{"git+https://github.com/org/repo.git", "git+https://github.com/org/repo.git"},
		{"git+ssh://git@github.com/org/repo.git", "git+ssh://git@github.com/org/repo.git"},
		{"git+http://example.com/repo.git", "git+http://example.com/repo.git"},

		// Git URLs — converted to dolt remote format
		{"https://github.com/org/repo.git", "git+https://github.com/org/repo.git"},
		{"http://github.com/org/repo.git", "git+http://github.com/org/repo.git"},
		{"ssh://git@github.com/org/repo.git", "git+ssh://git@github.com/org/repo.git"},
		{"git@github.com:org/repo.git", "git+ssh://git@github.com/org/repo.git"},
		{"C:/Users/alice/repos/beads.git", "git+C:/Users/alice/repos/beads.git"},
		{`D:\repos\beads.git`, `git+D:\repos\beads.git`},

		// Dolt remotesapi URLs — also converted (callers that need
		// pass-through for user-provided URLs should skip normalization)
		{"http://myserver:7007/mydb", "git+http://myserver:7007/mydb"},
		{"https://doltremoteapi.example.com/mydb", "git+https://doltremoteapi.example.com/mydb"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeRemoteURL(tt.input)
			if got != tt.want {
				t.Errorf("normalizeRemoteURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCommitBeadsConfigSkipsGitHooks(t *testing.T) {
	repo := t.TempDir()
	runGitForCommitConfigTest(t, repo, "init")
	runGitForCommitConfigTest(t, repo, "config", "user.email", "test@example.com")
	runGitForCommitConfigTest(t, repo, "config", "user.name", "Test User")

	beadsDir := filepath.Join(repo, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	configPath := filepath.Join(beadsDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("sync:\n  remote: git+https://example.com/repo.git\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	hooksDir := filepath.Join(repo, ".git", "hooks")
	hookMarker := filepath.Join(repo, "hook-ran")
	hook := "#!/bin/sh\n" +
		"echo hook-ran > " + shellQuoteForTest(hookMarker) + "\n" +
		"exit 42\n"
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-commit"), []byte(hook), 0o755); err != nil {
		t.Fatalf("write pre-commit hook: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	internalbeads.ResetCaches()
	internalgit.ResetCaches()
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
		internalbeads.ResetCaches()
		internalgit.ResetCaches()
	})

	commitBeadsConfig("bd: update sync.remote")

	if _, err := os.Stat(hookMarker); !os.IsNotExist(err) {
		t.Fatalf("pre-commit hook ran during internal config commit")
	}
	out := runGitForCommitConfigTest(t, repo, "log", "-1", "--format=%s")
	if got := strings.TrimSpace(out); got != "bd: update sync.remote" {
		t.Fatalf("commit subject = %q, want %q", got, "bd: update sync.remote")
	}
}

func runGitForCommitConfigTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func shellQuoteForTest(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
