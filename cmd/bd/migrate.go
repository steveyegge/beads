package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt/migrations"
	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
	"github.com/steveyegge/beads/internal/storage/migration"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

var migrateCmd = &cobra.Command{
	Use:     "migrate",
	GroupID: "maint",
	Short:   "Database migration commands",
	Long: `Database migration and data transformation commands.

Without subcommand, checks and updates database metadata to current version.

Subcommands:
  hooks       Plan git hook migration to marker-managed format
  issues      Move issues between repositories
  sync        Set up sync.branch workflow for multi-clone setups
`,
	Run: func(cmd *cobra.Command, _ []string) {
		autoYes, _ := cmd.Flags().GetBool("yes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		updateRepoID, _ := cmd.Flags().GetBool("update-repo-id")
		inspect, _ := cmd.Flags().GetBool("inspect")
		toBackend, _ := cmd.Flags().GetString("to")
		dsn, _ := cmd.Flags().GetString("dsn")
		force, _ := cmd.Flags().GetBool("force")
		source, _ := cmd.Flags().GetString("source")
		includeEvents, _ := cmd.Flags().GetBool("include-events")

		// Block writes in readonly mode (migration modifies data, --inspect is read-only)
		if !dryRun && !inspect {
			CheckReadonly("migrate")
		}

		// Handle cross-backend migration (Dolt → Postgres) before the legacy
		// in-place metadata-update path.
		if toBackend != "" {
			handleCrossBackendMigrate(toBackend, dsn, source, force, dryRun, includeEvents)
			return
		}

		// Handle --update-repo-id first
		if updateRepoID {
			handleUpdateRepoID(dryRun, autoYes)
			return
		}

		// Handle --inspect flag (show migration plan for AI agents)
		if inspect {
			handleInspect()
			return
		}

		// Find .beads directory
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"error":   "no_beads_directory",
					"message": activeWorkspaceNotFoundMessage() + " " + diagHint() + ".",
				})
				os.Exit(1)
			} else {
				FatalErrorWithHint(activeWorkspaceNotFoundError(), diagHint())
			}
		}

		// Load config
		cfg, err := loadOrCreateConfig(beadsDir)
		if err != nil {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"error":   "config_load_failed",
					"message": err.Error(),
				})
				os.Exit(1)
			}
			FatalError("failed to load config: %v", err)
		}

		// Handle Dolt metadata update
		handleDoltMetadataUpdate(cfg, dryRun)
	},
}

