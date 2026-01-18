package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/ui"
)

var migrateVarCmd = &cobra.Command{
	Use:   "layout",
	Short: "Migrate to var/ layout for volatile files",
	Long: `Migrate .beads/ to use var/ subdirectory for volatile files.

This organizes machine-local files (database, daemon, sync state) into
.beads/var/, separating them from git-tracked files.

The migration copies files to var/ and removes originals. Use --dry-run
to preview changes first. If stray files appear later at root, use
'bd doctor --fix' to move them.

After migration, .gitignore only needs a single 'var/' pattern.`,
	RunE: runMigrateVar,
}

func init() {
	migrateCmd.AddCommand(migrateVarCmd)
	migrateVarCmd.Flags().Bool("dry-run", false, "Preview changes without modifying files")
}

func runMigrateVar(cmd *cobra.Command, _ []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Block writes in readonly mode (migration modifies data)
	if !dryRun {
		CheckReadonly("migrate layout")
	}

	// Find .beads directory
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "no_beads_directory",
				"message": "No .beads directory found. Run 'bd init' first.",
			})
			os.Exit(1)
		}
		FatalErrorWithHint("no .beads directory found", "run 'bd init' to initialize bd")
	}

	// Follow redirect if present (worktree support)
	beadsDir = beads.FollowRedirect(beadsDir)

	// Load config
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "config_load_failed",
				"message": err.Error(),
			})
			os.Exit(1)
		}
		return fmt.Errorf("failed to load config: %w", err)
	}
	if cfg == nil {
		cfg = configfile.DefaultConfig()
	}

	// Check if already using var/ layout
	if cfg.Layout == configfile.LayoutV2 {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"status":  "already_migrated",
				"message": "Already using var/ layout",
			})
		} else {
			fmt.Println(ui.RenderPass("Already using var/ layout"))
		}
		return nil
	}

	// Also check for var/ directory presence (migration might have been interrupted)
	varDir := filepath.Join(beadsDir, "var")
	if info, err := os.Stat(varDir); err == nil && info.IsDir() && cfg.Layout != configfile.LayoutV2 {
		// var/ exists but layout not set - complete the migration
		if !jsonOutput && !dryRun {
			fmt.Println("Found var/ directory but layout not marked as v2")
			fmt.Println("Completing interrupted migration...")
		}
	}

	// Check if daemon is running - auto-stop it for migration
	running, pid := tryDaemonLock(beadsDir)
	if running {
		if !jsonOutput {
			fmt.Printf("Stopping daemon (pid %d)...\n", pid)
		}
		// Stop the daemon gracefully using VarPath to find pid file
		pidFile := beads.VarPath(beadsDir, "daemon.pid", "")
		stopDaemonQuiet(pidFile)
		// Give it a moment to fully release resources
		time.Sleep(100 * time.Millisecond)
	}

	// Find volatile files that exist at root
	var filesToMove []string
	for _, f := range beads.VolatileFiles {
		rootPath := filepath.Join(beadsDir, f)
		if _, err := os.Stat(rootPath); err == nil {
			filesToMove = append(filesToMove, f)
		}
	}

	// Also check for SQLite sibling files that match glob patterns
	entries, _ := os.ReadDir(beadsDir)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if matched, _ := filepath.Match("*.db-*", name); matched {
			// Check if already in the list
			found := false
			for _, f := range filesToMove {
				if f == name {
					found = true
					break
				}
			}
			if !found {
				filesToMove = append(filesToMove, name)
			}
		}
	}

	// Dry run mode
	if dryRun {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"dry_run":       true,
				"would_create":  "var/",
				"would_move":    filesToMove,
				"would_set":     "layout: v2",
				"files_to_move": len(filesToMove),
			})
		} else {
			fmt.Println("Dry run mode - no changes will be made")
			fmt.Println()
			fmt.Printf("Would create: %s/var/\n", beadsDir)
			if len(filesToMove) > 0 {
				fmt.Printf("Would move %d file(s):\n", len(filesToMove))
				for _, f := range filesToMove {
					fmt.Printf("  - %s\n", f)
				}
			} else {
				fmt.Println("No files to move")
			}
			fmt.Println("Would update metadata.json with layout: \"v2\"")
		}
		return nil
	}

	// Create var/ directory
	if err := os.MkdirAll(varDir, 0700); err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "create_var_failed",
				"message": err.Error(),
			})
			os.Exit(1)
		}
		return fmt.Errorf("failed to create var/ directory: %w", err)
	}

	if !jsonOutput {
		fmt.Printf("Created %s/var/\n", beadsDir)
	}

	// Move files
	movedCount := 0
	var moveErrors []string

	for _, f := range filesToMove {
		rootPath := filepath.Join(beadsDir, f)
		varPath := filepath.Join(varDir, f)

		// Check if destination already exists
		if _, err := os.Stat(varPath); err == nil {
			// Destination exists - remove source (it's a duplicate)
			if err := os.Remove(rootPath); err != nil {
				moveErrors = append(moveErrors, fmt.Sprintf("%s: failed to remove duplicate: %v", f, err))
			} else {
				if !jsonOutput {
					fmt.Printf("  Removed duplicate: %s (keeping var/ copy)\n", f)
				}
				movedCount++
			}
			continue
		}

		// Move file using rename (same filesystem, should be atomic)
		if err := os.Rename(rootPath, varPath); err != nil {
			// Try copy+delete for cross-filesystem moves
			if err := copyFileForMigration(rootPath, varPath); err != nil {
				moveErrors = append(moveErrors, fmt.Sprintf("%s: %v", f, err))
				continue
			}
			if err := os.Remove(rootPath); err != nil {
				// Copy succeeded but delete failed - warn but continue
				if !jsonOutput {
					fmt.Printf("  Warning: copied %s but couldn't remove original: %v\n", f, err)
				}
			}
		}

		if !jsonOutput {
			fmt.Printf("  Moved: %s -> var/%s\n", f, f)
		}
		movedCount++
	}

	if !jsonOutput && movedCount > 0 {
		fmt.Printf("Moved %d file(s) to var/\n", movedCount)
	}

	// Update metadata.json with layout: "v2"
	cfg.Layout = configfile.LayoutV2
	if err := cfg.Save(beadsDir); err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":       "save_config_failed",
				"message":     err.Error(),
				"files_moved": movedCount,
			})
			os.Exit(1)
		}
		return fmt.Errorf("failed to update metadata.json: %w", err)
	}

	if !jsonOutput {
		fmt.Println("Updated metadata.json with layout: \"v2\"")
	}

	// Report any errors
	if len(moveErrors) > 0 {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"status":      "partial",
				"message":     "Migration completed with errors",
				"files_moved": movedCount,
				"errors":      moveErrors,
			})
		} else {
			fmt.Println()
			fmt.Println(ui.RenderWarn("Warning: some files could not be moved:"))
			for _, e := range moveErrors {
				fmt.Printf("  - %s\n", e)
			}
			fmt.Println()
			fmt.Println("Run 'bd doctor --fix' to retry moving these files")
		}
		return nil
	}

	// Success
	if jsonOutput {
		outputJSON(map[string]interface{}{
			"status":      "success",
			"message":     "Migration complete",
			"files_moved": movedCount,
			"layout":      "v2",
		})
	} else {
		fmt.Println()
		fmt.Println(ui.RenderPass("Migration complete"))
		fmt.Println()
		fmt.Println("Tip: Run 'bd sync' to verify sync works with new layout")
	}

	return nil
}

// copyFileForMigration copies a file from src to dst for migration purposes.
// Used as fallback when os.Rename fails (e.g., cross-filesystem).
func copyFileForMigration(src, dst string) error {
	// Read source file
	data, err := os.ReadFile(src) // #nosec G304 - controlled path from migration
	if err != nil {
		return fmt.Errorf("read failed: %w", err)
	}

	// Get source file info for permissions
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat failed: %w", err)
	}

	// Write to destination with same permissions
	if err := os.WriteFile(dst, data, info.Mode()); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}

	return nil
}
