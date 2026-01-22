//go:build !windows

package dolt

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr sets platform-specific process attributes for clean shutdown.
// On Unix, this sets up a process group for clean shutdown.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
