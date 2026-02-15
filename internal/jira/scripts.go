package jira

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/beads"
)

// FindScript locates a Jira Python script by name.
func FindScript(name string) (string, error) {
	// Check environment variable first (allows users to specify script location)
	if envPath := os.Getenv("BD_JIRA_SCRIPT"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
		return "", fmt.Errorf("BD_JIRA_SCRIPT points to non-existent file: %s", envPath)
	}

	// Check common locations
	locations := []string{
		// Relative to current working directory
		filepath.Join("examples", "jira-import", name),
	}

	// Add executable-relative path
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		locations = append(locations, filepath.Join(exeDir, "examples", "jira-import", name))
		locations = append(locations, filepath.Join(exeDir, "..", "examples", "jira-import", name))
	}

	// Check BEADS_DIR or current .beads location
	if beadsDir := beads.FindBeadsDir(); beadsDir != "" {
		repoRoot := filepath.Dir(beadsDir)
		locations = append(locations, filepath.Join(repoRoot, "examples", "jira-import", name))
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			absPath, err := filepath.Abs(loc)
			if err == nil {
				return absPath, nil
			}
			return loc, nil
		}
	}

	return "", fmt.Errorf(`script not found: %s

The Jira sync feature requires the Python script from the beads repository.

To fix this, either:
  1. Set BD_JIRA_SCRIPT to point to the script:
     export BD_JIRA_SCRIPT=/path/to/jira2jsonl.py

  2. Or download it from GitHub:
     curl -o jira2jsonl.py https://raw.githubusercontent.com/steveyegge/beads/main/examples/jira-import/jira2jsonl.py
     export BD_JIRA_SCRIPT=$PWD/jira2jsonl.py

Looked in: %v`, name, locations)
}