// handleDoltMetadataUpdate handles version metadata updates for Dolt backends.
func handleDoltMetadataUpdate(cfg *configfile.Config, dryRun bool) {
	ctx := rootCtx
	store := getStore()
	if store == nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"status":  "no_databases",
				"message": "No Dolt database found in .beads/",
			})
		} else {
			fmt.Fprintf(os.Stderr, "No Dolt database found. Run 'bd init' to create a new database.\n")
		}
		return
	}

	// Check current state of all metadata fields
	currentVersion, _ := store.GetLocalMetadata(ctx, "bd_version")
	currentRepoID, _ := mustConfig(store).GetMetadata(ctx, "repo_id")
	currentCloneID, _ := mustConfig(store).GetMetadata(ctx, "clone_id")

	needsVersionUpdate := currentVersion != Version
	needsRepoID := currentRepoID == ""
	needsCloneID := currentCloneID == ""

	// If everything is already current, return early
	if !needsVersionUpdate && !needsRepoID && !needsCloneID {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"status":  "current",
				"message": fmt.Sprintf("Dolt database already at version %s", Version),
			})
		} else {
			fmt.Printf("Dolt database version: %s\n", currentVersion)
			fmt.Printf("%s\n", ui.RenderPass("✓ Version matches"))
			fmt.Printf("%s\n", ui.RenderPass("✓ All metadata fields present"))
		}
		return
	}

	if dryRun {
		dryRunResult := map[string]interface{}{
			"dry_run":              true,
			"needs_version_update": needsVersionUpdate,
			"needs_repo_id":        needsRepoID,
			"needs_clone_id":       needsCloneID,
		}
		if needsVersionUpdate {
			dryRunResult["current_version"] = currentVersion
			dryRunResult["target_version"] = Version
		}
		if jsonOutput {
			outputJSON(dryRunResult)
		} else {
			fmt.Println("Dry run mode - no changes will be made")
			if needsVersionUpdate {
				fmt.Printf("Would update Dolt version: %s → %s\n", currentVersion, Version)
			}
			if needsRepoID {
				fmt.Println("Would set repo_id")
			}
			if needsCloneID {
				fmt.Println("Would set clone_id")
			}
		}
		return
	}

	versionUpdated := false
	repoIDSet := false
	cloneIDSet := false

	// Update bd_version if needed
	if needsVersionUpdate {
		if !jsonOutput {
			fmt.Printf("Updating Dolt schema version: %s → %s\n", currentVersion, Version)
		}

		// Detect and set issue_prefix if missing
		prefix, err := store.GetConfig(ctx, "issue_prefix")
		if err != nil || prefix == "" {
			issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
			if err == nil && len(issues) > 0 {
				detectedPrefix := utils.ExtractIssuePrefix(issues[0].ID)
				if detectedPrefix != "" {
					if err := store.SetConfig(ctx, "issue_prefix", detectedPrefix); err != nil {
						if !jsonOutput {
							fmt.Fprintf(os.Stderr, "Warning: failed to set issue prefix: %v\n", err)
						}
					} else if !jsonOutput {
						fmt.Printf("%s\n", ui.RenderPass(fmt.Sprintf("✓ Detected and set issue prefix: %s", detectedPrefix)))
					}
				}
			}
		}

		// Update version metadata (fatal on failure — version is critical)
		if err := store.SetLocalMetadata(ctx, "bd_version", Version); err != nil {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"error":   "version_update_failed",
					"message": err.Error(),
				})
				os.Exit(1)
			}
			FatalError("failed to update version: %v", err)
		}
		versionUpdated = true

		if !jsonOutput {
			fmt.Printf("%s\n", ui.RenderPass("✓ Version updated"))
		}
	}

	// Set repo_id if missing (non-fatal — may fail in non-git environments)
	if needsRepoID {
		computed, err := beads.ComputeRepoID()
		if err != nil {
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "Warning: could not compute repo_id: %v\n", err)
			}
		} else {
			if err := mustConfig(store).SetMetadata(ctx, "repo_id", computed); err != nil {
				if !jsonOutput {
					fmt.Fprintf(os.Stderr, "Warning: failed to set repo_id: %v\n", err)
				}
			} else {
				repoIDSet = true
				if !jsonOutput {
					fmt.Printf("%s\n", ui.RenderPass(fmt.Sprintf("✓ Set repo_id: %s", truncateID(computed, 8))))
				}
			}
		}
	}

	// Set clone_id if missing (non-fatal — may fail in non-git environments)
	if needsCloneID {
		computed, err := beads.GetCloneID()
		if err != nil {
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "Warning: could not compute clone_id: %v\n", err)
			}
		} else {
			if err := mustConfig(store).SetMetadata(ctx, "clone_id", computed); err != nil {
				if !jsonOutput {
					fmt.Fprintf(os.Stderr, "Warning: failed to set clone_id: %v\n", err)
				}
			} else {
				cloneIDSet = true
				if !jsonOutput {
					fmt.Printf("%s\n", ui.RenderPass(fmt.Sprintf("✓ Set clone_id: %s", truncateID(computed, 8))))
				}
			}
		}
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"status":           "success",
			"current_database": cfg.Database,
			"backend":          "dolt",
			"version":          Version,
			"version_updated":  versionUpdated,
			"repo_id_set":      repoIDSet,
			"clone_id_set":     cloneIDSet,
		})
	} else {
		fmt.Printf("\nDolt database: %s (version %s)\n", cfg.Database, Version)
	}

	if versionUpdated || repoIDSet || cloneIDSet {
		commandDidWrite.Store(true)
	}
}

