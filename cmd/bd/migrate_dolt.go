//go:build cgo

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// handleToDolt migrates from SQLite to Dolt backend.
// This implements Part 7 of DOLT-STORAGE-DESIGN.md:
// 1. Creates Dolt database in `.beads/dolt/`
// 2. Imports all issues from SQLite
// 3. Updates `metadata.json` to use Dolt
// 4. Keeps JSONL export enabled by default
// 5. SQLite file can be deleted after verification
func handleToDoltMigration(dryRun bool, autoYes bool) {
	// Find .beads directory
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "no_beads_directory",
				"message": "No .beads directory found. Run 'bd init' first.",
			})
		} else {
			FatalErrorWithHint("no .beads directory found", "run 'bd init' to initialize bd")
		}
		os.Exit(1)
	}

	// Load config
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "config_load_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to load config: %v\n", err)
		}
		os.Exit(1)
	}
	if cfg == nil {
		cfg = configfile.DefaultConfig()
	}

	// Check if already using Dolt
	if cfg.GetBackend() == configfile.BackendDolt {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"status":  "noop",
				"message": "Already using Dolt backend",
			})
		} else {
			fmt.Printf("%s\n", ui.RenderPass("✓ Already using Dolt backend"))
			fmt.Println("No migration needed")
		}
		return
	}

	// Find SQLite database
	sqlitePath := cfg.DatabasePath(beadsDir)
	if _, err := os.Stat(sqlitePath); os.IsNotExist(err) {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "no_sqlite_database",
				"message": "No SQLite database found to migrate",
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: no SQLite database found at %s\n", sqlitePath)
			fmt.Fprintf(os.Stderr, "Hint: run 'bd init' to initialize bd first\n")
		}
		os.Exit(1)
	}

	// Dolt path
	doltPath := filepath.Join(beadsDir, "dolt")

	// Check if Dolt directory already exists
	if _, err := os.Stat(doltPath); err == nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "dolt_exists",
				"message": fmt.Sprintf("Dolt directory already exists at %s", doltPath),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: Dolt directory already exists at %s\n", doltPath)
			fmt.Fprintf(os.Stderr, "Hint: remove it first if you want to re-migrate, or switch backend with 'bd config backend dolt'\n")
		}
		os.Exit(1)
	}

	// Open SQLite to count issues
	ctx := context.Background()
	sqliteStore, err := sqlite.NewReadOnly(ctx, sqlitePath)
	if err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "sqlite_open_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to open SQLite database: %v\n", err)
		}
		os.Exit(1)
	}

	// Get all issues from SQLite
	issues, err := sqliteStore.SearchIssues(ctx, "", types.IssueFilter{IncludeTombstones: true})
	if err != nil {
		_ = sqliteStore.Close()
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "fetch_issues_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to fetch issues: %v\n", err)
		}
		os.Exit(1)
	}

	// Get labels for each issue
	issueIDs := make([]string, len(issues))
	for i, issue := range issues {
		issueIDs[i] = issue.ID
	}
	labelsMap, err := sqliteStore.GetLabelsForIssues(ctx, issueIDs)
	if err != nil {
		_ = sqliteStore.Close()
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "fetch_labels_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to fetch labels: %v\n", err)
		}
		os.Exit(1)
	}

	// Get dependencies for each issue
	allDeps, err := sqliteStore.GetAllDependencyRecords(ctx)
	if err != nil {
		_ = sqliteStore.Close()
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "fetch_deps_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to fetch dependencies: %v\n", err)
		}
		os.Exit(1)
	}

	// Get config values
	prefix, _ := sqliteStore.GetConfig(ctx, "issue_prefix")

	// Close SQLite - we have all the data we need
	_ = sqliteStore.Close()

	// Assign labels and dependencies to issues
	for _, issue := range issues {
		if labels, ok := labelsMap[issue.ID]; ok {
			issue.Labels = labels
		}
		if deps, ok := allDeps[issue.ID]; ok {
			issue.Dependencies = deps
		}
	}

	if !jsonOutput {
		fmt.Printf("SQLite to Dolt Migration\n")
		fmt.Printf("========================\n\n")
		fmt.Printf("Source: %s\n", sqlitePath)
		fmt.Printf("Target: %s\n", doltPath)
		fmt.Printf("Issues to migrate: %d\n", len(issues))
		if prefix != "" {
			fmt.Printf("Issue prefix: %s\n", prefix)
		}
		fmt.Println()
	}

	// Dry run mode
	if dryRun {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"dry_run":      true,
				"source":       sqlitePath,
				"target":       doltPath,
				"issue_count":  len(issues),
				"prefix":       prefix,
				"would_backup": true,
			})
		} else {
			fmt.Println("Dry run mode - no changes will be made")
			fmt.Println("Would perform:")
			fmt.Printf("  1. Create backup of SQLite database\n")
			fmt.Printf("  2. Create Dolt database at %s\n", doltPath)
			fmt.Printf("  3. Import %d issues with labels and dependencies\n", len(issues))
			fmt.Printf("  4. Update metadata.json to use Dolt backend\n")
		}
		return
	}

	// Prompt for confirmation
	if !autoYes && !jsonOutput {
		fmt.Printf("This will:\n")
		fmt.Printf("  1. Create a backup of your SQLite database\n")
		fmt.Printf("  2. Create a Dolt database and import all issues\n")
		fmt.Printf("  3. Update metadata.json to use Dolt backend\n")
		fmt.Printf("  4. Keep your SQLite database (can be deleted after verification)\n\n")
		fmt.Printf("Continue? [y/N] ")
		var response string
		_, _ = fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			fmt.Println("Migration canceled")
			return
		}
		fmt.Println()
	}

	// Create backup
	backupPath := strings.TrimSuffix(sqlitePath, ".db") + ".backup-pre-dolt-" + time.Now().Format("20060102-150405") + ".db"
	if err := copyFile(sqlitePath, backupPath); err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "backup_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to create backup: %v\n", err)
		}
		os.Exit(1)
	}
	if !jsonOutput {
		fmt.Printf("%s\n", ui.RenderPass(fmt.Sprintf("✓ Created backup: %s", filepath.Base(backupPath))))
	}

	// Create Dolt database
	if !jsonOutput {
		fmt.Printf("Creating Dolt database...\n")
	}

	doltStore, err := dolt.New(ctx, &dolt.Config{Path: doltPath})
	if err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "dolt_create_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to create Dolt database: %v\n", err)
		}
		os.Exit(1)
	}

	// Set issue prefix
	if prefix != "" {
		if err := doltStore.SetConfig(ctx, "issue_prefix", prefix); err != nil {
			_ = doltStore.Close()
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"error":   "set_prefix_failed",
					"message": err.Error(),
				})
			} else {
				fmt.Fprintf(os.Stderr, "Error: failed to set issue prefix: %v\n", err)
			}
			os.Exit(1)
		}
	}

	// Import issues
	if !jsonOutput {
		fmt.Printf("Importing %d issues...\n", len(issues))
	}

	imported, skipped, err := importIssuesToDolt(ctx, doltStore, issues)
	if err != nil {
		_ = doltStore.Close()
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "import_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to import issues: %v\n", err)
		}
		os.Exit(1)
	}

	// Commit the migration
	commitMsg := fmt.Sprintf("Migrate from SQLite: %d issues imported", imported)
	if err := doltStore.Commit(ctx, commitMsg); err != nil {
		// Non-fatal - data is still in the database
		if !jsonOutput {
			fmt.Printf("%s\n", ui.RenderWarn(fmt.Sprintf("Warning: failed to create Dolt commit: %v", err)))
		}
	}

	_ = doltStore.Close()

	if !jsonOutput {
		fmt.Printf("%s\n", ui.RenderPass(fmt.Sprintf("✓ Imported %d issues (%d skipped)", imported, skipped)))
	}

	// Update metadata.json
	cfg.Backend = configfile.BackendDolt
	cfg.Database = "dolt" // Use dolt directory
	if err := cfg.Save(beadsDir); err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "config_save_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to update metadata.json: %v\n", err)
		}
		os.Exit(1)
	}

	if !jsonOutput {
		fmt.Printf("%s\n", ui.RenderPass("✓ Updated metadata.json to use Dolt backend"))
	}

	// Final status
	if jsonOutput {
		outputJSON(map[string]interface{}{
			"status":          "success",
			"backend":         "dolt",
			"issues_imported": imported,
			"issues_skipped":  skipped,
			"backup_path":     backupPath,
			"dolt_path":       doltPath,
		})
	} else {
		fmt.Println()
		fmt.Printf("%s\n", ui.RenderPass("✓ Migration complete!"))
		fmt.Println()
		fmt.Printf("Your beads now use Dolt storage.\n")
		fmt.Printf("SQLite backup: %s\n", backupPath)
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("  • Verify data: bd list")
		fmt.Println("  • After verification, you can delete the old SQLite database:")
		fmt.Printf("    rm %s\n", sqlitePath)
		fmt.Println()
		fmt.Println("To switch back to SQLite: bd migrate --to-sqlite")
	}
}

