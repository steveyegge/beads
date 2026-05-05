package migration

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// allMigratedTables enumerates every destination table this package writes,
// in FK-safe insert order. Used by countLosslessRows (lossless slice only),
// truncateBDTables (full slice), and copyAllTables (full slice). Audit
// tables (events, wisp_events) and operational tables (local_metadata,
// repo_mtimes, bd_schema_migrations, federation_peers) are intentionally
// absent — see ADR be-l7t.5 §5.
var allMigratedTables = []string{
	// Lossless (FR-6) — parents first so FK constraints inside the same
	// transaction never fire.
	"issues",
	"wisps",
	"dependencies",
	"wisp_dependencies",
	"labels",
	"wisp_labels",
	"comments",
	"wisp_comments",
	// Configuration carryover (recommended) — order does not matter, kept
	// alphabetical for stable diffs.
	"child_counters",
	"compaction_snapshots",
	"config",
	"custom_statuses",
	"custom_types",
	"issue_counter",
	"issue_snapshots",
	"metadata",
}

// losslessTables is the eight-table set covered by the empty-destination
// check. A non-zero count in any of these aborts a non-force migration.
var losslessTables = []string{
	"issues",
	"wisps",
	"dependencies",
	"wisp_dependencies",
	"labels",
	"wisp_labels",
	"comments",
	"wisp_comments",
}

// truncateBDTables clears every destination table in allMigratedTables in a
// single TRUNCATE … CASCADE statement and returns the cleared list.
//
// CASCADE is required because dependencies/labels/comments/child_counters/
// snapshots all FK into issues (and similar wisp shapes), and PG refuses
// TRUNCATE without it. The CASCADE chain stays inside our owned set —
// bd_schema_migrations, local_metadata, and repo_mtimes have no inbound
// references from the listed tables, so they are unaffected.
func truncateBDTables(ctx context.Context, tx pgx.Tx) ([]string, error) {
	stmt := "TRUNCATE TABLE " + joinIdent(allMigratedTables) + " CASCADE"
	if _, err := tx.Exec(ctx, stmt); err != nil {
		return nil, fmt.Errorf("truncate: %w", err)
	}
	cleared := make([]string, len(allMigratedTables))
	copy(cleared, allMigratedTables)
	return cleared, nil
}

// countAuditEvents returns the combined row count of events + wisp_events on
// the source. Surfaced as the FR-9 stderr warning; not migrated.
func countAuditEvents(ctx context.Context, srcDB *sql.DB) (int, error) {
	var total int
	for _, table := range []string{"events", "wisp_events"} {
		var n int
		err := srcDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+identMySQL(table)).Scan(&n)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

// countLosslessRows returns destination row counts for the eight lossless
// tables. Used both for the empty-destination check and dry-run reporting.
func countLosslessRows(ctx context.Context, pool *pgxpool.Pool) (map[string]int, error) {
	out := make(map[string]int, len(losslessTables))
	for _, table := range losslessTables {
		var n int
		err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+identPG(table)).Scan(&n)
		if err != nil {
			return nil, fmt.Errorf("count %s: %w", table, err)
		}
		out[table] = n
	}
	return out, nil
}

// countSourceRows returns row counts for every migrated source table. Only
// used by --dry-run; the live path always streams.
func countSourceRows(ctx context.Context, srcDB *sql.DB) (map[string]int, error) {
	out := make(map[string]int, len(allMigratedTables))
	for _, table := range allMigratedTables {
		var n int
		err := srcDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+identMySQL(table)).Scan(&n)
		if err != nil {
			return nil, fmt.Errorf("count source %s: %w", table, err)
		}
		out[table] = n
	}
	return out, nil
}

// joinIdent joins safe identifiers with commas. The list is closed (only
// table names from allMigratedTables) so simple concatenation is OK; no
// user input flows here.
func joinIdent(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += identPG(n)
	}
	return out
}

// identPG returns a PG identifier — table names from our allowlist do not
// require quoting (all lowercase, no reserved words).
func identPG(name string) string { return name }

// identMySQL returns a MySQL/Dolt identifier. Same allowlist as identPG.
func identMySQL(name string) string { return name }
