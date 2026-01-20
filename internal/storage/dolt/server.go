// Package dolt implements the storage interface using Dolt (versioned MySQL-compatible database).
//
// This file contains server lifecycle management for dolt sql-server (bd-f4f78a, bd-649383).
// The server mode allows multiple concurrent clients without lock contention.

package dolt

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ServerConfig holds configuration for dolt sql-server
type ServerConfig struct {
	DataDir  string // Directory containing Dolt databases
	Host     string // Listen host (default: 127.0.0.1)
	Port     int    // Listen port (default: 3306)
	User     string // MySQL user (default: root)
	Password string // MySQL password (default: empty)
	LogLevel string // Log level: trace, debug, info, warn, error (default: info)
	LogFile  string // Log file path (default: stderr)
}

// DefaultServerConfig returns server config with default values
func DefaultServerConfig(dataDir string) *ServerConfig {
	return &ServerConfig{
		DataDir:  dataDir,
		Host:     "127.0.0.1",
		Port:     3306,
		User:     "root",
		Password: "",
		LogLevel: "info",
	}
}

// ServerStatus represents the status of a dolt sql-server
type ServerStatus struct {
	Running bool
	PID     int
	Host    string
	Port    int
	Uptime  time.Duration
}

// StartServer starts a dolt sql-server process in the background (bd-649383).
// Returns the PID of the started process.
func StartServer(ctx context.Context, cfg *ServerConfig) (int, error) {
	if cfg.DataDir == "" {
		return 0, fmt.Errorf("data directory is required")
	}

	// Check if server is already running
	if IsServerRunning(cfg.Host, cfg.Port) {
		return 0, fmt.Errorf("server already running on %s:%d", cfg.Host, cfg.Port)
	}

	// Ensure data directory exists
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return 0, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Build command arguments
	// Note: --user and --password were removed in dolt 1.80+
	// Users should be created with CREATE USER and GRANT statements instead
	args := []string{
		"sql-server",
		"--host", cfg.Host,
		"--port", strconv.Itoa(cfg.Port),
		"--data-dir", cfg.DataDir,
	}

	if cfg.LogLevel != "" {
		args = append(args, "--loglevel", cfg.LogLevel)
	}

	// Create the command
	cmd := exec.CommandContext(ctx, "dolt", args...)

	// Set the working directory to the data directory
	cmd.Dir = cfg.DataDir

	// Set up log file if specified, otherwise use temp file
	logPath := cfg.LogFile
	if logPath == "" {
		logPath = filepath.Join(cfg.DataDir, "sql-server.log")
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return 0, fmt.Errorf("failed to open log file: %w", err)
	}

	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Detach from parent process group so server survives parent exit
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Start the server
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return 0, fmt.Errorf("failed to start dolt sql-server: %w", err)
	}

	pid := cmd.Process.Pid

	// Write PID file for later management
	pidFile := filepath.Join(cfg.DataDir, "sql-server.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0o640); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write PID file: %v\n", err)
	}

	// Don't wait for command - let it run in background
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
		// Clean up PID file when server exits
		_ = os.Remove(pidFile)
	}()

	// Wait for server to be ready
	if err := waitForServer(cfg.Host, cfg.Port, 30*time.Second); err != nil {
		// Server didn't start properly, try to kill it
		_ = cmd.Process.Kill()
		// Read the log file to see what went wrong
		if logContent, readErr := os.ReadFile(logPath); readErr == nil {
			return 0, fmt.Errorf("server failed to start: %w\nServer log:\n%s", err, string(logContent))
		}
		return 0, fmt.Errorf("server failed to start: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Dolt sql-server started (PID %d) on %s:%d\n", pid, cfg.Host, cfg.Port)
	return pid, nil
}

// StopServer stops a running dolt sql-server (bd-649383).
func StopServer(dataDir string) error {
	pidFile := filepath.Join(dataDir, "sql-server.pid")

	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no PID file found - server may not be running")
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		return fmt.Errorf("invalid PID in file: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	// Send SIGTERM for graceful shutdown
	if err := process.Signal(syscall.SIGTERM); err != nil {
		// Process might already be dead
		_ = os.Remove(pidFile)
		return fmt.Errorf("failed to send SIGTERM to process %d: %w", pid, err)
	}

	// Wait for process to exit (with timeout)
	done := make(chan error, 1)
	go func() {
		_, err := process.Wait()
		done <- err
	}()

	select {
	case <-done:
		// Process exited
	case <-time.After(10 * time.Second):
		// Force kill if graceful shutdown takes too long
		_ = process.Signal(syscall.SIGKILL)
		<-done
	}

	// Clean up PID file
	_ = os.Remove(pidFile)

	fmt.Fprintf(os.Stderr, "Dolt sql-server stopped (PID %d)\n", pid)
	return nil
}

// GetServerStatus returns the status of a dolt sql-server (bd-649383).
func GetServerStatus(dataDir string, host string, port int) (*ServerStatus, error) {
	status := &ServerStatus{
		Host: host,
		Port: port,
	}

	// Check if server is responding
	status.Running = IsServerRunning(host, port)

	// Try to get PID from file
	pidFile := filepath.Join(dataDir, "sql-server.pid")
	if pidBytes, err := os.ReadFile(pidFile); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes))); err == nil {
			status.PID = pid
		}
	}

	return status, nil
}

// IsServerRunning checks if a dolt sql-server is responding on the given host:port.
func IsServerRunning(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// waitForServer waits for the server to be ready to accept connections.
func waitForServer(host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if IsServerRunning(host, port) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for server on %s:%d", host, port)
}

// EnsureServerRunning starts the server if it's not already running (bd-649383).
// This is useful for auto-starting the server when ServerMode is enabled.
func EnsureServerRunning(ctx context.Context, cfg *ServerConfig) error {
	if IsServerRunning(cfg.Host, cfg.Port) {
		return nil // Already running
	}

	_, err := StartServer(ctx, cfg)
	return err
}

// ServerConfigFromStoreConfig converts a storage Config to ServerConfig.
func ServerConfigFromStoreConfig(cfg *Config) *ServerConfig {
	return &ServerConfig{
		DataDir:  cfg.Path,
		Host:     cfg.ServerHost,
		Port:     cfg.ServerPort,
		User:     cfg.ServerUser,
		Password: cfg.ServerPass,
		LogLevel: "info",
	}
}
