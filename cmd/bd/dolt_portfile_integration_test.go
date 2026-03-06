//go:build cgo && integration

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadCommandBackfillsMissingDoltServerPortFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	if runtime.GOOS == windowsOS {
		t.Skip("dolt port-file integration test not supported on windows")
	}

	tmpDir := newCLIIntegrationRepo(t)
	env := cliIntegrationEnv()

	initOut, initErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "init", "--backend", "dolt", "--prefix", "test", "--quiet")
	if initErr != nil {
		skipIfDoltBackendUnavailable(t, initOut)
		t.Fatalf("bd init --backend dolt failed: %v\n%s", initErr, initOut)
	}

	createOut, createErr := runBDExecAllowErrorWithEnv(t, tmpDir, env,
		"create", "Port file backfill",
		"--description", "Exercise dolt-server.port backfill on read commands.",
		"--json")
	if createErr != nil {
		t.Fatalf("bd create failed: %v\n%s", createErr, createOut)
	}

	jsonStart := strings.Index(createOut, "{")
	if jsonStart < 0 {
		t.Fatalf("No JSON in create output: %s", createOut)
	}
	var issue map[string]any
	if err := json.Unmarshal([]byte(createOut[jsonStart:]), &issue); err != nil {
		t.Fatalf("Failed to parse create JSON: %v\n%s", err, createOut)
	}
	issueID, _ := issue["id"].(string)
	if issueID == "" {
		t.Fatalf("Create output missing issue ID: %s", createOut)
	}

	if out, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "show", issueID, "--json"); err != nil {
		t.Fatalf("initial bd show failed: %v\n%s", err, out)
	}

	portFile := filepath.Join(tmpDir, ".beads", "dolt-server.port")
	portBytes, err := os.ReadFile(portFile)
	if err != nil {
		t.Fatalf("read dolt-server.port: %v", err)
	}
	originalPort := strings.TrimSpace(string(portBytes))
	if originalPort == "" {
		t.Fatal("expected non-empty dolt-server.port")
	}

	if err := os.Remove(portFile); err != nil {
		t.Fatalf("remove dolt-server.port: %v", err)
	}

	if out, err := runBDExecAllowErrorWithEnv(t, tmpDir, env, "show", issueID, "--json"); err != nil {
		t.Fatalf("bd show after port-file removal failed: %v\n%s", err, out)
	}

	restoredBytes, err := os.ReadFile(portFile)
	if err != nil {
		t.Fatalf("re-read dolt-server.port: %v", err)
	}
	restoredPort := strings.TrimSpace(string(restoredBytes))
	if restoredPort == "" {
		t.Fatal("expected recreated dolt-server.port to be non-empty")
	}
	if restoredPort != originalPort {
		t.Fatalf("expected recreated port %q, got %q", originalPort, restoredPort)
	}
}
