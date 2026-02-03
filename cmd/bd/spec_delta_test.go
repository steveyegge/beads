package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type specDeltaTestResult struct {
	Delta struct {
		Added   []struct{} `json:"added"`
		Removed []struct{} `json:"removed"`
		Changed []struct{} `json:"changed"`
	} `json:"delta"`
}

func TestSpecDelta(t *testing.T) {
	requireTestGuardDisabled(t)

	bdExe := buildBDForTest(t)
	ws := mkTmpDirInTmp(t, "bd-spec-delta-*")
	dbPath := filepath.Join(ws, ".beads", "beads.db")

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "init", "--prefix", "test", "--quiet"); err != nil {
		t.Fatalf("bd init failed: %v", err)
	}

	specDir := filepath.Join(ws, "specs", "active")
	if err := os.MkdirAll(specDir, 0755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}

	specPath := filepath.Join(specDir, "delta.md")
	if err := os.WriteFile(specPath, []byte("# Delta\nv1"), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "scan", "--path", "specs"); err != nil {
		t.Fatalf("bd spec scan failed: %v", err)
	}

	out, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "delta", "--json")
	if err != nil {
		t.Fatalf("bd spec delta failed: %v\n%s", err, out)
	}

	var first specDeltaTestResult
	if err := json.Unmarshal([]byte(out), &first); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}
	if len(first.Delta.Added) != 1 {
		t.Fatalf("added = %d, want 1", len(first.Delta.Added))
	}

	now := time.Now().UTC().Truncate(time.Second).Add(2 * time.Second)
	if err := os.WriteFile(specPath, []byte("# Delta\nv2"), 0644); err != nil {
		t.Fatalf("rewrite spec: %v", err)
	}
	_ = os.Chtimes(specPath, now, now)

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "scan", "--path", "specs"); err != nil {
		t.Fatalf("bd spec scan failed: %v", err)
	}

	out, err = runBDSideDB(t, bdExe, ws, dbPath, "spec", "delta", "--json")
	if err != nil {
		t.Fatalf("bd spec delta failed: %v\n%s", err, out)
	}
	var second specDeltaTestResult
	if err := json.Unmarshal([]byte(out), &second); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}
	if len(second.Delta.Changed) == 0 {
		t.Fatalf("expected changes, got none")
	}
}
