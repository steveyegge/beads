package doctor

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// TestRunDeepValidation_NoBeadsDir verifies deep validation handles missing .beads directory
func TestRunDeepValidation_NoBeadsDir(t *testing.T) {
	tmpDir := t.TempDir()
	result := RunDeepValidation(tmpDir)

	if len(result.AllChecks) != 1 {
		t.Errorf("Expected 1 check, got %d", len(result.AllChecks))
	}
	if result.AllChecks[0].Status != StatusOK {
		t.Errorf("Status = %q, want %q", result.AllChecks[0].Status, StatusOK)
	}
}

// TestRunDeepValidation_EmptyBeadsDir verifies deep validation with empty .beads directory
func TestRunDeepValidation_EmptyBeadsDir(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	result := RunDeepValidation(tmpDir)

	// Should return OK with "no database" message
	if len(result.AllChecks) != 1 {
		t.Errorf("Expected 1 check, got %d", len(result.AllChecks))
	}
	if result.AllChecks[0].Status != StatusOK {
		t.Errorf("Status = %q, want %q", result.AllChecks[0].Status, StatusOK)
	}
}

// TestRunDeepValidation_WithDatabase verifies deep validation with a basic database
func TestRunDeepValidation_WithDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a minimal database (use canonical name beads.db)
	dbPath := filepath.Join(beadsDir, "beads.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create minimal schema matching what deep validation expects
	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'open',
			issue_type TEXT NOT NULL DEFAULT 'task',
			notes TEXT DEFAULT ''
		);
		CREATE TABLE dependencies (
			issue_id TEXT NOT NULL,
			depends_on_id TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'blocks',
			created_by TEXT NOT NULL DEFAULT '',
			thread_id TEXT DEFAULT '',
			PRIMARY KEY (issue_id, depends_on_id)
		);
		CREATE TABLE labels (
			issue_id TEXT NOT NULL,
			label TEXT NOT NULL,
			PRIMARY KEY (issue_id, label)
		);
	`)
	if err != nil {
		t.Fatal(err)
	}

	result := RunDeepValidation(tmpDir)

	// Should have 6 checks (one for each validation type)
	if len(result.AllChecks) != 6 {
		// Log what we got for debugging
		t.Logf("Got %d checks:", len(result.AllChecks))
		for i, check := range result.AllChecks {
			t.Logf("  %d: %s - %s", i, check.Name, check.Message)
		}
		t.Errorf("Expected 6 checks, got %d", len(result.AllChecks))
	}

	// All should pass on empty database
	for _, check := range result.AllChecks {
		if check.Status == StatusError {
			t.Errorf("Check %s failed: %s", check.Name, check.Message)
		}
	}
}

// TestCheckParentConsistency_OrphanedDeps verifies detection of orphaned parent-child deps
func TestCheckParentConsistency_OrphanedDeps(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create schema
	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'open'
		);
		CREATE TABLE dependencies (
			issue_id TEXT NOT NULL,
			depends_on_id TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'blocks',
			PRIMARY KEY (issue_id, depends_on_id)
		);
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Insert an issue
	_, err = db.Exec(`INSERT INTO issues (id, title, status) VALUES ('bd-1', 'Test Issue', 'open')`)
	if err != nil {
		t.Fatal(err)
	}

	// Insert a parent-child dep pointing to non-existent parent
	_, err = db.Exec(`INSERT INTO dependencies (issue_id, depends_on_id, type) VALUES ('bd-1', 'bd-missing', 'parent-child')`)
	if err != nil {
		t.Fatal(err)
	}

	check := checkParentConsistency(db)

	if check.Status != StatusError {
		t.Errorf("Status = %q, want %q", check.Status, StatusError)
	}
}

// TestCheckEpicCompleteness_CompletedEpic verifies detection of closeable epics
func TestCheckEpicCompleteness_CompletedEpic(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create schema
	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'open',
			issue_type TEXT NOT NULL DEFAULT 'task'
		);
		CREATE TABLE dependencies (
			issue_id TEXT NOT NULL,
			depends_on_id TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'blocks',
			PRIMARY KEY (issue_id, depends_on_id)
		);
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Insert an open epic
	_, err = db.Exec(`INSERT INTO issues (id, title, status, issue_type) VALUES ('epic-1', 'Epic', 'open', 'epic')`)
	if err != nil {
		t.Fatal(err)
	}

	// Insert a closed child task
	_, err = db.Exec(`INSERT INTO issues (id, title, status, issue_type) VALUES ('task-1', 'Task', 'closed', 'task')`)
	if err != nil {
		t.Fatal(err)
	}

	// Create parent-child relationship
	_, err = db.Exec(`INSERT INTO dependencies (issue_id, depends_on_id, type) VALUES ('task-1', 'epic-1', 'parent-child')`)
	if err != nil {
		t.Fatal(err)
	}

	check := checkEpicCompleteness(db)

	// Epic with all children closed should be detected
	if check.Status != StatusWarning {
		t.Errorf("Status = %q, want %q", check.Status, StatusWarning)
	}
}

// TestCheckMailThreadIntegrity_ValidThreads verifies valid thread references pass
func TestCheckMailThreadIntegrity_ValidThreads(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(beadsDir, "beads.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create schema with thread_id column
	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'open'
		);
		CREATE TABLE dependencies (
			issue_id TEXT NOT NULL,
			depends_on_id TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'blocks',
			thread_id TEXT DEFAULT '',
			PRIMARY KEY (issue_id, depends_on_id)
		);
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Insert issues
	_, err = db.Exec(`INSERT INTO issues (id, title, status) VALUES ('thread-root', 'Thread Root', 'open')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO issues (id, title, status) VALUES ('reply-1', 'Reply', 'open')`)
	if err != nil {
		t.Fatal(err)
	}

	// Insert a dependency with valid thread_id
	_, err = db.Exec(`INSERT INTO dependencies (issue_id, depends_on_id, type, thread_id) VALUES ('reply-1', 'thread-root', 'replies-to', 'thread-root')`)
	if err != nil {
		t.Fatal(err)
	}

	check := checkMailThreadIntegrity(db)

	if check.Status != StatusOK {
		t.Errorf("Status = %q, want %q: %s", check.Status, StatusOK, check.Message)
	}
}

// TestDeepValidationResultJSON verifies JSON serialization
func TestDeepValidationResultJSON(t *testing.T) {
	result := DeepValidationResult{
		TotalIssues:       10,
		TotalDependencies: 5,
		OverallOK:         true,
		AllChecks: []DoctorCheck{
			{Name: "Test", Status: StatusOK, Message: "All good"},
		},
	}

	jsonBytes, err := DeepValidationResultJSON(result)
	if err != nil {
		t.Fatalf("Failed to serialize: %v", err)
	}

	if len(jsonBytes) == 0 {
		t.Error("Expected non-empty JSON output")
	}

	// Should contain expected fields
	jsonStr := string(jsonBytes)
	if !contains(jsonStr, "total_issues") {
		t.Error("JSON should contain total_issues")
	}
	if !contains(jsonStr, "overall_ok") {
		t.Error("JSON should contain overall_ok")
	}
}
