package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type specDuplicatesTestResult struct {
	Count       int    `json:"count"`
	Fix         bool   `json:"fix"`
	Applied     bool   `json:"applied"`
	Deleted     int    `json:"deleted"`
	Skipped     int    `json:"skipped"`
	Errors      int    `json:"errors"`
	Resolutions []struct {
		Keep       string  `json:"keep"`
		Delete     string  `json:"delete"`
		Similarity float64 `json:"similarity"`
		Action     string  `json:"action"`
	} `json:"resolutions"`
}

func TestSpecDuplicates(t *testing.T) {
	requireTestGuardDisabled(t)

	bdExe := buildBDForTest(t)
	ws := mkTmpDirInTmp(t, "bd-spec-dup-*")
	dbPath := filepath.Join(ws, ".beads", "beads.db")

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "init", "--prefix", "test", "--quiet"); err != nil {
		t.Fatalf("bd init failed: %v", err)
	}

	specDir := filepath.Join(ws, "specs", "active")
	if err := os.MkdirAll(specDir, 0755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}

	if err := os.WriteFile(filepath.Join(specDir, "auth.md"), []byte("# Auth Flow\nOAuth login flow"), 0644); err != nil {
		t.Fatalf("write auth.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "auth-v2.md"), []byte("# Authentication Flow\nOAuth login"), 0644); err != nil {
		t.Fatalf("write auth-v2.md: %v", err)
	}

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "scan", "--path", "specs"); err != nil {
		t.Fatalf("bd spec scan failed: %v", err)
	}

	out, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "duplicates", "--json", "--threshold", "0.3")
	if err != nil {
		t.Fatalf("bd spec duplicates failed: %v\n%s", err, out)
	}

	var result specDuplicatesTestResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}
	if result.Count == 0 {
		t.Fatalf("expected duplicates, got 0")
	}
}

func TestSpecDuplicatesFix(t *testing.T) {
	requireTestGuardDisabled(t)

	bdExe := buildBDForTest(t)
	ws := mkTmpDirInTmp(t, "bd-spec-dup-fix-*")
	dbPath := filepath.Join(ws, ".beads", "beads.db")

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "init", "--prefix", "test", "--quiet"); err != nil {
		t.Fatalf("bd init failed: %v", err)
	}

	// Create specs in active/ and archive/ with identical content
	activeDir := filepath.Join(ws, "specs", "active")
	archiveDir := filepath.Join(ws, "specs", "archive")
	if err := os.MkdirAll(activeDir, 0755); err != nil {
		t.Fatalf("mkdir active: %v", err)
	}
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		t.Fatalf("mkdir archive: %v", err)
	}

	specContent := []byte("# Widget Design Spec\nDesign for the widget component with extensive details about layout and behavior")
	if err := os.WriteFile(filepath.Join(activeDir, "widget.md"), specContent, 0644); err != nil {
		t.Fatalf("write active/widget.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "widget.md"), specContent, 0644); err != nil {
		t.Fatalf("write archive/widget.md: %v", err)
	}

	// Scan specs into registry
	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "scan", "--path", "specs"); err != nil {
		t.Fatalf("bd spec scan failed: %v", err)
	}

	// Test 1: --fix without --apply (dry-run)
	out, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "duplicates", "--json", "--fix", "--threshold", "0.85")
	if err != nil {
		t.Fatalf("bd spec duplicates --fix failed: %v\n%s", err, out)
	}

	var dryResult specDuplicatesTestResult
	if err := json.Unmarshal([]byte(out), &dryResult); err != nil {
		t.Fatalf("unmarshal dry-run: %v\n%s", err, out)
	}

	if !dryResult.Fix {
		t.Fatal("expected fix=true in dry-run result")
	}
	if dryResult.Applied {
		t.Fatal("expected applied=false in dry-run result")
	}
	if len(dryResult.Resolutions) == 0 {
		t.Fatal("expected resolutions in dry-run result")
	}

	// Verify resolution: archive should be kept, active should be deleted
	found := false
	for _, r := range dryResult.Resolutions {
		if r.Action == "delete" {
			found = true
			if r.Keep != "specs/archive/widget.md" {
				t.Errorf("expected keep=specs/archive/widget.md, got %q", r.Keep)
			}
			if r.Delete != "specs/active/widget.md" {
				t.Errorf("expected delete=specs/active/widget.md, got %q", r.Delete)
			}
		}
	}
	if !found {
		t.Fatal("expected at least one 'delete' resolution")
	}

	// Verify files still exist (dry-run should not delete)
	if _, err := os.Stat(filepath.Join(activeDir, "widget.md")); err != nil {
		t.Fatalf("active/widget.md should still exist after dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(archiveDir, "widget.md")); err != nil {
		t.Fatalf("archive/widget.md should still exist after dry-run: %v", err)
	}

	// Test 2: --fix --apply (actual deletion)
	out, err = runBDSideDB(t, bdExe, ws, dbPath, "spec", "duplicates", "--json", "--fix", "--apply", "--threshold", "0.85")
	if err != nil {
		t.Fatalf("bd spec duplicates --fix --apply failed: %v\n%s", err, out)
	}

	var applyResult specDuplicatesTestResult
	if err := json.Unmarshal([]byte(out), &applyResult); err != nil {
		t.Fatalf("unmarshal apply: %v\n%s", err, out)
	}

	if !applyResult.Fix {
		t.Fatal("expected fix=true in apply result")
	}
	if !applyResult.Applied {
		t.Fatal("expected applied=true in apply result")
	}
	if applyResult.Deleted == 0 {
		t.Fatal("expected deleted > 0 in apply result")
	}
	if applyResult.Errors != 0 {
		t.Errorf("expected errors=0, got %d", applyResult.Errors)
	}

	// Verify the non-canonical file was deleted
	if _, err := os.Stat(filepath.Join(activeDir, "widget.md")); !os.IsNotExist(err) {
		t.Fatal("active/widget.md should have been deleted")
	}
	// Verify the canonical file was kept
	if _, err := os.Stat(filepath.Join(archiveDir, "widget.md")); err != nil {
		t.Fatalf("archive/widget.md should still exist: %v", err)
	}
}