// importIssuesToDolt imports issues to Dolt, returning (imported, skipped, error)
func importIssuesToDolt(ctx context.Context, store *dolt.DoltStore, issues []*types.Issue) (int, int, error) {
	tx, err := store.UnderlyingDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	imported := 0
	skipped := 0
	seenIDs := make(map[string]bool)

	for _, issue := range issues {
		// Skip duplicates
		if seenIDs[issue.ID] {
			skipped++
			continue
		}
		seenIDs[issue.ID] = true

		// Compute content hash if missing
		if issue.ContentHash == "" {
			issue.ContentHash = issue.ComputeContentHash()
		}

		// Insert issue directly via SQL (bypass validation since we're migrating existing data)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO issues (
				id, content_hash, title, description, design, acceptance_criteria, notes,
				status, priority, issue_type, assignee, estimated_minutes,
				created_at, created_by, owner, updated_at, closed_at, external_ref,
				compaction_level, compacted_at, compacted_at_commit, original_size,
				deleted_at, deleted_by, delete_reason, original_type,
				sender, ephemeral, pinned, is_template, crystallizes,
				mol_type, work_type, quality_score, source_system, source_repo, close_reason,
				event_kind, actor, target, payload,
				await_type, await_id, timeout_ns, waiters,
				hook_bead, role_bead, agent_state, last_activity, role_type, rig,
				due_at, defer_until
			) VALUES (
				?, ?, ?, ?, ?, ?, ?,
				?, ?, ?, ?, ?,
				?, ?, ?, ?, ?, ?,
				?, ?, ?, ?,
				?, ?, ?, ?,
				?, ?, ?, ?, ?,
				?, ?, ?, ?, ?, ?,
				?, ?, ?, ?,
				?, ?, ?, ?,
				?, ?, ?, ?, ?, ?,
				?, ?
			)
		`,
			issue.ID, issue.ContentHash, issue.Title, issue.Description, issue.Design, issue.AcceptanceCriteria, issue.Notes,
			issue.Status, issue.Priority, issue.IssueType, nullableString(issue.Assignee), nullableIntPtr(issue.EstimatedMinutes),
			issue.CreatedAt, issue.CreatedBy, issue.Owner, issue.UpdatedAt, issue.ClosedAt, nullableStringPtr(issue.ExternalRef),
			issue.CompactionLevel, issue.CompactedAt, nullableStringPtr(issue.CompactedAtCommit), nullableInt(issue.OriginalSize),
			issue.DeletedAt, issue.DeletedBy, issue.DeleteReason, issue.OriginalType,
			issue.Sender, issue.Ephemeral, issue.Pinned, issue.IsTemplate, issue.Crystallizes,
			issue.MolType, issue.WorkType, nullableFloat32Ptr(issue.QualityScore), issue.SourceSystem, issue.SourceRepo, issue.CloseReason,
			issue.EventKind, issue.Actor, issue.Target, issue.Payload,
			issue.AwaitType, issue.AwaitID, issue.Timeout.Nanoseconds(), formatWaiters(issue.Waiters),
			issue.HookBead, issue.RoleBead, issue.AgentState, issue.LastActivity, issue.RoleType, issue.Rig,
			issue.DueAt, issue.DeferUntil,
		)
		if err != nil {
			if strings.Contains(err.Error(), "Duplicate entry") ||
				strings.Contains(err.Error(), "UNIQUE constraint") {
				skipped++
				continue
			}
			return imported, skipped, fmt.Errorf("failed to insert issue %s: %w", issue.ID, err)
		}

		// Insert labels
		for _, label := range issue.Labels {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO labels (issue_id, label)
				VALUES (?, ?)
			`, issue.ID, label)
			if err != nil && !strings.Contains(err.Error(), "Duplicate entry") {
				return imported, skipped, fmt.Errorf("failed to insert label for %s: %w", issue.ID, err)
			}
		}

		imported++
	}

	// Import dependencies in a second pass (after all issues exist)
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			// Check if both issues exist
			var exists int
			err := tx.QueryRowContext(ctx, "SELECT 1 FROM issues WHERE id = ?", dep.DependsOnID).Scan(&exists)
			if err != nil {
				// Target doesn't exist, skip dependency
				continue
			}

			_, err = tx.ExecContext(ctx, `
				INSERT INTO dependencies (issue_id, depends_on_id, type, created_by, created_at)
				VALUES (?, ?, ?, ?, ?)
				ON DUPLICATE KEY UPDATE type = type
			`, dep.IssueID, dep.DependsOnID, dep.Type, dep.CreatedBy, dep.CreatedAt)
			if err != nil && !strings.Contains(err.Error(), "Duplicate entry") {
				// Non-fatal for dependencies - log and continue
				continue
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return imported, skipped, fmt.Errorf("failed to commit: %w", err)
	}

	return imported, skipped, nil
}

