package lockfile

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
)

// ProbeBusy distinguishes "another process holds the lock" from "this
// filesystem cannot be trusted to report contention" after try reported
// the real lock busy. It runs the same try on a freshly created private
// file (probePrefix + ".probe-<pid>-<rand>") that no other process can
// know, so nobody can contend for it:
//
//   - busy or unsupported there means the filesystem is lying (a 9P or
//     CIFS redirector that reports every file as held), and the caller gets
//     an *UnsupportedError;
//   - success means the probe file merely locks, so the original busy
//     result stands — a real holder exists — and the return is nil;
//   - anything else (file cannot be created, any other lock error) is
//     unverifiable, and the return is nil so callers keep exactly their
//     previous behavior.
//
// The probe file is unlocked, closed, and removed on every path. With
// probePrefix equal to a workspace gate file ("<dir>/.beads.gate.lock"),
// the probe file matches the managed "*.gate.lock*" ignore patterns.
//
// Call ProbeBusy only after try reported busy, never on its own.
func ProbeBusy(probePrefix string, try func(*os.File) error) error {
	f, name, created := openProbeFile(probePrefix)
	if !created {
		// Unverifiable: keep today's behavior, whatever busy meant.
		return nil
	}
	defer func() {
		_ = os.Remove(name)
		_ = f.Close()
	}()
	perr := try(f)
	// Release the probe whether or not the lock was taken; on a busy or
	// unsupported result nothing was locked, and unlock is then harmless.
	_ = FlockUnlock(f)
	if perr == nil {
		// The probe locks fine: a real holder is what made the gate busy.
		return nil
	}
	if errors.Is(perr, ErrLockBusy) || IsLocked(perr) || errors.Is(perr, ErrUnsupported) {
		return &UnsupportedError{Op: "probe", Err: perr}
	}
	// Any other failure is unverifiable: keep today's behavior.
	return nil
}

// openProbeFile creates the private probe file, retrying once with a new
// random suffix on the (near-impossible) collision with an existing file.
func openProbeFile(probePrefix string) (f *os.File, name string, created bool) {
	for attempt := 0; attempt < 2; attempt++ {
		name = fmt.Sprintf("%s.probe-%d-%d", probePrefix, os.Getpid(), rand.Int63()) //nolint:gosec // G404: the suffix only avoids filename collisions between probes; it is not a secret
		var err error
		f, err = os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600) //nolint:gosec // G304: name derives from the caller's lock path plus this process's own PID and a random suffix
		if err == nil {
			return f, name, true
		}
		if !os.IsExist(err) {
			// Cannot probe here: treat as unverifiable.
			return nil, "", false
		}
	}
	return nil, "", false
}