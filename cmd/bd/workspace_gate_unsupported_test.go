package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/lockfile"
	"github.com/steveyegge/beads/internal/workspacegate"
)

// failGatesUnsupported makes every gate acquisition fail the way
// workspacegate reports a filesystem that cannot lock (here, Windows' 9P
// redirector reporting busy with no holder), and counts the calls.
func failGatesUnsupported(t *testing.T) *int {
	t.Helper()
	calls := 0
	old := acquireAllWorkspaceGates
	acquireAllWorkspaceGates = func(_ context.Context, _ workspacegate.Mode, _ workspacegate.Options, gates ...workspacegate.Gate) (*workspacegate.MultiHandle, error) {
		calls++
		return nil, fmt.Errorf("workspacegate: lock %s: %w", gates[0].Path(),
			&lockfile.UnsupportedError{Op: "probe", Err: errors.New("the lock reports busy even on a private file")})
	}
	t.Cleanup(func() { acquireAllWorkspaceGates = old })
	return &calls
}

// An ordinary command on a filesystem that cannot lock runs ungated at once,
// with one warning that says why, instead of waiting out its budget and
// blaming a maintenance operation that is not running.
func TestAcquireCommandWorkspaceGatesUnsupportedFailsOpen(t *testing.T) {
	for _, quiet := range []bool{false, true} {
		t.Run(fmt.Sprintf("quiet=%v", quiet), func(t *testing.T) {
			resetGateTestEnv(t)
			t.Cleanup(releaseWorkspaceGates)
			beadsDir := newGateTestWorkspace(t)
			calls := failGatesUnsupported(t)
			oldQuiet := quietFlag
			quietFlag = quiet
			t.Cleanup(func() { quietFlag = oldQuiet })

			list := &cobra.Command{Use: "list"}
			var err error
			start := time.Now()
			stderr := captureStderr(t, func() {
				err = acquireCommandWorkspaceGates(context.Background(), list, beadsDir)
			})
			if err != nil {
				t.Fatalf("acquireCommandWorkspaceGates = %v, want nil (fail open)", err)
			}
			if waited := time.Since(start); waited > time.Second {
				t.Errorf("took %s, want an immediate return", waited)
			}
			if *calls != 1 {
				t.Errorf("gate acquisition attempted %d times, want once", *calls)
			}
			if workspaceGateHandle != nil {
				t.Error("fail-open path must hold no gate")
			}
			if quiet {
				if stderr != "" {
					t.Errorf("--quiet printed %q, want nothing", stderr)
				}
				return
			}
			if n := strings.Count(stderr, "warning:"); n != 1 {
				t.Errorf("printed %d warnings, want exactly one: %q", n, stderr)
			}
			for _, want := range []string{beadsDir, "filesystem that does not support file locking", "continuing without the workspace gate"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("warning %q missing %q", stderr, want)
				}
			}
		})
	}
}

// Maintenance refuses on a filesystem that cannot lock, as it did before,
// but says so instead of claiming live bd activity or a busy workspace.
func TestAcquireInitMutationGateUnsupportedMessage(t *testing.T) {
	t.Run("bd init", func(t *testing.T) {
		resetGateTestEnv(t)
		beadsDir := newGateTestWorkspace(t)
		physicalRoot := filepath.Join(filepath.Dir(beadsDir), "dolt-data")
		failGatesUnsupported(t)

		preflightRan := false
		h, err := acquireInitMutationGate(context.Background(), beadsDir, physicalRoot, func() error {
			preflightRan = true
			return nil
		})
		if err == nil {
			_ = h.Release()
			t.Fatal("bd init acquired its gates on a filesystem that cannot lock")
		}
		if !errors.Is(err, lockfile.ErrUnsupported) {
			t.Errorf("error %v does not wrap lockfile.ErrUnsupported", err)
		}
		for _, want := range []string{"bd init cannot run here", beadsDir, "filesystem that does not support file locking"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q missing %q", err, want)
			}
		}
		if strings.Contains(err.Error(), "live bd activity") {
			t.Errorf("error %q blames live bd activity", err)
		}
		if preflightRan {
			t.Error("preflight ran without the gates")
		}
	})

	t.Run("bd backup restore", func(t *testing.T) {
		resetGateTestEnv(t)
		t.Cleanup(releaseWorkspaceGates)
		beadsDir := newGateTestWorkspace(t)
		failGatesUnsupported(t)
		root := &cobra.Command{Use: "bd"}
		backup := &cobra.Command{Use: "backup"}
		restore := &cobra.Command{Use: "restore [path]"}
		backup.AddCommand(restore)
		root.AddCommand(backup)
		if !commandNeedsExclusiveGate(restore) {
			t.Fatal("backup restore must take the exclusive gate")
		}

		var err error
		stderr := captureStderr(t, func() {
			err = acquireCommandWorkspaceGates(context.Background(), restore, beadsDir)
		})
		if err == nil {
			t.Fatal("backup restore proceeded on a filesystem that cannot lock")
		}
		for _, want := range []string{"backup restore cannot run here", beadsDir, "filesystem that does not support file locking"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr %q missing %q", stderr, want)
			}
		}
		if workspaceGateHandle != nil {
			t.Error("refused restore must hold no gate")
		}
	})
}
