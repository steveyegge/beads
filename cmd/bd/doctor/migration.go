package doctor

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/git"
	"github.com/steveyegge/beads/internal/syncbranch"
)

// PendingMigration represents a single pending migration
type PendingMigration struct {
	Name        string // e.g., "hash-ids", "tombstones", "sync"
	Description string // e.g., "Convert sequential IDs to hash-based IDs"
	Command     string // e.g., "bd migrate hash-ids"
	Priority    int    // 1 = critical, 2 = recommended, 3 = optional
}

// DetectPendingMigrations detects all pending migrations for a beads directory
func DetectPendingMigrations(path string) []PendingMigration {
	var pending []PendingMigration

	// Follow redirect to resolve actual beads directory
	beadsDir := resolveBeadsDir(filepath.Join(path, ".beads"))

	// Skip if .beads doesn't exist
	if _, err := os.Stat(beadsDir); os.IsNotExist(err) {
		return pending
	}

	// Check for sequential IDs (hash-ids migration)
	if needsHashIDsMigration(beadsDir) {
		pending = append(pending, PendingMigration{
			Name:        "hash-ids",
			Description: "Convert sequential IDs to hash-based IDs",
			Command:     "bd migrate hash-ids",
			Priority:    2,
		})
	}

	// Check for legacy deletions.jsonl (tombstones migration)
	if needsTombstonesMigration(beadsDir) {
		pending = append(pending, PendingMigration{
			Name:        "tombstones",
			Description: "Convert deletions.jsonl to inline tombstones",
			Command:     "bd migrate tombstones",
			Priority:    2,
		})
	}

	// Check for missing sync-branch config (sync migration)
	if needsSyncMigration(path) {
		pending = append(pending, PendingMigration{
			Name:        "sync",
			Description: "Configure sync branch for multi-clone setup",
			Command:     "bd migrate sync beads-sync",
			Priority:    3,
		})
	}

	// Check for database version mismatch (main migrate command)
	if versionMismatch := checkDatabaseVersionMismatch(beadsDir); versionMismatch != "" {
		pending = append(pending, PendingMigration{
			Name:        "database",
			Description: versionMismatch,
			Command:     "bd migrate",
			Priority:    1,
		})
	}

	// Check for stray files in wrong location (var/ layout active but files at root)
	if files := FilesInWrongLocation(beadsDir); len(files) > 0 {
		pending = append(pending, PendingMigration{
			Name:        "stray-files",
			Description: fmt.Sprintf("%d volatile file(s) at root should be in var/", len(files)),
			Command:     "bd doctor --fix",
			Priority:    2, // Warning - fixable
		})
	}

	// Check for optional var/ layout migration (legacy layout with volatile files)
	if needsVarMigration(beadsDir) {
		pending = append(pending, PendingMigration{
			Name:        "var-layout",
			Description: "Recommended: migrate to var/ layout (legacy layout is deprecated)",
			Command:     "bd migrate layout",
			Priority:    2, // Warning - deprecated layout
		})
	}

	return pending
}

// CheckPendingMigrations returns a doctor check summarizing all pending migrations
func CheckPendingMigrations(path string) DoctorCheck {
	pending := DetectPendingMigrations(path)

	if len(pending) == 0 {
		return DoctorCheck{
			Name:     "Pending Migrations",
			Status:   StatusOK,
			Message:  "None required",
			Category: CategoryMaintenance,
		}
	}

	// Build message with count
	message := fmt.Sprintf("%d available", len(pending))

	// Build detail with list of migrations
	var details []string
	var fixes []string
	for _, m := range pending {
		priority := ""
		switch m.Priority {
		case 1:
			priority = " [critical]"
		case 2:
			priority = " [recommended]"
		case 3:
			priority = " [optional]"
		}
		details = append(details, fmt.Sprintf("• %s: %s%s", m.Name, m.Description, priority))
		fixes = append(fixes, m.Command)
	}

	// Determine status based on highest priority migration
	status := StatusOK
	for _, m := range pending {
		if m.Priority == 1 {
			status = StatusError
			break
		} else if m.Priority == 2 && status != StatusError {
			status = StatusWarning
		}
	}

	return DoctorCheck{
		Name:     "Pending Migrations",
		Status:   status,
		Message:  message,
		Detail:   strings.Join(details, "\n"),
		Fix:      strings.Join(fixes, "\n"),
		Category: CategoryMaintenance,
	}
}

