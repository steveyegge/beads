package fix

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
)

// StrayVolatileFiles moves volatile files from root to var/ when var/ layout is active.
// This fixes the case where external tools or interrupted migrations left files at root.
// Returns the number of files moved and any error encountered.
func StrayVolatileFiles(path string) (int, error) {
	if err := validateBeadsWorkspace(path); err != nil {
		return 0, err
	}

	beadsDir := resolveBeadsDir(filepath.Join(path, ".beads"))

	// Get layout from config
	layout := getLayoutFromConfig(beadsDir)

	// Only act if var/ layout is active
	if !beads.IsVarLayout(beadsDir, layout) {
		fmt.Println("  Not using var/ layout - no files to move")
		return 0, nil
	}

	// Ensure var/ directory exists
	varDir := filepath.Join(beadsDir, "var")
	if err := os.MkdirAll(varDir, 0700); err != nil {
		return 0, fmt.Errorf("failed to create var/ directory: %w", err)
	}

	var moved int
	var errors []string

	for _, f := range beads.VolatileFiles {
		rootPath := filepath.Join(beadsDir, f)
		varPath := filepath.Join(varDir, f)

		// Check if file exists at root
		if _, err := os.Stat(rootPath); os.IsNotExist(err) {
			continue
		}

		// Check if file already exists in var/ (shouldn't happen often, but handle it)
		if _, err := os.Stat(varPath); err == nil {
			// File exists in both locations - remove root copy
			if err := os.Remove(rootPath); err != nil {
				errors = append(errors, fmt.Sprintf("%s: failed to remove duplicate: %v", f, err))
			} else {
				fmt.Printf("  Removed duplicate: %s (keeping var/ copy)\n", f)
				moved++
			}
			continue
		}

		// Move file from root to var/
		if err := moveFile(rootPath, varPath); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", f, err))
		} else {
			fmt.Printf("  Moved: %s -> var/%s\n", f, f)
			moved++
		}
	}

	if len(errors) > 0 {
		return moved, fmt.Errorf("some files could not be moved: %v", errors)
	}

	if moved == 0 {
		fmt.Println("  No stray files to move")
	} else {
		fmt.Printf("  Moved %d file(s) to var/\n", moved)
	}

	return moved, nil
}

// getLayoutFromConfig reads the layout field from metadata.json.
// Returns empty string if metadata cannot be read.
func getLayoutFromConfig(beadsDir string) string {
	cfg, err := configfile.Load(beadsDir)
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.Layout
}
