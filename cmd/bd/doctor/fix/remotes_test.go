package fix

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/doltserver"
	"github.com/steveyegge/beads/internal/storage/doltutil"
	"github.com/steveyegge/beads/internal/testutil"
)

func TestResolveRemoteConsistencyContext_SharedWorktreeFallback(t *testing.T) {
	mainRepoDir, worktreeDir := setupSharedWorktreeWorkspace(t)
	sharedBeadsDir := filepath.Join(mainRepoDir, ".beads")
	if err := os.MkdirAll(sharedBeadsDir, 0o755); err != nil {
		t.Fatalf("failed to create shared .beads dir: %v", err)
	}

	cfg := &configfile.Config{
		Backend:      configfile.BackendDolt,
		DoltDatabase: "shared_beads",
	}
	if err := cfg.Save(sharedBeadsDir); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	ctx, err := resolveRemoteConsistencyContext(worktreeDir)
	if err != nil {
		t.Fatalf("resolveRemoteConsistencyContext() error = %v", err)
	}

	wantBeadsDir := resolvePathForTest(t, sharedBeadsDir)
	if ctx.beadsDir != wantBeadsDir {
		t.Fatalf("beadsDir = %q, want %q", ctx.beadsDir, wantBeadsDir)
	}

	wantDBDir := filepath.Join(doltserver.ResolveDoltDir(wantBeadsDir), cfg.GetDoltDatabase())
	if ctx.dbDir != wantDBDir {
		t.Fatalf("dbDir = %q, want %q", ctx.dbDir, wantDBDir)
	}
}

func TestResolveRemoteConsistencyContext_MissingConfig(t *testing.T) {
	dir := setupTestWorkspace(t)

	_, err := resolveRemoteConsistencyContext(dir)
	if err == nil {
		t.Fatal("expected missing config error")
	}
	if !strings.Contains(err.Error(), "failed to load config") {
		t.Fatalf("expected failed config load error, got: %v", err)
	}
}

func TestRemoteConsistency_InvalidWorkspace(t *testing.T) {
	err := RemoteConsistency(t.TempDir())
	if err == nil {
		t.Fatal("expected invalid workspace error")
	}
	if !strings.Contains(err.Error(), "not a beads workspace") {
		t.Fatalf("expected invalid workspace error, got: %v", err)
	}
}

// TestMigrateServerRootRemotes verifies that remotes added at the dolt server
// root (the wrong place) are migrated into the database subdirectory where
// dolt CLI push/pull targets them.
func TestMigrateServerRootRemotes(t *testing.T) {
	testutil.RequireDoltBinary(t)

	rootDir := t.TempDir()
	dbDir := filepath.Join(rootDir, "testdb")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir dbDir: %v", err)
	}

	if out, err := doltInitFor(rootDir); err != nil {
		t.Fatalf("dolt init in rootDir: %s: %v", out, err)
	}
	if out, err := doltInitFor(dbDir); err != nil {
		t.Fatalf("dolt init in dbDir: %s: %v", out, err)
	}

	const (
		remoteName = "test-root-remote"
		remoteURL  = "file:///tmp/test-root-remote"
	)
	if err := doltutil.AddCLIRemote(rootDir, remoteName, remoteURL); err != nil {
		t.Fatalf("add remote at root: %v", err)
	}

	if url := doltutil.FindCLIRemote(dbDir, remoteName); url != "" {
		t.Fatalf("remote should not yet be in dbDir, found: %s", url)
	}

	migrateServerRootRemotes(remoteConsistencyContext{doltDir: rootDir, dbDir: dbDir})

	url := doltutil.FindCLIRemote(dbDir, remoteName)
	if url == "" {
		t.Fatal("expected remote to be migrated into dbDir")
	}
	if url != remoteURL {
		t.Fatalf("migrated remote URL = %q, want %q", url, remoteURL)
	}
}

func doltInitFor(dir string) ([]byte, error) {
	cmd := exec.Command("dolt", "init", "--name", "test", "--email", "test@test.com")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}
