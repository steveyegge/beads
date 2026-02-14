package doctor

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/factory"
)

// openDoctorDB opens a storage backend for the given beads directory and returns
// the underlying *sql.DB for direct SQL queries. The returned closeFunc must be
// called when done (it closes both the store and database).
func openDoctorDB(beadsDir string) (*sql.DB, func(), error) {
	ctx := context.Background()
	store, err := factory.NewFromConfigWithOptions(ctx, beadsDir, factory.Options{
		AllowWithRemoteDaemon: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open storage: %w", err)
	}
	db := store.UnderlyingDB()
	if db == nil {
		_ = store.Close()
		return nil, nil, fmt.Errorf("backend does not expose underlying database")
	}
	closer := func() { _ = store.Close() }
	return db, closer, nil
}

// openDoctorStore opens a storage.Storage for the given beads directory.
// Used by doctor checks that work through the storage interface.
func openDoctorStore(beadsDir string) (storage.Storage, error) {
	ctx := context.Background()
	return factory.NewFromConfigWithOptions(ctx, beadsDir, factory.Options{
		AllowWithRemoteDaemon: true,
	})
}
