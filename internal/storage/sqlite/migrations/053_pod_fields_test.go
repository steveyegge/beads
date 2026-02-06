package migrations

import (
	"database/sql"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// TestMigratePodFields_AddsColumns verifies that the migration adds all five
// pod columns to an existing issues table that lacks them.
func TestMigratePodFields_AddsColumns(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Create a minimal issues table without pod columns
	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'open'
		)
	`)
	if err != nil {
		t.Fatalf("failed to create issues table: %v", err)
	}

	// Run migration
	if err := MigratePodFields(db); err != nil {
		t.Fatalf("MigratePodFields failed: %v", err)
	}

	// Verify all five columns exist
	expectedCols := []string{"pod_name", "pod_ip", "pod_node", "pod_status", "screen_session"}
	for _, col := range expectedCols {
		var exists bool
		err := db.QueryRow(`
			SELECT COUNT(*) > 0
			FROM pragma_table_info('issues')
			WHERE name = ?
		`, col).Scan(&exists)
		if err != nil {
			t.Fatalf("failed to check column %s: %v", col, err)
		}
		if !exists {
			t.Errorf("expected column %s to exist after migration, but it doesn't", col)
		}
	}
}

// TestMigratePodFields_DefaultValues verifies that pod columns default to empty strings.
func TestMigratePodFields_DefaultValues(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT ''
		)
	`)
	if err != nil {
		t.Fatalf("failed to create issues table: %v", err)
	}

	// Insert a row before migration
	_, err = db.Exec(`INSERT INTO issues (id, title) VALUES ('pre-migration', 'Existing Issue')`)
	if err != nil {
		t.Fatalf("failed to insert pre-migration row: %v", err)
	}

	if err := MigratePodFields(db); err != nil {
		t.Fatalf("MigratePodFields failed: %v", err)
	}

	// Verify pre-existing row gets empty string defaults
	var podName, podIP, podNode, podStatus, screenSession string
	err = db.QueryRow(`
		SELECT pod_name, pod_ip, pod_node, pod_status, screen_session
		FROM issues WHERE id = 'pre-migration'
	`).Scan(&podName, &podIP, &podNode, &podStatus, &screenSession)
	if err != nil {
		t.Fatalf("failed to query pre-migration row: %v", err)
	}

	if podName != "" {
		t.Errorf("pod_name default: got %q, want empty string", podName)
	}
	if podIP != "" {
		t.Errorf("pod_ip default: got %q, want empty string", podIP)
	}
	if podNode != "" {
		t.Errorf("pod_node default: got %q, want empty string", podNode)
	}
	if podStatus != "" {
		t.Errorf("pod_status default: got %q, want empty string", podStatus)
	}
	if screenSession != "" {
		t.Errorf("screen_session default: got %q, want empty string", screenSession)
	}
}

// TestMigratePodFields_Idempotent verifies the migration is safe to run multiple times.
func TestMigratePodFields_Idempotent(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT ''
		)
	`)
	if err != nil {
		t.Fatalf("failed to create issues table: %v", err)
	}

	// Run migration twice - should not error
	if err := MigratePodFields(db); err != nil {
		t.Fatalf("first MigratePodFields failed: %v", err)
	}
	if err := MigratePodFields(db); err != nil {
		t.Fatalf("second MigratePodFields failed: %v", err)
	}

	// Verify columns still exist (not duplicated)
	var colCount int
	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('issues')
		WHERE name IN ('pod_name', 'pod_ip', 'pod_node', 'pod_status', 'screen_session')
	`).Scan(&colCount)
	if err != nil {
		t.Fatalf("failed to count pod columns: %v", err)
	}
	if colCount != 5 {
		t.Errorf("expected 5 pod columns, got %d", colCount)
	}
}

// TestMigratePodFields_PartialMigration verifies migration handles a table that
// already has some pod columns (e.g., interrupted previous migration).
func TestMigratePodFields_PartialMigration(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Create table with some pod columns already present
	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			pod_name TEXT DEFAULT '',
			pod_ip TEXT DEFAULT ''
		)
	`)
	if err != nil {
		t.Fatalf("failed to create issues table: %v", err)
	}

	// Run migration - should add remaining columns without error
	if err := MigratePodFields(db); err != nil {
		t.Fatalf("MigratePodFields with partial columns failed: %v", err)
	}

	// Verify all five columns exist
	var colCount int
	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('issues')
		WHERE name IN ('pod_name', 'pod_ip', 'pod_node', 'pod_status', 'screen_session')
	`).Scan(&colCount)
	if err != nil {
		t.Fatalf("failed to count pod columns: %v", err)
	}
	if colCount != 5 {
		t.Errorf("expected 5 pod columns, got %d", colCount)
	}
}

