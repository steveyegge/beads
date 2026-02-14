package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
	storagefactory "github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/types"
)

// writeDoltMetadata writes a metadata.json with backend=dolt so that
// getBackendAndBeadsDir returns the Dolt backend for the given beads dir.
func writeDoltMetadata(t *testing.T, beadsDir string) {
	t.Helper()
	cfg := &configfile.Config{Backend: configfile.BackendDolt}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), data, 0600); err != nil {
		t.Fatalf("failed to write metadata.json: %v", err)
	}
}

// setupDoltDatabase creates a real Dolt-backed store at the expected path
// inside the given beads directory. It writes metadata.json and initializes
// a Dolt database with an issues table containing the provided issues.
// Returns the store path (beadsDir/dolt).
func setupDoltDatabase(t *testing.T, beadsDir string, issues []testIssue) {
	t.Helper()

	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt binary not in PATH, skipping test")
	}

	writeDoltMetadata(t, beadsDir)

	doltPath := filepath.Join(beadsDir, "dolt")
	ctx := context.Background()
	store, err := storagefactory.NewWithOptions(ctx, configfile.BackendDolt, doltPath, storagefactory.Options{})
	if err != nil {
		t.Fatalf("failed to create Dolt store: %v", err)
	}

	// Set issue_prefix so ID generation works
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		store.Close()
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// Set a bd_version in metadata
	if err := store.SetMetadata(ctx, "bd_version", "0.1.0"); err != nil {
		store.Close()
		t.Fatalf("failed to set bd_version: %v", err)
	}

	// Insert issues if provided
	for _, issue := range issues {
		if err := store.CreateIssue(ctx, issue.toTypesIssue(), "test-user"); err != nil {
			store.Close()
			t.Fatalf("failed to create issue %s: %v", issue.id, err)
		}
	}

	store.Close()
}

// testIssue is a simple struct for test issue data.
type testIssue struct {
	id     string
	title  string
	status string
}

func (ti testIssue) toTypesIssue() *types.Issue {
	return &types.Issue{
		ID:    ti.id,
		Title: ti.title,
	}
}

func TestCheckDatabaseIntegrity(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		expectedStatus string
		expectMessage  string
	}{
		{
			name: "no database - dolt backend with no dolt dir",
			setup: func(t *testing.T, dir string) {
				writeDoltMetadata(t, filepath.Join(dir, ".beads"))
			},
			expectedStatus: "ok",
			expectMessage:  "N/A (no database)",
		},
		{
			name: "valid dolt database",
			setup: func(t *testing.T, dir string) {
				setupDoltDatabase(t, filepath.Join(dir, ".beads"), nil)
			},
			expectedStatus: "ok",
			expectMessage:  "Basic query check passed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			beadsDir := filepath.Join(tmpDir, ".beads")
			if err := os.MkdirAll(beadsDir, 0755); err != nil {
				t.Fatal(err)
			}

			tt.setup(t, tmpDir)

			check := CheckDatabaseIntegrity(tmpDir)

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %q, got %q (message: %s)", tt.expectedStatus, check.Status, check.Message)
			}
			if tt.expectMessage != "" && check.Message != tt.expectMessage {
				t.Errorf("expected message %q, got %q", tt.expectMessage, check.Message)
			}
		})
	}
}

func TestCheckDatabaseJSONLSync(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		expectedStatus string
		expectMessage  string
	}{
		{
			name: "dolt backend with JSONL but no dolt dir",
			setup: func(t *testing.T, dir string) {
				writeDoltMetadata(t, filepath.Join(dir, ".beads"))
				jsonlPath := filepath.Join(dir, ".beads", "issues.jsonl")
				if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1","title":"Test"}`+"\n"), 0600); err != nil {
					t.Fatalf("failed to create JSONL: %v", err)
				}
			},
			expectedStatus: "ok",
			expectMessage:  "N/A (dolt backend)",
		},
		{
			name: "dolt backend with no JSONL",
			setup: func(t *testing.T, dir string) {
				writeDoltMetadata(t, filepath.Join(dir, ".beads"))
			},
			expectedStatus: "ok",
			expectMessage:  "N/A (no JSONL file)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			beadsDir := filepath.Join(tmpDir, ".beads")
			if err := os.MkdirAll(beadsDir, 0755); err != nil {
				t.Fatal(err)
			}

			tt.setup(t, tmpDir)

			check := CheckDatabaseJSONLSync(tmpDir)

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %q, got %q (message: %s)", tt.expectedStatus, check.Status, check.Message)
			}
			if tt.expectMessage != "" && check.Message != tt.expectMessage {
				t.Errorf("expected message %q, got %q", tt.expectMessage, check.Message)
			}
		})
	}
}

