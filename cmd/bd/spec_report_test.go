package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSpecReport(t *testing.T) {
	requireTestGuardDisabled(t)

	bdExe := buildBDForTest(t)
	ws := mkTmpDirInTmp(t, "bd-spec-report-*")
	dbPath := filepath.Join(ws, ".beads", "beads.db")

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "init", "--prefix", "test", "--quiet"); err != nil {
		t.Fatalf("bd init failed: %v", err)
	}

	specDir := filepath.Join(ws, "specs", "active")
	if err := os.MkdirAll(specDir, 0755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "report.md"), []byte("# Report\n"), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "scan", "--path", "specs"); err != nil {
		t.Fatalf("bd spec scan failed: %v", err)
	}

	outDir := filepath.Join(ws, "out")
	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "report", "--format", "md", "--out", outDir); err != nil {
		t.Fatalf("bd spec report failed: %v", err)
	}

	path := filepath.Join(outDir, "spec_radar_report.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("report not written: %v", err)
	}
}
