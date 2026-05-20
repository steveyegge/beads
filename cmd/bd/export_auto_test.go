package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// TestGitAddFile_InWorktreeHook_StagesCorrectPath is a regression test for
// GH#3311: when bd's pre-commit hook calls git add with GIT_DIR inherited
// from the parent hook invocation, git defaults the work-tree to cwd and
// mis-stages the file at the root of the repo instead of under .beads/.
//
// This test verifies the file ends up staged at .beads/issues.jsonl, not
// at repo-root "issues.jsonl".
func TestGitAddFile_InWorktreeHook_StagesCorrectPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir, err := os.MkdirTemp("", "bd-gh3311-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	// Resolve symlinks so toplevel comparisons below match git's canonical view
	// (on macOS /var -> /private/var).
	tmpDir, err = filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	mainRepo := filepath.Join(tmpDir, "main")
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s failed: %v\n%s", args, dir, err, out)
		}
	}
	runGit(mainRepo, "init", "-q")
	runGit(mainRepo, "config", "user.email", "t@t")
	runGit(mainRepo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(mainRepo, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(mainRepo, "add", "README.md")
	runGit(mainRepo, "commit", "-qm", "init")

	worktree := filepath.Join(tmpDir, "wt")
	runGit(mainRepo, "worktree", "add", worktree, "-b", "feat")
	t.Cleanup(func() {
		c := exec.Command("git", "worktree", "remove", "--force", worktree)
		c.Dir = mainRepo
		_ = c.Run()
	})

	beadsDir := filepath.Join(worktree, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"id":"x"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate the environment inside a git pre-commit hook: GIT_DIR points
	// at the worktree's per-worktree gitdir.
	out, err := exec.Command("git", "-C", worktree, "rev-parse", "--git-dir").Output()
	if err != nil {
		t.Fatal(err)
	}
	gitDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktree, gitDir)
	}
	if gitDir, err = filepath.EvalSymlinks(gitDir); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GIT_DIR", gitDir)

	// Call the function under test from a state that matches the hook
	// subprocess: cwd not particularly interesting here, but gitAddFile sets
	// cmd.Dir = filepath.Dir(path) internally.
	t.Chdir(worktree)
	if err := gitAddFile(jsonlPath); err != nil {
		t.Fatalf("gitAddFile: %v", err)
	}

	// Inspect the worktree's index: the staged path must be ".beads/issues.jsonl",
	// NOT bare "issues.jsonl" at repo root.
	lsFiles := exec.Command("git", "ls-files", "--stage")
	lsFiles.Dir = worktree
	data, err := lsFiles.CombinedOutput()
	if err != nil {
		t.Fatalf("git ls-files: %v\n%s", err, data)
	}
	staged := string(data)
	if !strings.Contains(staged, ".beads/issues.jsonl") {
		t.Errorf("expected .beads/issues.jsonl to be staged, got:\n%s", staged)
	}
	// Regression guard: the pre-fix bug stages bare "issues.jsonl" at the root.
	for _, line := range strings.Split(strings.TrimSpace(staged), "\n") {
		// Each line is "<mode> <sha> <stage>\t<path>"
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[1] == "issues.jsonl" {
			t.Errorf("regression: issues.jsonl staged at repo root (GH#3311):\n%s", staged)
		}
	}
}