func TestCheckDatabaseVersion(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		expectedStatus string
		expectMessage  string
	}{
		{
			name: "fresh clone with JSONL - dolt backend",
			setup: func(t *testing.T, dir string) {
				writeDoltMetadata(t, filepath.Join(dir, ".beads"))
				jsonlPath := filepath.Join(dir, ".beads", "issues.jsonl")
				if err := os.WriteFile(jsonlPath, []byte(`{"id":"test-1","title":"Test"}`+"\n"), 0600); err != nil {
					t.Fatalf("failed to create JSONL: %v", err)
				}
			},
			expectedStatus: "warning",
			expectMessage:  "Fresh clone detected (no dolt database)",
		},
		{
			name: "no database no jsonl - dolt backend",
			setup: func(t *testing.T, dir string) {
				writeDoltMetadata(t, filepath.Join(dir, ".beads"))
			},
			expectedStatus: "error",
			expectMessage:  "No dolt database found",
		},
		{
			name: "valid dolt database with matching version",
			setup: func(t *testing.T, dir string) {
				setupDoltDatabase(t, filepath.Join(dir, ".beads"), nil)
			},
			expectedStatus: "ok",
			expectMessage:  "version 0.1.0",
		},
		{
			name: "dolt database with version mismatch",
			setup: func(t *testing.T, dir string) {
				setupDoltDatabase(t, filepath.Join(dir, ".beads"), nil)
				// The database was created with bd_version=0.1.0, but we'll pass a different CLI version
			},
			expectedStatus: "warning",
			expectMessage:  "version 0.1.0 (CLI: 99.99.99)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			beadsDir := filepath.Join(tmpDir, ".beads")
			if err := os.MkdirAll(beadsDir, 0755); err != nil {
				t.Fatal(err)
			}

			tt.setup(t, tmpDir)

			// Use matching CLI version except for the mismatch test
			cliVersion := "0.1.0"
			if tt.name == "dolt database with version mismatch" {
				cliVersion = "99.99.99"
			}

			check := CheckDatabaseVersion(tmpDir, cliVersion)

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %q, got %q (message: %s)", tt.expectedStatus, check.Status, check.Message)
			}
			if tt.expectMessage != "" && check.Message != tt.expectMessage {
				t.Errorf("expected message %q, got %q", tt.expectMessage, check.Message)
			}
		})
	}
}

func TestCheckSchemaCompatibility(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		expectedStatus string
		expectMessage  string
	}{
		{
			name: "no database - dolt backend",
			setup: func(t *testing.T, dir string) {
				writeDoltMetadata(t, filepath.Join(dir, ".beads"))
			},
			expectedStatus: "ok",
			expectMessage:  "N/A (no database)",
		},
		{
			name: "valid dolt database",
			setup: func(t *testing.T, dir string) {
				setupDoltDatabase(t, filepath.Join(dir, ".beads"), nil)
			},
			expectedStatus: "ok",
			expectMessage:  "Basic queries succeeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			beadsDir := filepath.Join(tmpDir, ".beads")
			if err := os.MkdirAll(beadsDir, 0755); err != nil {
				t.Fatal(err)
			}

			tt.setup(t, tmpDir)

			check := CheckSchemaCompatibility(tmpDir)

			if check.Status != tt.expectedStatus {
				t.Errorf("expected status %q, got %q (message: %s, detail: %s)", tt.expectedStatus, check.Status, check.Message, check.Detail)
			}
			if tt.expectMessage != "" && check.Message != tt.expectMessage {
				t.Errorf("expected message %q, got %q", tt.expectMessage, check.Message)
			}
		})
	}
}

