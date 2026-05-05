package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var (
	initTestBD     string
	initTestBDOnce sync.Once
	initTestBDErr  error
)

func buildBDForInitTests(t *testing.T) string {
	t.Helper()
	initTestBDOnce.Do(func() {
		bdBinary := "bd"
		if runtime.GOOS == "windows" {
			bdBinary = "bd.exe"
		}
		repoRoot := filepath.Join("..", "..")
		existingBD := filepath.Join(repoRoot, bdBinary)
		if _, err := os.Stat(existingBD); err == nil {
			initTestBD, _ = filepath.Abs(existingBD)
			return
		}
		tmpDir, err := os.MkdirTemp("", "bd-init-test-*")
		if err != nil {
			initTestBDErr = fmt.Errorf("failed to create temp dir: %w", err)
			return
		}
		initTestBD = filepath.Join(tmpDir, bdBinary)
		cmd := exec.Command("go", "build", "-tags", "gms_pure_go", "-o", initTestBD, ".")
		if out, err := cmd.CombinedOutput(); err != nil {
			initTestBDErr = fmt.Errorf("go build failed: %v\n%s", err, out)
		}
	})
	if initTestBDErr != nil {
		t.Fatalf("Failed to build bd binary: %v", initTestBDErr)
	}
	return initTestBD
}
