//go:build unix

package lockfile

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// A filesystem that cannot lock (NFS without lockd, some FUSE and SMB
// mounts) must be told apart from one where another process holds the lock,
// without losing the errno callers may still match on.
func TestClassifyLockErr(t *testing.T) {
	for _, tc := range []struct {
		name        string
		errno       unix.Errno
		unsupported bool
	}{
		{"ENOLCK", unix.ENOLCK, true},
		{"ENOTSUP", unix.ENOTSUP, true},
		{"EOPNOTSUPP", unix.EOPNOTSUPP, true},
		{"ENOSYS", unix.ENOSYS, true},
		{"EBADF", unix.EBADF, false},
		{"EACCES", unix.EACCES, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyLockErr(tc.errno)
			if !errors.Is(got, tc.errno) {
				t.Errorf("classifyLockErr(%v) = %v, lost the original errno", tc.errno, got)
			}
			if IsLocked(got) || errors.Is(got, ErrLockBusy) {
				t.Errorf("classifyLockErr(%v) = %v, reads as busy", tc.errno, got)
			}
			if !tc.unsupported {
				if got != error(tc.errno) {
					t.Errorf("classifyLockErr(%v) = %#v, want the errno unchanged", tc.errno, got)
				}
				return
			}
			if !errors.Is(got, ErrUnsupported) {
				t.Errorf("classifyLockErr(%v) = %v, want a match for ErrUnsupported", tc.errno, got)
			}
		})
	}
	if got := classifyLockErr(nil); got != nil {
		t.Errorf("classifyLockErr(nil) = %v, want nil", got)
	}
}