func TestCountJSONLIssues(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectedCount int
		expectError   bool
	}{
		{
			name:          "empty file",
			content:       "",
			expectedCount: 0,
			expectError:   false,
		},
		{
			name:          "single issue",
			content:       `{"id":"test-1","title":"Test"}` + "\n",
			expectedCount: 1,
			expectError:   false,
		},
		{
			name: "multiple issues",
			content: `{"id":"test-1","title":"Test 1"}
{"id":"test-2","title":"Test 2"}
{"id":"test-3","title":"Test 3"}
`,
			expectedCount: 3,
			expectError:   false,
		},
		{
			name: "counts all including tombstones",
			content: `{"id":"test-1","title":"Test 1","status":"open"}
{"id":"test-2","title":"Test 2","status":"tombstone"}
{"id":"test-3","title":"Test 3","status":"closed"}
`,
			expectedCount: 3, // CountJSONLIssues counts all records including tombstones
			expectError:   false,
		},
		{
			name: "skips empty lines",
			content: `{"id":"test-1","title":"Test 1"}

{"id":"test-2","title":"Test 2"}
`,
			expectedCount: 2,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
			if err := os.WriteFile(jsonlPath, []byte(tt.content), 0600); err != nil {
				t.Fatalf("failed to create JSONL: %v", err)
			}

			count, _, err := CountJSONLIssues(jsonlPath)

			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if count != tt.expectedCount {
				t.Errorf("expected count %d, got %d", tt.expectedCount, count)
			}
		})
	}
}

func TestCountJSONLIssues_NonexistentFile(t *testing.T) {
	count, _, err := CountJSONLIssues("/nonexistent/path/issues.jsonl")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
}

func TestCountJSONLIssues_ExtractsPrefixes(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
	content := `{"id":"bd-123","title":"Test 1"}
{"id":"bd-456","title":"Test 2"}
{"id":"proj-789","title":"Test 3"}
`
	if err := os.WriteFile(jsonlPath, []byte(content), 0600); err != nil {
		t.Fatalf("failed to create JSONL: %v", err)
	}

	count, prefixes, err := CountJSONLIssues(jsonlPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}

	// Check prefixes were extracted
	if _, ok := prefixes["bd"]; !ok {
		t.Error("expected 'bd' prefix to be detected")
	}
	if _, ok := prefixes["proj"]; !ok {
		t.Error("expected 'proj' prefix to be detected")
	}
}

// Edge case tests

func TestCheckDatabaseIntegrity_EdgeCases(t *testing.T) {
	t.Skip("SQLite-specific edge cases (locked/read-only database) not applicable with Dolt backend")
}

func TestCheckDatabaseJSONLSync_EdgeCases(t *testing.T) {
	// With Dolt backend, CheckDatabaseJSONLSync always returns early with
	// "N/A (dolt backend)" or "N/A (no JSONL file)" - the SQLite-specific
	// malformed JSONL / missing ID / count mismatch tests don't apply because
	// the Dolt code path never compares DB vs JSONL counts.
	t.Skip("SQLite-specific JSONL sync edge cases not applicable with Dolt backend")
}

func TestCheckDatabaseVersion_EdgeCases(t *testing.T) {
	t.Skip("SQLite-specific version edge cases (future version, missing version) not applicable with Dolt backend")
}

func TestCheckSchemaCompatibility_EdgeCases(t *testing.T) {
	t.Skip("SQLite-specific schema edge cases (partial schema, missing tables/columns) not applicable with Dolt backend")
}