// TestScrubGitHookEnv verifies that the env-scrubbing helper drops exactly
// the git-hook-injected variables that would otherwise poison `git add`'s
// repo auto-discovery (or divert its object writes / config).
func TestScrubGitHookEnv(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"GIT_DIR=/some/.git",
		"GIT_WORK_TREE=/some",
		"GIT_INDEX_FILE=/some/.git/index",
		"GIT_COMMON_DIR=/some/.git",
		"GIT_PREFIX=sub/",
		"GIT_OBJECT_DIRECTORY=/some/.git/objects",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=/elsewhere/.git/objects",
		"GIT_CEILING_DIRECTORIES=/home",
		"GIT_DISCOVERY_ACROSS_FILESYSTEM=1",
		"GIT_CONFIG=/etc/some.conf",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.worktree",
		"GIT_CONFIG_VALUE_0=/elsewhere",
		"GIT_CONFIG_PARAMETERS='core.worktree=/elsewhere'",
		"GIT_CONFIG_GLOBAL=/tmp/gcfg",
		"GIT_CONFIG_SYSTEM=/tmp/scfg",
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME=/home/u",
		// Non-discovery vars that must pass through.
		"GIT_AUTHOR_NAME=kept",
		"GIT_COMMITTER_EMAIL=kept@example.com",
		"GIT_EDITOR=vim",
		"GIT_PAGER=less",
	}
	out := scrubGitHookEnv(in)
	joined := strings.Join(out, "\n")
	banned := []string{
		"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_COMMON_DIR=",
		"GIT_PREFIX=", "GIT_OBJECT_DIRECTORY=", "GIT_ALTERNATE_OBJECT_DIRECTORIES=",
		"GIT_CEILING_DIRECTORIES=", "GIT_DISCOVERY_ACROSS_FILESYSTEM=",
		"GIT_CONFIG=", "GIT_CONFIG_COUNT=", "GIT_CONFIG_KEY_0=", "GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_PARAMETERS=", "GIT_CONFIG_GLOBAL=", "GIT_CONFIG_SYSTEM=", "GIT_CONFIG_NOSYSTEM=",
	}
	for _, b := range banned {
		if strings.Contains(joined, b) {
			t.Errorf("scrubGitHookEnv leaked %s\nresult:\n%s", b, joined)
		}
	}
	kept := []string{
		"PATH=/usr/bin", "HOME=/home/u",
		"GIT_AUTHOR_NAME=kept", "GIT_COMMITTER_EMAIL=kept@example.com",
		"GIT_EDITOR=vim", "GIT_PAGER=less",
	}
	for _, k := range kept {
		if !strings.Contains(joined, k) {
			t.Errorf("scrubGitHookEnv dropped %s\nresult:\n%s", k, joined)
		}
	}
}

func TestShouldRunPostCommandAutoExportSkipsReadOnlyCommands(t *testing.T) {
	if shouldRunPostCommandAutoExport(&cobra.Command{Use: "search"}) {
		t.Fatal("search is read-only and must not trigger post-command auto-export")
	}
	if !shouldRunPostCommandAutoExport(&cobra.Command{Use: "create"}) {
		t.Fatal("write commands should still trigger post-command auto-export")
	}
}

// TestPathInsideDir covers the common structural cases plus the
// fresh-file + symlinked-parent case that tripped the initial fix
// (macOS /tmp -> /private/tmp asymmetry when the target file doesn't
// yet exist).
func TestPathInsideDir(t *testing.T) {
	tmpRaw, err := os.MkdirTemp("", "bd-pathinside-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpRaw) })

	// Provoke a symlinked-parent asymmetry: keep `raw` as the un-resolved
	// tmp form (/tmp/...) and derive `real` as the canonical form
	// (/private/tmp/...) so tests can compare across the boundary.
	real, err := filepath.EvalSymlinks(tmpRaw)
	if err != nil {
		t.Fatal(err)
	}

	wt := filepath.Join(real, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	wtRaw := filepath.Join(tmpRaw, "wt") // un-resolved view of same dir

	existing := filepath.Join(wt, "existing.txt")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
		dir  string
		want bool
	}{
		{"identical paths", wt, wt, true},
		{"existing descendant", existing, wt, true},
		{"fresh nonexistent descendant", filepath.Join(wt, "not-yet.txt"), wt, true},
		{"sibling path with shared prefix", filepath.Join(real, "wt-other/x"), wt, false},
		{"outside dir", filepath.Join(real, "elsewhere/x"), wt, false},
		// The regression: fresh path expressed via /tmp symlink vs dir
		// expressed via /private/tmp canonical. Must still say "inside".
		{"fresh path with symlinked parent form", filepath.Join(wtRaw, "fresh.txt"), wt, true},
		{"existing path with symlinked parent form", filepath.Join(wtRaw, "existing.txt"), wt, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pathInsideDir(tc.path, tc.dir)
			if got != tc.want {
				t.Errorf("pathInsideDir(%q, %q) = %v, want %v", tc.path, tc.dir, got, tc.want)
			}
		})
	}
}

