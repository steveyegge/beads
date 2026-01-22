//go:build windows

package dolt

import (
	"os/exec"
)

// setSysProcAttr sets platform-specific process attributes for clean shutdown.
// On Windows, no special process attributes are needed.
func setSysProcAttr(cmd *exec.Cmd) {
	// No special SysProcAttr needed on Windows
}
