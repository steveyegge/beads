package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/debug"
)

func TestLogConfigDiscoveryIncludesMetadataAndYAMLState(t *testing.T) {
	beadsDir := t.TempDir()
	metadataPath := filepath.Join(beadsDir, configfile.ConfigFileName)
	if err := os.WriteFile(metadataPath, []byte(`{"database":"dolt"}`), 0o600); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	debug.SetVerbose(true)
	defer debug.SetVerbose(false)

	stderr := captureStderr(t, func() {
		logConfigDiscovery(beadsDir, "metadata loaded without dolt_database; using default database name \"beads\"")
	})

	for _, want := range []string{
		`metadata loaded without dolt_database; using default database name "beads"`,
		"metadata=true",
		"config.yaml=false",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("debug output missing %q:\n%s", want, stderr)
		}
	}
}
