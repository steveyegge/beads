package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type specSyncTestResult struct {
	Changes []struct {
		SpecID string `json:"spec_id"`
	} `json:"changes"`
	Applied int `json:"applied"`
}

type createIssueResult struct {
	ID string `json:"id"`
}

func TestSpecSyncDryRun(t *testing.T) {
	requireTestGuardDisabled(t)

	bdExe := buildBDForTest(t)
	ws := mkTmpDirInTmp(t, "bd-spec-sync-*")
	dbPath := filepath.Join(ws, ".beads", "beads.db")

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "init", "--prefix", "test", "--quiet"); err != nil {
		t.Fatalf("bd init failed: %v", err)
	}

	specDir := filepath.Join(ws, "specs", "active")
	if err := os.MkdirAll(specDir, 0755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	specPath := filepath.Join(specDir, "sync.md")
	if err := os.WriteFile(specPath, []byte("# Sync\n"), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "scan", "--path", "specs"); err != nil {
		t.Fatalf("bd spec scan failed: %v", err)
	}

	out, err := runBDSideDB(t, bdExe, ws, dbPath, "create", "--json", "--spec", "specs/active/sync.md", "-p", "1", "Sync issue")
	if err != nil {
		t.Fatalf("bd create failed: %v\n%s", err, out)
	}
	var created createIssueResult
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("unmarshal create output: %v\n%s", err, out)
	}
	if created.ID == "" {
		t.Fatalf("missing issue id")
	}
	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "close", created.ID, "--reason", "done"); err != nil {
		t.Fatalf("bd close failed: %v", err)
	}

	out, err = runBDSideDB(t, bdExe, ws, dbPath, "spec", "sync", "--json")
	if err != nil {
		t.Fatalf("bd spec sync failed: %v\n%s", err, out)
	}
	var result specSyncTestResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal sync output: %v\n%s", err, out)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(result.Changes))
	}

	out, err = runBDSideDB(t, bdExe, ws, dbPath, "spec", "sync", "--json", "--apply", "--yes")
	if err != nil {
		t.Fatalf("bd spec sync apply failed: %v\n%s", err, out)
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal sync apply output: %v\n%s", err, out)
	}
	if result.Applied != 1 {
		t.Fatalf("applied = %d, want 1", result.Applied)
	}
}
