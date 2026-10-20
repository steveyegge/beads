package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExternallyManagedFreshStartRefusal pins the guard against the phantom
// database: in a workspace whose server is managed outside bd
// (dolt.auto-start: false), a lost port file used to send operators to
// `bd dolt start`, which ran `dolt init` on an empty .beads/dolt and served
// that instead of the real database. The refusal must fire before anything
// is created, and must not get in the way of a start over existing data, of
// an auto-start workspace, or of an explicit --force.
func TestExternallyManagedFreshStartRefusal(t *testing.T) {
	t.Setenv("BEADS_DOLT_SHARED_SERVER", "0")
	t.Setenv("BEADS_DOLT_DATA_DIR", "")

	newBeadsDir := func(t *testing.T) string {
		t.Helper()
		dir := filepath.Join(t.TempDir(), ".beads")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("auto-start off, no data dir: refused, nothing created", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_AUTO_START", "0")
		beadsDir := newBeadsDir(t)
		err := externallyManagedFreshStartRefusal(beadsDir, false)
		if err == nil {
			t.Fatal("expected a refusal for a fresh data dir with auto-start off")
		}
		for _, want := range []string{"new, empty database", "dolt-server.port", "bd dolt start --force"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal should mention %q; got:\n%s", want, err)
			}
		}
		if _, statErr := os.Stat(filepath.Join(beadsDir, "dolt")); !os.IsNotExist(statErr) {
			t.Errorf(".beads/dolt must not exist after a refused start (stat err: %v)", statErr)
		}
	})

	t.Run("auto-start off, empty data dir: refused", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_AUTO_START", "0")
		beadsDir := newBeadsDir(t)
		if err := os.MkdirAll(filepath.Join(beadsDir, "dolt"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := externallyManagedFreshStartRefusal(beadsDir, false); err == nil {
			t.Fatal("an empty data dir is as fresh as a missing one")
		}
	})

	t.Run("auto-start off, existing data: allowed", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_AUTO_START", "0")
		beadsDir := newBeadsDir(t)
		if err := os.MkdirAll(filepath.Join(beadsDir, "dolt", ".dolt"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := externallyManagedFreshStartRefusal(beadsDir, false); err != nil {
			t.Fatalf("a start over existing data must stay allowed: %v", err)
		}
	})

	t.Run("auto-start off, --force: allowed", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_AUTO_START", "0")
		if err := externallyManagedFreshStartRefusal(newBeadsDir(t), true); err != nil {
			t.Fatalf("--force must override the refusal: %v", err)
		}
	})

	t.Run("auto-start on: allowed", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_AUTO_START", "1")
		if err := externallyManagedFreshStartRefusal(newBeadsDir(t), false); err != nil {
			t.Fatalf("a workspace bd manages may start fresh: %v", err)
		}
	})
}