// TestHookWorkTreeRoot covers the documented GIT_DIR shapes and the
// not-a-hook case.
func TestHookWorkTreeRoot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "bd-hwt-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	tmpDir, err = filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Case 1: GIT_DIR not set → "" (normal non-hook context).
	if err := os.Unsetenv("GIT_DIR"); err != nil {
		t.Fatal(err)
	}
	if got := hookWorkTreeRoot(); got != "" {
		t.Errorf("with GIT_DIR unset: hookWorkTreeRoot = %q, want \"\"", got)
	}

	// Case 2: linked-worktree style — GIT_DIR = main/.git/worktrees/<n>,
	// and that dir contains a `gitdir` file pointing at the worktree's
	// .git file. Worktree root = parent of that .git file.
	wtDotGit := filepath.Join(tmpDir, "wt", ".git")
	if err := os.MkdirAll(filepath.Dir(wtDotGit), 0o755); err != nil {
		t.Fatal(err)
	}
	linkedGitDir := filepath.Join(tmpDir, "main", ".git", "worktrees", "wt")
	if err := os.MkdirAll(linkedGitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linkedGitDir, "gitdir"), []byte(wtDotGit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", linkedGitDir)
	if got, want := hookWorkTreeRoot(), filepath.Dir(wtDotGit); got != want {
		t.Errorf("linked worktree: hookWorkTreeRoot = %q, want %q", got, want)
	}

	// Case 3: plain repo — GIT_DIR = <repo>/.git. Worktree root is its parent.
	plainGitDir := filepath.Join(tmpDir, "plain", ".git")
	if err := os.MkdirAll(plainGitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", plainGitDir)
	if got, want := hookWorkTreeRoot(), filepath.Dir(plainGitDir); got != want {
		t.Errorf("plain repo: hookWorkTreeRoot = %q, want %q", got, want)
	}

	// Case 4: unrecognized shape (no gitdir file, basename != .git) → "".
	// Bare-repo-ish; we conservatively decline to identify a worktree.
	bare := filepath.Join(tmpDir, "bare.git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", bare)
	if got := hookWorkTreeRoot(); got != "" {
		t.Errorf("bare/unrecognized GIT_DIR: hookWorkTreeRoot = %q, want \"\"", got)
	}
}

// TestGitAddFile_NonHookContext_GuardDoesNotFire verifies the worktree
// guard is a no-op when GIT_DIR is not set (normal bd invocation, not
// inside a git hook). Regression guard so a future tightening of
// hookWorkTreeRoot does not silently break the common path.
func TestGitAddFile_NonHookContext_GuardDoesNotFire(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir, err := os.MkdirTemp("", "bd-nonhook-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	tmpDir, err = filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q")
	runGit("config", "user.email", "t@t")
	runGit("config", "user.name", "t")

	target := filepath.Join(repo, ".beads", "issues.jsonl")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{"id":"x"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Unsetenv("GIT_DIR"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	if err := gitAddFile(target); err != nil {
		t.Fatalf("gitAddFile: %v", err)
	}

	c := exec.Command("git", "ls-files", "--stage")
	c.Dir = repo
	data, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("ls-files: %v\n%s", err, data)
	}
	if !strings.Contains(string(data), ".beads/issues.jsonl") {
		t.Errorf("non-hook path did not stage .beads/issues.jsonl:\n%s", data)
	}
}

// TestGitAddFile_CapturesStderrOnFailure verifies that when `git add` fails,
// the returned error wraps git's stderr text instead of just the bare exit
// status. Regression guard for the silent "Warning: auto-export: git add
// failed: exit status 1" noise where the user has no signal as to why.
func TestGitAddFile_CapturesStderrOnFailure(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir, err := os.MkdirTemp("", "bd-stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	tmpDir, err = filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q")
	runGit("config", "user.email", "t@t")
	runGit("config", "user.name", "t")

	// Force git add to fail by gitignoring the target. Common real-world
	// trigger: a parent .gitignore excluding .beads/ that the user is
	// unaware of.
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".beads/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(repo, ".beads", "issues.jsonl")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{"id":"x"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Unsetenv("GIT_DIR"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	err = gitAddFile(target)
	if err == nil {
		t.Fatal("expected gitAddFile to fail on gitignored target, got nil")
	}
	msg := err.Error()
	// Bare-exit-status regression guard: pre-fix message was just "exit
	// status 1" with nothing else. Post-fix must include git's stderr.
	if !strings.Contains(strings.ToLower(msg), "ignored") {
		t.Errorf("expected error to surface git's stderr (containing 'ignored'), got: %q", msg)
	}
}

// TestGitAddFile_RedirectCase_DoesNotStageInMainRepo regresses the
// silent-stage-in-main follow-up from the GH#3311 review: when a worktree
// has .beads/redirect -> main/.beads, the worktree's pre-commit hook must
// NOT stage the redirected path into main's index. That would silently
// pollute a repo the user did not tell us to touch. Expected behavior is
// to skip staging entirely (the file content on disk is still correct).
func TestGitAddFile_RedirectCase_DoesNotStageInMainRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir, err := os.MkdirTemp("", "bd-gh3311-redirect-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
	tmpDir, err = filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	mainRepo := filepath.Join(tmpDir, "main")
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s failed: %v\n%s", args, dir, err, out)
		}
	}
	runGit(mainRepo, "init", "-q")
	runGit(mainRepo, "config", "user.email", "t@t")
	runGit(mainRepo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(mainRepo, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(mainRepo, "add", "README.md")
	runGit(mainRepo, "commit", "-qm", "init")

	// Create main's .beads directory with an issues.jsonl the hook would
	// target via the redirect.
	mainBeads := filepath.Join(mainRepo, ".beads")
	if err := os.MkdirAll(mainBeads, 0o755); err != nil {
		t.Fatal(err)
	}
	mainJSONL := filepath.Join(mainBeads, "issues.jsonl")
	if err := os.WriteFile(mainJSONL, []byte(`{"id":"from-redirect"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create worktree; GIT_DIR env var simulation captures the hook context.
	worktree := filepath.Join(tmpDir, "wt")
	runGit(mainRepo, "worktree", "add", worktree, "-b", "feat")
	t.Cleanup(func() {
		c := exec.Command("git", "worktree", "remove", "--force", worktree)
		c.Dir = mainRepo
		_ = c.Run()
	})

	out, err := exec.Command("git", "-C", worktree, "rev-parse", "--git-dir").Output()
	if err != nil {
		t.Fatal(err)
	}
	gitDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktree, gitDir)
	}
	if gitDir, err = filepath.EvalSymlinks(gitDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", gitDir)

	// Act: stage the main-repo-resident path from inside the worktree hook.
	t.Chdir(worktree)
	if err := gitAddFile(mainJSONL); err != nil {
		t.Fatalf("gitAddFile: %v", err)
	}

	// Assert: neither the worktree's index nor main's index got a bogus
	// staging entry from the worktree's hook firing.
	checkNoStage := func(label, repoDir string) {
		t.Helper()
		c := exec.Command("git", "ls-files", "--stage")
		c.Dir = repoDir
		data, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: ls-files: %v\n%s", label, err, data)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) != 2 {
				continue
			}
			if strings.Contains(parts[1], "issues.jsonl") {
				t.Errorf("%s staged issues.jsonl when it should not have; ls-files output:\n%s", label, data)
			}
		}
	}
	// Both checks use env with GIT_DIR unset so we observe each repo's
	// own index rather than routing through the inherited hook gitdir.
	// t.Setenv can only set (not unset); the outer Setenv of GIT_DIR has
	// a Cleanup that restores it, so unsetting here is safe for the rest
	// of this test and the outer cleanup will re-set if another test
	// relies on the parent env.
	if err := os.Unsetenv("GIT_DIR"); err != nil {
		t.Fatal(err)
	}
	checkNoStage("worktree", worktree)
	checkNoStage("main", mainRepo)
}

// TestIsExportPathGitignored exercises the gitignore probe used by the
// auto-export throttle bypass for GH#3848. Pure helper test — no Dolt
// store, no CGO needed; the maybeAutoExport integration coverage lives
// in export_auto_embedded_test.go.
//
// Each sub-case runs `git init` + filesystem I/O in `t.TempDir()`; the
// whole table completes in ~120ms total so it stays in the default-run
// lane (no `if testing.Short()` skip) so coverage is attributable. The
// heavy embedded-Dolt counterpart lives in export_auto_embedded_test.go.
func TestIsExportPathGitignored(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	type scenario struct {
		name       string
		setup      func(t *testing.T, repo string)
		initGit    bool   // run `git init` in repo
		beadsRel   string // relative path under repo for .beads/, default ".beads"
		exportName string // filename to probe inside .beads/, default "issues.jsonl"
		want       bool
	}

	cases := []scenario{
		{
			name:    "gitignored beads dir via dir pattern returns true",
			initGit: true,
			setup: func(t *testing.T, repo string) {
				writeTestFile(t, filepath.Join(repo, ".gitignore"), []byte(".beads/\n"))
			},
			want: true,
		},
		{
			name:    "tracked beads dir (no .gitignore) returns false",
			initGit: true,
			setup:   func(t *testing.T, repo string) {},
			want:    false,
		},
		{
			name:    "not in a git repo returns false (no .git/ parent)",
			initGit: false,
			setup:   func(t *testing.T, repo string) {},
			want:    false,
		},
		{
			name:    "parent .gitignore (umbrella workspace) matches nested .beads/",
			initGit: true,
			setup: func(t *testing.T, repo string) {
				writeTestFile(t, filepath.Join(repo, ".gitignore"), []byte(".beads/\n"))
			},
			beadsRel: "project-a/.beads",
			want:     true,
		},
		{
			name:    "file-level wildcard *.jsonl matches even when dir is tracked",
			initGit: true,
			setup: func(t *testing.T, repo string) {
				writeTestFile(t, filepath.Join(repo, ".gitignore"), []byte("*.jsonl\n"))
			},
			want: true,
		},
		{
			name:    ".git/info/exclude (per-clone) treated same as .gitignore",
			initGit: true,
			setup: func(t *testing.T, repo string) {
				writeTestFile(t, filepath.Join(repo, ".git", "info", "exclude"), []byte(".beads/\n"))
			},
			want: true,
		},
		{
			name:    "issues.jsonl absent on disk still returns correct ignore status",
			initGit: true,
			setup: func(t *testing.T, repo string) {
				writeTestFile(t, filepath.Join(repo, ".gitignore"), []byte(".beads/\n"))
				// Note: we deliberately do NOT create issues.jsonl inside
				// beadsDir — git check-ignore must answer based on the
				// rule alone, regardless of whether the path exists yet.
			},
			exportName: "issues.jsonl",
			want:       true,
		},
		{
			name:    "non-default export path honors the actual path",
			initGit: true,
			setup: func(t *testing.T, repo string) {
				// Only "global-issues.jsonl" is gitignored; the default
				// "issues.jsonl" is not.
				writeTestFile(t, filepath.Join(repo, ".gitignore"), []byte("global-issues.jsonl\n"))
			},
			exportName: "global-issues.jsonl",
			want:       true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			repo, err := filepath.EvalSymlinks(repo)
			if err != nil {
				t.Fatal(err)
			}
			if tc.initGit {
				runGit(t, repo, "init", "-q")
			}
			tc.setup(t, repo)

			beadsRel := tc.beadsRel
			if beadsRel == "" {
				beadsRel = ".beads"
			}
			exportName := tc.exportName
			if exportName == "" {
				exportName = "issues.jsonl"
			}

			beadsDir := filepath.Join(repo, beadsRel)
			if err := os.MkdirAll(beadsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			fullPath := filepath.Join(beadsDir, exportName)

			// Each sub-test gets its own context so the probe's
			// internal timeout is honored independently.
			got := runGitignoreProbe(context.Background(), beadsDir, fullPath)
			if got != tc.want {
				t.Errorf("runGitignoreProbe(%q, %q) = %v, want %v", beadsDir, fullPath, got, tc.want)
			}
		})
	}
}

// TestRunGitignoreProbe_Timeout asserts the probe honors its 2s deadline
// when the git subprocess hangs (slow filesystem, hostile wrapper). We
// can't easily simulate a hanging git binary in CI, so we exercise the
// context-cancellation path by passing a context that is already cancelled.
func TestRunGitignoreProbe_Timeout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repo := t.TempDir()
	repo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before invoking

	beadsDir := filepath.Join(repo, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	got := runGitignoreProbe(ctx, beadsDir, filepath.Join(beadsDir, "issues.jsonl"))
	if got != false {
		t.Errorf("expected runGitignoreProbe to return false when ctx is cancelled; got true")
	}
}

// TestIsExportPathGitignored_Cache verifies the per-process cache: a
// second call with the same key does not re-invoke the subprocess. We
// exercise this by checking the cached value sticks even when the
// underlying .gitignore changes mid-process.
func TestIsExportPathGitignored_Cache(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Reset cache so the test is hermetic relative to other tests that
	// may have populated it.
	gitignoreProbeCacheMu.Lock()
	gitignoreProbeCache = map[string]bool{}
	gitignoreProbeCacheMu.Unlock()

	repo := t.TempDir()
	repo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	writeTestFile(t, filepath.Join(repo, ".gitignore"), []byte(".beads/\n"))
	beadsDir := filepath.Join(repo, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fullPath := filepath.Join(beadsDir, "issues.jsonl")

	first := isExportPathGitignored(context.Background(), beadsDir, fullPath)
	if !first {
		t.Fatalf("setup error: expected first probe = true (gitignored), got false")
	}

	// Mutate .gitignore — without cache, the next call would return false.
	if err := os.Remove(filepath.Join(repo, ".gitignore")); err != nil {
		t.Fatal(err)
	}

	second := isExportPathGitignored(context.Background(), beadsDir, fullPath)
	if second != first {
		t.Errorf("expected cached value to persist; got %v, want %v", second, first)
	}
}

// runGit is a tiny test helper used by the gitignore-probe tests. Lifted
// to package scope so multiple tests can share it (TestIsExportPath* +
// TestRunGitignoreProbe_*). Mirrors the pattern of the closures inside
// the GitAddFile tests above.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s failed: %v\n%s", args, dir, err, out)
	}
}

// writeTestFile writes content to path, creating parent dirs as needed.
// Lives in this (non-tagged) test file so it is available under both
// CGO and pure-Go builds. The `writeFile` helper in explicit_db_nodb_test.go
// is `//go:build cgo`, so its symbol is unreachable from the pure-Go test
// compile — using a distinct name here avoids both the collision and the
// missing-symbol error in `cmd/bd pure-Go tests compile (CGO_ENABLED=0)`.
func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestShouldExport covers the pure throttle-window decision used by
// maybeAutoExport. It's a no-I/O table-driven test specifically so the
// coverage tool (which runs the fast / pure-Go lane) can attribute
// lines to the new logic in export_auto.go. The integration variants
// that wire shouldExport through maybeAutoExport live in
// export_auto_embedded_test.go and are CGO-only.
func TestShouldExport(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name     string
		state    *exportAutoState
		interval time.Duration
		want     bool
	}{
		{
			name:     "first run (zero timestamp) always exports",
			state:    &exportAutoState{},
			interval: time.Minute,
			want:     true,
		},
		{
			name:     "throttle window active — block",
			state:    &exportAutoState{Timestamp: now.Add(-10 * time.Second)},
			interval: time.Minute,
			want:     false,
		},
		{
			name:     "throttle window just elapsed — allow",
			state:    &exportAutoState{Timestamp: now.Add(-2 * time.Minute)},
			interval: time.Minute,
			want:     true,
		},
		{
			name:     "exactly at interval boundary — allow (>=)",
			state:    &exportAutoState{Timestamp: now.Add(-time.Minute)},
			interval: time.Minute,
			want:     true,
		},
		{
			name:     "zero interval (effectively disabled) always exports",
			state:    &exportAutoState{Timestamp: now.Add(-time.Microsecond)},
			interval: 0,
			want:     true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := shouldExport(tc.state, tc.interval)
			if got != tc.want {
				t.Errorf("shouldExport(%+v, %s) = %v, want %v",
					tc.state, tc.interval, got, tc.want)
			}
		})
	}
}

// TestShouldBypassThrottle covers the GH#3848 bypass policy: when the
// JSONL path is git-ignored, the throttle is overridden because the
// JSONL is the only cross-machine sync substrate. Pure function —
// coverage-tool-visible.
func TestShouldBypassThrottle(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name       string
		state      *exportAutoState
		interval   time.Duration
		gitignored bool
		want       bool
	}{
		{
			name:       "first run never bypasses (shouldExport handles it)",
			state:      &exportAutoState{},
			interval:   time.Minute,
			gitignored: true,
			want:       false,
		},
		{
			name:       "window elapsed — no bypass needed",
			state:      &exportAutoState{Timestamp: now.Add(-2 * time.Minute)},
			interval:   time.Minute,
			gitignored: true,
			want:       false,
		},
		{
			name:       "window active + gitignored — bypass (the bug fix)",
			state:      &exportAutoState{Timestamp: now.Add(-10 * time.Second)},
			interval:   time.Minute,
			gitignored: true,
			want:       true,
		},
		{
			name:       "window active + tracked — DO NOT bypass (regression guard)",
			state:      &exportAutoState{Timestamp: now.Add(-10 * time.Second)},
			interval:   time.Minute,
			gitignored: false,
			want:       false,
		},
		{
			name:       "exactly at interval boundary — no bypass (window has elapsed)",
			state:      &exportAutoState{Timestamp: now.Add(-time.Minute)},
			interval:   time.Minute,
			gitignored: true,
			want:       false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := shouldBypassThrottle(tc.state, tc.interval, tc.gitignored)
			if got != tc.want {
				t.Errorf("shouldBypassThrottle(%+v, %s, gitignored=%v) = %v, want %v",
					tc.state, tc.interval, tc.gitignored, got, tc.want)
			}
		})
	}
}
