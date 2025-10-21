//go:build unix || linux || darwin

package main

import (
	"os"
	"os/exec"
	"syscall"
)

var daemonSignals = []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP}

// configureDaemonProcess sets up platform-specific process attributes for daemon
func configureDaemonProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func sendStopSignal(process *os.Process) error {
	return process.Signal(syscall.SIGTERM)
}

func isReloadSignal(sig os.Signal) bool {
	return sig == syscall.SIGHUP
}

func isProcessRunning(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
