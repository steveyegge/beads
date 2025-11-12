package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CheckLegacyBeadsSlashCommands detects old /beads:* slash commands in documentation
// and recommends migration to bd prime hooks for better token efficiency.
//
// Old pattern: /beads:quickstart, /beads:ready (~10.5k tokens per session)
// New pattern: bd prime hooks (~50-2k tokens per session)
func CheckLegacyBeadsSlashCommands(repoPath string) DoctorCheck {
	docFiles := []string{
		filepath.Join(repoPath, "AGENTS.md"),
		filepath.Join(repoPath, "CLAUDE.md"),
		filepath.Join(repoPath, ".claude", "CLAUDE.md"),
	}

	var filesWithLegacyCommands []string
	legacyPattern := "/beads:"

	for _, docFile := range docFiles {
		content, err := os.ReadFile(docFile) // #nosec G304 - controlled paths from repoPath
		if err != nil {
			continue // File doesn't exist or can't be read
		}

		if strings.Contains(string(content), legacyPattern) {
			filesWithLegacyCommands = append(filesWithLegacyCommands, filepath.Base(docFile))
		}
	}

	if len(filesWithLegacyCommands) == 0 {
		return DoctorCheck{
			Name:    "Documentation",
			Status:  "ok",
			Message: "No legacy beads slash commands detected",
		}
	}

	return DoctorCheck{
		Name:    "Documentation",
		Status:  "warning",
		Message: fmt.Sprintf("Legacy /beads:* slash commands found in: %s", strings.Join(filesWithLegacyCommands, ", ")),
		Detail:  "Old pattern: /beads:quickstart, /beads:ready (~10.5k tokens)",
		Fix:     "Migration steps:\n" +
			"  1. Run 'bd setup claude' (or 'bd setup cursor') to install bd prime hooks\n" +
			"  2. Remove /beads:* slash commands from AGENTS.md/CLAUDE.md\n" +
			"  3. Add this to AGENTS.md/CLAUDE.md for team members without hooks:\n" +
			"     \"This project uses bd (beads) for issue tracking. Run 'bd prime' at session start for workflow context.\"\n" +
			"  Token savings: ~10.5k → ~50-2k tokens per session",
	}
}

// CheckLegacyJSONLFilename detects if project is using legacy issues.jsonl
// instead of the canonical beads.jsonl filename.
func CheckLegacyJSONLFilename(repoPath string) DoctorCheck {
	beadsDir := filepath.Join(repoPath, ".beads")

	var jsonlFiles []string
	hasIssuesJSON := false

	for _, name := range []string{"issues.jsonl", "beads.jsonl"} {
		jsonlPath := filepath.Join(beadsDir, name)
		if _, err := os.Stat(jsonlPath); err == nil {
			jsonlFiles = append(jsonlFiles, name)
			if name == "issues.jsonl" {
				hasIssuesJSON = true
			}
		}
	}

	if len(jsonlFiles) == 0 {
		return DoctorCheck{
			Name:    "JSONL Files",
			Status:  "ok",
			Message: "No JSONL files found (database-only mode)",
		}
	}

	if len(jsonlFiles) == 1 {
		// Single JSONL file - check if it's the legacy name
		if hasIssuesJSON {
			return DoctorCheck{
				Name:    "JSONL Files",
				Status:  "warning",
				Message: "Using legacy JSONL filename: issues.jsonl",
				Fix:     "Run 'git mv .beads/issues.jsonl .beads/beads.jsonl' to use canonical name (matches beads.db)",
			}
		}
		return DoctorCheck{
			Name:    "JSONL Files",
			Status:  "ok",
			Message: fmt.Sprintf("Using %s", jsonlFiles[0]),
		}
	}

	// Multiple JSONL files found
	return DoctorCheck{
		Name:    "JSONL Files",
		Status:  "warning",
		Message: fmt.Sprintf("Multiple JSONL files found: %s", strings.Join(jsonlFiles, ", ")),
		Fix:     "Run 'git rm .beads/issues.jsonl' to standardize on beads.jsonl (canonical name)",
	}
}
