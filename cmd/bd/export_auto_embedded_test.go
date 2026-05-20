//go:build cgo

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
)

// TestEmbeddedAutoExport_GitignoredBeads_BypassesThrottle verifies that
// when the export path is gitignored, an auto-export pending due to a
// Dolt write within the throttle window is NOT dropped — i.e. the
// throttle is bypassed and the JSONL on disk reflects the most recent
// store state. Regression coverage for GH#3848.
//
// Pre-fix behavior: the auto-export-state.json timestamp gates the
// export; with `.beads/` gitignored and no git-add fallback, the write
// is silently lost when the embedded Dolt state is rebuilt.
//
// Post-fix behavior: `isExportPathGitignored` is consulted; bypass
// fires; the JSONL is rewritten on disk and contains the latest write.
func TestEmbeddedAutoExport_GitignoredBeads_BypassesThrottle(t *testing.T) {
	if testing.Short() {
		t.Skip("slow embedded-Dolt integration test")
	}

	repo := t.TempDir()
	repo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "t@t")
	runGit(t, repo, "config", "user.name", "t")
	writeTestFile(t, filepath.Join(repo, ".gitignore"), []byte(".beads/\n"))
	runGit(t, repo, "add", ".gitignore")
	runGit(t, repo, "commit", "-qm", "init")

	beadsDir := filepath.Join(repo, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Set up the test store wired up the same way bd init would. If the
	// Dolt test server is not running, newTestStoreWithPrefix skips —
	// that's expected on dev boxes without the Dolt Docker image; CI
	// runs this path.
	dbPath := filepath.Join(beadsDir, "beads.db")
	testStore := newTestStoreWithPrefix(t, dbPath, "ig") // *dolt.DoltStore
	store = testStore
	t.Cleanup(func() { store = nil })

	// Run from inside the repo so FindBeadsDir() resolves correctly.
	t.Chdir(repo)

	// Reset the gitignore-probe cache so this test is hermetic.
	gitignoreProbeCacheMu.Lock()
	gitignoreProbeCache = map[string]bool{}
	gitignoreProbeCacheMu.Unlock()

	ctx := context.Background()

	// Force a known throttle window so we can prove the bypass fires.
	// Without this we'd race the default 60s.
	config.Set("export.interval", "1h")
	t.Cleanup(func() { config.Set("export.interval", "") })

	// First write — sets the throttle state.
	mustCreateIssue(t, ctx, testStore, "ig-1", "first issue")
	maybeAutoExport(ctx)

	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	if !fileContains(t, jsonlPath, `"id":"ig-1"`) {
		t.Fatalf("setup error: first issue not in JSONL after initial export — file content:\n%s", readFile(t, jsonlPath))
	}

	// Second write within the throttle window. Pre-fix: throttle drops
	// the auto-export; post-fix: bypass fires because path is gitignored.
	mustCreateIssue(t, ctx, testStore, "ig-2", "second issue (must persist)")
	maybeAutoExport(ctx)

	got := readFile(t, jsonlPath)
	if !strings.Contains(got, `"id":"ig-2"`) {
		t.Errorf("expected JSONL to contain ig-2 after throttled bypass; got:\n%s", got)
	}
}

// TestEmbeddedAutoExport_TrackedBeads_ThrottlesAsBefore is the inverse
// regression guard: when `.beads/` is NOT gitignored, the throttle must
// continue to suppress redundant exports as it did pre-fix. This proves
// the change does not perturb the common case (git-tracked workspaces).
func TestEmbeddedAutoExport_TrackedBeads_ThrottlesAsBefore(t *testing.T) {
	if testing.Short() {
		t.Skip("slow embedded-Dolt integration test")
	}

	repo := t.TempDir()
	repo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "t@t")
	runGit(t, repo, "config", "user.name", "t")
	// Deliberately NO .gitignore so .beads/ is tracked.

	beadsDir := filepath.Join(repo, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")
	testStore := newTestStoreWithPrefix(t, dbPath, "tk")
	store = testStore
	t.Cleanup(func() { store = nil })

	t.Chdir(repo)

	gitignoreProbeCacheMu.Lock()
	gitignoreProbeCache = map[string]bool{}
	gitignoreProbeCacheMu.Unlock()

	ctx := context.Background()
	config.Set("export.interval", "1h")
	t.Cleanup(func() { config.Set("export.interval", "") })

	mustCreateIssue(t, ctx, testStore, "tk-1", "first issue")
	maybeAutoExport(ctx)

	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	if !fileContains(t, jsonlPath, `"id":"tk-1"`) {
		t.Fatalf("setup error: first issue not in JSONL — file content:\n%s", readFile(t, jsonlPath))
	}

	// Second write within throttle window — pre-fix throttle behavior
	// should hold (no bypass because .beads/ is tracked).
	mustCreateIssue(t, ctx, testStore, "tk-2", "second issue (should NOT persist due to throttle)")
	maybeAutoExport(ctx)

	got := readFile(t, jsonlPath)
	if strings.Contains(got, `"id":"tk-2"`) {
		t.Errorf("expected JSONL to NOT contain tk-2 (throttle should hold for tracked .beads/); got:\n%s", got)
	}

	// Force the throttle window to elapse and confirm the export catches up.
	state := loadExportAutoState(beadsDir)
	state.Timestamp = state.Timestamp.Add(-2 * time.Hour)
	saveExportAutoState(beadsDir, state)
	maybeAutoExport(ctx)
	got = readFile(t, jsonlPath)
	if !strings.Contains(got, `"id":"tk-2"`) {
		t.Errorf("expected JSONL to contain tk-2 after throttle expiry; got:\n%s", got)
	}
}

// mustCreateIssue inserts a minimal issue into the test store via the
// same CreateIssue path other tests use. Each call advances the Dolt
// commit hash, which is what the auto-export change-detection observes.
func mustCreateIssue(t *testing.T, ctx context.Context, st *dolt.DoltStore, id, title string) {
	t.Helper()
	issue := &types.Issue{
		ID:        id,
		Title:     title,
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
	}
	if err := st.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("CreateIssue(%s): %v", id, err)
	}
}

func fileContains(t *testing.T, path, substr string) bool {
	t.Helper()
	return strings.Contains(readFile(t, path), substr)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readFile(%s): %v", path, err)
	}
	return string(b)
}