func TestSpecDuplicatesFixSkip(t *testing.T) {
	requireTestGuardDisabled(t)

	bdExe := buildBDForTest(t)
	ws := mkTmpDirInTmp(t, "bd-spec-dup-skip-*")
	dbPath := filepath.Join(ws, ".beads", "beads.db")

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "init", "--prefix", "test", "--quiet"); err != nil {
		t.Fatalf("bd init failed: %v", err)
	}

	// Create two specs in the same directory (should be skipped)
	activeDir := filepath.Join(ws, "specs", "active")
	if err := os.MkdirAll(activeDir, 0755); err != nil {
		t.Fatalf("mkdir active: %v", err)
	}

	specContent := []byte("# Widget Design Spec\nDesign for the widget component with extensive details about layout and behavior")
	if err := os.WriteFile(filepath.Join(activeDir, "widget-v1.md"), specContent, 0644); err != nil {
		t.Fatalf("write widget-v1.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(activeDir, "widget-v2.md"), specContent, 0644); err != nil {
		t.Fatalf("write widget-v2.md: %v", err)
	}

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "scan", "--path", "specs"); err != nil {
		t.Fatalf("bd spec scan failed: %v", err)
	}

	out, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "duplicates", "--json", "--fix", "--threshold", "0.85")
	if err != nil {
		t.Fatalf("bd spec duplicates --fix failed: %v\n%s", err, out)
	}

	var result specDuplicatesTestResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}

	if result.Skipped == 0 {
		t.Fatal("expected skipped > 0 for same-directory duplicates")
	}

	// All resolutions should be "skip"
	for _, r := range result.Resolutions {
		if r.Action != "skip" {
			t.Errorf("expected action=skip for same-dir pair, got %q", r.Action)
		}
	}
}

func TestSpecDuplicatesApplyRequiresFix(t *testing.T) {
	requireTestGuardDisabled(t)

	bdExe := buildBDForTest(t)
	ws := mkTmpDirInTmp(t, "bd-spec-dup-gate-*")
	dbPath := filepath.Join(ws, ".beads", "beads.db")

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "init", "--prefix", "test", "--quiet"); err != nil {
		t.Fatalf("bd init failed: %v", err)
	}

	// --apply without --fix should fail
	_, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "duplicates", "--json", "--apply")
	if err == nil {
		t.Fatal("expected error when using --apply without --fix")
	}
}