func TestCountJSONLIssues_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		setupContent  func() string
		expectedCount int
		expectError   bool
		errorContains string
	}{
		{
			name: "malformed JSON lines",
			setupContent: func() string {
				return `{"id":"valid-1","title":"Valid"}
{this is not json
{"id":"valid-2","title":"Also Valid"}
{malformed: true}
{"id":"valid-3","title":"Third Valid"}
`
			},
			expectedCount: 3,
			expectError:   true,
			errorContains: "malformed",
		},
		{
			name: "very large file with 10000 issues",
			setupContent: func() string {
				var sb strings.Builder
				for i := 0; i < 10000; i++ {
					sb.WriteString(fmt.Sprintf(`{"id":"issue-%d","title":"Issue %d","status":"open"}`, i, i))
					sb.WriteString("\n")
				}
				return sb.String()
			},
			expectedCount: 10000,
			expectError:   false,
		},
		{
			name: "file with unicode and special characters",
			setupContent: func() string {
				return `{"id":"test-1","title":"Issue with emojis","description":"Unicode: test"}
{"id":"test-2","title":"Quotes \"escaped\" and 'mixed'","status":"open"}
{"id":"test-3","title":"Newlines\nand\ttabs","status":"closed"}
`
			},
			expectedCount: 3,
			expectError:   false,
		},
		{
			name: "file with trailing whitespace",
			setupContent: func() string {
				return `{"id":"test-1","title":"Test"}
  {"id":"test-2","title":"Test 2"}
{"id":"test-3","title":"Test 3"}
`
			},
			expectedCount: 3,
			expectError:   false,
		},
		{
			name: "all lines are malformed",
			setupContent: func() string {
				return `not json
also not json
{still: not valid}
`
			},
			expectedCount: 0,
			expectError:   true,
			errorContains: "malformed",
		},
		{
			name: "valid JSON but missing id in all entries",
			setupContent: func() string {
				return `{"title":"No ID 1","status":"open"}
{"title":"No ID 2","status":"closed"}
{"title":"No ID 3","status":"pending"}
`
			},
			expectedCount: 0,
			expectError:   false,
		},
		{
			name: "entries with numeric ids",
			setupContent: func() string {
				return `{"id":123,"title":"Numeric ID"}
{"id":"valid-1","title":"String ID"}
{"id":null,"title":"Null ID"}
`
			},
			expectedCount: 1, // Only the string ID counts
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			jsonlPath := filepath.Join(tmpDir, "issues.jsonl")
			content := tt.setupContent()
			if err := os.WriteFile(jsonlPath, []byte(content), 0600); err != nil {
				t.Fatalf("failed to create JSONL: %v", err)
			}

			count, _, err := CountJSONLIssues(jsonlPath)

			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.expectError && err != nil && tt.errorContains != "" {
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			}
			if count != tt.expectedCount {
				t.Errorf("expected count %d, got %d", tt.expectedCount, count)
			}
		})
	}
}

// TestCheckDatabaseJSONLSync_MoleculePrefix verifies that molecule/wisp prefixes
// are recognized as valid variants and don't trigger false positive warnings.
// Regression test for GitHub issue #811.
//
// With Dolt as the sole backend, CheckDatabaseJSONLSync returns early with
// "N/A (dolt backend)" and never performs prefix checks. The prefix validation
// logic itself is still exercised through the CountJSONLIssues tests.
func TestCheckDatabaseJSONLSync_MoleculePrefix(t *testing.T) {
	t.Skip("SQLite-specific prefix mismatch test not applicable with Dolt backend (Dolt sync returns N/A early)")
}

func TestCountJSONLIssues_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "large.jsonl")

	// Create a very large JSONL file (100k issues)
	file, err := os.Create(jsonlPath)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	for i := 0; i < 100000; i++ {
		line := fmt.Sprintf(`{"id":"perf-%d","title":"Performance Test Issue %d","status":"open","description":"Testing performance with large files"}`, i, i)
		if _, err := file.WriteString(line + "\n"); err != nil {
			file.Close()
			t.Fatalf("failed to write line: %v", err)
		}
	}
	file.Close()

	// Measure time to count issues
	start := time.Now()
	count, prefixes, err := CountJSONLIssues(jsonlPath)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if count != 100000 {
		t.Errorf("expected count 100000, got %d", count)
	}
	if len(prefixes) != 1 || prefixes["perf"] != 100000 {
		t.Errorf("expected single prefix 'perf' with count 100000, got %v", prefixes)
	}

	// Performance should be reasonable (< 5 seconds for 100k issues)
	if duration > 5*time.Second {
		t.Logf("Warning: counting 100k issues took %v (expected < 5s)", duration)
	} else {
		t.Logf("Performance: counted 100k issues in %v", duration)
	}
}