// truncateID safely truncates an ID string to maxLen characters.
func truncateID(id string, maxLen int) string {
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen]
}

// handleCrossBackendMigrate runs the Dolt → Postgres data copy for
// `bd migrate --to=postgres`. The implementation lives in
// internal/storage/migration; this wrapper handles flag validation,
// source/destination opening, error formatting, and JSON output.
func handleCrossBackendMigrate(toBackend, dsn, source string, force, dryRun, includeEvents bool) {
	ctx := rootCtx
	if toBackend != "postgres" {
		emitMigrateError("invalid_target_backend",
			fmt.Sprintf("--to=%s not supported (only 'postgres' in v1)", toBackend))
		os.Exit(1)
	}
	if dsn == "" {
		emitMigrateError("missing_dsn", "--dsn is required when --to=postgres")
		os.Exit(1)
	}

	srcDB, srcCloser, err := openMigrateSourceDB(ctx, source)
	if err != nil {
		emitMigrateError("source_open_failed", err.Error())
		os.Exit(1)
	}
	defer srcCloser()

	dst, err := storage.Open(ctx, storage.BackendPostgres, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		emitMigrateError("destination_open_failed", err.Error())
		os.Exit(1)
	}
	defer func() { _ = dst.Close() }()

	result, err := migration.MigrateFromDB(ctx, srcDB, dst, migration.Options{
		Force:         force,
		DryRun:        dryRun,
		IncludeEvents: includeEvents,
		Stderr:        os.Stderr,
	})
	if err != nil {
		emitCrossBackendMigrateError(err)
		os.Exit(1)
	}

	emitCrossBackendMigrateSuccess(result)
}

// openMigrateSourceDB returns the *sql.DB to read the Dolt source from, plus
// a teardown function the caller must call.
//
// When source is empty (active-workspace path) we borrow from the already-open
// global store. The teardown is a no-op so the deferred close in the handler
// does not double-close the global store.
//
// When source is provided we open the dolt files directly via the embedded
// driver, deliberately bypassing the `BackendDolt` registry path. The latter
// honors `BEADS_DOLT_SERVER_PORT` (and central-config server settings) and
// would silently connect to whatever dolt server is running on that port,
// even when its data_dir does not match the requested --source. That hits
// migration on a completely different database than the user intended; see
// be-b0h for the reproduction.
func openMigrateSourceDB(ctx context.Context, source string) (*sql.DB, func(), error) {
	if source == "" {
		s := getStore()
		if s == nil {
			return nil, func() {}, errors.New("no active beads workspace; pass --source=<beads-dir>")
		}
		borrower, ok := storage.UnwrapStore(s).(migration.SourceDBBorrower)
		if !ok {
			return nil, func() {}, errors.New("active beads workspace does not expose a migration source DB (expected a Dolt-backed store)")
		}
		db, release, err := borrower.BorrowSourceDB(ctx)
		if err != nil {
			return nil, func() {}, fmt.Errorf("borrow active source db: %w", err)
		}
		return db, func() { _ = release() }, nil
	}

	dataDir, database, err := resolveDoltSourceDataDir(source)
	if err != nil {
		return nil, func() {}, err
	}
	db, release, err := embeddeddolt.OpenSQL(ctx, dataDir, database, "")
	if err != nil {
		return nil, func() {}, fmt.Errorf("open --source=%s: %w", source, err)
	}
	return db, func() { _ = release() }, nil
}

