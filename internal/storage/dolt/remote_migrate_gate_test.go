package dolt

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage/schema"
)

// TestDoltNew_RemoteMigrateGate_BlocksReopen is a full-chain integration test:
// opening a read/write server-mode store against an existing, remote-backed
// database that is behind the binary must refuse to auto-migrate and return a
// *schema.RemoteMigrateGateError instead of silently forking the schema (#4259).
func TestDoltNew_RemoteMigrateGate_BlocksReopen(t *testing.T) {
	skipIfNoDolt(t)
	t.Setenv(schema.AllowRemoteMigrateEnv, "0")

	ctx, cancel := testContext(t)
	defer cancel()

	tmpDir := t.TempDir()
	dbName := uniqueTestDBName(t)

	// Create and fully migrate the database.
	store, err := New(ctx, &Config{
		Path:            tmpDir,
		CommitterName:   "test",
		CommitterEmail:  "test@example.com",
		Database:        dbName,
		CreateIfMissing: true,
	})
	if err != nil {
		t.Fatalf("New (create): %v", err)
	}
	defer func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*testTimeout)
		defer dropCancel()
		_, _ = store.db.ExecContext(dropCtx, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
		store.Close()
	}()

	// Simulate an existing database one migration behind this binary by dropping
	// the latest cursor row (the schema change itself stays applied; only the
	// recorded version regresses, so the gate sees a pending migration).
	if _, err := store.db.ExecContext(ctx,
		"DELETE FROM schema_migrations WHERE version = ?", schema.LatestVersion()); err != nil {
		t.Fatalf("regress schema_migrations: %v", err)
	}
	// Register a remote so the database is remote-backed.
	if _, err := store.db.ExecContext(ctx,
		"CALL DOLT_REMOTE('add', 'origin', ?)", "file://"+filepath.Join(tmpDir, "remote")); err != nil {
		t.Fatalf("add remote: %v", err)
	}

	// Reopening read/write must hit the gate.
	reopened, reErr := New(ctx, &Config{
		Path:           tmpDir,
		CommitterName:  "test",
		CommitterEmail: "test@example.com",
		Database:       dbName,
	})
	if reErr == nil {
		reopened.Close()
		t.Fatal("New (reopen) = nil, want *schema.RemoteMigrateGateError for a behind, remote-backed DB")
	}
	if !schema.IsRemoteMigrateGateError(reErr) {
		t.Fatalf("error = %T (%v), want error wrapping *schema.RemoteMigrateGateError", reErr, reErr)
	}
}
