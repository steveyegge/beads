package lockfile

import (
	"errors"
	"fmt"
)

// ErrLocked is returned when a lock cannot be acquired because it is held by another process.
var ErrLocked = errProcessLocked

// ErrLockBusy is returned when a non-blocking lock cannot be acquired
// because another process holds a conflicting lock.
var ErrLockBusy = errors.New("lock busy: held by another process")

// IsLocked returns true if the error indicates a lock is held by another process.
func IsLocked(err error) bool {
	return errors.Is(err, errProcessLocked)
}

// ErrUnsupported reports that the filesystem cannot support file locking at
// all (NFS mounted nolock, the Windows 9P redirector, some CIFS mounts).
// It is distinct from contention: no amount of waiting can succeed, and a
// caller that reports busy may be looking at a filesystem that reports
// every file as held. Match with errors.Is.
var ErrUnsupported = errors.New("this filesystem does not support file locking")

// UnsupportedError wraps a filesystem error that proves this filesystem
// cannot support advisory locks (ENOLCK, ERROR_INVALID_FUNCTION, ...).
// errors.Is(err, ErrUnsupported) is true for it, and Unwrap returns the
// original error so errno checks keep working.
type UnsupportedError struct {
	// Op names the operation that hit the unsupported filesystem, e.g.
	// "lock" or "probe".
	Op string
	// Err is the underlying errno.
	Err error
}

func (e *UnsupportedError) Error() string {
	if e.Op == "" {
		return fmt.Sprintf("%s: %v", ErrUnsupported, e.Err)
	}
	return fmt.Sprintf("%s: %s: %v", e.Op, ErrUnsupported, e.Err)
}

func (e *UnsupportedError) Is(target error) bool { return target == ErrUnsupported }

func (e *UnsupportedError) Unwrap() error { return e.Err }
