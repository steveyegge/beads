package rpc

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/testutil/teststore"
)

// newTestStore creates a test store with issue_prefix configured (bd-166)
// This prevents "database not initialized" errors in tests
func newTestStore(t *testing.T, dbPath string) storage.Storage {
	t.Helper()
	// dbPath is ignored - teststore creates its own isolated store
	_ = dbPath
	return teststore.New(t)
}

func newTestSocketPath(t *testing.T) string {
	t.Helper()

	// On unix, AF_UNIX socket paths have small length limits (notably on darwin).
	// Prefer a short base dir when available.
	if runtime.GOOS != "windows" {
		d, err := os.MkdirTemp("/tmp", "beads-sock-")
		if err == nil {
			t.Cleanup(func() { _ = os.RemoveAll(d) })
			return filepath.Join(d, "rpc.sock")
		}
	}

	return filepath.Join(t.TempDir(), "rpc.sock")
}
