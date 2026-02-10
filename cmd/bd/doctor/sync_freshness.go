package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/config"
)

// CheckSyncFreshness checks for signs of stale or problematic sync state
// using only git and file-based signals (no SQLite access).
func CheckSyncFreshness(path string) DoctorCheck {
	// Skip for dolt-native mode (JSONL checks don't apply)
	if config.GetSyncMode() == config.SyncModeDoltNative {
		return DoctorCheck{
			Name:    "Sync Freshness",
			Status:  StatusOK,
			Message: "N/A (dolt-native mode)",
		}
	}

	beadsDir := filepath.Join(path, ".beads")

	var warnings []string

	if w := checkJSONLUncommitted(path, beadsDir); w != "" {
		warnings = append(warnings, w)
	}
	if w := checkSyncConflicts(beadsDir); w != "" {
		warnings = append(warnings, w)
	}

	if len(warnings) == 0 {
		return DoctorCheck{
			Name:    "Sync Freshness",
			Status:  StatusOK,
			Message: "No sync freshness issues detected",
		}
	}

	return DoctorCheck{
		Name:    "Sync Freshness",
		Status:  StatusWarning,
		Message: fmt.Sprintf("Sync freshness: %d issue(s) detected", len(warnings)),
		Detail:  strings.Join(warnings, "; "),
		Fix:     "Run 'bd sync --status' to check full sync state",
	}
}

// checkJSONLUncommitted checks if the JSONL file has uncommitted changes in git.
func checkJSONLUncommitted(repoPath, beadsDir string) string {
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	if _, err := os.Stat(jsonlPath); os.IsNotExist(err) {
		return "" // no JSONL file, nothing to check
	}

	// Use relative path for git status
	relPath, err := filepath.Rel(repoPath, jsonlPath)
	if err != nil {
		return "" // fail-safe
	}

	cmd := exec.Command("git", "status", "--porcelain", "--", relPath) // #nosec G204 -- relPath derived from validated paths
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return "" // fail-safe (not in a git repo, etc.)
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed != "" {
		return "JSONL file has uncommitted changes"
	}
	return ""
}

// checkSyncConflicts checks if sync_conflicts.json exists and has entries.
func checkSyncConflicts(beadsDir string) string {
	conflictPath := filepath.Join(beadsDir, "sync_conflicts.json")
	data, err := os.ReadFile(conflictPath) // #nosec G304 -- path constructed from known beadsDir
	if err != nil {
		return "" // no conflict file
	}

	// Try to parse as array or object with conflicts
	var conflicts []any
	if err := json.Unmarshal(data, &conflicts); err == nil {
		if len(conflicts) > 0 {
			return fmt.Sprintf("%d unresolved sync conflict(s)", len(conflicts))
		}
		return ""
	}

	// Try as object with "conflicts" key
	var wrapper struct {
		Conflicts []any `json:"conflicts"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil {
		if len(wrapper.Conflicts) > 0 {
			return fmt.Sprintf("%d unresolved sync conflict(s)", len(wrapper.Conflicts))
		}
	}

	return ""
}
