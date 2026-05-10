package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage/dolt"
)

func saveStoreModeState(t *testing.T) {
	t.Helper()

	oldServerMode := serverMode
	oldProxiedServerMode := proxiedServerMode
	oldCmdCtx := cmdCtx
	t.Cleanup(func() {
		serverMode = oldServerMode
		proxiedServerMode = oldProxiedServerMode
		cmdCtx = oldCmdCtx
	})

	serverMode = false
	proxiedServerMode = false
	cmdCtx = nil
}

func TestLoadServerModeFromBeadsDirSharedServerWithoutMetadata(t *testing.T) {
	saveStoreModeState(t)
	t.Setenv("BEADS_DOLT_SHARED_SERVER", "1")

	beadsDir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beadsDir, 0o700); err != nil {
		t.Fatal(err)
	}

	cmdCtx = &CommandContext{}
	loadServerModeFromBeadsDir(beadsDir)

	if !serverMode {
		t.Fatal("shared-server env should select server mode even when metadata.json is absent")
	}
	if proxiedServerMode {
		t.Fatal("shared-server env without metadata must not select proxied-server mode")
	}
	if cmdCtx == nil || !cmdCtx.ServerMode || cmdCtx.ProxiedServerMode {
		t.Fatalf("cmdCtx mode = server:%v proxied:%v, want server:true proxied:false", cmdCtx != nil && cmdCtx.ServerMode, cmdCtx != nil && cmdCtx.ProxiedServerMode)
	}
}

func TestApplyRuntimeSharedServerModeCoversDefaultConfigPath(t *testing.T) {
	t.Setenv("BEADS_DOLT_SHARED_SERVER", "1")

	cfg := &dolt.Config{}
	applyRuntimeSharedServerMode(cfg)

	if !cfg.ServerMode {
		t.Fatal("shared-server env should mark default dolt config as server mode")
	}
}

func TestApplyRuntimeSharedServerModePreservesProxiedServer(t *testing.T) {
	t.Setenv("BEADS_DOLT_SHARED_SERVER", "1")

	cfg := &dolt.Config{ProxiedServer: true}
	applyRuntimeSharedServerMode(cfg)

	if cfg.ServerMode {
		t.Fatal("shared-server env should not override proxied-server mode")
	}
}

func TestIsReadOnlySQLQuery(t *testing.T) {
	for _, query := range []string{
		"SELECT count(*) FROM issues",
		"  explain SELECT * FROM issues",
		"pragma table_info(issues)",
		"SHOW TABLES",
		"describe issues",
		"WITH open_issues AS (SELECT * FROM issues) SELECT * FROM open_issues",
	} {
		if !isReadOnlySQLQuery(query) {
			t.Fatalf("isReadOnlySQLQuery(%q) = false, want true", query)
		}
	}

	for _, query := range []string{
		"UPDATE issues SET title = 'x'",
		"DELETE FROM issues",
		"INSERT INTO issues (id) VALUES ('x')",
		"CREATE TABLE scratch (id text)",
	} {
		if isReadOnlySQLQuery(query) {
			t.Fatalf("isReadOnlySQLQuery(%q) = true, want false", query)
		}
	}
}
