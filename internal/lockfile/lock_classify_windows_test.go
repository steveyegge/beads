//go:build windows

package lockfile

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

// The errnos a Windows filesystem returns when it cannot support advisory
// locks at all — the 9P redirector over \\wsl$ answers LockFileEx with
// ERROR_INVALID_FUNCTION ("Incorrect function"), and other redirectors
// with the not-supported family — must classify as ErrUnsupported while
// keeping the original errno matchable via errors.Is.
func TestClassifyLockErrUnsupportedErrnos(t *testing.T) {
	for _, tc := range []struct {
		name  string
		errno error
	}{
		{"ERROR_INVALID_FUNCTION", windows.ERROR_INVALID_FUNCTION},
		{"ERROR_NOT_SUPPORTED", windows.ERROR_NOT_SUPPORTED},
		{"ERROR_CALL_NOT_IMPLEMENTED", windows.ERROR_CALL_NOT_IMPLEMENTED},
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

// Every other errno is returned unchanged, including the busy mapping's
// ERROR_LOCK_VIOLATION.
func TestClassifyLockErrLeavesOtherErrnosUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name  string
		errno error
	}{
		{"ERROR_LOCK_VIOLATION", windows.ERROR_LOCK_VIOLATION},
		{"ERROR_ACCESS_DENIED", windows.ERROR_ACCESS_DENIED},
		{"ERROR_INVALID_HANDLE", windows.ERROR_INVALID_HANDLE},
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