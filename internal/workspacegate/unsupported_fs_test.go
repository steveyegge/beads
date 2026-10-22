package workspacegate

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/lockfile"
)

// The lying-busy filesystems (GH#3613: the Windows 9P redirector, some CIFS
// and NFS-nolock mounts) report contention for every file, so a busy gate
// is indistinguishable from a live holder by the gate lock alone. The
// acquisition path distinguishes them on the FIRST busy signal by locking
// a private probe file no other process can know: busy there is a
// filesystem lie, surfacing as lockfile.ErrUnsupported (not ErrBusy)
// without waiting and without leaving anything behind.

// fakeLyingBusy installs lock primitives that report every file as held,
// as a 9P redirector does. The returned callback records which files the
// probe locked, distinguishing the probe file from the gate file.
func fakeLyingBusy(t *testing.T) *[]string {
	t.Helper()
	var probed []string
	oldShared, oldExclusive := flockSharedNB, flockExclusiveNB
	flockSharedNB = func(f *os.File) error {
		if strings.Contains(f.Name(), ".probe-") {
			probed = append(probed, f.Name())
		}
		return lockfile.ErrLockBusy
	}
	flockExclusiveNB = func(f *os.File) error {
		if strings.Contains(f.Name(), ".probe-") {
			probed = append(probed, f.Name())
		}
		return lockfile.ErrLockBusy
	}
	t.Cleanup(func() { flockSharedNB, flockExclusiveNB = oldShared, oldExclusive })
	return &probed
}

// (AC-1) A lock primitive that reports busy for every file must fail the
// acquisition fast with lockfile.ErrUnsupported — not ErrBusy, not a 2s
// wait, no waiting notice, and no probe file left behind.
func TestAcquireLyingBusyFilesystemIsUnsupported(t *testing.T) {
	for _, mode := range []Mode{Shared, Exclusive} {
		t.Run(mode.String(), func(t *testing.T) {
			g, dir := testGate(t)
			fakeLyingBusy(t)

			waited := 0
			start := time.Now()
			h, err := g.Acquire(context.Background(), mode, Options{
				Wait:   2 * time.Second,
				OnWait: func(string) { waited++ },
			})
			elapsed := time.Since(start)
			if h != nil {
				_ = h.Release()
			}
			if err == nil {
				t.Fatal("Acquire on a filesystem whose locks always report busy must fail")
			}
			if !errors.Is(err, lockfile.ErrUnsupported) {
				t.Errorf("err = %v, want lockfile.ErrUnsupported in the chain", err)
			}
			if errors.Is(err, ErrBusy) {
				t.Errorf("err = %v, a lying-busy filesystem must not be reported as ErrBusy", err)
			}
			// Under half the 2s budget: the probe answers at once; only a
			// fall-through to the wait loop could approach the budget.
			if elapsed >= time.Second {
				t.Errorf("Acquire took %s, want the probe to fail fast instead of waiting out the budget", elapsed)
			}
			if waited != 0 {
				t.Errorf("OnWait fired %d times, want none: no waiting notice before the probe", waited)
			}
			for _, name := range gateDirEntries(t, dir) {
				if strings.Contains(name, ".probe-") {
					t.Errorf("probe file %s left behind in %s", name, dir)
				}
			}
		})
	}
}
// (AC-2) A REAL holder on a local filesystem is the opposite case: the
// probe file locks fine, so the busy result stands — the acquisition waits
// out its budget and fails with ErrBusy, never ErrUnsupported, and the
// probe leaves nothing behind in the gate's directory.
func TestAcquireUnderRealHolderIsBusyNotUnsupported(t *testing.T) {
	g, dir := testGate(t)
	holder := mustAcquire(t, g, Exclusive, Options{Reason: "test maintenance"})
	defer func() { _ = holder.Release() }()
	before := gateDirEntries(t, dir)

	const wait = 300 * time.Millisecond
	waited := 0
	start := time.Now()
	h, err := g.Acquire(context.Background(), Shared, Options{
		Wait:   wait,
		OnWait: func(string) { waited++ },
	})
	elapsed := time.Since(start)
	if h != nil {
		_ = h.Release()
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("Acquire under a real exclusive holder: err = %v, want ErrBusy once the wait expires", err)
	}
	if errors.Is(err, lockfile.ErrUnsupported) {
		t.Errorf("err = %v: a real holder on a lockable filesystem must not be classified unsupported", err)
	}
	if elapsed < wait {
		t.Errorf("gave up after %s, before its %s budget: a real holder must still be waited out", elapsed, wait)
	}
	if waited != 1 {
		t.Errorf("OnWait fired %d times, want once as before the probe existed", waited)
	}
	if got := gateDirEntries(t, dir); !slices.Equal(got, before) {
		t.Errorf("the probe left files in %s\n before: %v\n after:  %v", dir, before, got)
	}
}
