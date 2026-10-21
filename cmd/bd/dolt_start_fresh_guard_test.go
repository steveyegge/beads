package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
)

// bd-7pkvvd (PR #7729): with dolt.auto-start off, `bd dolt start` must refuse
// to create a new, empty Dolt database before anything is written. The
// externally-managed server's data lives elsewhere; a start here runs dolt
// init on an empty directory, launches a second unsupervised sql-server, and
// publishes its port — and every later bd command then resolves to that empty
// phantom database instead of the real one.

// freshStartTestEnv isolates the bd subprocess from the ambient environment:
// BEADS_*/BD_* stripped (BEADS_DIR pointed at the workspace), auto-start off,
// and HOME/XDG_CONFIG_HOME redirected so no developer or CI config.yaml can
// decide the outcome.
func freshStartTestEnv(t *testing.T, beadsDir string) []string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	return append(removedBackendTestEnv(beadsDir),
		"BD_DISABLE_METRICS=1",
		"BEADS_DOLT_AUTO_START=0",
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
	)
}

// runDoltStartInWorkspace runs `bd dolt start` in repoDir and returns its
// combined output and exit error.
func runDoltStartInWorkspace(t *testing.T, bd, repoDir string, env []string, extraArgs ...string) (string, error) {
	t.Helper()
	args := append([]string{"dolt", "start"}, extraArgs...)
	cmd := exec.Command(bd, args...)
	cmd.Dir = repoDir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// writeServerModeMetadata writes the metadata of a server-mode dolt workspace
// with no port pinned and no data directory.
func writeServerModeMetadata(t *testing.T, beadsDir string) {
	t.Helper()
	cfg := &configfile.Config{
		Backend:        configfile.BackendDolt,
		DoltMode:       configfile.DoltModeServer,
		DoltServerHost: "127.0.0.1",
	}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("save metadata: %v", err)
	}
}

func TestDoltStartRefusesFreshDataDirWhenAutoStartOff(t *testing.T) {
	bd := buildBDForInitTests(t)

	t.Run("missing data dir", func(t *testing.T) {
		root := t.TempDir()
		beadsDir := filepath.Join(root, ".beads")
		if err := os.MkdirAll(beadsDir, 0o700); err != nil {
			t.Fatal(err)
		}
		writeServerModeMetadata(t, beadsDir)

		out, err := runDoltStartInWorkspace(t, bd, root, freshStartTestEnv(t, beadsDir))
		if err == nil {
			t.Fatalf("bd dolt start succeeded over a missing data dir with auto-start off:\n%s", out)
		}
		msg := out
		if !strings.Contains(msg, "--force") {
			t.Errorf("refusal does not name --force:\n%s", msg)
		}
		if !strings.Contains(msg, filepath.Join(beadsDir, "dolt")) {
			t.Errorf("refusal does not name the data dir:\n%s", msg)
		}
		if strings.Contains(msg, "auto-start is disabled") {
			t.Errorf("refusal contains 'auto-start is disabled':\n%s", msg)
		}
		for _, name := range []string{"dolt", "dolt-server.port", "dolt-server.pid", "dolt-server.lock"} {
			if _, statErr := os.Stat(filepath.Join(beadsDir, name)); !os.IsNotExist(statErr) {
				t.Errorf("refused start created %s (stat error: %v)", name, statErr)
			}
		}
	})

	t.Run("empty data dir", func(t *testing.T) {
		root := t.TempDir()
		beadsDir := filepath.Join(root, ".beads")
		doltDir := filepath.Join(beadsDir, "dolt")
		if err := os.MkdirAll(doltDir, 0o700); err != nil {
			t.Fatal(err)
		}
		writeServerModeMetadata(t, beadsDir)

		out, err := runDoltStartInWorkspace(t, bd, root, freshStartTestEnv(t, beadsDir))
		if err == nil {
			t.Fatalf("bd dolt start succeeded over an empty data dir with auto-start off:\n%s", out)
		}
		if !strings.Contains(out, "--force") {
			t.Errorf("refusal does not name --force:\n%s", out)
		}
		entries, readErr := os.ReadDir(doltDir)
		if readErr != nil {
			t.Fatalf("data dir missing after refusal: %v", readErr)
		}
		if len(entries) != 0 {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			t.Errorf("refused start wrote to the data dir: %v", names)
		}
		for _, name := range []string{"dolt-server.port", "dolt-server.pid"} {
			if _, statErr := os.Stat(filepath.Join(beadsDir, name)); !os.IsNotExist(statErr) {
				t.Errorf("refused start created %s (stat error: %v)", name, statErr)
			}
		}
	})
}

