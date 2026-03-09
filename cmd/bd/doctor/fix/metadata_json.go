package fix

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
)

// FixMissingMetadataJSON detects and regenerates a missing metadata.json file.
// This is the most common failure scenario: the file is deleted but .beads/ exists.
// Regenerates with default config values (similar to bd init). (GH#2478)
func FixMissingMetadataJSON(path string) error {
	beadsDir := filepath.Join(path, ".beads")
	// Follow redirects (symlinked .beads dirs)
	beadsDir = beads.FollowRedirect(beadsDir)

	// Check if .beads directory exists
	if _, err := os.Stat(beadsDir); os.IsNotExist(err) {
		return fmt.Errorf("not a beads workspace: .beads directory not found at %s", path)
	}

	configPath := configfile.ConfigPath(beadsDir)

	// Only fix if metadata.json is actually missing
	if _, err := os.Stat(configPath); err == nil {
		return nil // File exists, nothing to fix
	}

	// Generate default config
	cfg := configfile.DefaultConfig()
	cfg.Backend = configfile.BackendDolt
	cfg.Database = "dolt"
	cfg.DoltMode = configfile.DoltModeServer

	if err := cfg.Save(beadsDir); err != nil {
		return fmt.Errorf("failed to regenerate metadata.json: %w", err)
	}

	fmt.Printf("  Regenerated metadata.json with default values\n")
	return nil
}