// TestMigratePodFields_ColumnTypes verifies columns are TEXT type.
func TestMigratePodFields_ColumnTypes(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT ''
		)
	`)
	if err != nil {
		t.Fatalf("failed to create issues table: %v", err)
	}

	if err := MigratePodFields(db); err != nil {
		t.Fatalf("MigratePodFields failed: %v", err)
	}

	// Verify column types
	expectedCols := []string{"pod_name", "pod_ip", "pod_node", "pod_status", "screen_session"}
	for _, col := range expectedCols {
		var colType string
		err := db.QueryRow(`
			SELECT type
			FROM pragma_table_info('issues')
			WHERE name = ?
		`, col).Scan(&colType)
		if err != nil {
			t.Fatalf("failed to get type for column %s: %v", col, err)
		}
		if colType != "TEXT" {
			t.Errorf("column %s: got type %q, want TEXT", col, colType)
		}
	}
}

// TestMigratePodFields_WriteAndRead verifies that data can be written and read
// from the new pod columns after migration.
func TestMigratePodFields_WriteAndRead(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT ''
		)
	`)
	if err != nil {
		t.Fatalf("failed to create issues table: %v", err)
	}

	if err := MigratePodFields(db); err != nil {
		t.Fatalf("MigratePodFields failed: %v", err)
	}

	// Insert with pod field values
	_, err = db.Exec(`
		INSERT INTO issues (id, title, pod_name, pod_ip, pod_node, pod_status, screen_session)
		VALUES ('agent-1', 'Test Agent', 'emma-pod-abc', '10.0.1.5', 'node-1', 'running', 'emma-screen')
	`)
	if err != nil {
		t.Fatalf("failed to insert issue with pod fields: %v", err)
	}

	// Read back
	var podName, podIP, podNode, podStatus, screenSession string
	err = db.QueryRow(`
		SELECT pod_name, pod_ip, pod_node, pod_status, screen_session
		FROM issues WHERE id = 'agent-1'
	`).Scan(&podName, &podIP, &podNode, &podStatus, &screenSession)
	if err != nil {
		t.Fatalf("failed to query pod fields: %v", err)
	}

	if podName != "emma-pod-abc" {
		t.Errorf("pod_name: got %q, want %q", podName, "emma-pod-abc")
	}
	if podIP != "10.0.1.5" {
		t.Errorf("pod_ip: got %q, want %q", podIP, "10.0.1.5")
	}
	if podNode != "node-1" {
		t.Errorf("pod_node: got %q, want %q", podNode, "node-1")
	}
	if podStatus != "running" {
		t.Errorf("pod_status: got %q, want %q", podStatus, "running")
	}
	if screenSession != "emma-screen" {
		t.Errorf("screen_session: got %q, want %q", screenSession, "emma-screen")
	}
}

// TestMigratePodFields_NewDatabaseHasColumns verifies that a database created
// via the full SQLite store (with schema + migrations) has the pod columns.
// This is an integration-style test using an in-memory DB with manual schema setup.
func TestMigratePodFields_NewDatabaseHasColumns(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Create a realistic issues table with core columns (simulate schema.go)
	_, err = db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			content_hash TEXT,
			title TEXT NOT NULL CHECK(length(title) <= 500),
			description TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'open',
			priority INTEGER NOT NULL DEFAULT 2,
			issue_type TEXT NOT NULL DEFAULT 'task',
			assignee TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			closed_at DATETIME,
			hook_bead TEXT DEFAULT '',
			role_bead TEXT DEFAULT '',
			agent_state TEXT DEFAULT '',
			last_activity DATETIME,
			role_type TEXT DEFAULT '',
			rig TEXT DEFAULT ''
		)
	`)
	if err != nil {
		t.Fatalf("failed to create issues table: %v", err)
	}

	// Run pod migration (as would happen in full store init)
	if err := MigratePodFields(db); err != nil {
		t.Fatalf("MigratePodFields failed: %v", err)
	}

	// Verify pod columns exist alongside agent columns
	expectedCols := []string{
		"hook_bead", "role_bead", "agent_state", "role_type", "rig",
		"pod_name", "pod_ip", "pod_node", "pod_status", "screen_session",
	}
	for _, col := range expectedCols {
		var exists bool
		err := db.QueryRow(`
			SELECT COUNT(*) > 0
			FROM pragma_table_info('issues')
			WHERE name = ?
		`, col).Scan(&exists)
		if err != nil {
			t.Fatalf("failed to check column %s: %v", col, err)
		}
		if !exists {
			t.Errorf("expected column %s to exist, but it doesn't", col)
		}
	}
}
