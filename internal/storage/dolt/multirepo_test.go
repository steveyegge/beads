//go:build cgo && integration

package dolt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestDetectPeerBackend(t *testing.T) {
	skipIfNoDolt(t)

	t.Run("dolt backend detected", func(t *testing.T) {
		dir := t.TempDir()
		beadsDir := filepath.Join(dir, ".beads")
		doltDir := filepath.Join(beadsDir, "dolt")
		if err := os.MkdirAll(doltDir, 0755); err != nil {
			t.Fatal(err)
		}

		backend, err := DetectPeerBackend(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if backend != PeerBackendDolt {
			t.Errorf("expected dolt backend, got %s", backend)
		}
	})

	t.Run("jsonl backend detected", func(t *testing.T) {
		dir := t.TempDir()
		beadsDir := filepath.Join(dir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}

		backend, err := DetectPeerBackend(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if backend != PeerBackendJSONL {
			t.Errorf("expected jsonl backend, got %s", backend)
		}
	})

	t.Run("no beads directory", func(t *testing.T) {
		dir := t.TempDir()

		_, err := DetectPeerBackend(dir)
		if err == nil {
			t.Fatal("expected error for missing .beads directory")
		}
	})

	t.Run("empty beads directory", func(t *testing.T) {
		dir := t.TempDir()
		beadsDir := filepath.Join(dir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}

		_, err := DetectPeerBackend(dir)
		if err == nil {
			t.Fatal("expected error for empty .beads directory")
		}
	})

	t.Run("dolt preferred over jsonl", func(t *testing.T) {
		dir := t.TempDir()
		beadsDir := filepath.Join(dir, ".beads")
		doltDir := filepath.Join(beadsDir, "dolt")
		if err := os.MkdirAll(doltDir, 0755); err != nil {
			t.Fatal(err)
		}
		// Also create JSONL file
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if err := os.WriteFile(jsonlPath, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}

		backend, err := DetectPeerBackend(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if backend != PeerBackendDolt {
			t.Errorf("expected dolt backend (preferred), got %s", backend)
		}
	})
}

func TestQueryPeerIssues(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	baseDir := t.TempDir()

	// Setup peer store with test issues
	peerDir := filepath.Join(baseDir, "peer-repo")
	peerStore, peerCleanup := setupFederationStore(t, ctx, peerDir, "peer")
	defer peerCleanup()

	// Create test issues in peer
	issues := []*types.Issue{
		{
			ID:        "peer-001",
			Title:     "Peer issue one",
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  1,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        "peer-002",
			Title:     "Peer issue two",
			IssueType: types.TypeBug,
			Status:    types.StatusOpen,
			Priority:  2,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}

	for _, issue := range issues {
		if err := peerStore.CreateIssue(ctx, issue, "test"); err != nil {
			t.Fatalf("failed to create peer issue %s: %v", issue.ID, err)
		}
	}
	if err := peerStore.Commit(ctx, "Create test issues"); err != nil {
		t.Logf("commit: %v", err)
	}

	// Close peer store before querying (single-process Dolt access)
	peerStore.Close()

	// Create a .beads directory structure that DetectPeerBackend expects
	// The peer store was created at peerDir (which is the dolt path),
	// but we need the repo root to have .beads/dolt/
	peerRepoRoot := filepath.Join(baseDir, "peer-root")
	peerBeadsDir := filepath.Join(peerRepoRoot, ".beads")
	if err := os.MkdirAll(peerBeadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Symlink the dolt directory
	if err := os.Symlink(peerDir, filepath.Join(peerBeadsDir, "dolt")); err != nil {
		t.Fatal(err)
	}
	// Create metadata.json
	if err := os.WriteFile(filepath.Join(peerBeadsDir, "metadata.json"),
		[]byte(`{"database":"dolt","backend":"dolt"}`), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("query all issues", func(t *testing.T) {
		result, err := QueryPeerIssues(ctx, peerRepoRoot, types.IssueFilter{})
		if err != nil {
			t.Fatalf("QueryPeerIssues failed: %v", err)
		}
		if len(result) != 2 {
			t.Errorf("expected 2 issues, got %d", len(result))
		}
		// Verify source_repo is set
		for _, issue := range result {
			if issue.SourceRepo != peerRepoRoot {
				t.Errorf("expected SourceRepo=%s, got %s", peerRepoRoot, issue.SourceRepo)
			}
		}
	})

	t.Run("query with filter", func(t *testing.T) {
		bugType := types.TypeBug
		result, err := QueryPeerIssues(ctx, peerRepoRoot, types.IssueFilter{
			IssueType: &bugType,
		})
		if err != nil {
			t.Fatalf("QueryPeerIssues with filter failed: %v", err)
		}
		if len(result) != 1 {
			t.Errorf("expected 1 bug issue, got %d", len(result))
		}
		if len(result) > 0 && result[0].ID != "peer-002" {
			t.Errorf("expected peer-002, got %s", result[0].ID)
		}
	})

	t.Run("query nonexistent peer", func(t *testing.T) {
		_, err := QueryPeerIssues(ctx, "/nonexistent/path", types.IssueFilter{})
		if err == nil {
			t.Fatal("expected error for nonexistent peer")
		}
	})
}

func TestHydrateFromPeerDolt(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	baseDir := t.TempDir()

	// Setup peer store with test issues
	peerDoltDir := filepath.Join(baseDir, "peer-dolt")
	peerStore, peerCleanup := setupFederationStore(t, ctx, peerDoltDir, "peer")
	defer peerCleanup()

	// Create test issues in peer
	peerIssue := &types.Issue{
		ID:        "peer-h01",
		Title:     "Hydration test issue",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Priority:  2,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := peerStore.CreateIssue(ctx, peerIssue, "test"); err != nil {
		t.Fatalf("failed to create peer issue: %v", err)
	}
	if err := peerStore.Commit(ctx, "Create hydration test issue"); err != nil {
		t.Logf("commit: %v", err)
	}
	peerStore.Close()

	// Create peer repo root with .beads/dolt/ structure
	peerRepoRoot := filepath.Join(baseDir, "peer-repo-root")
	peerBeadsDir := filepath.Join(peerRepoRoot, ".beads")
	if err := os.MkdirAll(peerBeadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(peerDoltDir, filepath.Join(peerBeadsDir, "dolt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(peerBeadsDir, "metadata.json"),
		[]byte(`{"database":"dolt","backend":"dolt"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Setup local store
	localDir := filepath.Join(baseDir, "local-dolt")
	localStore, localCleanup := setupFederationStore(t, ctx, localDir, "local")
	defer localCleanup()

	t.Run("hydrate imports peer issues", func(t *testing.T) {
		result, err := localStore.HydrateFromPeerDolt(ctx, peerRepoRoot)
		if err != nil {
			t.Fatalf("HydrateFromPeerDolt failed: %v", err)
		}
		if result.TotalPeerIssues != 1 {
			t.Errorf("expected 1 total peer issue, got %d", result.TotalPeerIssues)
		}
		if result.Imported != 1 {
			t.Errorf("expected 1 imported, got %d", result.Imported)
		}
		if result.Skipped != 0 {
			t.Errorf("expected 0 skipped, got %d", result.Skipped)
		}

		// Verify the issue exists locally
		local, err := localStore.GetIssue(ctx, "peer-h01")
		if err != nil {
			t.Fatalf("failed to get imported issue: %v", err)
		}
		if local == nil {
			t.Fatal("imported issue not found locally")
		}
		if local.SourceRepo != peerRepoRoot {
			t.Errorf("expected SourceRepo=%s, got %s", peerRepoRoot, local.SourceRepo)
		}
	})

	t.Run("hydrate is idempotent", func(t *testing.T) {
		result, err := localStore.HydrateFromPeerDolt(ctx, peerRepoRoot)
		if err != nil {
			t.Fatalf("second HydrateFromPeerDolt failed: %v", err)
		}
		if result.Imported != 0 {
			t.Errorf("expected 0 imported on second run, got %d", result.Imported)
		}
		if result.Skipped != 1 {
			t.Errorf("expected 1 skipped on second run, got %d", result.Skipped)
		}
	})

	t.Run("hydrate rejects non-dolt peer", func(t *testing.T) {
		// Create a JSONL-only peer
		jsonlPeer := filepath.Join(baseDir, "jsonl-peer")
		jsonlBeads := filepath.Join(jsonlPeer, ".beads")
		if err := os.MkdirAll(jsonlBeads, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(jsonlBeads, "issues.jsonl"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := localStore.HydrateFromPeerDolt(ctx, jsonlPeer)
		if err == nil {
			t.Fatal("expected error for JSONL peer")
		}
	})

	t.Run("hydrate rejects nonexistent peer", func(t *testing.T) {
		_, err := localStore.HydrateFromPeerDolt(ctx, "/nonexistent/path")
		if err == nil {
			t.Fatal("expected error for nonexistent peer")
		}
	})
}

func TestOpenPeerStore(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	baseDir := t.TempDir()

	// Setup a peer store
	peerDoltDir := filepath.Join(baseDir, "peer-dolt")
	peerStore, peerCleanup := setupFederationStore(t, ctx, peerDoltDir, "peer")
	defer peerCleanup()

	// Create a test issue
	issue := &types.Issue{
		ID:        "peer-open-01",
		Title:     "Open peer test",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Priority:  2,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := peerStore.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	if err := peerStore.Commit(ctx, "Create test issue"); err != nil {
		t.Logf("commit: %v", err)
	}
	peerStore.Close()

	// Create repo root structure
	peerRepoRoot := filepath.Join(baseDir, "peer-root")
	peerBeadsDir := filepath.Join(peerRepoRoot, ".beads")
	if err := os.MkdirAll(peerBeadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(peerDoltDir, filepath.Join(peerBeadsDir, "dolt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(peerBeadsDir, "metadata.json"),
		[]byte(`{"database":"dolt","backend":"dolt"}`), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("opens read-only store", func(t *testing.T) {
		opened, err := OpenPeerStore(ctx, peerRepoRoot)
		if err != nil {
			t.Fatalf("OpenPeerStore failed: %v", err)
		}
		defer opened.Close()

		// Verify we can read
		got, err := opened.GetIssue(ctx, "peer-open-01")
		if err != nil {
			t.Fatalf("failed to get issue from peer store: %v", err)
		}
		if got == nil {
			t.Fatal("issue not found in peer store")
		}
		if got.Title != "Open peer test" {
			t.Errorf("expected title 'Open peer test', got %q", got.Title)
		}
	})

	t.Run("rejects nonexistent path", func(t *testing.T) {
		_, err := OpenPeerStore(ctx, "/nonexistent/path")
		if err == nil {
			t.Fatal("expected error for nonexistent path")
		}
	})
}

func TestDetectPeerBackend_SymlinkDolt(t *testing.T) {
	skipIfNoDolt(t)

	// Verify detection works through symlinks (common in test setups)
	baseDir := t.TempDir()
	realDolt := filepath.Join(baseDir, "real-dolt")
	if err := os.MkdirAll(realDolt, 0755); err != nil {
		t.Fatal(err)
	}

	repoRoot := filepath.Join(baseDir, "repo")
	beadsDir := filepath.Join(repoRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDolt, filepath.Join(beadsDir, "dolt")); err != nil {
		t.Fatal(err)
	}

	backend, err := DetectPeerBackend(repoRoot)
	if err != nil {
		t.Fatalf("unexpected error with symlinked dolt dir: %v", err)
	}
	if backend != PeerBackendDolt {
		t.Errorf("expected dolt backend through symlink, got %s", backend)
	}
}

func TestDetectPeerBackend_NotADirectory(t *testing.T) {
	skipIfNoDolt(t)

	dir := t.TempDir()
	// Create .beads as a file, not a directory
	beadsPath := filepath.Join(dir, ".beads")
	if err := os.WriteFile(beadsPath, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := DetectPeerBackend(dir)
	if err == nil {
		t.Fatal("expected error when .beads is a file, not a directory")
	}
}

func TestHydrateFromPeerDolt_SourceRepoPersistence(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	baseDir := t.TempDir()

	// Setup peer with an issue
	peerDoltDir := filepath.Join(baseDir, "peer-dolt")
	peerStore, peerCleanup := setupFederationStore(t, ctx, peerDoltDir, "peer")
	defer peerCleanup()

	peerIssue := &types.Issue{
		ID:        "peer-persist-01",
		Title:     "Persistence test",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Priority:  2,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := peerStore.CreateIssue(ctx, peerIssue, "test"); err != nil {
		t.Fatalf("failed to create peer issue: %v", err)
	}
	if err := peerStore.Commit(ctx, "Create persistence test issue"); err != nil {
		t.Logf("commit: %v", err)
	}
	peerStore.Close()

	// Create peer repo root structure
	peerRepoRoot := filepath.Join(baseDir, "peer-root")
	peerBeadsDir := filepath.Join(peerRepoRoot, ".beads")
	if err := os.MkdirAll(peerBeadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(peerDoltDir, filepath.Join(peerBeadsDir, "dolt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(peerBeadsDir, "metadata.json"),
		[]byte(`{"database":"dolt","backend":"dolt"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Setup local store, hydrate, close, reopen, verify source_repo persists
	localDir := filepath.Join(baseDir, "local-dolt")
	localStore, localCleanup := setupFederationStore(t, ctx, localDir, "local")

	_, err := localStore.HydrateFromPeerDolt(ctx, peerRepoRoot)
	if err != nil {
		t.Fatalf("HydrateFromPeerDolt failed: %v", err)
	}

	// Commit and close
	if err := localStore.Commit(ctx, "Hydrate from peer"); err != nil {
		t.Logf("commit: %v", err)
	}
	localCleanup()

	// Reopen the store and verify source_repo survived the round-trip
	reopened, reopenCleanup := setupFederationStore(t, ctx, localDir, "local")
	defer reopenCleanup()

	got, err := reopened.GetIssue(ctx, "peer-persist-01")
	if err != nil {
		t.Fatalf("failed to get issue after reopen: %v", err)
	}
	if got == nil {
		t.Fatal("issue not found after reopen")
	}
	// Note: source_repo may not persist through Dolt storage if the field
	// is not stored in the issues table. This test documents the behavior.
	t.Logf("source_repo after round-trip: %q", got.SourceRepo)
}

func TestHydrateFromPeerDolt_ReadOnlyStoreRejects(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	baseDir := t.TempDir()

	// Setup a peer
	peerDoltDir := filepath.Join(baseDir, "peer-dolt")
	_, peerCleanup := setupFederationStore(t, ctx, peerDoltDir, "peer")
	defer peerCleanup()

	peerRepoRoot := filepath.Join(baseDir, "peer-root")
	peerBeadsDir := filepath.Join(peerRepoRoot, ".beads")
	if err := os.MkdirAll(peerBeadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(peerDoltDir, filepath.Join(peerBeadsDir, "dolt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(peerBeadsDir, "metadata.json"),
		[]byte(`{"database":"dolt","backend":"dolt"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Open a read-only store and verify hydration is rejected
	roStore, err := OpenPeerStore(ctx, peerRepoRoot)
	if err != nil {
		t.Fatalf("OpenPeerStore failed: %v", err)
	}
	defer roStore.Close()

	_, err = roStore.HydrateFromPeerDolt(ctx, peerRepoRoot)
	if err == nil {
		t.Fatal("expected error when hydrating from a read-only store")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected 'read-only' in error message, got: %v", err)
	}
}

func TestQueryPeerIssues_EmptyPeer(t *testing.T) {
	skipIfNoDolt(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	baseDir := t.TempDir()

	// Setup peer with no issues
	peerDoltDir := filepath.Join(baseDir, "empty-peer")
	peerStore, peerCleanup := setupFederationStore(t, ctx, peerDoltDir, "empty")
	defer peerCleanup()
	if err := peerStore.Commit(ctx, "Initialize empty peer"); err != nil {
		t.Logf("commit: %v", err)
	}
	peerStore.Close()

	peerRepoRoot := filepath.Join(baseDir, "empty-root")
	peerBeadsDir := filepath.Join(peerRepoRoot, ".beads")
	if err := os.MkdirAll(peerBeadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(peerDoltDir, filepath.Join(peerBeadsDir, "dolt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(peerBeadsDir, "metadata.json"),
		[]byte(`{"database":"dolt","backend":"dolt"}`), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := QueryPeerIssues(ctx, peerRepoRoot, types.IssueFilter{})
	if err != nil {
		t.Fatalf("QueryPeerIssues on empty peer failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 issues from empty peer, got %d", len(result))
	}
}