// resolveDoltSourceDataDir returns the (dataDir, database) tuple that the
// embedded dolt driver needs to read the dolt repository at source. Server
// mode keeps data at <source>/dolt/<db>; embedded mode at
// <source>/embeddeddolt/<db>. We prefer server-mode layout when both exist
// because that is the canonical clone target.
func resolveDoltSourceDataDir(source string) (string, string, error) {
	abs, err := filepath.Abs(source)
	if err != nil {
		return "", "", fmt.Errorf("resolve --source=%s: %w", source, err)
	}
	cfg, err := configfile.Load(abs)
	if err != nil {
		return "", "", fmt.Errorf("read --source metadata.json: %w", err)
	}
	database := configfile.DefaultDoltDatabase
	if cfg != nil {
		database = cfg.GetDoltDatabase()
	}
	for _, candidate := range []string{
		filepath.Join(abs, "dolt"),
		filepath.Join(abs, "embeddeddolt"),
	} {
		dbPath := filepath.Join(candidate, database, ".dolt")
		if info, err := os.Stat(dbPath); err == nil && info.IsDir() {
			return candidate, database, nil
		}
	}
	return "", "", fmt.Errorf("--source=%s: no dolt database %q under dolt/ or embeddeddolt/", source, database)
}

func emitCrossBackendMigrateSuccess(result *migration.Result) {
	if jsonOutput {
		outputJSON(map[string]interface{}{
			"status":               "success",
			"dry_run":              result.DryRun,
			"tables_cleared":       result.TablesCleared,
			"rows_by_table":        result.RowsByTable,
			"audit_events_skipped": result.AuditEventsSkipped,
		})
		return
	}
	if result.DryRun {
		fmt.Println("Dry run — no changes written.")
	}
	if len(result.TablesCleared) > 0 {
		fmt.Printf("Cleared destination tables: %s\n", strings.Join(result.TablesCleared, ", "))
	}
	totalRows := 0
	for _, n := range result.RowsByTable {
		totalRows += n
	}
	if result.DryRun {
		fmt.Printf("Source rows: %d across %d tables\n", totalRows, len(result.RowsByTable))
	} else {
		fmt.Printf("%s\n", ui.RenderPass(fmt.Sprintf("✓ Migrated %d rows across %d tables", totalRows, len(result.RowsByTable))))
	}
	if result.AuditEventsSkipped > 0 {
		fmt.Printf("Audit-trail events not migrated: %d (see docs/AUDIT_TRAIL_POSTGRES.md)\n", result.AuditEventsSkipped)
	}
}

// emitCrossBackendMigrateError translates the migration package's typed
// errors into either a friendly stderr message or a JSON envelope that bd
// agents can parse.
func emitCrossBackendMigrateError(err error) {
	var notEmpty *migration.ErrDestinationNotEmpty
	switch {
	case errors.As(err, &notEmpty):
		emitMigrateError("destination_not_empty", err.Error())
	case errors.Is(err, migration.ErrUnimplementedFeature):
		emitMigrateError("feature_not_implemented", err.Error())
	default:
		emitMigrateError("migration_failed", err.Error())
	}
}

func emitMigrateError(code, msg string) {
	if jsonOutput {
		outputJSON(map[string]interface{}{
			"error":   code,
			"message": msg,
		})
		return
	}
	fmt.Fprintf(os.Stderr, "error: %s\n", msg)
}

// loadOrCreateConfig loads metadata.json or creates default if not found
func loadOrCreateConfig(beadsDir string) (*configfile.Config, error) {
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		return nil, err
	}

	// Create default if no config exists
	if cfg == nil {
		cfg = configfile.DefaultConfig()
	}

	return cfg, nil
}

