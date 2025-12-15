package syncbranch

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"

	// Import SQLite driver (same as used by storage/sqlite)
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

const (
	// ConfigKey is the database config key for sync branch
	ConfigKey = "sync.branch"

	// ConfigYAMLKey is the config.yaml key for sync branch
	ConfigYAMLKey = "sync-branch"

	// EnvVar is the environment variable for sync branch
	EnvVar = "BEADS_SYNC_BRANCH"
)

// branchNamePattern validates git branch names
// Based on git-check-ref-format rules
var branchNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]*[a-zA-Z0-9]$`)

// ValidateBranchName checks if a branch name is valid according to git rules
func ValidateBranchName(name string) error {
	if name == "" {
		return nil // Empty is valid (means use current branch)
	}

	// Basic length check
	if len(name) > 255 {
		return fmt.Errorf("branch name too long (max 255 characters)")
	}

	// Check pattern
	if !branchNamePattern.MatchString(name) {
		return fmt.Errorf("invalid branch name: must start and end with alphanumeric, can contain .-_/ in middle")
	}

	// Disallow certain patterns
	if name == "HEAD" || name == "." || name == ".." {
		return fmt.Errorf("invalid branch name: %s is reserved", name)
	}

	// No consecutive dots
	if regexp.MustCompile(`\.\.`).MatchString(name) {
		return fmt.Errorf("invalid branch name: cannot contain '..'")
	}

	// No leading/trailing slashes
	if name[0] == '/' || name[len(name)-1] == '/' {
		return fmt.Errorf("invalid branch name: cannot start or end with '/'")
	}

	return nil
}

// Get retrieves the sync branch configuration with the following precedence:
// 1. BEADS_SYNC_BRANCH environment variable
// 2. sync-branch from config.yaml (version controlled, shared across clones)
// 3. sync.branch from database config (legacy, for backward compatibility)
// 4. Empty string (meaning use current branch)
func Get(ctx context.Context, store storage.Storage) (string, error) {
	// Check environment variable first (highest priority)
	if envBranch := os.Getenv(EnvVar); envBranch != "" {
		if err := ValidateBranchName(envBranch); err != nil {
			return "", fmt.Errorf("invalid %s: %w", EnvVar, err)
		}
		return envBranch, nil
	}

	// Check config.yaml (version controlled, shared across clones)
	// This is the recommended way to configure sync branch for teams
	if yamlBranch := config.GetString(ConfigYAMLKey); yamlBranch != "" {
		if err := ValidateBranchName(yamlBranch); err != nil {
			return "", fmt.Errorf("invalid %s in config.yaml: %w", ConfigYAMLKey, err)
		}
		return yamlBranch, nil
	}

	// Check database config (legacy, for backward compatibility)
	dbBranch, err := store.GetConfig(ctx, ConfigKey)
	if err != nil {
		return "", fmt.Errorf("failed to get %s from config: %w", ConfigKey, err)
	}

	if dbBranch != "" {
		if err := ValidateBranchName(dbBranch); err != nil {
			return "", fmt.Errorf("invalid %s in database: %w", ConfigKey, err)
		}
	}

	return dbBranch, nil
}

// GetFromYAML retrieves sync branch from config.yaml only (no database lookup).
// This is useful for hooks and checks that need to know if sync-branch is configured
// in the version-controlled config without database access.
func GetFromYAML() string {
	// Check environment variable first
	if envBranch := os.Getenv(EnvVar); envBranch != "" {
		return envBranch
	}
	return config.GetString(ConfigYAMLKey)
}

// IsConfigured returns true if sync-branch is configured in config.yaml or env var.
// This is a fast check that doesn't require database access.
func IsConfigured() bool {
	return GetFromYAML() != ""
}

// IsConfiguredWithDB returns true if sync-branch is configured in any source:
// 1. BEADS_SYNC_BRANCH environment variable
// 2. sync-branch in config.yaml
// 3. sync.branch in database config
//
// The dbPath parameter should be the path to the beads.db file.
// If dbPath is empty, it will attempt to find the database in the current directory's .beads folder.
// This function is safe to call even if the database doesn't exist (returns false in that case).
func IsConfiguredWithDB(dbPath string) bool {
	// First check env var and config.yaml (fast path)
	if GetFromYAML() != "" {
		return true
	}

	// Try to read from database
	if dbPath == "" {
		// Try to find database in .beads directory
		dbPath = findBeadsDB()
		if dbPath == "" {
			return false
		}
	}

	// Read sync.branch from database config table
	branch := getConfigFromDB(dbPath, ConfigKey)
	return branch != ""
}

// findBeadsDB attempts to find the beads.db file.
// It first checks if we're in a git worktree and looks in the main repo root.
// Otherwise, it searches up the directory tree from the current directory.
// Returns empty string if not found.
func findBeadsDB() string {
	// First, check if we're in a git worktree and find the main repo root
	mainRepoRoot := getMainRepoRoot()
	if mainRepoRoot != "" {
		dbPath := filepath.Join(mainRepoRoot, ".beads", "beads.db")
		if _, err := os.Stat(dbPath); err == nil {
			return dbPath
		}
	}

	// Fall back to searching up the directory tree from current directory
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	// Search up the directory tree
	for {
		beadsDir := filepath.Join(dir, ".beads")
		dbPath := filepath.Join(beadsDir, "beads.db")
		if _, err := os.Stat(dbPath); err == nil {
			return dbPath
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root
			return ""
		}
		dir = parent
	}
}

// getMainRepoRoot returns the main repository root directory.
// For worktrees, this is the parent of git-common-dir.
// For regular repos, this is the parent of git-dir.
// Returns empty string if not in a git repo.
func getMainRepoRoot() string {
	// Get git-dir and git-common-dir
	gitDirOut, err := exec.Command("git", "rev-parse", "--git-dir").Output()
	if err != nil {
		return ""
	}
	gitDir := strings.TrimSpace(string(gitDirOut))

	commonDirOut, err := exec.Command("git", "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	commonDir := strings.TrimSpace(string(commonDirOut))

	// Make paths absolute
	if !filepath.IsAbs(gitDir) {
		cwd, _ := os.Getwd()
		gitDir = filepath.Join(cwd, gitDir)
	}
	if !filepath.IsAbs(commonDir) {
		cwd, _ := os.Getwd()
		commonDir = filepath.Join(cwd, commonDir)
	}

	// Clean paths for comparison
	gitDir = filepath.Clean(gitDir)
	commonDir = filepath.Clean(commonDir)

	// If git-dir != git-common-dir, we're in a worktree
	// The main repo root is the parent of git-common-dir
	if gitDir != commonDir {
		return filepath.Dir(commonDir)
	}

	// Regular repo - main repo root is parent of git-dir
	return filepath.Dir(gitDir)
}

// getConfigFromDB reads a config value directly from the database file.
// This is a lightweight read that doesn't require the full storage layer.
// Returns empty string if the database doesn't exist or the key is not found.
func getConfigFromDB(dbPath string, key string) string {
	// Check if database exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return ""
	}

	// Open database in read-only mode
	// Use file: prefix as required by ncruces/go-sqlite3 driver
	connStr := fmt.Sprintf("file:%s?mode=ro", dbPath)
	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return ""
	}
	defer db.Close()

	// Query the config table
	var value string
	err = db.QueryRow(`SELECT value FROM config WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return ""
	}

	return value
}

// Set stores the sync branch configuration in the database
func Set(ctx context.Context, store storage.Storage, branch string) error {
	if err := ValidateBranchName(branch); err != nil {
		return err
	}

	return store.SetConfig(ctx, ConfigKey, branch)
}

// Unset removes the sync branch configuration from the database
func Unset(ctx context.Context, store storage.Storage) error {
	return store.DeleteConfig(ctx, ConfigKey)
}
