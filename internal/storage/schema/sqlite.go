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
	createTableRe          = regexp.MustCompile(`(?is)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?` + "`?" + `([A-Za-z0-9_]+)` + "`?")
	inlineIndexRe          = regexp.MustCompile(`(?i)^\s*(UNIQUE\s+)?(?:INDEX|KEY)\s+` + "`?" + `([A-Za-z0-9_]+)` + "`?" + `\s*(\([^)]+\))\s*,?\s*$`)
	quotedAlterAddColumnRe = regexp.MustCompile(`(?is)'(ALTER\s+TABLE\s+` + "`?" + `[A-Za-z0-9_]+` + "`?" + `\s+ADD\s+COLUMN\s+[^']+)'`)
	quotedRenameTableRe    = regexp.MustCompile(`(?is)'(RENAME\s+TABLE\s+` + "`?" + `[A-Za-z0-9_]+` + "`?" + `\s+TO\s+` + "`?" + `[A-Za-z0-9_]+` + "`?" + `)'`)
	renameTableRe          = regexp.MustCompile(`(?is)^\s*RENAME\s+TABLE\s+` + "`?" + `([A-Za-z0-9_]+)` + "`?" + `\s+TO\s+` + "`?" + `([A-Za-z0-9_]+)` + "`?" + `\s*$`)
)

// MigrateUpSQLite applies the shared Beads migrations after translating the
// small MySQL/Dolt SQL subset that SQLite does not parse.
func MigrateUpSQLite(ctx context.Context, db DBConn) (int, error) {
	if _, err := db.ExecContext(ctx, mainSource.bootstrapSQL()); err != nil {
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
	for _, mf := range ignoredSource.list() {
		data, err := ignoredSource.files.ReadFile(ignoredSource.dir + "/" + mf.name)
		if err != nil {
			return fmt.Errorf("reading ignored migration %s: %w", mf.name, err)
		}
		for _, stmt := range translateSQLiteStatements(splitStatements(string(data))) {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				if !isConcurrentInitError(err) {
					return fmt.Errorf("create ignored table: %w\nSQL: %s", err, stmt)
				}
			}
		}
	}
	return nil
}

func splitStatements(sqlText string) []string {
	sqlText = regexp.MustCompile(`(?is)/\*.*?\*/`).ReplaceAllString(sqlText, "")
	sqlText = stripSQLiteLineComments(sqlText)
	parts := strings.Split(sqlText, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if stmt := strings.TrimSpace(part); stmt != "" {
			out = append(out, stmt)
		}
	}
	return out
}

func isConcurrentInitError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "already exists") ||
		strings.Contains(text, "already another table or index") ||
		strings.Contains(text, "duplicate column") ||
		strings.Contains(text, "duplicate key") ||
		strings.Contains(text, "no such table: dolt_ignore")
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
	stmt = stripSQLiteLineComments(stmt)
	repls := []struct{ old, new string }{
		{"INSERT IGNORE INTO", "INSERT OR IGNORE INTO"},
		{"ON UPDATE CURRENT_TIMESTAMP", ""},
		{"JSON DEFAULT (JSON_OBJECT())", "TEXT DEFAULT '{}'"},
		{"JSON DEFAULT (json_object())", "TEXT DEFAULT '{}'"},
		{" JSON ", " TEXT "},
		{"depends_on_id VARCHAR(255) NOT NULL", "depends_on_id VARCHAR(255)"},
		{" DEFAULT (UUID())", ""},
		{" DEFAULT (uuid())", ""},
		{"NOW()", "CURRENT_TIMESTAMP"},
		{"now()", "CURRENT_TIMESTAMP"},
		{"CREATE OR REPLACE VIEW", "CREATE VIEW IF NOT EXISTS"},
		{"create or replace view", "CREATE VIEW IF NOT EXISTS"},
		{"ESCAPE '\\\\'", "ESCAPE '\\'"},
		{" PRIMARY KEY FIRST", ""},
		{" NOT NULL FIRST", ""},
		{" FIRST", ""},
	}
	for _, r := range repls {
		stmt = strings.ReplaceAll(stmt, r.old, r.new)
	}
	if m := renameTableRe.FindStringSubmatch(stmt); len(m) == 3 {
		return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", m[1], m[2])
	}
	if regexp.MustCompile(`(?i)^CREATE\s+INDEX\s+`).MatchString(stmt) &&
		!regexp.MustCompile(`(?i)^CREATE\s+INDEX\s+IF\s+NOT\s+EXISTS\s+`).MatchString(stmt) {
		stmt = regexp.MustCompile(`(?i)^CREATE\s+INDEX\s+`).ReplaceAllString(stmt, "CREATE INDEX IF NOT EXISTS ")
	}
	if regexp.MustCompile(`(?i)^CALL\s+DOLT_(ADD|COMMIT)\(`).MatchString(stmt) {
		return ""
	}
	if strings.Contains(strings.ToLower(stmt), "dolt_nonlocal_tables") {
		return ""
	}
	if regexp.MustCompile(`(?i)^SET\b`).MatchString(strings.TrimSpace(stmt)) {
		if strings.Contains(stmt, "@needs_migration") {
			return ""
		}
		if rename := extractQuotedRenameTable(stmt); rename != "" {
			return translateSQLiteBasics(rename)
		}
		if alter := extractQuotedAlterAddColumn(stmt); alter != "" {
			return translateSQLiteBasics(alter)
		}
		return ""
	}
	if regexp.MustCompile(`(?i)^(SET|PREPARE|EXECUTE|DEALLOCATE\s+PREPARE)\b`).MatchString(strings.TrimSpace(stmt)) {
		return ""
	}
	return stmt
}

func extractQuotedRenameTable(stmt string) string {
	matches := quotedRenameTableRe.FindAllStringSubmatch(stmt, -1)
	for _, match := range matches {
		if len(match) == 2 {
			return strings.ReplaceAll(match[1], "''", "'")
		}
	}
	return ""
}

func extractQuotedAlterAddColumn(stmt string) string {
	matches := quotedAlterAddColumnRe.FindAllStringSubmatch(stmt, -1)
	for _, match := range matches {
		if len(match) == 2 {
			return strings.ReplaceAll(match[1], "''", "'")
		}
	}
	return ""
}

func stripSQLiteLineComments(stmt string) string {
	lines := strings.Split(stmt, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
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
		if len(idx) == 4 {
			unique := ""
			if strings.TrimSpace(idx[1]) != "" {
				unique = "UNIQUE "
			}
			indexes = append(indexes, fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s %s", unique, idx[2], table, idx[3]))
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
