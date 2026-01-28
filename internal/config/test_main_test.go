package config

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("BEADS_TEST_ALLOW_PROJECT_CONFIG", "1")
	cwd, _ := os.Getwd()
	tmpDir, err := os.MkdirTemp("", "beads-config-test")
	if err == nil {
		_ = os.Chdir(tmpDir)
	}
	ResetForTesting()

	code := m.Run()

	ResetForTesting()
	if err == nil {
		_ = os.Chdir(cwd)
		_ = os.RemoveAll(tmpDir)
	}
	os.Exit(code)
}
