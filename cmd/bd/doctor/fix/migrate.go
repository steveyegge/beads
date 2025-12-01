package fix

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// DatabaseVersion fixes database version mismatches by running bd migrate,
// or creates the database from JSONL by running bd init for fresh clones (bd-4h9).
func DatabaseVersion(path string) error {
	// Validate workspace
	if err := validateBeadsWorkspace(path); err != nil {
		return err
	}

	// Get bd binary path
	bdBinary, err := getBdBinary()
	if err != nil {
		return err
	}

	// Check if database exists - if not, run init instead of migrate (bd-4h9)
	beadsDir := filepath.Join(path, ".beads")
	dbPath := filepath.Join(beadsDir, "beads.db")

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		// No database - this is a fresh clone, run bd init
		fmt.Println("→ No database found, running 'bd init' to hydrate from JSONL...")
		cmd := exec.Command(bdBinary, "init") // #nosec G204 -- bdBinary from validated executable path
		cmd.Dir = path
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to initialize database: %w", err)
		}
		return nil
	}

	// Database exists - run bd migrate
	cmd := exec.Command(bdBinary, "migrate") // #nosec G204 -- bdBinary from validated executable path
	cmd.Dir = path                           // Set working directory without changing process dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	return nil
}

// SchemaCompatibility fixes schema compatibility issues by running bd migrate
func SchemaCompatibility(path string) error {
	return DatabaseVersion(path)
}