// TestExternallyManagedFreshStartRefusal pins the refusal predicate
// (bd-7pkvvd): with auto-start off and no Dolt database at the data dir, the
// start is refused; nothing is created either way.
func TestExternallyManagedFreshStartRefusal(t *testing.T) {
	t.Setenv("BEADS_DOLT_SHARED_SERVER", "")
	t.Setenv("BEADS_DOLT_DATA_DIR", "")

	// freshServerDir returns an isolated server dir whose data dir is laid
	// out by setup.
	freshServerDir := func(t *testing.T, setup func(t *testing.T, beadsDir string)) string {
		t.Helper()
		serverDir := t.TempDir()
		if setup != nil {
			setup(t, serverDir)
		}
		return serverDir
	}
	refusalChecks := func(t *testing.T, err error, serverDir string) {
		t.Helper()
		if err == nil {
			t.Fatal("expected a refusal")
		}
		msg := err.Error()
		if !strings.Contains(msg, filepath.Join(serverDir, "dolt")) {
			t.Errorf("refusal does not name the data dir:\n%s", msg)
		}
		if !strings.Contains(msg, "--force") {
			t.Errorf("refusal does not name --force:\n%s", msg)
		}
		if strings.Contains(msg, "auto-start is disabled") {
			t.Errorf("refusal contains 'auto-start is disabled':\n%s", msg)
		}
		// The predicate is read-only: a refusal must not create server
		// state. (The data dir itself is only asserted absent in the
		// missing-dir row — the other rows pre-create it.)
		for _, name := range []string{"dolt-server.port", "dolt-server.pid"} {
			if _, statErr := os.Stat(filepath.Join(serverDir, name)); !os.IsNotExist(statErr) {
				t.Errorf("refusal created %s (stat error: %v)", name, statErr)
			}
		}
	}

	t.Run("refuses a missing data dir", func(t *testing.T) {
		serverDir := freshServerDir(t, nil)
		err := externallyManagedFreshStartRefusal(serverDir, true, false)
		refusalChecks(t, err, serverDir)
		// A missing data dir must still be absent after the refusal.
		if _, statErr := os.Stat(filepath.Join(serverDir, "dolt")); !os.IsNotExist(statErr) {
			t.Errorf("refusal created the data dir (stat error: %v)", statErr)
		}
	})

	t.Run("refuses an empty data dir", func(t *testing.T) {
		serverDir := freshServerDir(t, func(t *testing.T, beadsDir string) {
			if err := os.MkdirAll(filepath.Join(beadsDir, "dolt"), 0o700); err != nil {
				t.Fatal(err)
			}
		})
		err := externallyManagedFreshStartRefusal(serverDir, true, false)
		refusalChecks(t, err, serverDir)
	})

	t.Run("refuses a data dir with only stray files", func(t *testing.T) {
		serverDir := freshServerDir(t, func(t *testing.T, beadsDir string) {
			doltDir := filepath.Join(beadsDir, "dolt")
			if err := os.MkdirAll(filepath.Join(doltDir, ".doltcfg"), 0o700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{".bd-dolt-ok", "config.yaml"} {
				if err := os.WriteFile(filepath.Join(doltDir, name), []byte("x\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		})
		err := externallyManagedFreshStartRefusal(serverDir, true, false)
		refusalChecks(t, err, serverDir)
	})

	t.Run("force skips the refusal", func(t *testing.T) {
		serverDir := freshServerDir(t, nil)
		if err := externallyManagedFreshStartRefusal(serverDir, true, true); err != nil {
			t.Fatalf("--force must skip the refusal, got: %v", err)
		}
	})
}
