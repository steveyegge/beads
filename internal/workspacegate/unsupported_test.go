package workspacegate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/lockfile"
)

// setFlockSeams swaps the gate's non-blocking lock primitives for the test.
func setFlockSeams(t *testing.T, shared, exclusive func(*os.File) error) {
	t.Helper()
	oldShared, oldExclusive := flockSharedNB, flockExclusiveNB
	flockSharedNB, flockExclusiveNB = shared, exclusive
	t.Cleanup(func() { flockSharedNB, flockExclusiveNB = oldShared, oldExclusive })
}

// countingBusy reports busy for every file, as Windows' 9P redirector can
// for \\wsl$ paths, and counts the calls that were made on a probe file.
func countingBusy(probes *atomic.Int32) func(*os.File) error {
	return func(f *os.File) error {
		if strings.Contains(filepath.Base(f.Name()), ".probe-") {
			probes.Add(1)
		}
		return lockfile.ErrLockBusy
	}
}

func probeFiles(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, "*.probe-*"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// A lock that reports busy with no holder must not cost a command its whole
// wait budget and then blame a maintenance operation that is not running.
func TestAcquireLyingBusyFilesystemIsUnsupported(t *testing.T) {
	for _, mode := range []Mode{Shared, Exclusive} {
		for _, staleIntent := range []bool{false, true} {
			name := mode.String()
			if staleIntent {
				name += " with leftover intent file"
			}
			t.Run(name, func(t *testing.T) {
				var probes atomic.Int32
				setFlockSeams(t, countingBusy(&probes), countingBusy(&probes))
				g, dir := testGate(t)
				if staleIntent {
					// A failed bd init leaves its intent file behind; on a
					// lying filesystem it then reads as a queued writer.
					if err := os.WriteFile(g.intentPath(), nil, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				waited := false
				start := time.Now()
				h, err := g.Acquire(context.Background(), mode, Options{
					Wait:   2 * time.Second,
					OnWait: func(string) { waited = true },
				})
				elapsed := time.Since(start)
				if h != nil {
					_ = h.Release()
				}
				if !errors.Is(err, lockfile.ErrUnsupported) {
					t.Fatalf("Acquire = %v, want lockfile.ErrUnsupported", err)
				}
				if errors.Is(err, ErrBusy) {
					t.Errorf("Acquire = %v, must not read as ErrBusy", err)
				}
				if elapsed >= time.Second {
					t.Errorf("Acquire took %s, want well under the 2s wait", elapsed)
				}
				if waited {
					t.Error("OnWait called for a filesystem that cannot lock")
				}
				if got := probes.Load(); got != 1 {
					t.Errorf("probe ran %d times, want once", got)
				}
				if left := probeFiles(t, dir); len(left) != 0 {
					t.Errorf("probe files left behind: %v", left)
				}
			})
		}
	}

	t.Run("uncontended acquire never probes", func(t *testing.T) {
		var probes atomic.Int32
		ok := func(f *os.File) error {
			if strings.Contains(filepath.Base(f.Name()), ".probe-") {
				probes.Add(1)
			}
			return nil
		}
		setFlockSeams(t, ok, ok)
		g, dir := testGate(t)
		for _, mode := range []Mode{Shared, Exclusive} {
			h := mustAcquire(t, g, mode, Options{Wait: time.Second})
			_ = h.Release()
		}
		if got := probes.Load(); got != 0 {
			t.Errorf("probe ran %d times on uncontended acquires", got)
		}
		if left := probeFiles(t, dir); len(left) != 0 {
			t.Errorf("probe files left behind: %v", left)
		}
	})

	t.Run("fail-fast shared", func(t *testing.T) {
		var probes atomic.Int32
		setFlockSeams(t, countingBusy(&probes), countingBusy(&probes))
		g, _ := testGate(t)
		_, err := g.Acquire(context.Background(), Shared, Options{})
		if !errors.Is(err, lockfile.ErrUnsupported) || errors.Is(err, ErrBusy) {
			t.Fatalf("fail-fast Acquire = %v, want lockfile.ErrUnsupported and not ErrBusy", err)
		}
	})
}
