//go:build cgo

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// CLI-level integration tests for dependency type rejection.
// These validate that bd create, bd dep add, and related commands
// correctly reject non-well-known dependency types and malformed
// external refs at the CLI boundary.
//
// Uses exec.Command because FatalError/FatalErrorRespectJSON call
// os.Exit(1), which would kill the test process if run in-process.

var (
	depTestBD     string
	depTestBDOnce sync.Once
)

func getDepTestBD(t *testing.T) string {
	t.Helper()
	depTestBDOnce.Do(func() {
		bdBinary := "bd"
		if runtime.GOOS == "windows" {
			bdBinary = "bd.exe"
		}

		// Check for existing binary at repo root
		repoRoot := filepath.Join("..", "..")
		existingBD := filepath.Join(repoRoot, bdBinary)
		if abs, err := filepath.Abs(existingBD); err == nil {
			if _, err := os.Stat(abs); err == nil {
				depTestBD = abs
				return
			}
		}

		// Build once
		tmpDir, err := os.MkdirTemp("", "bd-dep-test-*")
		if err != nil {
			panic(err)
		}
		depTestBD = filepath.Join(tmpDir, bdBinary)
		cmd := exec.Command("go", "build", "-o", depTestBD, ".")
		if out, err := cmd.CombinedOutput(); err != nil {
			panic(string(out))
		}
	})
	return depTestBD
}

func initDepTestDB(t *testing.T) string {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "bd-dep-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	cmd := exec.Command(getDepTestBD(t), "init", "--prefix", "test", "--quiet")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init failed: %v\n%s", err, out)
	}
	return tmpDir
}

func createDepTestIssue(t *testing.T, dir, title string) string {
	t.Helper()
	cmd := exec.Command(getDepTestBD(t), "create", title, "-p", "1", "--json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bd create %q failed: %v\n%s", title, err, out)
	}

	outStr := string(out)
	jsonStart := strings.Index(outStr, "{")
	if jsonStart < 0 {
		t.Fatalf("No JSON in create output: %s", outStr)
	}

	var issue map[string]interface{}
	if err := json.Unmarshal([]byte(outStr[jsonStart:]), &issue); err != nil {
		t.Fatalf("Failed to parse create output: %v\n%s", err, outStr)
	}
	return issue["id"].(string)
}

func TestCLI_CreateRejectsUnknownDepType(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI test in short mode")
	}
	tmpDir := initDepTestDB(t)

	cmd := exec.Command(getDepTestBD(t), "create", "Test issue", "-p", "1",
		"--deps", "custom-type:test-123")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Error("Expected error for unknown dependency type, got none")
	}
	if !strings.Contains(string(out), "unknown dependency type") {
		t.Errorf("Expected 'unknown dependency type' error, got: %s", out)
	}
}

func TestCLI_DepAddRejectsUnknownType(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI test in short mode")
	}
	tmpDir := initDepTestDB(t)
	id1 := createDepTestIssue(t, tmpDir, "First")
	id2 := createDepTestIssue(t, tmpDir, "Second")

	cmd := exec.Command(getDepTestBD(t), "dep", "add", id1, id2, "--type", "custom-type")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Error("Expected error for unknown dependency type, got none")
	}
	if !strings.Contains(string(out), "unknown dependency type") {
		t.Errorf("Expected 'unknown dependency type' error, got: %s", out)
	}
}

func TestCLI_CreateRejectsMalformedExternalRef(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI test in short mode")
	}
	tmpDir := initDepTestDB(t)

	// "external:proj" is missing the capability segment (needs external:proj:cap)
	cmd := exec.Command(getDepTestBD(t), "create", "Test issue", "-p", "1",
		"--deps", "external:proj")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Error("Expected error for malformed external ref, got none")
	}
	if !strings.Contains(string(out), "invalid external") {
		t.Errorf("Expected 'invalid external' error, got: %s", out)
	}
}

func TestCLI_DepAddRejectsCommonAlias(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI test in short mode")
	}
	tmpDir := initDepTestDB(t)
	id1 := createDepTestIssue(t, tmpDir, "First")
	id2 := createDepTestIssue(t, tmpDir, "Second")

	// "depends-on" is a common alias — should get a friendly error suggesting "blocks"
	cmd := exec.Command(getDepTestBD(t), "dep", "add", id1, id2, "--type", "depends-on")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "BEADS_NO_DAEMON=1")
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Error("Expected error for alias dependency type, got none")
	}
	outStr := string(out)
	if !strings.Contains(outStr, "depends-on") || !strings.Contains(outStr, "blocks") {
		t.Errorf("Expected friendly alias error suggesting 'blocks', got: %s", outStr)
	}
}
