//go:build windows

package lockfile

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

var errProcessLocked = errors.New("lock already held by another process")

// classifyLockErr surfaces lock errors that mean "this filesystem cannot
// support advisory locks at all" as *UnsupportedError: the 9P redirector
// over \\wsl$ answers LockFileEx with ERROR_INVALID_FUNCTION ("Incorrect
// function"), and other redirectors with the not-supported family
// (ERROR_NOT_SUPPORTED, ERROR_CALL_NOT_IMPLEMENTED — the errnos
// errors.ErrUnsupported covers). Callers map busy first; every other
// errno is returned unchanged.
func classifyLockErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, windows.ERROR_INVALID_FUNCTION) || errors.Is(err, errors.ErrUnsupported) {
		return &UnsupportedError{Op: "lock", Err: err}
	}
	return err
}

// flockExclusive acquires an exclusive non-blocking lock on the file using LockFileEx
func flockExclusive(f *os.File) error {
	// LOCKFILE_EXCLUSIVE_LOCK (2) | LOCKFILE_FAIL_IMMEDIATELY (1) = 3
	const flags = windows.LOCKFILE_EXCLUSIVE_LOCK | windows.LOCKFILE_FAIL_IMMEDIATELY

	// Create overlapped structure for the entire file
	ol := &windows.Overlapped{}

	// Lock entire file (0xFFFFFFFF, 0xFFFFFFFF = maximum range)
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		flags,
		0,          // reserved
		0xFFFFFFFF, // number of bytes to lock (low)
		0xFFFFFFFF, // number of bytes to lock (high)
		ol,
	)

	if err == windows.ERROR_LOCK_VIOLATION || err == syscall.EWOULDBLOCK {
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
	// LOCKFILE_EXCLUSIVE_LOCK only (no FAIL_IMMEDIATELY = blocking)
	const flags = windows.LOCKFILE_EXCLUSIVE_LOCK

	ol := &windows.Overlapped{}

	return classifyLockErr(windows.LockFileEx(
		windows.Handle(f.Fd()),
		flags,
		0,
		0xFFFFFFFF,
		0xFFFFFFFF,
		ol,
	))
}

// FlockUnlock releases a lock on the file.
func FlockUnlock(f *os.File) error {
	ol := &windows.Overlapped{}

	return windows.UnlockFileEx(
		windows.Handle(f.Fd()),
		0,
		0xFFFFFFFF,
		0xFFFFFFFF,
		ol,
	)
}
