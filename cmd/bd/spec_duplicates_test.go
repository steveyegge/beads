package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type specDuplicatesTestResult struct {
	Count int `json:"count"`
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
