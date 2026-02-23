//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// configureBackgroundProcess sets platform-specific process attributes to
// detach the child process so it survives after the parent (bd) exits.
func configureBackgroundProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}
