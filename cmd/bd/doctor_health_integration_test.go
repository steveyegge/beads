//go:build cgo && integration

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/testutil"
)

func TestDoctorCheckHealthReportsVersionMismatchOnRepoLocalPort(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	if runtime.GOOS == windowsOS {
		t.Skip("doctor health integration test not supported on windows")
	}

	tmpDir := newCLIIntegrationRepo(t)
	serverPort, err := testutil.FindFreePort()
	if err != nil {
		t.Fatalf("FindFreePort: %v", err)
	}
	env := cliIntegrationEnv(
		"BEADS_DOLT_SERVER_PORT=" + strconv.Itoa(serverPort),
	)

	initOut, initErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "init", "--backend", "dolt", "--prefix", "test", "--quiet")
	if initErr != nil {
		skipIfDoltBackendUnavailable(t, initOut)
		t.Fatalf("bd init --backend dolt failed: %v\n%s", initErr, initOut)
	}

	startOut, startErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "dolt", "start")
	if startErr != nil {
		t.Fatalf("bd dolt start failed: %v\n%s", startErr, startOut)
	}

	portBytes, err := os.ReadFile(filepath.Join(tmpDir, ".beads", "dolt-server.port"))
	if err != nil {
		t.Fatalf("read dolt-server.port: %v", err)
	}
	port := strings.TrimSpace(string(portBytes))
	if port == "" {
		t.Fatal("expected non-empty dolt-server.port")
	}
	if port == "3307" {
		t.Skip("derived repo-local port unexpectedly matched 3307; not exercising regression")
	}

	sqlOut, sqlErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "sql", "UPDATE metadata SET value = '0.0.0' WHERE `key` = 'bd_version'")
	if sqlErr != nil {
		t.Fatalf("bd sql UPDATE failed: %v\n%s", sqlErr, sqlOut)
	}

	healthOut, healthErr := runBDExecAllowErrorWithEnv(t, tmpDir, env, "doctor", "--check-health")
	if healthErr == nil {
		t.Fatalf("expected bd doctor --check-health to fail on version mismatch; output:\n%s", healthOut)
	}
	if !strings.Contains(healthOut, "Version mismatch") {
		t.Fatalf("expected version mismatch in doctor --check-health output; output:\n%s", healthOut)
	}
	if !strings.Contains(healthOut, "CLI: "+Version) {
		t.Fatalf("expected CLI version %q in output; output:\n%s", Version, healthOut)
	}
	if !strings.Contains(healthOut, "database: 0.0.0") {
		t.Fatalf("expected database version in output; output:\n%s", healthOut)
	}
}