// needsHashIDsMigration checks if the database uses sequential IDs
func needsHashIDsMigration(beadsDir string) bool {
	var dbPath string
	if cfg, err := configfile.Load(beadsDir); err == nil && cfg != nil && cfg.Database != "" {
		dbPath = cfg.DatabasePath(beadsDir)
	} else {
		dbPath = filepath.Join(beadsDir, beads.CanonicalDatabaseName)
	}

	// Skip if no database
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return false
	}

	db, err := sql.Open("sqlite3", sqliteConnString(dbPath, true))
	if err != nil {
		return false
	}
	defer db.Close()

	// Get sample of issues
	rows, err := db.Query("SELECT id FROM issues ORDER BY created_at LIMIT 10")
	if err != nil {
		return false
	}
	defer rows.Close()

	var issueIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			issueIDs = append(issueIDs, id)
		}
	}

	if len(issueIDs) == 0 {
		return false
	}

	// Returns true if NOT using hash-based IDs (i.e., using sequential)
	return !DetectHashBasedIDs(db, issueIDs)
}

// needsTombstonesMigration checks if deletions.jsonl exists with entries
func needsTombstonesMigration(beadsDir string) bool {
	deletionsPath := filepath.Join(beadsDir, "deletions.jsonl")

	info, err := os.Stat(deletionsPath)
	if err != nil {
		return false // File doesn't exist
	}

	if info.Size() == 0 {
		return false // Empty file
	}

	// Count non-empty lines
	file, err := os.Open(deletionsPath) //nolint:gosec
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			return true // Has at least one entry
		}
	}

	return false
}

// needsSyncMigration checks if sync-branch should be configured
func needsSyncMigration(repoPath string) bool {
	// Check if already configured
	if syncbranch.GetFromYAML() != "" {
		return false
	}

	// Check if we're in a git repository
	_, err := git.GetGitDir()
	if err != nil {
		return false
	}

	// Check if has remote (multi-clone indicator)
	return hasGitRemote(repoPath)
}

// hasGitRemote checks if the repository has a git remote
func hasGitRemote(repoPath string) bool {
	cmd := exec.Command("git", "remote")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(output))) > 0
}

// needsVarMigration returns true if using legacy layout and volatile files exist at root.
// This indicates the user could benefit from migrating to var/ layout.
func needsVarMigration(beadsDir string) bool {
	// Get layout from metadata
	layout := getLayoutFromMetadata(beadsDir)

	// If already using var/ layout, no migration needed
	if beads.IsVarLayout(beadsDir, layout) {
		return false
	}

	// Check if any volatile files exist at root (legacy location)
	for _, f := range beads.VolatileFiles {
		rootPath := filepath.Join(beadsDir, f)
		if _, err := os.Stat(rootPath); err == nil {
			return true
		}
	}
	return false
}

// FilesInWrongLocation returns volatile files at root when var/ layout is active.
// These are files that should be in var/ but are in the root directory.
func FilesInWrongLocation(beadsDir string) []string {
	// Get layout from metadata
	layout := getLayoutFromMetadata(beadsDir)

	// Not using var/ layout - no "wrong" location
	if !beads.IsVarLayout(beadsDir, layout) {
		return nil
	}

	var wrongLocation []string
	for _, f := range beads.VolatileFiles {
		rootPath := filepath.Join(beadsDir, f)
		if _, err := os.Stat(rootPath); err == nil {
			wrongLocation = append(wrongLocation, f)
		}
	}
	return wrongLocation
}

// getLayoutFromMetadata reads the layout field from metadata.json.
// Returns empty string if metadata cannot be read.
func getLayoutFromMetadata(beadsDir string) string {
	cfg, err := configfile.Load(beadsDir)
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.Layout
}

// checkDatabaseVersionMismatch returns a description if database version is old
func checkDatabaseVersionMismatch(beadsDir string) string {
	var dbPath string
	if cfg, err := configfile.Load(beadsDir); err == nil && cfg != nil && cfg.Database != "" {
		dbPath = cfg.DatabasePath(beadsDir)
	} else {
		dbPath = filepath.Join(beadsDir, beads.CanonicalDatabaseName)
	}

	// Skip if no database
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return ""
	}

	db, err := sql.Open("sqlite3", sqliteConnString(dbPath, true))
	if err != nil {
		return ""
	}
	defer db.Close()

	// Get stored version
	var storedVersion string
	err = db.QueryRow("SELECT value FROM metadata WHERE key = 'bd_version'").Scan(&storedVersion)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return "Database schema needs update (pre-metadata table)"
		}
		// No version stored
		return ""
	}

	// Note: We can't compare to current version here since we don't have access
	// to the Version variable from main package. The individual check does this.
	// This function is just for detecting obviously old databases.
	if storedVersion == "" || storedVersion == "unknown" {
		return "Database version unknown"
	}

	return ""
}
