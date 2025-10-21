//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

const stillActive = 259

var daemonSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// configureDaemonProcess sets up platform-specific process attributes for daemon
func configureDaemonProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}

func sendStopSignal(process *os.Process) error {
	if err := process.Signal(syscall.SIGTERM); err == nil {
		return nil
	}
	return process.Kill()
}

func isReloadSignal(os.Signal) bool {
	return false
}

func isProcessRunning(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)

	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}

	return code == stillActive
}
