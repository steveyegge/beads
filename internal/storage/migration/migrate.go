package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/steveyegge/beads/internal/storage"
)

// Options bundles caller-specified migration knobs. Field zero-values are
// safe defaults: forced clear is opt-in, dry-run is opt-in, audit events
// stay deferred.
type Options struct {
	// Force, when true, TRUNCATEs every bd-owned destination table inside the
	// migration transaction before copying. Without --force, the migration
	// refuses to write into a non-empty destination and returns
	// ErrDestinationNotEmpty.
	Force bool
	// DryRun reports the planned action (counts read on the source, what
	// would be cleared on the destination) without committing.
	DryRun bool
	// IncludeEvents is a v1 placeholder that always returns
	// ErrUnimplementedFeature. The audit trail copying ships post-v1 once
	// bd_commits is wired (see ADR be-l7t.7).
	IncludeEvents bool
	// Stderr receives the audit-trail warning ("note: <N> audit-trail events
	// not migrated; see docs/AUDIT_TRAIL_POSTGRES.md"). May be nil — the
	// warning is suppressed when no writer is configured.
	Stderr io.Writer
}

// Result describes the work performed by a single Migrate call.
type Result struct {
	DryRun             bool
	TablesCleared      []string
	RowsByTable        map[string]int
	AuditEventsSkipped int
}

// Migrate copies bd-owned data from src (Dolt) to dst (Postgres). Both
// stores must already be open; the caller owns lifecycle. The destination
// must already have schema applied via storage.Open — this function never
// runs DDL.
//
// On success the destination contains a byte-identical copy of the source's
// lossless data set; the configuration carryover tables are populated so the
// PG instance is immediately usable.
func Migrate(ctx context.Context, src, dst storage.Storage, opts Options) (*Result, error) {
	if opts.IncludeEvents {
		return nil, fmt.Errorf("%w: --include-events", ErrUnimplementedFeature)
	}
	borrower, ok := storage.UnwrapStore(src).(sourceDBBorrower)
	if !ok {
		return nil, errors.New("migration: source backend does not expose BorrowSourceDB (need a Dolt-backed source)")
	}
	srcDB, releaseSrc, err := borrower.BorrowSourceDB(ctx)
	if err != nil {
		return nil, fmt.Errorf("borrow source db: %w", err)
	}
	defer func() { _ = releaseSrc() }()
	if srcDB == nil {
		return nil, errors.New("migration: source backend returned nil *sql.DB")
	}
	return MigrateFromDB(ctx, srcDB, dst, opts)
}

