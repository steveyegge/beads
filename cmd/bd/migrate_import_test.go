package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// TestMigrateImport_ColumnParityWithInsertIssue verifies that the migration
// INSERT in importToDolt covers the same columns as the canonical insertIssue
// helper in internal/storage/dolt/issues.go. This catches silent data loss
// when new columns are added to the schema but not to the migration path.
func TestMigrateImport_ColumnParityWithInsertIssue(t *testing.T) {
	root := moduleRoot(t)
	migrateColumns := extractInsertColumns(t, filepath.Join(root, "cmd/bd/migrate_import.go"))
	canonicalColumns := extractInsertColumns(t, filepath.Join(root, "internal/storage/dolt/issues.go"))

	if len(migrateColumns) == 0 {
		t.Fatal("failed to extract columns from migrate_import.go")
	}
	if len(canonicalColumns) == 0 {
		t.Fatal("failed to extract columns from issues.go")
	}

	// Sort both for comparison
	sort.Strings(migrateColumns)
	sort.Strings(canonicalColumns)

	// Find columns in canonical but missing from migrate
	missing := []string{}
	migrateSet := make(map[string]bool)
	for _, col := range migrateColumns {
		migrateSet[col] = true
	}
	for _, col := range canonicalColumns {
		if !migrateSet[col] {
			missing = append(missing, col)
		}
	}

	if len(missing) > 0 {
		t.Errorf("migrate_import.go INSERT is missing columns present in insertIssue: %v\n"+
			"migrate has %d columns, canonical has %d columns",
			missing, len(migrateColumns), len(canonicalColumns))
	}
}

// TestMigrateImport_DependencyInsertPlaceholderParity verifies that the
// dependency migration INSERT keeps SQL arity correct when columns change.
func TestMigrateImport_DependencyInsertPlaceholderParity(t *testing.T) {
	root := moduleRoot(t)
	path := filepath.Join(root, "cmd/bd/migrate_import.go")
	data, err := os.ReadFile(path) // #nosec G304 - test reads known source files
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}

	// Capture INSERT INTO dependencies (...) VALUES (...) in importToDolt.
	re := regexp.MustCompile(`(?s)INSERT INTO dependencies\s*\(\s*(.*?)\)\s*VALUES\s*\(\s*(.*?)\s*\)\s*ON DUPLICATE KEY UPDATE`)
	matches := re.FindSubmatch(data)
	if len(matches) < 3 {
		t.Fatal("failed to locate dependencies INSERT statement in migrate_import.go")
	}

	columnList := strings.TrimSpace(string(matches[1]))
	valuesList := strings.TrimSpace(string(matches[2]))

	columns := splitColumns(columnList)
	placeholders := regexp.MustCompile(`\?`).FindAllString(valuesList, -1)

	if len(columns) != len(placeholders) {
		t.Fatalf("dependencies INSERT arity mismatch: %d columns, %d placeholders\ncolumns=%q\nvalues=%q",
			len(columns), len(placeholders), columnList, valuesList)
	}
}

func TestNormalizeDependencyMetadata_JSONSafety(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		want       string
		wantQuoted bool
	}{
		{name: "empty", in: "", want: "{}", wantQuoted: false},
		{name: "whitespace", in: "   ", want: "{}", wantQuoted: false},
		{name: "valid object", in: `{"k":"v"}`, want: `{"k":"v"}`, wantQuoted: false},
		{name: "valid array", in: `[1,2,3]`, want: `[1,2,3]`, wantQuoted: false},
		{name: "plain text", in: "legacy-metadata", want: `"legacy-metadata"`, wantQuoted: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeDependencyMetadata(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeDependencyMetadata(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if !json.Valid([]byte(got)) {
				t.Fatalf("normalizeDependencyMetadata(%q) produced invalid JSON: %q", tc.in, got)
			}
			if tc.wantQuoted && !(strings.HasPrefix(got, `"`) && strings.HasSuffix(got, `"`)) {
				t.Fatalf("expected quoted JSON string for %q, got %q", tc.in, got)
			}
		})
	}
}

// moduleRoot finds the Go module root by walking up from the test file.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	dir := filepath.Dir(filename)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find module root (no go.mod)")
		}
		dir = parent
	}
}

// extractInsertColumns reads a Go source file and extracts column names from
// the first INSERT INTO issues (...) statement.
func extractInsertColumns(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 - test reads known source files
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}

	// Match INSERT INTO issues ( ... ) — capture the column list
	re := regexp.MustCompile(`(?s)INSERT INTO issues\s*\(\s*(.*?)\)\s*VALUES`)
	matches := re.FindSubmatch(data)
	if len(matches) < 2 {
		return nil
	}

	raw := string(matches[1])
	// Split on commas and clean up
	parts := strings.Split(raw, ",")
	columns := make([]string, 0, len(parts))
	for _, p := range parts {
		col := strings.TrimSpace(p)
		col = strings.Trim(col, "`")
		if col != "" {
			columns = append(columns, col)
		}
	}
	return columns
}

func splitColumns(raw string) []string {
	parts := strings.Split(raw, ",")
	columns := make([]string, 0, len(parts))
	for _, p := range parts {
		col := strings.TrimSpace(strings.Trim(p, "`"))
		if col != "" {
			columns = append(columns, col)
		}
	}
	return columns
}
