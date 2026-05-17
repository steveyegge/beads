package schema

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type DBConn interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

//go:embed migrations/*.up.sql
var upMigrations embed.FS

//go:embed migrations/ignored/*.up.sql
var upIgnoredMigrations embed.FS

type migrationSource struct {
	files       embed.FS
	dir         string
	cursorTable string
}

var (
	mainSource = migrationSource{
		files:       upMigrations,
		dir:         "migrations",
		cursorTable: "schema_migrations",
	}
	ignoredSource = migrationSource{
		files:       upIgnoredMigrations,
		dir:         "migrations/ignored",
		cursorTable: "ignored_schema_migrations",
	}
)

var (
	latestOnce        sync.Once
	latestVer         int
	latestIgnoredOnce sync.Once
	latestIgnoredVer  int
)

func LatestVersion() int {
	latestOnce.Do(func() {
		latestVer = mainSource.latest()
	})
	return latestVer
}

func LatestIgnoredVersion() int {
	latestIgnoredOnce.Do(func() {
		latestIgnoredVer = ignoredSource.latest()
	})
	return latestIgnoredVer
}

func AllMigrationsSQL() string {
	var b strings.Builder
	b.WriteString(mainSource.bootstrapSQL())
	b.WriteString(";\n")
	for _, f := range mainSource.list() {
		data, err := mainSource.files.ReadFile(mainSource.dir + "/" + f.name)
		if err != nil {
			continue
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}

func parseVersion(name string) (int, error) {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) == 0 {
		return 0, fmt.Errorf("no version prefix")
	}
	return strconv.Atoi(parts[0])
}

func MigrateUp(ctx context.Context, db DBConn) (int, error) {
	applied, err := mainSource.migrate(ctx, db)
	if err != nil {
		return applied, err
	}

	backfilled, err := ensureBackfilledCustomStatusesCustomTypes(ctx, db)
	if err != nil {
		return applied, fmt.Errorf("backfill custom tables: %w", err)
	}

	appliedIgnored, err := MigrateIgnoredUp(ctx, db)
	if err != nil {
		return applied, err
	}

	if applied == 0 && !backfilled && appliedIgnored == 0 {
		return applied, nil
	}

	if _, err := db.ExecContext(ctx, "CALL DOLT_ADD('-A')"); err != nil {
		return applied, fmt.Errorf("staging migrations: %w", err)
	}
	if _, err := db.ExecContext(ctx, "CALL DOLT_COMMIT('-m', 'schema: apply migrations')"); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "nothing to commit") {
			return applied, fmt.Errorf("committing migrations: %w", err)
		}
	}

	return applied, nil
}

// MigrateIgnoredUp applies migrations for Dolt-ignored local tables such as
// wisps, wisp auxiliary tables, local metadata, and repo mtimes.
func MigrateIgnoredUp(ctx context.Context, db DBConn) (int, error) {
	if _, err := db.ExecContext(ctx, "REPLACE INTO dolt_ignore VALUES ('ignored_schema_migrations', true)"); err != nil {
		return 0, fmt.Errorf("registering ignored_schema_migrations in dolt_ignore: %w", err)
	}
	applied, err := ignoredSource.migrate(ctx, db)
	if err != nil {
		return applied, fmt.Errorf("ignored migrations: %w", err)
	}
	return applied, nil
}

type migrationFile struct {
	version int
	name    string
}

func (m migrationSource) bootstrapSQL() string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	version INT PRIMARY KEY,
	applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`, m.cursorTable)
}

func (m migrationSource) list() []migrationFile {
	entries, err := fs.ReadDir(m.files, m.dir)
	if err != nil {
		panic(fmt.Sprintf("schema: failed to read embedded %s: %v", m.dir, err))
	}
	var files []migrationFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		v, err := parseVersion(e.Name())
		if err != nil {
			panic(fmt.Sprintf("schema: invalid migration filename %q: %v", e.Name(), err))
		}
		files = append(files, migrationFile{version: v, name: e.Name()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
	return files
}

func (m migrationSource) latest() int {
	files := m.list()
	if len(files) == 0 {
		return 0
	}
	return files[len(files)-1].version
}

func (m migrationSource) migrate(ctx context.Context, db DBConn) (int, error) {
	if _, err := db.ExecContext(ctx, m.bootstrapSQL()); err != nil {
		return 0, fmt.Errorf("creating %s: %w", m.cursorTable, err)
	}

	var current int
	err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM "+m.cursorTable).Scan(&current)
	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("reading %s version: %w", m.cursorTable, err)
	}

	if current >= m.latest() {
		return 0, nil
	}

	count := 0
	for _, mf := range m.list() {
		if mf.version <= current {
			continue
		}
		data, err := m.files.ReadFile(m.dir + "/" + mf.name)
		if err != nil {
			return count, fmt.Errorf("reading migration %s: %w", mf.name, err)
		}
		if _, err := db.ExecContext(ctx, string(data)); err != nil {
			return count, fmt.Errorf("migration %s: %w", mf.name, err)
		}
		if _, err := db.ExecContext(ctx, "INSERT IGNORE INTO "+m.cursorTable+" (version) VALUES (?)", mf.version); err != nil {
			return count, fmt.Errorf("recording %s in %s: %w", mf.name, m.cursorTable, err)
		}
		count++
	}
	return count, nil
}
