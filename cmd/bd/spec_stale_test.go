package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type specStaleTestBucket struct {
	Count int `json:"count"`
}

type specStaleTestResult struct {
	Buckets map[string]specStaleTestBucket `json:"buckets"`
}

func TestSpecStaleBuckets(t *testing.T) {
	requireTestGuardDisabled(t)

	bdExe := buildBDForTest(t)
	ws := mkTmpDirInTmp(t, "bd-spec-stale-*")
	dbPath := filepath.Join(ws, ".beads", "beads.db")

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "init", "--prefix", "test", "--quiet"); err != nil {
		t.Fatalf("bd init failed: %v", err)
	}

	specDir := filepath.Join(ws, "specs", "active")
	if err := os.MkdirAll(specDir, 0755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	fixtures := []struct {
		name string
		age  time.Duration
	}{
		{"fresh.md", 2 * 24 * time.Hour},
		{"aging.md", 10 * 24 * time.Hour},
		{"stale.md", 40 * 24 * time.Hour},
		{"ancient.md", 120 * 24 * time.Hour},
	}

	for _, f := range fixtures {
		path := filepath.Join(specDir, f.name)
		if err := os.WriteFile(path, []byte("# "+f.name), 0644); err != nil {
			t.Fatalf("write %s: %v", f.name, err)
		}
		ts := now.Add(-f.age)
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatalf("chtimes %s: %v", f.name, err)
		}
	}

	if _, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "scan", "--path", "specs"); err != nil {
		t.Fatalf("bd spec scan failed: %v", err)
	}

	out, err := runBDSideDB(t, bdExe, ws, dbPath, "spec", "stale", "--json", "--limit", "10")
	if err != nil {
		t.Fatalf("bd spec stale failed: %v\n%s", err, out)
	}

	var result specStaleTestResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}

	if result.Buckets["fresh"].Count != 1 {
		t.Fatalf("fresh count = %d, want 1", result.Buckets["fresh"].Count)
	}
	if result.Buckets["aging"].Count != 1 {
		t.Fatalf("aging count = %d, want 1", result.Buckets["aging"].Count)
	}
	if result.Buckets["stale"].Count != 1 {
		t.Fatalf("stale count = %d, want 1", result.Buckets["stale"].Count)
	}
	if result.Buckets["ancient"].Count != 1 {
		t.Fatalf("ancient count = %d, want 1", result.Buckets["ancient"].Count)
	}
}