// handleToSQLite migrates from Dolt to SQLite backend (escape hatch).
func handleToSQLiteMigration(dryRun bool, autoYes bool) {
	// Find .beads directory
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "no_beads_directory",
				"message": "No .beads directory found. Run 'bd init' first.",
			})
		} else {
			FatalErrorWithHint("no .beads directory found", "run 'bd init' to initialize bd")
		}
		os.Exit(1)
	}

	// Load config
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "config_load_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to load config: %v\n", err)
		}
		os.Exit(1)
	}
	if cfg == nil {
		cfg = configfile.DefaultConfig()
	}

	// Check if already using SQLite
	if cfg.GetBackend() == configfile.BackendSQLite {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"status":  "noop",
				"message": "Already using SQLite backend",
			})
		} else {
			fmt.Printf("%s\n", ui.RenderPass("✓ Already using SQLite backend"))
			fmt.Println("No migration needed")
		}
		return
	}

	// Find Dolt database
	doltPath := cfg.DatabasePath(beadsDir)
	if _, err := os.Stat(doltPath); os.IsNotExist(err) {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "no_dolt_database",
				"message": "No Dolt database found to migrate",
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: no Dolt database found at %s\n", doltPath)
		}
		os.Exit(1)
	}

	// SQLite path
	sqlitePath := filepath.Join(beadsDir, "beads.db")

	// Check if SQLite database already exists
	if _, err := os.Stat(sqlitePath); err == nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "sqlite_exists",
				"message": fmt.Sprintf("SQLite database already exists at %s", sqlitePath),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: SQLite database already exists at %s\n", sqlitePath)
			fmt.Fprintf(os.Stderr, "Hint: remove it first if you want to re-migrate\n")
		}
		os.Exit(1)
	}

	ctx := context.Background()

	// Open Dolt to count issues
	doltStore, err := dolt.New(ctx, &dolt.Config{Path: doltPath, ReadOnly: true})
	if err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "dolt_open_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to open Dolt database: %v\n", err)
		}
		os.Exit(1)
	}

	// Get all issues from Dolt
	issues, err := doltStore.SearchIssues(ctx, "", types.IssueFilter{IncludeTombstones: true})
	if err != nil {
		_ = doltStore.Close()
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "fetch_issues_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to fetch issues: %v\n", err)
		}
		os.Exit(1)
	}

	// Get labels and dependencies for each issue
	issueIDs := make([]string, len(issues))
	for i, issue := range issues {
		issueIDs[i] = issue.ID
	}
	labelsMap, err := doltStore.GetLabelsForIssues(ctx, issueIDs)
	if err != nil {
		_ = doltStore.Close()
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "fetch_labels_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to fetch labels: %v\n", err)
		}
		os.Exit(1)
	}

	allDeps, err := doltStore.GetAllDependencyRecords(ctx)
	if err != nil {
		_ = doltStore.Close()
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "fetch_deps_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to fetch dependencies: %v\n", err)
		}
		os.Exit(1)
	}

	// Get config values
	prefix, _ := doltStore.GetConfig(ctx, "issue_prefix")

	_ = doltStore.Close()

	// Assign labels and dependencies to issues
	for _, issue := range issues {
		if labels, ok := labelsMap[issue.ID]; ok {
			issue.Labels = labels
		}
		if deps, ok := allDeps[issue.ID]; ok {
			issue.Dependencies = deps
		}
	}

	if !jsonOutput {
		fmt.Printf("Dolt to SQLite Migration\n")
		fmt.Printf("========================\n\n")
		fmt.Printf("Source: %s\n", doltPath)
		fmt.Printf("Target: %s\n", sqlitePath)
		fmt.Printf("Issues to migrate: %d\n", len(issues))
		if prefix != "" {
			fmt.Printf("Issue prefix: %s\n", prefix)
		}
		fmt.Println()
	}

	// Dry run mode
	if dryRun {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"dry_run":     true,
				"source":      doltPath,
				"target":      sqlitePath,
				"issue_count": len(issues),
				"prefix":      prefix,
			})
		} else {
			fmt.Println("Dry run mode - no changes will be made")
			fmt.Println("Would perform:")
			fmt.Printf("  1. Create SQLite database at %s\n", sqlitePath)
			fmt.Printf("  2. Import %d issues with labels and dependencies\n", len(issues))
			fmt.Printf("  3. Update metadata.json to use SQLite backend\n")
		}
		return
	}

	// Prompt for confirmation
	if !autoYes && !jsonOutput {
		fmt.Printf("This will:\n")
		fmt.Printf("  1. Create a SQLite database and import all issues\n")
		fmt.Printf("  2. Update metadata.json to use SQLite backend\n")
		fmt.Printf("  3. Keep your Dolt database (can be deleted after verification)\n\n")
		fmt.Printf("Continue? [y/N] ")
		var response string
		_, _ = fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			fmt.Println("Migration canceled")
			return
		}
		fmt.Println()
	}

	// Create SQLite database
	if !jsonOutput {
		fmt.Printf("Creating SQLite database...\n")
	}

	sqliteStore, err := sqlite.New(ctx, sqlitePath)
	if err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "sqlite_create_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to create SQLite database: %v\n", err)
		}
		os.Exit(1)
	}

	// Set issue prefix
	if prefix != "" {
		if err := sqliteStore.SetConfig(ctx, "issue_prefix", prefix); err != nil {
			_ = sqliteStore.Close()
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"error":   "set_prefix_failed",
					"message": err.Error(),
				})
			} else {
				fmt.Fprintf(os.Stderr, "Error: failed to set issue prefix: %v\n", err)
			}
			os.Exit(1)
		}
	}

	// Import issues
	if !jsonOutput {
		fmt.Printf("Importing %d issues...\n", len(issues))
	}

	imported, skipped, err := importIssuesToSQLite(ctx, sqliteStore, issues)
	if err != nil {
		_ = sqliteStore.Close()
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "import_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to import issues: %v\n", err)
		}
		os.Exit(1)
	}

	_ = sqliteStore.Close()

	if !jsonOutput {
		fmt.Printf("%s\n", ui.RenderPass(fmt.Sprintf("✓ Imported %d issues (%d skipped)", imported, skipped)))
	}

	// Update metadata.json
	cfg.Backend = configfile.BackendSQLite
	cfg.Database = "beads.db"
	if err := cfg.Save(beadsDir); err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "config_save_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to update metadata.json: %v\n", err)
		}
		os.Exit(1)
	}

	if !jsonOutput {
		fmt.Printf("%s\n", ui.RenderPass("✓ Updated metadata.json to use SQLite backend"))
	}

	// Final status
	if jsonOutput {
		outputJSON(map[string]interface{}{
			"status":          "success",
			"backend":         "sqlite",
			"issues_imported": imported,
			"issues_skipped":  skipped,
			"sqlite_path":     sqlitePath,
		})
	} else {
		fmt.Println()
		fmt.Printf("%s\n", ui.RenderPass("✓ Migration complete!"))
		fmt.Println()
		fmt.Printf("Your beads now use SQLite storage.\n")
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("  • Verify data: bd list")
		fmt.Println("  • After verification, you can delete the Dolt directory:")
		fmt.Printf("    rm -rf %s\n", doltPath)
	}
}

