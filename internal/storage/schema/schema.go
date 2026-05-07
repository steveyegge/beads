package schema

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// progressOut is where migration progress lines are written. Defaults to
// os.Stderr so that JSON pipelines on stdout (e.g. bd list --json | jq) are
// not polluted. Unexported so tests in this package can swap it without
// leaking a setter into production API.
var progressOut io.Writer = os.Stderr

const largeRigThreshold = 10000

// issueRowCounter returns the current issues-table row count, or an error if
// the table is unreachable (fresh install → table doesn't exist yet). The
// caller uses the error as the "no warning" signal. Variable so tests in this
// package that exercise runMigrations against a non-DB mock can stub out the
// query without panicking on QueryRowContext.
var issueRowCounter = func(ctx context.Context, db DBConn) (int64, error) {
	var n int64
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM issues").Scan(&n)
	return n, err
}

// emitLargeRigNotice writes the one-line large-rig warning to out when the
// issues count exceeds largeRigThreshold. An error from the counter is
// treated as "fresh install / table missing" and suppresses the warning —
// see be-8ja for the UX rationale.
func emitLargeRigNotice(out io.Writer, count int64, err error) {
	if err != nil || count <= largeRigThreshold {
		return
	}
	fmt.Fprintf(out, "Large rig detected (%d issues). This migration may take up to 60 seconds; do not interrupt.\n", count)
}

// humanMigrationName turns "0033_add_date_indexes.up.sql" into
// "add_date_indexes" for the progress line.
func humanMigrationName(filename string) string {
	s := strings.TrimSuffix(filename, ".up.sql")
	parts := strings.SplitN(s, "_", 2)
	if len(parts) < 2 {
		return s
	}
	return parts[1]
}

// DBConn is the minimal interface satisfied by *sql.DB, *sql.Tx, and *sql.Conn.
// It provides query and exec methods needed by the migration runner.
type DBConn interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

//go:embed migrations/*.up.sql
var upMigrations embed.FS

var (
	latestOnce sync.Once
	latestVer  int
)

const schemaMigrationsBootstrapSQL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version INT PRIMARY KEY,
	applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`

// LatestVersion returns the highest version number among the embedded .up.sql files.
// Computed once and cached.
func LatestVersion() int {
	latestOnce.Do(func() {
		entries, err := fs.ReadDir(upMigrations, "migrations")
		if err != nil {
			panic(fmt.Sprintf("schema: failed to read embedded migrations: %v", err))
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
				continue
			}
			v, err := parseVersion(e.Name())
			if err != nil {
				panic(fmt.Sprintf("schema: invalid migration filename %q: %v", e.Name(), err))
			}
			if v > latestVer {
				latestVer = v
			}
		}
	})
	return latestVer
}

// AllMigrationsSQL returns the schema_migrations bootstrap plus all .up.sql
// migration contents concatenated in order. Used by integration tests that need
// to initialize a schema via dolt sql CLI.
func AllMigrationsSQL() string {
	entries, err := fs.ReadDir(upMigrations, "migrations")
	if err != nil {
		panic(fmt.Sprintf("schema: failed to read embedded migrations: %v", err))
	}

	type mf struct {
		version int
		name    string
	}
	var files []mf
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		v, err := parseVersion(e.Name())
		if err != nil {
			continue
		}
		files = append(files, mf{version: v, name: e.Name()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })

	var b strings.Builder
	b.WriteString(schemaMigrationsBootstrapSQL)
	b.WriteString(";\n")
	for _, f := range files {
		data, err := upMigrations.ReadFile("migrations/" + f.name)
		if err != nil {
			continue
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}

// parseVersion extracts the leading integer from a migration filename like "0001_create_issues.up.sql".
func parseVersion(name string) (int, error) {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) == 0 {
		return 0, fmt.Errorf("no version prefix")
	}
	return strconv.Atoi(parts[0])
}

// MigrateUp applies all embedded .up.sql migrations that haven't been applied yet.
// Returns the number of migrations applied. Safe for use with both *sql.Tx and
// *sql.DB — the caller controls transaction boundaries.
func MigrateUp(ctx context.Context, db DBConn) (int, error) {
	if _, err := db.ExecContext(ctx, schemaMigrationsBootstrapSQL); err != nil {
		return 0, fmt.Errorf("creating schema_migrations table: %w", err)
	}

	// Find the current version.
	var current int
	err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current)
	if err == sql.ErrNoRows {
		current = 0
	} else if err != nil {
		return 0, fmt.Errorf("reading current migration version: %w", err)
	}

	if current >= LatestVersion() {
		return 0, nil
	}

	return runMigrations(ctx, db, current)
}

type migrationFile struct {
	version int
	name    string
}

func runMigrations(ctx context.Context, db DBConn, minVersion int) (int, error) {
	entries, err := fs.ReadDir(upMigrations, "migrations")
	if err != nil {
		return 0, fmt.Errorf("reading embedded migrations: %w", err)
	}

	var pending []migrationFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		v, err := parseVersion(e.Name())
		if err != nil {
			return 0, fmt.Errorf("parsing migration filename %q: %w", e.Name(), err)
		}
		if v > minVersion {
			pending = append(pending, migrationFile{version: v, name: e.Name()})
		}
	}

	sort.Slice(pending, func(i, j int) bool { return pending[i].version < pending[j].version })

	if len(pending) == 0 {
		return 0, nil
	}

	// One-shot large-rig notice. Treats a missing issues table as "fresh
	// install" and emits nothing — on a first-ever run there is no rig to
	// warn about, and the COUNT(*) query would error on the missing table.
	count, countErr := issueRowCounter(ctx, db)
	emitLargeRigNotice(progressOut, count, countErr)

	for _, mf := range pending {
		data, err := upMigrations.ReadFile("migrations/" + mf.name)
		if err != nil {
			return 0, fmt.Errorf("reading migration %s: %w", mf.name, err)
		}

		fmt.Fprintf(progressOut, "Applying migration %04d: %s…\n", mf.version, humanMigrationName(mf.name))
		start := time.Now()

		if _, err := db.ExecContext(ctx, string(data)); err != nil {
			return 0, fmt.Errorf("migration %s: %w", mf.name, err)
		}

		if _, err := db.ExecContext(ctx, "INSERT IGNORE INTO schema_migrations (version) VALUES (?)", mf.version); err != nil {
			return 0, fmt.Errorf("recording migration %s: %w", mf.name, err)
		}

		fmt.Fprintf(progressOut, "  done (%.1fs)\n", time.Since(start).Seconds())
	}

	return len(pending), nil
}
