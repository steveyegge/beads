package fix

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/configfile"
)

// DatabaseConfig auto-detects and fixes metadata.json database config mismatches.
func DatabaseConfig(path string) error {
	if err := validateBeadsWorkspace(path); err != nil {
		return err
	}

	beadsDir := filepath.Join(path, ".beads")

	// Load existing config
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if cfg == nil {
		// No config exists - nothing to fix
		return fmt.Errorf("no metadata.json found")
	}

	fixed := false

	// Check if configured database name matches the actual DB file on disk
	actualDB := findActualDBFile(beadsDir)
	if actualDB != "" && cfg.Database != actualDB {
		fmt.Printf("  Updating database: %s → %s\n", cfg.Database, actualDB)
		cfg.Database = actualDB
		fixed = true
	}

	if !fixed {
		return fmt.Errorf("no configuration mismatches detected")
	}

	// Save updated config
	if err := cfg.Save(beadsDir); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// findActualDBFile scans .beads/ for the actual database file in use.
// Prefers beads.db (canonical name), skips backups and vc.db.
func findActualDBFile(beadsDir string) string {
	entries, err := os.ReadDir(beadsDir)
	if err != nil {
		return ""
	}

	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Must end with .db
		if !strings.HasSuffix(name, ".db") {
			continue
		}

		// Skip backups and vc.db
		if strings.Contains(name, "backup") || name == "vc.db" {
			continue
		}

		candidates = append(candidates, name)
	}

	if len(candidates) == 0 {
		return ""
	}

	// Prefer beads.db (canonical name)
	for _, name := range candidates {
		if name == "beads.db" {
			return name
		}
	}

	// Fall back to first candidate
	return candidates[0]
}