func handleUpdateRepoID(dryRun bool, autoYes bool) {
	// Find .beads directory
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "no_database",
				"message": "No beads database found. " + diagHint() + ".",
			})
			os.Exit(1)
		}
		FatalErrorWithHint("no beads database found", diagHint())
	}

	// Compute new repo ID
	newRepoID, err := beads.ComputeRepoID()
	if err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "compute_failed",
				"message": err.Error(),
			})
			os.Exit(1)
		}
		FatalError("failed to compute repository ID: %v", err)
	}

	store := getStore()
	if store == nil {
		FatalError("no database — run 'bd init' first")
	}

	// Get old repo ID
	ctx := rootCtx
	oldRepoID, err := mustConfig(store).GetMetadata(ctx, "repo_id")
	if err != nil && err.Error() != "metadata key not found: repo_id" {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "read_failed",
				"message": err.Error(),
			})
			os.Exit(1)
		}
		FatalError("failed to read repo_id: %v", err)
	}

	oldDisplay := "none"
	if len(oldRepoID) >= 8 {
		oldDisplay = oldRepoID[:8]
	}

	if dryRun {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"dry_run":     true,
				"old_repo_id": oldDisplay,
				"new_repo_id": truncateID(newRepoID, 8),
			})
		} else {
			fmt.Println("Dry run mode - no changes will be made")
			fmt.Printf("Would update repository ID:\n")
			fmt.Printf("  Old: %s\n", oldDisplay)
			fmt.Printf("  New: %s\n", truncateID(newRepoID, 8))
		}
		return
	}

	// Prompt for confirmation if repo_id exists and differs
	if oldRepoID != "" && oldRepoID != newRepoID && !autoYes && !jsonOutput {
		fmt.Printf("WARNING: Changing repository ID can break sync if other clones exist.\n\n")
		fmt.Printf("Current repo ID: %s\n", oldDisplay)
		fmt.Printf("New repo ID:     %s\n\n", truncateID(newRepoID, 8))
		fmt.Printf("Continue? [y/N] ")
		var response string
		_, _ = fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			fmt.Println("Canceled")
			return
		}
	}

	// Update repo ID
	if err := mustConfig(store).SetMetadata(ctx, "repo_id", newRepoID); err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "update_failed",
				"message": err.Error(),
			})
			os.Exit(1)
		}
		FatalError("failed to update repo_id: %v", err)
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"status":      "success",
			"old_repo_id": oldDisplay,
			"new_repo_id": truncateID(newRepoID, 8),
		})
	} else {
		fmt.Printf("%s\n\n", ui.RenderPass("✓ Repository ID updated"))
		fmt.Printf("  Old: %s\n", oldDisplay)
		fmt.Printf("  New: %s\n", truncateID(newRepoID, 8))
	}

	commandDidWrite.Store(true)
}

