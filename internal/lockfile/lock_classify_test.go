//go:build unix

package lockfile

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// The errnos a POSIX filesystem returns when it cannot support advisory
// locks at all — NFS mounted nolock (ENOLCK), FUSE passthroughs and
// 9P-style redirectors (ENOSYS, ENOTSUP, EOPNOTSUPP) — must classify as
// ErrUnsupported while keeping the original errno matchable via
// errors.Is, and must not look like contention to IsLocked or ErrLockBusy.
func TestClassifyLockErrUnsupportedErrnos(t *testing.T) {
	for _, tc := range []struct {
		name  string
		errno error
	}{
		{"ENOLCK", unix.ENOLCK},
		{"ENOSYS", unix.ENOSYS},
		{"ENOTSUP", unix.ENOTSUP},
		{"EOPNOTSUPP", unix.EOPNOTSUPP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyLockErr(tc.errno)
			if !errors.Is(got, ErrUnsupported) {
				t.Errorf("classifyLockErr(%v) = %v, want an error matching ErrUnsupported", tc.errno, got)
			}
			if !errors.Is(got, tc.errno) {
				t.Errorf("classifyLockErr(%v) = %v, want the original errno still matchable via errors.Is", tc.errno, got)
			}
			if IsLocked(got) {
				t.Errorf("classifyLockErr(%v) = %v, want it not to look like a held lock", tc.errno, got)
			}
			if errors.Is(got, ErrLockBusy) {
				t.Errorf("classifyLockErr(%v) = %v, want it not to look busy", tc.errno, got)
			}
			var uerr *UnsupportedError
			if !errors.As(got, &uerr) {
				t.Errorf("classifyLockErr(%v) = %T, want *UnsupportedError carrying the errno", tc.errno, got)
			}
		})
	}
}

// Every other errno is returned unchanged: classification must not
// swallow or rewrap ordinary lock failures (EBADF, EACCES).
func TestClassifyLockErrLeavesOtherErrnosUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name  string
		errno error
	}{
		{"EBADF", unix.EBADF},
		{"EACCES", unix.EACCES},
		{"EWOULDBLOCK", unix.EWOULDBLOCK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyLockErr(tc.errno)
			if got != tc.errno {
				t.Errorf("classifyLockErr(%v) = %v (%T), want the errno returned unchanged", tc.errno, got, got)
			}
			if errors.Is(got, ErrUnsupported) {
				t.Errorf("classifyLockErr(%v) must not match ErrUnsupported", tc.errno)
			}
		})
	}
}