// MigrateFromDB performs the same Dolt → Postgres copy as Migrate but takes
// a pre-opened source *sql.DB. Use this when the caller has obtained the
// source DB outside the storage registry — for example, when bypassing the
// env-driven dolt server selection for explicit --source paths (be-b0h).
//
// The caller owns srcDB lifecycle.
func MigrateFromDB(ctx context.Context, srcDB *sql.DB, dst storage.Storage, opts Options) (*Result, error) {
	if opts.IncludeEvents {
		return nil, fmt.Errorf("%w: --include-events", ErrUnimplementedFeature)
	}
	pool, ok := pgxPoolFrom(dst)
	if !ok {
		return nil, errors.New("migration: destination backend is not Postgres-backed")
	}
	if srcDB == nil {
		return nil, errors.New("migration: source DB is nil")
	}

	auditEvents, err := countAuditEvents(ctx, srcDB)
	if err != nil {
		return nil, fmt.Errorf("count audit events on source: %w", err)
	}

	result := &Result{
		RowsByTable:        make(map[string]int, len(allMigratedTables)),
		AuditEventsSkipped: auditEvents,
	}

	if opts.DryRun {
		// Pre-flight only: report destination occupancy + source counts so
		// callers can make a decision without touching either side.
		dstCounts, err := countLosslessRows(ctx, pool)
		if err != nil {
			return nil, fmt.Errorf("count destination rows: %w", err)
		}
		nonEmpty := pickNonEmpty(dstCounts)
		if len(nonEmpty) > 0 && !opts.Force {
			return nil, &ErrDestinationNotEmpty{Counts: nonEmpty}
		}
		srcCounts, err := countSourceRows(ctx, srcDB)
		if err != nil {
			return nil, fmt.Errorf("count source rows: %w", err)
		}
		for table, n := range srcCounts {
			result.RowsByTable[table] = n
		}
		if opts.Force && len(nonEmpty) > 0 {
			result.TablesCleared = sortedKeys(nonEmpty)
		}
		result.DryRun = true
		emitAuditWarning(opts.Stderr, auditEvents)
		return result, nil
	}

	// Empty-destination check (FR-spec §6). Counts only the eight lossless
	// tables; configuration carryover tables are allowed to be pre-populated
	// (the seed config rows from migration 0001 ship with every fresh PG
	// schema).
	if !opts.Force {
		dstCounts, err := countLosslessRows(ctx, pool)
		if err != nil {
			return nil, fmt.Errorf("count destination rows: %w", err)
		}
		if nonEmpty := pickNonEmpty(dstCounts); len(nonEmpty) > 0 {
			return nil, &ErrDestinationNotEmpty{Counts: nonEmpty}
		}
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin destination transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	if opts.Force {
		cleared, err := truncateBDTables(ctx, tx)
		if err != nil {
			return nil, fmt.Errorf("truncate destination: %w", err)
		}
		result.TablesCleared = cleared
	}

	rows, err := copyAllTables(ctx, srcDB, tx)
	if err != nil {
		return nil, err
	}
	for table, n := range rows {
		result.RowsByTable[table] = n
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit destination transaction: %w", err)
	}
	committed = true

	emitAuditWarning(opts.Stderr, auditEvents)
	return result, nil
}

// ErrUnimplementedFeature is returned by Migrate when the caller requests a
// feature that is reserved for a post-v1 follow-up (currently:
// IncludeEvents).
var ErrUnimplementedFeature = errors.New("migration: feature not implemented in v1")

// ErrDestinationNotEmpty is returned by Migrate when --force is not set and
// any of the lossless destination tables already contains rows. Counts is
// the per-table row count for the populated tables, in deterministic order
// so error messages stay stable across runs.
type ErrDestinationNotEmpty struct {
	Counts map[string]int
}

func (e *ErrDestinationNotEmpty) Error() string {
	if len(e.Counts) == 0 {
		return "migration: destination contains existing data (use --force to overwrite)"
	}
	parts := make([]string, 0, len(e.Counts))
	for _, t := range sortedKeys(e.Counts) {
		parts = append(parts, fmt.Sprintf("%s=%d", t, e.Counts[t]))
	}
	return fmt.Sprintf("migration: destination contains existing data: %s (use --force to overwrite)",
		strings.Join(parts, ", "))
}

// sourceDBBorrower is the contract Migrate needs from a Dolt-backed source
// store: the ability to obtain a long-lived *sql.DB for the duration of the
// migration. DoltStore implements it by handing back its already-open db
// (no-op release); EmbeddedDoltStore implements it by opening a fresh
// connector that the caller must close. The interface lives here (rather
// than in the storage package) so cross-package consumers do not see it as
// part of the stable backend contract.
type sourceDBBorrower interface {
	BorrowSourceDB(ctx context.Context) (*sql.DB, func() error, error)
}

// SourceDBBorrower is the public face of sourceDBBorrower for callers that
// need to open the source DB themselves and pass it to MigrateFromDB. The
// migration source must implement this contract (every Dolt-backed store
// already does).
type SourceDBBorrower = sourceDBBorrower

// pgxPoolFrom returns the pgx pool from a Postgres-backed storage. The
// migration package depends on the *postgres.PostgresStore.Pool() method via
// this small interface so it does not import the postgres package directly
// (and so test fakes can satisfy it).
func pgxPoolFrom(s storage.Storage) (*pgxpool.Pool, bool) {
	type poolAccessor interface {
		Pool() *pgxpool.Pool
	}
	if p, ok := storage.UnwrapStore(s).(poolAccessor); ok {
		return p.Pool(), true
	}
	return nil, false
}

func emitAuditWarning(w io.Writer, count int) {
	if w == nil || count == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "note: %d audit-trail events not migrated; see docs/AUDIT_TRAIL_POSTGRES.md\n", count)
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func pickNonEmpty(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		if v > 0 {
			out[k] = v
		}
	}
	return out
}
