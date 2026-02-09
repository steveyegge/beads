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

// sendStopSignal sends a graceful shutdown signal to the daemon process on Windows.
//
// On Windows, SIGTERM is not supported and always fails, causing an immediate
// fallback to TerminateProcess (Kill) which interrupts in-flight operations.
// Instead, we use GenerateConsoleCtrlEvent with CTRL_BREAK_EVENT, which is the
// standard Windows mechanism for graceful shutdown. The daemon process is created
// with CREATE_NEW_PROCESS_GROUP (see configureDaemonProcess), so its process
// group ID equals its PID. The daemon's signal handler receives this as
// os.Interrupt and performs a clean shutdown.
func sendStopSignal(process *os.Process) error {
	// CTRL_BREAK_EVENT is delivered to all processes in the target process group.
	// Since the daemon was started with CREATE_NEW_PROCESS_GROUP, its group ID
	// is its own PID. This triggers os.Interrupt in the daemon's signal handler.
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(process.Pid))
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
