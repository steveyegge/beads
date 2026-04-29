package schema

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

var (
	createTableRe = regexp.MustCompile(`(?is)^\s*CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+` + "`?" + `([A-Za-z0-9_]+)` + "`?")
	inlineIndexRe = regexp.MustCompile(`(?i)^\s*(?:UNIQUE\s+)?INDEX\s+` + "`?" + `([A-Za-z0-9_]+)` + "`?" + `\s*(\([^)]+\))\s*,?\s*$`)
)

// MigrateUpSQLite applies the shared Beads migrations after translating the
// small MySQL/Dolt SQL subset that SQLite does not parse.
func MigrateUpSQLite(ctx context.Context, db DBConn) (int, error) {
	if _, err := db.ExecContext(ctx, schemaMigrationsBootstrapSQL); err != nil {
		return 0, fmt.Errorf("creating schema_migrations table: %w", err)
	}

	var current int
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current); err != nil {
		return 0, fmt.Errorf("reading current migration version: %w", err)
	}
	if current >= LatestVersion() {
		return 0, nil
	}

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
		if v > current {
			pending = append(pending, migrationFile{version: v, name: e.Name()})
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].version < pending[j].version })

	for _, mf := range pending {
		data, err := upMigrations.ReadFile("migrations/" + mf.name)
		if err != nil {
			return 0, fmt.Errorf("reading migration %s: %w", mf.name, err)
		}
		for _, stmt := range translateSQLiteStatements(splitStatements(string(data))) {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				if !isConcurrentInitError(err) {
					return 0, fmt.Errorf("migration %s: statement failed: %w\nSQL: %s", mf.name, err, stmt)
				}
			}
		}
		if _, err := db.ExecContext(ctx, "INSERT OR IGNORE INTO schema_migrations (version) VALUES (?)", mf.version); err != nil {
			if !isConcurrentInitError(err) {
				return 0, fmt.Errorf("recording migration %s: %w", mf.name, err)
			}
		}
	}
	return len(pending), nil
}

// CreateIgnoredTablesSQLite recreates dolt-ignored tables using SQLite syntax.
func CreateIgnoredTablesSQLite(ctx context.Context, db DBConn) error {
	if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS dolt_ignore (pattern TEXT NOT NULL PRIMARY KEY, ignored BOOLEAN NOT NULL)"); err != nil {
		return fmt.Errorf("create dolt_ignore: %w", err)
	}
	for _, stmt := range translateSQLiteStatements(IgnoredTableDDL()) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if !isConcurrentInitError(err) {
				return fmt.Errorf("create ignored table: %w\nSQL: %s", err, stmt)
			}
		}
	}
	return nil
}

func translateSQLiteStatements(stmts []string) []string {
	var out []string
	for _, stmt := range stmts {
		stmt = translateSQLiteBasics(stmt)
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stmt)), "CREATE TABLE") {
			tableStmt, indexes := splitInlineIndexes(stmt)
			out = append(out, tableStmt)
			out = append(out, indexes...)
			continue
		}
		out = append(out, stmt)
	}
	return out
}

func translateSQLiteBasics(stmt string) string {
	repls := []struct{ old, new string }{
		{"INSERT IGNORE INTO", "INSERT OR IGNORE INTO"},
		{"ON UPDATE CURRENT_TIMESTAMP", ""},
		{"JSON DEFAULT (JSON_OBJECT())", "TEXT DEFAULT '{}'"},
		{"JSON DEFAULT (json_object())", "TEXT DEFAULT '{}'"},
		{" JSON ", " TEXT "},
		{"NOW()", "CURRENT_TIMESTAMP"},
		{"now()", "CURRENT_TIMESTAMP"},
		{"CREATE OR REPLACE VIEW", "CREATE VIEW IF NOT EXISTS"},
		{"create or replace view", "CREATE VIEW IF NOT EXISTS"},
		{"ESCAPE '\\\\'", "ESCAPE '\\'"},
	}
	for _, r := range repls {
		stmt = strings.ReplaceAll(stmt, r.old, r.new)
	}
	if regexp.MustCompile(`(?i)^CREATE\s+INDEX\s+`).MatchString(stmt) &&
		!regexp.MustCompile(`(?i)^CREATE\s+INDEX\s+IF\s+NOT\s+EXISTS\s+`).MatchString(stmt) {
		stmt = regexp.MustCompile(`(?i)^CREATE\s+INDEX\s+`).ReplaceAllString(stmt, "CREATE INDEX IF NOT EXISTS ")
	}
	if regexp.MustCompile(`(?i)^CALL\s+DOLT_(ADD|COMMIT)\(`).MatchString(stmt) {
		return ""
	}
	return stmt
}

func splitInlineIndexes(stmt string) (string, []string) {
	m := createTableRe.FindStringSubmatch(stmt)
	if len(m) != 2 {
		return stmt, nil
	}
	table := m[1]
	lines := strings.Split(stmt, "\n")
	var kept []string
	var indexes []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		idx := inlineIndexRe.FindStringSubmatch(trimmed)
		if len(idx) == 3 {
			indexes = append(indexes, fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s %s", idx[1], table, idx[2]))
			continue
		}
		kept = append(kept, line)
	}
	for i := len(kept) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(kept[i])
		if trimmed == "" || trimmed == ")" || trimmed == ");" {
			continue
		}
		kept[i] = strings.TrimRight(kept[i], " \t,")
		break
	}
	return strings.Join(kept, "\n"), indexes
}
