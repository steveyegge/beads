//go:build !linux
// +build !linux

package main

import "fmt"

// IsSystemdAvailable returns false on non-Linux platforms.
func IsSystemdAvailable() bool {
	return false
}

// SetupSystemdDaemon is not supported on non-Linux platforms.
func SetupSystemdDaemon(workspacePath string, quiet bool) error {
	return fmt.Errorf("systemd integration is only available on Linux")
}

// IsSystemdServiceActive is not supported on non-Linux platforms.
func IsSystemdServiceActive(workspacePath string) bool {
	return false
}

// IsSystemdServiceEnabled is not supported on non-Linux platforms.
func IsSystemdServiceEnabled(workspacePath string) bool {
	return false
}

// GetSystemdServiceStatus is not supported on non-Linux platforms.
func GetSystemdServiceStatus(workspacePath string) (string, error) {
	return "", fmt.Errorf("systemd not available on this platform")
}

// GetSystemdServiceLogs is not supported on non-Linux platforms.
func GetSystemdServiceLogs(workspacePath string, lines int) (string, error) {
	return "", fmt.Errorf("systemd not available on this platform")
}

// RestartSystemdService is not supported on non-Linux platforms.
func RestartSystemdService(workspacePath string) error {
	return fmt.Errorf("systemd not available on this platform")
}

// StopSystemdService is not supported on non-Linux platforms.
func StopSystemdService(workspacePath string) error {
	return fmt.Errorf("systemd not available on this platform")
}
