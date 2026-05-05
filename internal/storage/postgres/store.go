// Package postgres implements the bd storage interface against PostgreSQL.
//
// The driver self-registers under storage.BackendPostgres at init() time;
// consumers blank-import this package to make `postgres` a valid backend
// for `storage.Open`. Pure Go via `github.com/jackc/pgx/v5`; no cgo.
//
// See ADR be-l7t.3 for the full design (interface coverage, schema
// translation, transaction semantics, credential safety).
package postgres

import (
	"context"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/steveyegge/beads/internal/storage"
)

// Compile-time interface assertions per ADR be-l7t.3 §6. Missing capability
// methods become build failures here, before any test runs.
var (
	_ storage.Storage              = (*PostgresStore)(nil)
	_ storage.BulkIssueStore       = (*PostgresStore)(nil)
	_ storage.DependencyQueryStore = (*PostgresStore)(nil)
	_ storage.AnnotationStore      = (*PostgresStore)(nil)
	_ storage.ConfigMetadataStore  = (*PostgresStore)(nil)
	_ storage.CompactionStore      = (*PostgresStore)(nil)
	_ storage.AdvancedQueryStore   = (*PostgresStore)(nil)
	_ storage.LifecycleManager     = (*PostgresStore)(nil)
)

// PostgresStore implements storage.Storage and the MUST capability sub-interfaces.
type PostgresStore struct {
	pool   *pgxpool.Pool
	closed atomic.Bool
}

func init() {
	storage.RegisterDriver(storage.BackendPostgres, openPostgres)
}

// openPostgres is the factory function dispatched from storage.Open. It is
// registered with the storage registry from init() and pinned to
// storage.Factory by var assertion below.
func openPostgres(ctx context.Context, cfg storage.ConnectionConfig) (storage.Storage, error) {
	store, err := openStore(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return store, nil
}

var _ storage.Factory = openPostgres

// Close shuts down the pool. Safe to call multiple times.
func (s *PostgresStore) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}

// IsClosed reports whether Close has been called.
func (s *PostgresStore) IsClosed() bool {
	return s.closed.Load()
}

// Pool returns the underlying pgxpool. Internal-package callers (notably the
// migration package) need direct pgx access for COPY FROM, which is not
// expressible through the generic Storage interface. Do not retain the
// pointer past the lifetime of the store.
func (s *PostgresStore) Pool() *pgxpool.Pool {
	return s.pool
}