// handleInspect shows migration plan and database state for AI agent analysis
func handleInspect() {
	// Find .beads directory
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "no_beads_directory",
				"message": activeWorkspaceNotFoundMessage() + " " + diagHint() + ".",
			})
			os.Exit(1)
		}
		FatalErrorWithHint(activeWorkspaceNotFoundError(), diagHint())
	}

	// Check if database is available via the global store
	dbExists := getStore() != nil

	// If database doesn't exist, return inspection with defaults
	if !dbExists {
		result := map[string]interface{}{
			"registered_migrations": listMigrations(),
			"current_state": map[string]interface{}{
				"schema_version": "missing",
				"issue_count":    0,
				"config":         map[string]string{},
				"missing_config": []string{},
				"db_exists":      false,
			},
			"warnings":            []string{"Database does not exist - " + diagHint()},
			"invariants_to_check": []string{},
		}

		if jsonOutput {
			outputJSON(result)
		} else {
			fmt.Println("\nMigration Inspection")
			fmt.Println("====================")
			fmt.Println("Database: missing")
			fmt.Println("\n⚠ Database does not exist - " + diagHint())
		}
		return
	}

	store := getStore()
	if store == nil {
		FatalError("no database — run 'bd init' first")
	}

	ctx := rootCtx

	// Get current schema version
	schemaVersion, err := store.GetLocalMetadata(ctx, "bd_version")
	if err != nil {
		schemaVersion = "unknown"
	}

	// Get issue count
	issueCount := 0
	if stats, err := store.GetStatistics(ctx); err == nil {
		issueCount = stats.TotalIssues
	}

	// Get config
	configMap := make(map[string]string)
	prefix, _ := store.GetConfig(ctx, "issue_prefix")
	if prefix != "" {
		configMap["issue_prefix"] = prefix
	}

	// Detect missing config
	missingConfig := []string{}
	if issueCount > 0 && prefix == "" {
		missingConfig = append(missingConfig, "issue_prefix")
	}

	// Get registered migrations
	registeredMigrations := listMigrations()

	// Generate warnings
	warnings := []string{}
	if issueCount > 0 && prefix == "" {
		detectedPrefix := ""
		if issues, err := store.SearchIssues(ctx, "", types.IssueFilter{}); err == nil && len(issues) > 0 {
			detectedPrefix = utils.ExtractIssuePrefix(issues[0].ID)
		}
		warnings = append(warnings, fmt.Sprintf("issue_prefix config not set - may break commands after migration (detected: %s)", detectedPrefix))
	}
	if schemaVersion != Version {
		warnings = append(warnings, fmt.Sprintf("schema version mismatch (current: %s, expected: %s)", schemaVersion, Version))
	}

	// Output result
	result := map[string]interface{}{
		"registered_migrations": registeredMigrations,
		"current_state": map[string]interface{}{
			"schema_version": schemaVersion,
			"issue_count":    issueCount,
			"config":         configMap,
			"missing_config": missingConfig,
			"db_exists":      true,
		},
		"warnings":            warnings,
		"invariants_to_check": []string{},
	}

	if jsonOutput {
		outputJSON(result)
	} else {
		fmt.Println("\nMigration Inspection")
		fmt.Println("====================")
		fmt.Printf("Schema Version: %s\n", schemaVersion)
		fmt.Printf("Issue Count: %d\n", issueCount)
		fmt.Printf("Registered Migrations: %d\n", len(registeredMigrations))

		if len(warnings) > 0 {
			fmt.Println("\nWarnings:")
			for _, w := range warnings {
				fmt.Printf("  ⚠ %s\n", w)
			}
		}

		if len(missingConfig) > 0 {
			fmt.Println("\nMissing Config:")
			for _, k := range missingConfig {
				fmt.Printf("  - %s\n", k)
			}
		}
		fmt.Println()
	}
}

// handleToSeparateBranch configures separate branch workflow for existing repos
func handleToSeparateBranch(branch string, dryRun bool) {
	// Validate branch name
	b := strings.TrimSpace(branch)
	if b == "" || strings.ContainsAny(b, " \t\n") {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "invalid_branch",
				"message": "Branch name cannot be empty or contain whitespace",
			})
			os.Exit(1)
		}
		FatalErrorWithHint(fmt.Sprintf("invalid branch name '%s'", branch), "branch name cannot be empty or contain whitespace")
	}

	// Find .beads directory
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "no_beads_directory",
				"message": activeWorkspaceNotFoundMessage() + " " + diagHint() + ".",
			})
			os.Exit(1)
		}
		FatalErrorWithHint(activeWorkspaceNotFoundError(), diagHint())
	}

	store := getStore()
	if store == nil {
		FatalError("no database — run 'bd init' first")
	}

	// Get current sync.branch config
	ctx := rootCtx
	current, _ := store.GetConfig(ctx, "sync.branch")

	// Dry-run mode
	if dryRun {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"dry_run":  true,
				"previous": current,
				"branch":   b,
				"changed":  current != b,
			})
		} else {
			fmt.Println("Dry run mode - no changes will be made")
			if current == b {
				fmt.Printf("sync.branch already set to '%s'\n", b)
			} else {
				fmt.Printf("Would set sync.branch: '%s' → '%s'\n", current, b)
			}
		}
		return
	}

	// Check if already set
	if current == b {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"status":  "noop",
				"branch":  b,
				"message": "sync.branch already set to this value",
			})
		} else {
			fmt.Printf("%s\n", ui.RenderPass(fmt.Sprintf("✓ sync.branch already set to '%s'", b)))
			fmt.Println("No changes needed")
		}
		return
	}

	// Update sync.branch config
	if err := store.SetConfig(ctx, "sync.branch", b); err != nil {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"error":   "config_update_failed",
				"message": err.Error(),
			})
			os.Exit(1)
		}
		FatalError("failed to set sync.branch: %v", err)
	}

	// Success output
	if jsonOutput {
		outputJSON(map[string]interface{}{
			"status":   "success",
			"previous": current,
			"branch":   b,
			"message":  "Enabled separate branch workflow",
		})
	} else {
		fmt.Printf("%s\n\n", ui.RenderPass("✓ Enabled separate branch workflow"))
		fmt.Printf("Set sync.branch to '%s'\n\n", b)
		fmt.Println("Next steps:")
		fmt.Println("  1. No restart required. sync.branch is active immediately.")
		fmt.Printf("     bd dolt push\n\n")
		fmt.Println("  2. Your existing data is preserved - no changes to git history")
		fmt.Println("  3. Future issue updates are stored in Dolt directly")
	}

	if !dryRun {
		commandDidWrite.Store(true)
	}
}

