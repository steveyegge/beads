package main

import (
	"path/filepath"

	"github.com/steveyegge/beads/internal/beads"
)

// getBeadsDir returns the active .beads directory.
//
// Do not derive this from dbPath alone: when BEADS_DOLT_DATA_DIR points at a
// shared/custom Dolt data directory, dbPath points outside .beads and
// filepath.Dir(dbPath) becomes the workspace root instead of the .beads
// directory.
func getBeadsDir() string {
	if beadsDir := beads.FindBeadsDir(); beadsDir != "" {
		return beadsDir
	}
	if dbPath != "" {
		return filepath.Dir(dbPath)
	}
	return ""
}
