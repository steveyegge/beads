//go:build cgo

package embeddeddolt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
	"github.com/steveyegge/beads/internal/storage/schema"
)

// TestEmbeddedRemoteMigrateGate_BlocksReopen verifies that the #4259
// remote-migrate gate also protects embedded mode (the mode the original report
// was filed against): reopening an existing, remote-backed embedded database
// that is behind the binary must refuse to auto-migrate.
func TestEmbeddedRemoteMigrateGate_BlocksReopen(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt tests")
	}
	t.Setenv(schema.AllowRemoteMigrateEnv, "0")

	ctx := t.Context()
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	dataDir := filepath.Join(beadsDir, "embeddeddolt")

	// Create and fully migrate the embedded database.
	store, err := embeddeddolt.Open(ctx, beadsDir, "testdb", "main")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Regress one migration and register a remote on a raw SQL connection so the
	// reopen sees a behind, remote-backed database.
	db, cleanup, err := embeddeddolt.OpenSQL(ctx, dataDir, "testdb", "main")
	if err != nil {
		store.Close()
		t.Fatalf("OpenSQL: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"DELETE FROM schema_migrations WHERE version = ?", schema.LatestVersion()); err != nil {
		t.Fatalf("regress schema_migrations: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"CALL DOLT_REMOTE('add', 'origin', ?)", "file://"+filepath.Join(t.TempDir(), "remote")); err != nil {
		t.Fatalf("add remote: %v", err)
	}
	_ = cleanup()
	store.Close()

	// Reopen must hit the gate.
	reopened, reErr := embeddeddolt.Open(ctx, beadsDir, "testdb", "main")
	if reErr == nil {
		reopened.Close()
		t.Fatal("Open (reopen) = nil, want *schema.RemoteMigrateGateError for a behind, remote-backed DB")
	}
	if !schema.IsRemoteMigrateGateError(reErr) {
		t.Fatalf("error = %T (%v), want error wrapping *schema.RemoteMigrateGateError", reErr, reErr)
	}
}
