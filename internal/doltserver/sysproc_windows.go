//go:build windows

package doltserver

import "os/exec"

// setDetachedProcessGroup is a no-op on Windows.
// Windows does not support Setpgid; the CREATE_NEW_PROCESS_GROUP flag
// could be used here if needed in the future.
func setDetachedProcessGroup(cmd *exec.Cmd) {
	// no-op on Windows
}