// listMigrations returns registered Dolt schema migrations.
func listMigrations() []string {
	return migrations.ListCompatMigrations()
}

// migrateSyncCmd is the "bd migrate sync <branch>" subcommand that
// configures the separate-branch workflow for multi-clone setups.
// Previously this was documented but never wired as an actual subcommand,
// so bd doctor's recommendation to run "bd migrate sync beads-sync" would fail.
var migrateSyncCmd = &cobra.Command{
	Use:   "sync <branch>",
	Short: "Set up sync.branch workflow for multi-clone setups",
	Long: `Configure separate branch workflow for multi-clone setups.

This sets the sync.branch config value so that issue data is committed
to a dedicated branch, keeping your main branch clean.

Example:
  bd migrate sync beads-sync`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		if !dryRun {
			CheckReadonly("migrate sync")
		}
		handleToSeparateBranch(args[0], dryRun)
	},
}

func init() {
	migrateCmd.Flags().Bool("yes", false, "Auto-confirm prompts")
	migrateCmd.Flags().Bool("dry-run", false, "Show what would be done without making changes")
	migrateCmd.Flags().Bool("update-repo-id", false, "Update repository ID (use after changing git remote)")
	migrateCmd.Flags().Bool("inspect", false, "Show migration plan and database state for AI agent analysis")
	migrateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output migration statistics in JSON format")

	// Cross-backend migration flags (be-6fk.5). When --to is set the command
	// runs a Dolt → Postgres data copy; otherwise the existing in-place
	// metadata path runs.
	migrateCmd.Flags().String("to", "", "Target backend for data migration (currently only 'postgres')")
	migrateCmd.Flags().String("dsn", "", "Destination DSN (required when --to=postgres)")
	migrateCmd.Flags().Bool("force", false, "TRUNCATE destination tables before copy (required for non-empty destinations)")
	migrateCmd.Flags().String("source", "", "Path to source .beads directory (defaults to the active workspace)")
	migrateCmd.Flags().Bool("include-events", false, "Include audit-trail events (reserved; v1 returns ErrUnimplemented)")

	migrateSyncCmd.Flags().Bool("dry-run", false, "Show what would be done without making changes")
	migrateSyncCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	migrateCmd.AddCommand(migrateSyncCmd)

	migrateHooksCmd.Flags().Bool("dry-run", false, "Show what would be done without making changes")
	migrateHooksCmd.Flags().Bool("apply", false, "Apply planned hook migration changes")
	migrateHooksCmd.Flags().Bool("yes", false, "Skip confirmation prompt for --apply")
	migrateHooksCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	migrateCmd.AddCommand(migrateHooksCmd)

	rootCmd.AddCommand(migrateCmd)
}
