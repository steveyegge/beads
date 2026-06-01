package doltutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
)

func TestRemoteCacheRoundTrip(t *testing.T) {
	t.Setenv("BEADS_NO_REMOTE_CACHE", "")

	dbPath := filepath.Join(t.TempDir(), "db")
	InvalidateCLIRemotes(dbPath)
	t.Cleanup(func() { InvalidateCLIRemotes(dbPath) })

	if _, ok := readRemoteCache(dbPath); ok {
		t.Fatal("cache unexpectedly existed before write")
	}

	want := []storage.RemoteInfo{{Name: "origin", URL: "git+ssh://git@example.com/org/repo.git"}}
	writeRemoteCache(dbPath, want)

	got, ok := readRemoteCache(dbPath)
	if !ok {
		t.Fatal("cache miss after write")
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("cache = %#v, want %#v", got, want)
	}
}

func TestRemoteCacheRejectsExpiredEntries(t *testing.T) {
	t.Setenv("BEADS_NO_REMOTE_CACHE", "")

	dbPath := filepath.Join(t.TempDir(), "db")
	InvalidateCLIRemotes(dbPath)
	t.Cleanup(func() { InvalidateCLIRemotes(dbPath) })

	entry := remoteCacheEntry{
		Stamp:   time.Now().Add(-remoteListTTL - time.Second),
		Remotes: []storage.RemoteInfo{{Name: "origin", URL: "git+ssh://git@example.com/org/repo.git"}},
	}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remoteCacheFile(dbPath), b, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, ok := readRemoteCache(dbPath); ok {
		t.Fatal("expired cache entry was accepted")
	}
}

func TestListCLIRemotesSkipsNonDoltDirectory(t *testing.T) {
	t.Setenv("BEADS_NO_REMOTE_CACHE", "")

	dbPath := t.TempDir()
	InvalidateCLIRemotes(dbPath)
	t.Cleanup(func() { InvalidateCLIRemotes(dbPath) })

	got, err := ListCLIRemotes(dbPath)
	if err != nil {
		t.Fatalf("ListCLIRemotes(non-dolt dir) error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListCLIRemotes(non-dolt dir) = %#v, want empty", got)
	}
	if cached, ok := readRemoteCache(dbPath); !ok || len(cached) != 0 {
		t.Fatalf("negative cache = %#v, ok=%v; want empty cached result", cached, ok)
	}
}
