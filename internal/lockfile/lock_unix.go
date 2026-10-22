//go:build unix

package lockfile

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

var errProcessLocked = errors.New("lock already held by another process")

// classifyLockErr surfaces lock errors that mean "this filesystem cannot
// support advisory locks at all" as *UnsupportedError: ENOLCK is the
// canonical refusal (NFS mounted nolock), and ENOSYS/ENOTSUP/EOPNOTSUPP —
// the errnos errors.ErrUnsupported covers — come from FUSE passthroughs
// and 9P-style redirectors. Callers map busy first; every other errno is
// returned unchanged.
func classifyLockErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.ENOLCK) || errors.Is(err, errors.ErrUnsupported) {
		return &UnsupportedError{Op: "lock", Err: err}
	}
	return err
}

// flockExclusive acquires an exclusive non-blocking lock on the file
func flockExclusive(f *os.File) error {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == unix.EWOULDBLOCK {
		return errProcessLocked
	}
	return classifyLockErr(err)
}

// FlockExclusiveNonBlocking attempts to acquire an exclusive lock without blocking.
// Returns ErrLocked if the lock is held by another process.
func FlockExclusiveNonBlocking(f *os.File) error {
	return flockExclusive(f)
}

// FlockExclusiveBlocking acquires an exclusive blocking lock on the file.
// This will wait until the lock is available.
func FlockExclusiveBlocking(f *os.File) error {
	return classifyLockErr(unix.Flock(int(f.Fd()), unix.LOCK_EX))
}

// FlockUnlock releases a lock on the file.
func FlockUnlock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