// importIssuesToSQLite imports issues to SQLite, returning (imported, skipped, error)
func importIssuesToSQLite(ctx context.Context, store *sqlite.SQLiteStorage, issues []*types.Issue) (int, int, error) {
	imported := 0
	skipped := 0

	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		seenIDs := make(map[string]bool)

		for _, issue := range issues {
			if seenIDs[issue.ID] {
				skipped++
				continue
			}
			seenIDs[issue.ID] = true

			// Create issue (labels are handled separately)
			savedLabels := issue.Labels
			savedDeps := issue.Dependencies
			issue.Labels = nil       // Don't process in CreateIssue
			issue.Dependencies = nil // Don't process in CreateIssue

			if err := tx.CreateIssue(ctx, issue, "migration"); err != nil {
				if strings.Contains(err.Error(), "UNIQUE constraint") ||
					strings.Contains(err.Error(), "already exists") {
					skipped++
					issue.Labels = savedLabels
					issue.Dependencies = savedDeps
					continue
				}
				return fmt.Errorf("failed to insert issue %s: %w", issue.ID, err)
			}

			// Add labels
			for _, label := range savedLabels {
				if err := tx.AddLabel(ctx, issue.ID, label, "migration"); err != nil {
					// Non-fatal for labels
					continue
				}
			}

			issue.Labels = savedLabels
			issue.Dependencies = savedDeps
			imported++
		}

		// Import dependencies in a second pass
		for _, issue := range issues {
			for _, dep := range issue.Dependencies {
				if err := tx.AddDependency(ctx, dep, "migration"); err != nil {
					// Non-fatal for dependencies
					continue
				}
			}
		}

		return nil
	})

	if err != nil {
		return imported, skipped, err
	}

	return imported, skipped, nil
}

// Helper functions for nullable values
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullableStringPtr(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

func nullableIntPtr(i *int) interface{} {
	if i == nil {
		return nil
	}
	return *i
}

func nullableInt(i int) interface{} {
	if i == 0 {
		return nil
	}
	return i
}

func nullableFloat32Ptr(f *float32) interface{} {
	if f == nil {
		return nil
	}
	return *f
}

func formatWaiters(arr []string) string {
	if len(arr) == 0 {
		return ""
	}
	return strings.Join(arr, ",")
}
