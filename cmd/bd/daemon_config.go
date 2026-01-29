package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/rpc"
)

// ensureBeadsDir ensures the local beads directory exists (.beads in the current workspace).
// Uses FindBeadsDir() to locate the proper .beads directory with config files.
// This is important for Dolt server mode where the database may be in a separate location.
func ensureBeadsDir() (string, error) {
	// Use FindBeadsDir() for proper .beads directory resolution
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		// Fallback: derive from database path (for explicit --db usage)
		if dbPath != "" {
			beadsDir = filepath.Dir(dbPath)
		} else if foundDB := beads.FindDatabasePath(); foundDB != "" {
			dbPath = foundDB // Store it for later use
			beadsDir = filepath.Dir(foundDB)
		} else {
			return "", fmt.Errorf("no database path configured (run 'bd init' or set BEADS_DB)")
		}
	}

	if err := os.MkdirAll(beadsDir, 0700); err != nil {
		return "", fmt.Errorf("cannot create beads directory: %w", err)
	}

	return beadsDir, nil
}

// boolToFlag returns the flag string if condition is true, otherwise returns empty string
func boolToFlag(condition bool, flag string) string {
	if condition {
		return flag
	}
	return ""
}

// getEnvInt reads an integer from environment variable with a default value
func getEnvInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// getEnvBool reads a boolean from environment variable with a default value
func getEnvBool(key string, defaultValue bool) bool {
	if val := os.Getenv(key); val != "" {
		return val == "true" || val == "1"
	}
	return defaultValue
}

// getSocketPathForPID determines the socket path for a given PID file.
// If BD_SOCKET env var is set, uses that value instead.
// Uses rpc.ShortSocketPath to avoid Unix socket path length limits (macOS: 104 chars).
func getSocketPathForPID(pidFile string) string {
	// Check environment variable first (enables test isolation)
	if socketPath := os.Getenv("BD_SOCKET"); socketPath != "" {
		return socketPath
	}
	// PID file is in .beads/, so workspace is parent of that
	beadsDir := filepath.Dir(pidFile)
	workspacePath := filepath.Dir(beadsDir)
	return rpc.ShortSocketPath(workspacePath)
}

// getPIDFilePath returns the path to the daemon PID file
func getPIDFilePath() (string, error) {
	beadsDir, err := ensureBeadsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(beadsDir, "daemon.pid"), nil
}

// getLogFilePath returns the path to the daemon log file
func getLogFilePath(userPath string) (string, error) {
	if userPath != "" {
		return userPath, nil
	}

	beadsDir, err := ensureBeadsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(beadsDir, "daemon.log"), nil
}
