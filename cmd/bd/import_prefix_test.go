//go:build cgo && integration

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_InitFromJSONLSkipsPrefixValidation_E2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow CLI test in short mode")
	}

	projDir := newCLIIntegrationRepo(t)
	legacyIssue := `{"id":"legacy-123","title":"Legacy issue","status":"open","priority":2,"issue_type":"task","created_at":"2026-01-01T00:00:00Z"}`
	jsonlPath := filepath.Join(projDir, ".beads", "issues.jsonl")
	if err := os.MkdirAll(filepath.Dir(jsonlPath), 0o755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}
	if err := os.WriteFile(jsonlPath, []byte(legacyIssue+"\n"), 0o644); err != nil {
		t.Fatalf("Failed to write legacy JSONL: %v", err)
	}

	initOut, initErr := runBDExecAllowErrorWithEnv(t, projDir, cliIntegrationEnv(),
		"init", "--prefix", "current", "--database", uniqueTestDBName(t), "--quiet", "--from-jsonl")
	if initErr != nil {
		t.Fatalf("bd init --from-jsonl failed: %v\nOutput: %s", initErr, initOut)
	}

	out, err := runBDExecAllowErrorWithEnv(t, projDir, cliIntegrationEnv(), "list", "--json")
	if err != nil {
		t.Fatalf("bd list failed: %v\nOutput: %s", err, out)
	}
	if !strings.Contains(out, "legacy-123") {
		t.Errorf("Expected legacy-123 to be imported, but list output was: %s", out)
	}
}
