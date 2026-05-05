package postgres

import (
	"context"
	_ "embed"
	"fmt"
	"time"
)

// bdSchemaMigrationLockID identifies the advisory lock used for first-connect
// schema migrations. Constant 64-bit value derived once from the FNV-1a hash
// of "beads-schema-migration"; never derived at runtime so concurrent migrators
// across bd versions always serialize on the same key.
const bdSchemaMigrationLockID int64 = 0x6265616473736370

//go:embed migrations/0001_initial.up.sql
var initialUpSQL string

// embeddedMigration carries a versioned SQL body. The slice below is the
// authoritative migration list; order matters.
type embeddedMigration struct {
	Version int
	Body    string
}

var embeddedMigrations = []embeddedMigration{
	{Version: 1, Body: initialUpSQL},
}

// runMigrations brings the database schema up to the latest embedded version.
// In ReadOnly mode it only verifies the schema is current and returns
// ErrSchemaOutOfDate otherwise.
func (s *PostgresStore) runMigrations(ctx context.Context, readOnly bool) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return wrapErr("acquire migration conn", err)
	}
	defer conn.Release()

	if readOnly {
		// Read-only: just confirm schema is at or beyond the latest embedded version.
		var current int
		err := conn.QueryRow(ctx,
			`SELECT COALESCE(MAX(version), 0) FROM bd_schema_migrations`,
		).Scan(&current)
		if err != nil {
			// bd_schema_migrations might not exist on a fresh DB — treat as out of date.
			return ErrSchemaOutOfDate
		}
		if current < latestEmbeddedVersion() {
			return ErrSchemaOutOfDate
		}
		return nil
	}

	// Acquire advisory lock; concurrent processes block here until release.
	if _, err := conn.Exec(ctx,
		"SELECT pg_advisory_lock($1)", bdSchemaMigrationLockID,
	); err != nil {
		return wrapErr("acquire advisory lock", err)
	}
	defer func() {
		// Release on the same connection that acquired it; pool releasing
		// returns the connection to the pool with the session lock still
		// held, so we explicitly release. Use a fresh context with a short
		// timeout — if the caller canceled the parent ctx mid-migration,
		// reusing it would no-op the unlock and leak the lock to the next
		// process that tries to migrate.
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", bdSchemaMigrationLockID)
	}()

	// Ensure bookkeeping table exists.
	if _, err := conn.Exec(ctx, `
        CREATE TABLE IF NOT EXISTS bd_schema_migrations (
            version    INTEGER PRIMARY KEY,
            applied_at TIMESTAMP NOT NULL DEFAULT NOW()
        )
    `); err != nil {
		return wrapErr("create bd_schema_migrations", err)
	}

	var current int
	if err := conn.QueryRow(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM bd_schema_migrations`,
	).Scan(&current); err != nil {
		return wrapErr("read bd_schema_migrations", err)
	}

	for _, m := range embeddedMigrations {
		if m.Version <= current {
			continue
		}
		// Each migration runs in its own transaction so a failure halfway
		// through one cannot leave the bookkeeping table out of sync.
		tx, err := conn.Begin(ctx)
		if err != nil {
			return wrapErr(fmt.Sprintf("begin migration %d", m.Version), err)
		}
		if _, err := tx.Exec(ctx, m.Body); err != nil {
			_ = tx.Rollback(ctx)
			return wrapErr(fmt.Sprintf("apply migration %d", m.Version), err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO bd_schema_migrations(version) VALUES ($1)`, m.Version,
		); err != nil {
			_ = tx.Rollback(ctx)
			return wrapErr(fmt.Sprintf("record migration %d", m.Version), err)
		}
		if err := tx.Commit(ctx); err != nil {
			return wrapErr(fmt.Sprintf("commit migration %d", m.Version), err)
		}
	}
	return nil
}

func latestEmbeddedVersion() int {
	if len(embeddedMigrations) == 0 {
		return 0
	}
	return embeddedMigrations[len(embeddedMigrations)-1].Version
}
