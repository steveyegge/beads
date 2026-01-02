package main

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

// runBDRepair runs the repair command with --path flag (bypasses normal db init)
func runBDRepair(t *testing.T, exe, path string, args ...string) (string, error) {
	t.Helper()
	fullArgs := []string{"repair", "--path", path}
	fullArgs = append(fullArgs, args...)

	cmd := exec.Command(exe, fullArgs...)
	cmd.Dir = path
	cmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestRepairOrphans_DryRun(t *testing.T) {
	requireTestGuardDisabled(t)

	if testing.Short() {
		t.Skip("skipping slow repair test in short mode")
	}

	bdExe := buildBDForTest(t)
	ws := mkTmpDirInTmp(t, "bd-repair-orphans-*")
	dbPath := filepath.Join(ws, ".beads", "beads.db")

	// Initialize with some issues
	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "init", "--prefix", "test", "--quiet"); err != nil {
		t.Fatalf("bd init failed: %v", err)
	}
	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "create", "Issue 1", "-p", "1"); err != nil {
		t.Fatalf("bd create failed: %v", err)
	}

	// Directly insert orphaned data into the database
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?_pragma=foreign_keys(OFF)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Insert orphaned dependency (issue_id doesn't exist)
	_, err = db.Exec("INSERT INTO dependencies (issue_id, depends_on_id, type, created_by) VALUES ('nonexistent-1', 'test-xyz', 'blocks', 'test')")
	if err != nil {
		t.Fatalf("insert orphan dep: %v", err)
	}

	// Insert orphaned dependency (depends_on_id doesn't exist)
	_, err = db.Exec("INSERT INTO dependencies (issue_id, depends_on_id, type, created_by) VALUES ('test-xyz', 'deleted-issue', 'blocks', 'test')")
	if err != nil {
		t.Fatalf("insert orphan dep2: %v", err)
	}

	// Insert orphaned label
	_, err = db.Exec("INSERT INTO labels (issue_id, label) VALUES ('gone-issue', 'bug')")
	if err != nil {
		t.Fatalf("insert orphan label: %v", err)
	}
	db.Close()

	// Run repair with --dry-run (use --path, not --db, so repair bypasses normal init)
	out, err := runBDRepair(t, bdExe, ws, "--dry-run")
	if err != nil {
		t.Fatalf("bd repair --dry-run failed: %v\n%s", err, out)
	}

	// Verify it found the orphans
	if !strings.Contains(out, "dependencies with missing issue_id") {
		t.Errorf("expected to find orphaned deps (issue_id), got: %s", out)
	}
	if !strings.Contains(out, "dependencies with missing depends_on_id") {
		t.Errorf("expected to find orphaned deps (depends_on_id), got: %s", out)
	}
	if !strings.Contains(out, "labels with missing issue_id") {
		t.Errorf("expected to find orphaned labels, got: %s", out)
	}
	if !strings.Contains(out, "[DRY-RUN]") {
		t.Errorf("expected DRY-RUN message, got: %s", out)
	}

	// Verify data wasn't actually deleted
	db2, err := sql.Open("sqlite3", "file:"+dbPath+"?_pragma=foreign_keys(OFF)")
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer db2.Close()

	var count int
	db2.QueryRow("SELECT COUNT(*) FROM dependencies WHERE issue_id = 'nonexistent-1'").Scan(&count)
	if count != 1 {
		t.Errorf("expected orphan dep to still exist after dry-run, got count=%d", count)
	}
}

func TestRepairOrphans_Fix(t *testing.T) {
	requireTestGuardDisabled(t)

	if testing.Short() {
		t.Skip("skipping slow repair test in short mode")
	}

	bdExe := buildBDForTest(t)
	ws := mkTmpDirInTmp(t, "bd-repair-fix-*")
	dbPath := filepath.Join(ws, ".beads", "beads.db")

	// Initialize with some issues
	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "init", "--prefix", "test", "--quiet"); err != nil {
		t.Fatalf("bd init failed: %v", err)
	}
	out, err := runBDSideDB(t, bdExe, ws, dbPath, "create", "Issue 1", "-p", "1", "--json")
	if err != nil {
		t.Fatalf("bd create failed: %v\n%s", err, out)
	}

	// Directly insert orphaned data
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?_pragma=foreign_keys(OFF)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// Insert orphaned dependencies and labels
	db.Exec("INSERT INTO dependencies (issue_id, depends_on_id, type, created_by) VALUES ('orphan-1', 'test-xyz', 'blocks', 'test')")
	db.Exec("INSERT INTO dependencies (issue_id, depends_on_id, type, created_by) VALUES ('test-xyz', 'deleted-2', 'blocks', 'test')")
	db.Exec("INSERT INTO labels (issue_id, label) VALUES ('orphan-3', 'wontfix')")
	db.Close()

	// Run repair (no --dry-run) - use --path to bypass normal db init
	out, err = runBDRepair(t, bdExe, ws)
	if err != nil {
		t.Fatalf("bd repair failed: %v\n%s", err, out)
	}

	// Verify it cleaned up
	if !strings.Contains(out, "Deleted") {
		t.Errorf("expected deletion messages, got: %s", out)
	}
	if !strings.Contains(out, "Repair complete") {
		t.Errorf("expected completion message, got: %s", out)
	}

	// Verify orphans are gone
	db2, err := sql.Open("sqlite3", "file:"+dbPath+"?_pragma=foreign_keys(OFF)")
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer db2.Close()

	var depCount, labelCount int
	db2.QueryRow("SELECT COUNT(*) FROM dependencies WHERE issue_id = 'orphan-1' OR depends_on_id = 'deleted-2'").Scan(&depCount)
	db2.QueryRow("SELECT COUNT(*) FROM labels WHERE issue_id = 'orphan-3'").Scan(&labelCount)

	if depCount != 0 {
		t.Errorf("expected orphan deps to be deleted, got count=%d", depCount)
	}
	if labelCount != 0 {
		t.Errorf("expected orphan labels to be deleted, got count=%d", labelCount)
	}
}

func TestRepairOrphans_CleanDatabase(t *testing.T) {
	requireTestGuardDisabled(t)

	if testing.Short() {
		t.Skip("skipping slow repair test in short mode")
	}

	bdExe := buildBDForTest(t)
	ws := mkTmpDirInTmp(t, "bd-repair-clean-*")
	dbPath := filepath.Join(ws, ".beads", "beads.db")

	// Initialize with a clean database
	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "init", "--prefix", "test", "--quiet"); err != nil {
		t.Fatalf("bd init failed: %v", err)
	}
	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "create", "Issue 1", "-p", "1"); err != nil {
		t.Fatalf("bd create failed: %v", err)
	}

	// Run repair on clean database - use --path to bypass normal db init
	out, err := runBDRepair(t, bdExe, ws)
	if err != nil {
		t.Fatalf("bd repair failed: %v\n%s", err, out)
	}

	// Should report no orphans found
	if !strings.Contains(out, "No orphaned references found") {
		t.Errorf("expected clean database message, got: %s", out)
	}
}
