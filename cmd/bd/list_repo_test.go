//go:build cgo

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
)

// TestListRepoPeerQuery tests the --repo flag for querying peer Dolt databases.
func TestListRepoPeerQuery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create a peer store with test issues via the test server
	peerDir := t.TempDir()
	peerDBPath := filepath.Join(peerDir, ".beads", "dolt")
	peerStore := newTestStoreWithPrefix(t, peerDBPath, "peer")

	peerIssue := &types.Issue{
		Title:     "Peer visible issue",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Priority:  2,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := peerStore.CreateIssue(ctx, peerIssue, "test"); err != nil {
		t.Fatalf("failed to create peer issue: %v", err)
	}

	t.Run("DetectPeerBackend finds dolt", func(t *testing.T) {
		backend, err := dolt.DetectPeerBackend(peerDir)
		if err != nil {
			t.Fatalf("DetectPeerBackend failed: %v", err)
		}
		if backend != dolt.PeerBackendDolt {
			t.Errorf("expected dolt backend, got %s", backend)
		}
	})

	t.Run("DetectPeerBackend rejects missing beads dir", func(t *testing.T) {
		emptyDir := t.TempDir()
		_, err := dolt.DetectPeerBackend(emptyDir)
		if err == nil {
			t.Fatal("expected error for directory without .beads")
		}
	})

	t.Run("DetectPeerBackend finds jsonl", func(t *testing.T) {
		jsonlDir := t.TempDir()
		beadsDir := filepath.Join(jsonlDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}

		backend, err := dolt.DetectPeerBackend(jsonlDir)
		if err != nil {
			t.Fatalf("DetectPeerBackend failed: %v", err)
		}
		if backend != dolt.PeerBackendJSONL {
			t.Errorf("expected jsonl backend, got %s", backend)
		}
	})

	// QueryPeerIssues and OpenPeerStore need their own server for the peer.
	// Since the test server is shared and the peer database is on it,
	// we test the detection and error paths here, and test the full
	// query/hydration flow in the integration tests (multirepo_test.go).

	t.Run("QueryPeerIssues rejects nonexistent path", func(t *testing.T) {
		_, err := dolt.QueryPeerIssues(ctx, "/nonexistent/path/to/repo", types.IssueFilter{})
		if err == nil {
			t.Fatal("expected error for nonexistent peer path")
		}
	})
}

// TestListRepoHydration tests HydrateFromPeerDolt error paths.
func TestListRepoHydration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create local store
	localDir := t.TempDir()
	localDBPath := filepath.Join(localDir, ".beads", "dolt")
	localStore := newTestStoreWithPrefix(t, localDBPath, "local")

	t.Run("hydrate rejects jsonl peer", func(t *testing.T) {
		jsonlDir := t.TempDir()
		beadsDir := filepath.Join(jsonlDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := localStore.HydrateFromPeerDolt(ctx, jsonlDir)
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
