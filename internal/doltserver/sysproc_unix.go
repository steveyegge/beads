//go:build !windows

package doltserver

import (
	"os/exec"
	"syscall"
)

// setDetachedProcessGroup puts the command in its own process group
// so it survives the parent (bd) exiting. On Unix, this uses Setpgid.
func setDetachedProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
