package fix

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

// DatabaseVersion fixes database version mismatches by updating metadata in-process.
// For fresh clones (no database), it creates a new Dolt store which auto-bootstraps
// from JSONL. For existing databases, it updates version metadata directly.
//
// This runs in-process to avoid Dolt lock contention that occurs when spawning
// bd subcommands while the parent process holds database connections. (GH#1805)
func DatabaseVersion(path string) error {
	return DatabaseVersionWithBdVersion(path, "")
}

// DatabaseVersionWithBdVersion is like DatabaseVersion but accepts an explicit
// bd version string for setting the bd_version metadata field.
func DatabaseVersionWithBdVersion(path string, bdVersion string) error {
	// Validate workspace
	if err := validateBeadsWorkspace(path); err != nil {
		return err
	}

	beadsDir := resolveBeadsDir(filepath.Join(path, ".beads"))

	// Load or create config
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		cfg = configfile.DefaultConfig()
	}
	if cfg == nil {
		cfg = configfile.DefaultConfig()
	}

	// Determine database path
	dbPath := cfg.DatabasePath(beadsDir)

	ctx := context.Background()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		// No database - create a new Dolt store (auto-bootstraps from JSONL)
		fmt.Println("  → No database found, creating Dolt store (will bootstrap from JSONL)...")

		store, err := dolt.NewFromConfig(ctx, beadsDir)
		if err != nil {
			return fmt.Errorf("failed to create database: %w", err)
		}
		defer func() { _ = store.Close() }()

		// Set version metadata if provided
		if bdVersion != "" {
			if err := store.SetMetadata(ctx, "bd_version", bdVersion); err != nil {
				fmt.Printf("  Warning: failed to set bd_version: %v\n", err)
			}
		}

		fmt.Println("  → Database created successfully")
		return nil
	}

	// Database exists - update metadata in-process
	fmt.Println("  → Updating database metadata...")

	store, err := dolt.NewFromConfig(ctx, beadsDir)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() { _ = store.Close() }()

	// Update bd_version if provided
	if bdVersion != "" {
		if err := store.SetMetadata(ctx, "bd_version", bdVersion); err != nil {
			return fmt.Errorf("failed to set bd_version: %w", err)
		}
	}

	// Detect and set issue_prefix if missing
	prefix, err := store.GetConfig(ctx, "issue_prefix")
	if err != nil || prefix == "" {
		issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
		if err == nil && len(issues) > 0 {
			detectedPrefix := utils.ExtractIssuePrefix(issues[0].ID)
			if detectedPrefix != "" {
				if err := store.SetConfig(ctx, "issue_prefix", detectedPrefix); err != nil {
					fmt.Printf("  Warning: failed to set issue prefix: %v\n", err)
				} else {
					fmt.Printf("  → Detected and set issue prefix: %s\n", detectedPrefix)
				}
			}
		}
	}

	fmt.Println("  → Metadata updated")
	return nil
}

// findJSONLPath returns the path to the JSONL file in the beads directory.
// Delegates to utils.FindJSONLInDir for path discovery but returns empty
// string if no JSONL file actually exists on disk.
func findJSONLPath(beadsDir string) string {
	path := utils.FindJSONLInDir(beadsDir)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

// SchemaCompatibility fixes schema compatibility issues by updating database metadata
func SchemaCompatibility(path string) error {
	return DatabaseVersion(path)
}
