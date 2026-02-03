package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// isFreshCloneError checks if the error is due to a fresh clone scenario
// where the database exists but is missing required config (like issue_prefix).
// This happens when someone clones a repo with beads but needs to initialize.
func isFreshCloneError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Check for the specific migration invariant error pattern
	return strings.Contains(errStr, "post-migration validation failed") &&
		strings.Contains(errStr, "required config key missing: issue_prefix")
}

// handleFreshCloneError displays a helpful message when a fresh clone is detected
// and returns true if the error was handled (so caller should exit).
// If not a fresh clone error, returns false and does nothing.
func handleFreshCloneError(err error, beadsDir string) bool {
	if !isFreshCloneError(err) {
		return false
	}

	// Look for JSONL file in the .beads directory
	jsonlPath := ""
	issueCount := 0

	if beadsDir != "" {
		// Check for issues.jsonl (canonical) first, then beads.jsonl (legacy)
		for _, name := range []string{"issues.jsonl", "beads.jsonl"} {
			candidate := filepath.Join(beadsDir, name)
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				jsonlPath = candidate
				// Count lines (approximately = issue count)
				// #nosec G304 -- candidate is constructed from beadsDir which is .beads/
				if data, readErr := os.ReadFile(candidate); readErr == nil {
					for _, line := range strings.Split(string(data), "\n") {
						if strings.TrimSpace(line) != "" {
							issueCount++
						}
					}
				}
				break
			}
		}
	}

	fmt.Fprintf(os.Stderr, "Error: Database not initialized\n\n")
	fmt.Fprintf(os.Stderr, "This appears to be a fresh clone or the database needs initialization.\n")

	if jsonlPath != "" && issueCount > 0 {
		fmt.Fprintf(os.Stderr, "Found: %s (%d issues)\n\n", jsonlPath, issueCount)
		fmt.Fprintf(os.Stderr, "To initialize from the JSONL file, run:\n")
		fmt.Fprintf(os.Stderr, "  bd import -i %s\n\n", jsonlPath)
	} else {
		fmt.Fprintf(os.Stderr, "\nTo initialize a new database, run:\n")
		fmt.Fprintf(os.Stderr, "  bd init --prefix <your-prefix>\n\n")
	}

	fmt.Fprintf(os.Stderr, "For more information: bd init --help\n")
	return true
}

