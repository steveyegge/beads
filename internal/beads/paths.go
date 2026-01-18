// Package beads provides core beads functionality including volatile file path resolution.
package beads

import (
	"os"
	"path/filepath"
)

// VolatileFiles lists all files that should live in var/
var VolatileFiles = []string{
	"beads.db", "beads.db-journal", "beads.db-wal", "beads.db-shm",
	"daemon.lock", "daemon.log", "daemon.pid", "bd.sock",
	"sync_base.jsonl", ".sync.lock", "sync-state.json",
	"beads.base.jsonl", "beads.base.meta.json",
	"beads.left.jsonl", "beads.left.meta.json",
	"beads.right.jsonl", "beads.right.meta.json",
	"last-touched", ".local_version", "export_hashes.db",
}

// LayoutVersion constants
const (
	LayoutV1 = "v1" // Legacy flat layout (or empty string)
	LayoutV2 = "v2" // var/ layout
)

// VarPath returns the path for a volatile file, using read-both pattern.
// For READS: checks var/ first, falls back to root (handles edge cases).
// For NEW files: uses layout preference (var/ if var/ layout active, else root).
//
// The layout parameter is optional - if nil, falls back to var/ directory check.
func VarPath(beadsDir, filename string, layout string) string {
	// Environment override for emergency fallback
	if os.Getenv("BD_LEGACY_LAYOUT") == "1" {
		return filepath.Join(beadsDir, filename)
	}

	varPath := filepath.Join(beadsDir, "var", filename)
	rootPath := filepath.Join(beadsDir, filename)

	// Read-both: check var/ first, then root (handles migration edge cases)
	if _, err := os.Stat(varPath); err == nil {
		return varPath
	}
	if _, err := os.Stat(rootPath); err == nil {
		return rootPath
	}

	// New file: use layout preference
	if IsVarLayout(beadsDir, layout) {
		return varPath
	}
	return rootPath
}

// VarPathForWrite returns the path for writing a volatile file.
// Always respects layout preference (no fallback checking).
func VarPathForWrite(beadsDir, filename string, layout string) string {
	if os.Getenv("BD_LEGACY_LAYOUT") == "1" {
		return filepath.Join(beadsDir, filename)
	}
	if IsVarLayout(beadsDir, layout) {
		return filepath.Join(beadsDir, "var", filename)
	}
	return filepath.Join(beadsDir, filename)
}

// VarDir returns the directory for volatile files.
// Returns var/ if var/ layout is active, otherwise beadsDir root.
func VarDir(beadsDir string, layout string) string {
	if IsVarLayout(beadsDir, layout) {
		return filepath.Join(beadsDir, "var")
	}
	return beadsDir
}

// IsVarLayout checks if .beads uses the var/ layout.
// layout parameter should be the value from metadata.json Layout field.
// For bootstrap scenarios (before metadata exists), falls back to checking var/ directory.
func IsVarLayout(beadsDir string, layout string) bool {
	if os.Getenv("BD_LEGACY_LAYOUT") == "1" {
		return false
	}

	// Primary: check layout field from metadata
	if layout == LayoutV2 {
		return true
	}
	if layout == LayoutV1 || layout != "" {
		// Explicitly set to v1 or some other known value
		return false
	}

	// Fallback: check var/ directory (bootstrap/migration scenarios)
	// This handles cases where layout field is empty but var/ exists
	varDir := filepath.Join(beadsDir, "var")
	info, err := os.Stat(varDir)
	return err == nil && info.IsDir()
}

// EnsureVarDir creates the var/ directory if it doesn't exist.
func EnsureVarDir(beadsDir string) error {
	varDir := filepath.Join(beadsDir, "var")
	return os.MkdirAll(varDir, 0700)
}

// IsVolatileFile checks if a filename is a volatile file.
func IsVolatileFile(filename string) bool {
	for _, vf := range VolatileFiles {
		if filename == vf {
			return true
		}
	}
	// Also check glob patterns for SQLite sibling files
	if matched, _ := filepath.Match("*.db-*", filename); matched {
		return true
	}
	return false
}
