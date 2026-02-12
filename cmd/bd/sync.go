package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/syncbranch"
)

// SyncBranchContext holds sync-branch configuration detected from the store.
// This consolidates the repeated pattern of checking for sync-branch config.
type SyncBranchContext struct {
	Branch   string // Sync branch name, empty if not configured
	RepoRoot string // Git repository root path
}

// IsConfigured returns true if a sync branch is configured.
func (s *SyncBranchContext) IsConfigured() bool {
	return s.Branch != ""
}

// getSyncBranchContext detects sync-branch configuration from the store.
// Returns a context with empty Branch if not configured or on error.
func getSyncBranchContext(ctx context.Context) *SyncBranchContext {
	sbc := &SyncBranchContext{}
	if err := ensureStoreActive(); err != nil || store == nil {
		return sbc
	}
	if sb, _ := syncbranch.Get(ctx, store); sb != "" {
		sbc.Branch = sb
		if rc, err := beads.GetRepoContext(); err == nil {
			sbc.RepoRoot = rc.RepoRoot
		}
	}
	return sbc
}

// hasUncommittedChanges checks if there are any pending changes to sync.
// This is a cheap status check to avoid expensive export operations when nothing has changed.
//
// For daemon mode: Uses daemonClient.VcsHasUncommitted() RPC.
// For Dolt backends: Uses StatusChecker.HasUncommittedChanges() to query dolt_status.
// For SQLite backends: Uses GetDirtyIssues() to check if any issues have changed since last export.
//
// gt-p1mpqx: Added to reduce sync overhead from defensive agent calls.
// bd-ma0s.6: Added daemon RPC routing.
func hasUncommittedChanges(ctx context.Context, s storage.Storage) (bool, error) {
	// bd-ma0s.6: Route through daemon RPC
	result, err := daemonClient.VcsHasUncommitted()
	if err != nil {
		debug.Logf("VcsHasUncommitted RPC failed, falling back to direct: %v", err)
		// Fall through to direct mode
	} else {
		return result.HasUncommitted, nil
	}

	// Try StatusChecker interface first (Dolt backend)
	if sc, ok := storage.AsStatusChecker(s); ok {
		return sc.HasUncommittedChanges(ctx)
	}

	// Fall back to GetDirtyIssues for SQLite/other backends
	dirtyIDs, err := s.GetDirtyIssues(ctx)
	if err != nil {
		return false, fmt.Errorf("checking dirty issues: %w", err)
	}

	return len(dirtyIDs) > 0, nil
}

// commitAndPushBeads commits and pushes .beads changes using the appropriate method.
// When sync-branch is configured, uses worktree-based commit/push.
// Otherwise, uses standard git commit/push on the current branch.
func commitAndPushBeads(ctx context.Context, sbc *SyncBranchContext, jsonlPath string, noPush bool, message string) error {
	if sbc.IsConfigured() {
		fmt.Printf("→ Committing to sync branch '%s'...\n", sbc.Branch)
		commitResult, err := syncbranch.CommitToSyncBranch(ctx, sbc.RepoRoot, sbc.Branch, jsonlPath, !noPush)
		if err != nil {
			return fmt.Errorf("committing to sync branch: %w", err)
		}
		if commitResult.Committed {
			fmt.Printf("  Committed: %s\n", commitResult.Message)
			if commitResult.Pushed {
				fmt.Println("  Pushed to remote")
			}
		} else {
			fmt.Println("→ No changes to commit")
		}
		return nil
	}

	// Standard git workflow
	hasChanges, err := gitHasBeadsChanges(ctx)
	if err != nil {
		return fmt.Errorf("checking git status: %w", err)
	}

	if hasChanges {
		fmt.Println("→ Committing changes...")
		if err := gitCommitBeadsDir(ctx, message); err != nil {
			return fmt.Errorf("committing: %w", err)
		}
	} else {
		fmt.Println("→ No changes to commit")
	}

	// Push to remote
	if !noPush && hasChanges {
		fmt.Println("→ Pushing to remote...")
		if err := gitPush(ctx, ""); err != nil {
			return fmt.Errorf("pushing: %w", err)
		}
	}

	return nil
}

var syncCmd = &cobra.Command{
	Use:        "sync",
	GroupID:    "sync",
	Short:      "[DEPRECATED] Dolt handles sync automatically",
	Deprecated: "Dolt backend handles synchronization automatically. Use 'bd export' or 'bd import' for manual JSONL operations.",
	Long: `DEPRECATED: bd sync is no longer needed.

Dolt backend now handles synchronization automatically. The bd sync command
is a no-op and will be removed in a future release.

For manual JSONL operations, use:
  bd export    Export database to JSONL
  bd import    Import from JSONL file

For git operations, use git directly:
  git pull --rebase
  git push`,
	Run: func(cmd *cobra.Command, _ []string) {
		// bd sync is deprecated - Dolt handles sync automatically.
		// Print deprecation notice and exit as no-op.
		fmt.Println("⚠️  bd sync is deprecated. Dolt handles synchronization automatically.")
		fmt.Println("   Use 'bd export' or 'bd import' for manual JSONL operations.")
	},
}

// The helper functions below (doPullFirstSync, doExportSync, etc.) are kept
// because other sync_*.go files in the package reference them.
// The old syncCmd Run body has been removed. See git history for the full implementation.

// doPullFirstSync implements the pull-first sync flow:
// Pull → Merge → Export → Commit → Push
//
// This eliminates the export-before-pull data loss pattern (#911) by
// seeing remote changes before exporting local state.
//
// The 3-way merge uses:
// - Base state: Last successful sync (.beads/sync_base.jsonl)
// - Local state: Current database contents
// - Remote state: JSONL after git pull
//
// When noPull is true, skips the pull/merge steps and just does:
// Export → Commit → Push
func doPullFirstSync(ctx context.Context, jsonlPath string, renameOnImport, noGitHistory, dryRun, noPush, noPull bool, message string, acceptRebase bool, sbc *SyncBranchContext) error {
	beadsDir := filepath.Dir(jsonlPath)
	_ = acceptRebase // Reserved for future sync branch force-push detection

	if dryRun {
		if noPull {
			fmt.Println("→ [DRY RUN] Would export pending changes to JSONL")
			fmt.Println("→ [DRY RUN] Would commit changes")
			if !noPush {
				fmt.Println("→ [DRY RUN] Would push to remote")
			}
		} else {
			fmt.Println("→ [DRY RUN] Would pull from remote")
			fmt.Println("→ [DRY RUN] Would load base state from sync_base.jsonl")
			fmt.Println("→ [DRY RUN] Would merge base, local, and remote issues (3-way)")
			fmt.Println("→ [DRY RUN] Would export merged state to JSONL")
			fmt.Println("→ [DRY RUN] Would update sync_base.jsonl")
			fmt.Println("→ [DRY RUN] Would commit and push changes")
		}
		fmt.Println("\n✓ Dry run complete (no changes made)")
		return nil
	}

	// If noPull, use simplified export-only flow
	if noPull {
		return doExportOnlySync(ctx, jsonlPath, noPush, message)
	}

	// Step 1: Load local state from DB BEFORE pulling
	// This captures the current DB state before remote changes arrive
	if err := ensureStoreActive(); err != nil {
		return fmt.Errorf("activating store: %w", err)
	}

	localIssues, err := store.SearchIssues(ctx, "", beads.IssueFilter{IncludeTombstones: true})
	if err != nil {
		return fmt.Errorf("loading local issues: %w", err)
	}
	fmt.Printf("→ Loaded %d local issues from database\n", len(localIssues))

	// Acquire exclusive lock to prevent concurrent sync corruption
	lockPath := filepath.Join(beadsDir, ".sync.lock")
	lock := flock.New(lockPath)
	locked, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("acquiring sync lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("another sync is in progress")
	}
	defer func() { _ = lock.Unlock() }()

	// Step 2: Load base state (last successful sync)
	fmt.Println("→ Loading base state...")
	baseIssues, err := loadBaseState(beadsDir)
	if err != nil {
		return fmt.Errorf("loading base state: %w", err)
	}
	if baseIssues == nil {
		fmt.Println("  No base state found (first sync)")
	} else {
		fmt.Printf("  Loaded %d issues from base state\n", len(baseIssues))
	}

	// Step 3: Pull from remote
	// Mode-specific pull behavior:
	// - dolt-native/belt-and-suspenders with Dolt remote: Pull from Dolt
	// - sync.branch configured: Pull from sync branch via worktree
	// - Default (git-portable): Normal git pull
	syncMode := GetSyncMode(ctx, store)
	shouldUseDolt := ShouldUseDoltRemote(ctx, store)

	if shouldUseDolt {
		// bd-ma0s.6: Route Dolt pull through daemon RPC
		fmt.Println("→ Pulling from Dolt remote (via daemon)...")
		_, err := daemonClient.VcsPull()
		if err != nil {
			if strings.Contains(err.Error(), "remote") {
				fmt.Println("⚠ No Dolt remote configured, skipping Dolt pull")
			} else {
				return fmt.Errorf("dolt pull failed: %w", err)
			}
		} else {
			fmt.Println("✓ Pulled from Dolt remote")
		}
		// For belt-and-suspenders, continue with git pull even if Dolt pull failed
	}

	// Git-based pull (for git-portable, belt-and-suspenders, or when Dolt not available)
	if ShouldExportJSONL(ctx, store) {
		if sbc.IsConfigured() {
			fmt.Printf("→ Pulling from sync branch '%s'...\n", sbc.Branch)
			pullResult, err := syncbranch.PullFromSyncBranch(ctx, sbc.RepoRoot, sbc.Branch, jsonlPath, false)
			if err != nil {
				return fmt.Errorf("pulling from sync branch: %w", err)
			}
			// Display any safety warnings from the pull
			for _, warning := range pullResult.SafetyWarnings {
				fmt.Fprintln(os.Stderr, warning)
			}
			if pullResult.Merged {
				fmt.Println("  Merged divergent sync branch histories")
			} else if pullResult.FastForwarded {
				fmt.Println("  Fast-forwarded to remote")
			}
		} else {
			fmt.Println("→ Pulling from remote...")
			if err := gitPull(ctx, ""); err != nil {
				return fmt.Errorf("pulling: %w", err)
			}
		}
	}

	// For dolt-native mode, we're done after pulling from Dolt remote
	// Dolt handles merging internally, no JSONL workflow needed
	if syncMode == SyncModeDoltNative {
		fmt.Println("\n✓ Sync complete (dolt-native mode)")
		return nil
	}

	// Step 4: Load remote state from JSONL (after pull)
	remoteIssues, err := loadIssuesFromJSONL(jsonlPath)
	if err != nil {
		return fmt.Errorf("loading remote issues from JSONL: %w", err)
	}
	fmt.Printf("  Loaded %d remote issues from JSONL\n", len(remoteIssues))

	// Step 5: Perform 3-way merge
	fmt.Println("→ Merging base, local, and remote issues (3-way)...")
	mergeResult := MergeIssues(baseIssues, localIssues, remoteIssues)

	// Report merge results
	localCount, remoteCount, sameCount := 0, 0, 0
	for _, strategy := range mergeResult.Strategy {
		switch strategy {
		case StrategyLocal:
			localCount++
		case StrategyRemote:
			remoteCount++
		case StrategySame:
			sameCount++
		}
	}
	fmt.Printf("  Merged: %d issues total\n", len(mergeResult.Merged))
	fmt.Printf("    Local wins: %d, Remote wins: %d, Same: %d, Conflicts (LWW): %d\n",
		localCount, remoteCount, sameCount, mergeResult.Conflicts)

	// Display manual conflicts that need user resolution
	if len(mergeResult.ManualConflicts) > 0 {
		displayManualConflicts(mergeResult.ManualConflicts)
	}

	// Step 6: Import merged state to DB
	// First, write merged result to JSONL so import can read it
	fmt.Println("→ Writing merged state to JSONL...")
	if err := writeMergedStateToJSONL(jsonlPath, mergeResult.Merged); err != nil {
		return fmt.Errorf("writing merged state: %w", err)
	}

	fmt.Println("→ Importing merged state to database...")
	if err := importFromJSONL(ctx, jsonlPath, renameOnImport, noGitHistory); err != nil {
		return fmt.Errorf("importing merged state: %w", err)
	}

	// Step 7: Export from DB to JSONL (ensures DB is source of truth)
	fmt.Println("→ Exporting from database to JSONL...")
	if err := exportToJSONL(ctx, jsonlPath); err != nil {
		return fmt.Errorf("exporting: %w", err)
	}

	// Step 8 & 9: Commit and push changes
	if err := commitAndPushBeads(ctx, sbc, jsonlPath, noPush, message); err != nil {
		return err
	}

	// Step 10: Update base state for next sync (after successful push)
	// Base state only updates after confirmed push to ensure consistency
	fmt.Println("→ Updating base state...")
	// Reload from exported JSONL to capture any normalization from import/export cycle
	finalIssues, err := loadIssuesFromJSONL(jsonlPath)
	if err != nil {
		return fmt.Errorf("reloading final state: %w", err)
	}
	if err := saveBaseState(beadsDir, finalIssues); err != nil {
		return fmt.Errorf("saving base state: %w", err)
	}
	fmt.Printf("  Saved %d issues to base state\n", len(finalIssues))

	// Step 11: Clear sync state on successful sync
	if bd := beads.FindBeadsDir(); bd != "" {
		_ = ClearSyncState(bd)
	}

	fmt.Println("\n✓ Sync complete")
	return nil
}

// doExportOnlySync handles the --no-pull case: just export, commit, and push
func doExportOnlySync(ctx context.Context, jsonlPath string, noPush bool, message string) error {
	beadsDir := filepath.Dir(jsonlPath)

	// Acquire exclusive lock to prevent concurrent sync corruption
	lockPath := filepath.Join(beadsDir, ".sync.lock")
	lock := flock.New(lockPath)
	locked, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("acquiring sync lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("another sync is in progress")
	}
	defer func() { _ = lock.Unlock() }()

	// Pre-export integrity checks
	if err := ensureStoreActive(); err == nil && store != nil {
		if err := validatePreExport(ctx, store, jsonlPath); err != nil {
			return fmt.Errorf("pre-export validation failed: %w", err)
		}
		if err := checkDuplicateIDs(ctx, store); err != nil {
			return fmt.Errorf("database corruption detected: %w", err)
		}
		if orphaned, err := checkOrphanedDeps(ctx, store); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: orphaned dependency check failed: %v\n", err)
		} else if len(orphaned) > 0 {
			fmt.Fprintf(os.Stderr, "Warning: found %d orphaned dependencies: %v\n", len(orphaned), orphaned)
		}
	}

	// Template validation before export
	if err := validateOpenIssuesForSync(ctx); err != nil {
		return err
	}

	// GH#1173: Detect sync-branch configuration and use appropriate commit method
	sbc := getSyncBranchContext(ctx)

	fmt.Println("→ Exporting pending changes to JSONL...")
	if err := exportToJSONL(ctx, jsonlPath); err != nil {
		return fmt.Errorf("exporting: %w", err)
	}

	// Commit and push using the appropriate method (sync-branch worktree or regular git)
	if err := commitAndPushBeads(ctx, sbc, jsonlPath, noPush, message); err != nil {
		return err
	}

	// Clear sync state on successful sync
	if bd := beads.FindBeadsDir(); bd != "" {
		_ = ClearSyncState(bd)
	}

	fmt.Println("\n✓ Sync complete")
	return nil
}

// writeMergedStateToJSONL writes merged issues to JSONL file
func writeMergedStateToJSONL(path string, issues []*beads.Issue) error {
	tempPath := path + ".tmp"
	file, err := os.Create(tempPath) //nolint:gosec // path is trusted internal beads path
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)

	for _, issue := range issues {
		if err := encoder.Encode(issue); err != nil {
			_ = file.Close() // Best-effort cleanup
			_ = os.Remove(tempPath)
			return err
		}
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(tempPath) // Best-effort cleanup
		return err
	}

	return os.Rename(tempPath, path)
}

// doExportSync exports the current database state based on sync mode.
// - git-portable, realtime: Export to JSONL
// - dolt-native: Commit and push to Dolt remote (skip JSONL)
// - belt-and-suspenders: Both JSONL export and Dolt push
// Does NOT stage or commit to git - that's the user's job.
//
// gt-p1mpqx: Added cheap status check to skip export when there are no changes.
// This reduces overhead when agents call `bd sync` defensively.
func doExportSync(ctx context.Context, jsonlPath string, force, dryRun bool) error {
	if err := ensureStoreActive(); err != nil {
		return fmt.Errorf("failed to initialize store: %w", err)
	}

	shouldExportJSONL := ShouldExportJSONL(ctx, store)
	shouldUseDolt := ShouldUseDoltRemote(ctx, store)

	// gt-p1mpqx: Cheap status check to skip export when there are no changes.
	// This significantly reduces overhead when agents call `bd sync` defensively.
	if !force && !dryRun {
		hasChanges, err := hasUncommittedChanges(ctx, store)
		if err != nil {
			debug.Logf("warning: status check failed: %v (proceeding with export)", err)
		} else if !hasChanges {
			fmt.Println("✓ Already synced (no changes)")
			return nil
		}
	}

	if dryRun {
		if shouldExportJSONL {
			fmt.Println("→ [DRY RUN] Would export database to JSONL")
		}
		if shouldUseDolt {
			fmt.Println("→ [DRY RUN] Would commit and push to Dolt remote")
		}
		return nil
	}

	// Handle Dolt remote operations for dolt-native and belt-and-suspenders modes
	if shouldUseDolt {
		// bd-ma0s.6: Route Dolt commit/push through daemon RPC
		fmt.Println("→ Committing to Dolt (via daemon)...")
		commandDidExplicitDoltCommit = true
		_, err := daemonClient.VcsCommit(&rpc.VcsCommitArgs{Message: "bd sync: auto-commit"})
		if err != nil {
			if !strings.Contains(err.Error(), "nothing to commit") {
				return fmt.Errorf("dolt commit failed: %w", err)
			}
		}

		fmt.Println("→ Pushing to Dolt remote (via daemon)...")
		_, err = daemonClient.VcsPush()
		if err != nil {
			if !strings.Contains(err.Error(), "remote") {
				return fmt.Errorf("dolt push failed: %w", err)
			}
			fmt.Println("⚠ No Dolt remote configured, skipping push")
		} else {
			fmt.Println("✓ Pushed to Dolt remote")
		}
	}

	// Export to JSONL for git-portable, realtime, and belt-and-suspenders modes
	if shouldExportJSONL {
		fmt.Println("Exporting beads to JSONL...")

		// Get count of dirty (changed) issues for incremental tracking
		var changedCount int
		if !force {
			dirtyIDs, err := store.GetDirtyIssues(ctx)
			if err != nil {
				debug.Logf("warning: failed to get dirty issues: %v", err)
			} else {
				changedCount = len(dirtyIDs)
			}
		}

		// Export to JSONL (uses incremental export for large repos)
		result, err := exportToJSONLIncrementalDeferred(ctx, jsonlPath)
		if err != nil {
			return fmt.Errorf("exporting: %w", err)
		}

		// Finalize export (update metadata)
		finalizeExport(ctx, result)

		// Report results
		totalCount := 0
		if result != nil {
			totalCount = len(result.ExportedIDs)
		}

		if changedCount > 0 && !force {
			fmt.Printf("✓ Exported %d issues (%d changed since last sync)\n", totalCount, changedCount)
		} else {
			fmt.Printf("✓ Exported %d issues\n", totalCount)
		}
		fmt.Printf("✓ %s updated\n", jsonlPath)
	}

	return nil
}

// showSyncStateStatus shows the current sync state per the spec.
// Output format:
//
//	Sync mode: git-portable
//	Last export: 2026-01-16 10:30:00 (commit abc123)
//	Pending changes: 3 issues modified since last export
//	Import branch: none
//	Conflicts: none
func showSyncStateStatus(ctx context.Context, jsonlPath string) error {
	if err := ensureStoreActive(); err != nil {
		return fmt.Errorf("failed to initialize store: %w", err)
	}

	beadsDir := filepath.Dir(jsonlPath)

	// Sync mode (from config)
	syncCfg := config.GetSyncConfig()
	fmt.Printf("Sync mode: %s (%s)\n", syncCfg.Mode, SyncModeDescription(string(syncCfg.Mode)))
	fmt.Printf("  Export on: %s, Import on: %s\n", syncCfg.ExportOn, syncCfg.ImportOn)

	// Conflict strategy
	conflictCfg := config.GetConflictConfig()
	fmt.Printf("Conflict strategy: %s\n", conflictCfg.Strategy)

	// Federation config (if set)
	fedCfg := config.GetFederationConfig()
	if fedCfg.Remote != "" {
		fmt.Printf("Federation remote: %s\n", fedCfg.Remote)
		if fedCfg.Sovereignty != "" {
			fmt.Printf("  Sovereignty: %s\n", fedCfg.Sovereignty)
		}
	}

	// Last export time
	lastExport, err := store.GetMetadata(ctx, "last_import_time")
	if err != nil || lastExport == "" {
		fmt.Println("Last export: never")
	} else {
		// Try to parse and format nicely
		t, err := time.Parse(time.RFC3339Nano, lastExport)
		if err != nil {
			fmt.Printf("Last export: %s\n", lastExport)
		} else {
			// Try to get the last commit hash for the JSONL file
			commitHash := getLastJSONLCommitHash(ctx, jsonlPath)
			if commitHash != "" {
				fmt.Printf("Last export: %s (commit %s)\n", t.Format("2006-01-02 15:04:05"), commitHash[:7])
			} else {
				fmt.Printf("Last export: %s\n", t.Format("2006-01-02 15:04:05"))
			}
		}
	}

	// Pending changes (dirty issues)
	dirtyIDs, err := store.GetDirtyIssues(ctx)
	if err != nil {
		fmt.Println("Pending changes: unknown (error getting dirty issues)")
	} else if len(dirtyIDs) == 0 {
		fmt.Println("Pending changes: none")
	} else {
		fmt.Printf("Pending changes: %d issues modified since last export\n", len(dirtyIDs))
	}

	// Import branch (sync branch status)
	syncBranch, _ := syncbranch.Get(ctx, store)
	if syncBranch == "" {
		fmt.Println("Import branch: none")
	} else {
		fmt.Printf("Import branch: %s\n", syncBranch)
	}

	// Conflicts - check for sync conflict state file
	syncConflictPath := filepath.Join(beadsDir, "sync_conflicts.json")
	if _, err := os.Stat(syncConflictPath); err == nil {
		conflictState, err := LoadSyncConflictState(beadsDir)
		if err != nil {
			fmt.Println("Conflicts: unknown (error reading sync state)")
		} else if len(conflictState.Conflicts) > 0 {
			fmt.Printf("Conflicts: %d unresolved\n", len(conflictState.Conflicts))
			for _, c := range conflictState.Conflicts {
				fmt.Printf("  - %s: %s\n", c.IssueID, c.Reason)
			}
		} else {
			fmt.Println("Conflicts: none")
		}
	} else {
		fmt.Println("Conflicts: none")
	}

	return nil
}

// getLastJSONLCommitHash returns the short commit hash of the last commit
// that touched the JSONL file, or empty string if unknown.
func getLastJSONLCommitHash(ctx context.Context, jsonlPath string) string {
	rc, err := beads.GetRepoContext()
	if err != nil {
		return ""
	}

	cmd := rc.GitCmd(ctx, "log", "-1", "--format=%h", "--", jsonlPath)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

// SyncConflictState tracks pending sync conflicts.
type SyncConflictState struct {
	Conflicts []SyncConflictRecord `json:"conflicts,omitempty"`
}

// SyncConflictRecord represents a conflict detected during sync.
type SyncConflictRecord struct {
	IssueID       string `json:"issue_id"`
	Reason        string `json:"reason"`
	LocalVersion  string `json:"local_version,omitempty"`
	RemoteVersion string `json:"remote_version,omitempty"`
	Strategy      string `json:"strategy,omitempty"` // how it was resolved
}

// LoadSyncConflictState loads the sync conflict state from disk.
func LoadSyncConflictState(beadsDir string) (*SyncConflictState, error) {
	path := filepath.Join(beadsDir, "sync_conflicts.json")
	// #nosec G304 -- path is derived from the workspace .beads directory
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SyncConflictState{}, nil
		}
		return nil, err
	}

	var state SyncConflictState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// SaveSyncConflictState saves the sync conflict state to disk.
func SaveSyncConflictState(beadsDir string, state *SyncConflictState) error {
	path := filepath.Join(beadsDir, "sync_conflicts.json")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// ClearSyncConflictState removes the sync conflict state file.
func ClearSyncConflictState(beadsDir string) error {
	path := filepath.Join(beadsDir, "sync_conflicts.json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// resolveSyncConflicts resolves pending sync conflicts using the specified strategy.
// Strategies:
//   - "newest": Keep whichever version has the newer updated_at timestamp (default)
//   - "ours": Keep local version
//   - "theirs": Keep remote version
//   - "manual": Interactive resolution with user prompts
func resolveSyncConflicts(ctx context.Context, jsonlPath string, strategy config.ConflictStrategy, dryRun bool) error {
	beadsDir := filepath.Dir(jsonlPath)

	conflictState, err := LoadSyncConflictState(beadsDir)
	if err != nil {
		return fmt.Errorf("loading sync conflicts: %w", err)
	}

	if len(conflictState.Conflicts) == 0 {
		fmt.Println("No conflicts to resolve")
		return nil
	}

	if dryRun {
		fmt.Printf("→ [DRY RUN] Would resolve %d conflicts using '%s' strategy\n", len(conflictState.Conflicts), strategy)
		for _, c := range conflictState.Conflicts {
			fmt.Printf("  - %s: %s\n", c.IssueID, c.Reason)
		}
		return nil
	}

	fmt.Printf("Resolving conflicts using '%s' strategy...\n", strategy)

	// Load base, local, and remote states for merge
	baseIssues, err := loadBaseState(beadsDir)
	if err != nil {
		return fmt.Errorf("loading base state: %w", err)
	}

	if err := ensureStoreActive(); err != nil {
		return fmt.Errorf("initializing store: %w", err)
	}

	localIssues, err := store.SearchIssues(ctx, "", beads.IssueFilter{IncludeTombstones: true})
	if err != nil {
		return fmt.Errorf("loading local issues: %w", err)
	}

	remoteIssues, err := loadIssuesFromJSONL(jsonlPath)
	if err != nil {
		return fmt.Errorf("loading remote issues: %w", err)
	}

	// Build maps for quick lookup
	baseMap := make(map[string]*beads.Issue)
	for _, issue := range baseIssues {
		baseMap[issue.ID] = issue
	}
	localMap := make(map[string]*beads.Issue)
	for _, issue := range localIssues {
		localMap[issue.ID] = issue
	}
	remoteMap := make(map[string]*beads.Issue)
	for _, issue := range remoteIssues {
		remoteMap[issue.ID] = issue
	}

	// Handle manual strategy with interactive resolution
	if strategy == config.ConflictStrategyManual {
		return resolveSyncConflictsManually(ctx, jsonlPath, beadsDir, conflictState, baseMap, localMap, remoteMap)
	}

	resolved := 0
	for _, conflict := range conflictState.Conflicts {
		local := localMap[conflict.IssueID]
		remote := remoteMap[conflict.IssueID]

		var winner string
		switch strategy {
		case config.ConflictStrategyOurs:
			winner = "local"
		case config.ConflictStrategyTheirs:
			winner = "remote"
		case config.ConflictStrategyNewest:
			fallthrough
		default:
			// Compare updated_at timestamps
			if local != nil && remote != nil {
				if local.UpdatedAt.After(remote.UpdatedAt) {
					winner = "local"
				} else {
					winner = "remote"
				}
			} else if local != nil {
				winner = "local"
			} else {
				winner = "remote"
			}
		}

		fmt.Printf("✓ %s: kept %s", conflict.IssueID, winner)
		if strategy == config.ConflictStrategyNewest {
			fmt.Print(" (newer)")
		}
		fmt.Println()
		resolved++
	}

	// Clear conflicts after resolution
	if err := ClearSyncConflictState(beadsDir); err != nil {
		return fmt.Errorf("clearing conflict state: %w", err)
	}

	// Re-run merge with the resolved conflicts
	mergeResult := MergeIssues(baseIssues, localIssues, remoteIssues)

	// Display any remaining manual conflicts
	if len(mergeResult.ManualConflicts) > 0 {
		displayManualConflicts(mergeResult.ManualConflicts)
	}

	// Write merged state
	if err := writeMergedStateToJSONL(jsonlPath, mergeResult.Merged); err != nil {
		return fmt.Errorf("writing merged state: %w", err)
	}

	// Import to database
	if err := importFromJSONLInline(ctx, jsonlPath, false, false, false); err != nil {
		return fmt.Errorf("importing merged state: %w", err)
	}

	// Export to ensure consistency
	if err := exportToJSONL(ctx, jsonlPath); err != nil {
		return fmt.Errorf("exporting: %w", err)
	}

	// Update base state
	finalIssues, err := loadIssuesFromJSONL(jsonlPath)
	if err != nil {
		return fmt.Errorf("reloading final state: %w", err)
	}
	if err := saveBaseState(beadsDir, finalIssues); err != nil {
		return fmt.Errorf("saving base state: %w", err)
	}

	fmt.Printf("✓ Merge complete (%d conflicts resolved)\n", resolved)

	return nil
}

// resolveSyncConflictsManually handles manual conflict resolution with interactive prompts.
func resolveSyncConflictsManually(ctx context.Context, jsonlPath, beadsDir string, conflictState *SyncConflictState,
	baseMap, localMap, remoteMap map[string]*beads.Issue) error {

	// Build interactive conflicts list
	var interactiveConflicts []InteractiveConflict
	for _, c := range conflictState.Conflicts {
		interactiveConflicts = append(interactiveConflicts, InteractiveConflict{
			IssueID: c.IssueID,
			Local:   localMap[c.IssueID],
			Remote:  remoteMap[c.IssueID],
			Base:    baseMap[c.IssueID],
		})
	}

	// Run interactive resolution
	resolvedIssues, skipped, err := resolveConflictsInteractively(interactiveConflicts)
	if err != nil {
		return fmt.Errorf("interactive resolution: %w", err)
	}

	if skipped > 0 {
		fmt.Printf("\n⚠ %d conflict(s) skipped - will remain unresolved\n", skipped)
	}

	if len(resolvedIssues) == 0 && skipped == len(conflictState.Conflicts) {
		fmt.Println("No conflicts were resolved")
		return nil
	}

	// Build the merged issue list:
	// 1. Start with issues that weren't in conflict
	// 2. Add the resolved issues
	conflictIDSet := make(map[string]bool)
	for _, c := range conflictState.Conflicts {
		conflictIDSet[c.IssueID] = true
	}

	// Build resolved issue map for quick lookup
	resolvedMap := make(map[string]*beads.Issue)
	for _, issue := range resolvedIssues {
		if issue != nil {
			resolvedMap[issue.ID] = issue
		}
	}

	// Collect all unique IDs from base, local, remote
	allIDSet := make(map[string]bool)
	for id := range baseMap {
		allIDSet[id] = true
	}
	for id := range localMap {
		allIDSet[id] = true
	}
	for id := range remoteMap {
		allIDSet[id] = true
	}

	// Build final merged list
	var mergedIssues []*beads.Issue
	for id := range allIDSet {
		if conflictIDSet[id] {
			// This was a conflict
			if resolved, ok := resolvedMap[id]; ok {
				// User resolved this conflict - use their choice
				mergedIssues = append(mergedIssues, resolved)
			} else {
				// Skipped - keep local version in output, conflict remains for later
				if local := localMap[id]; local != nil {
					mergedIssues = append(mergedIssues, local)
				}
			}
		} else {
			// Not a conflict - use standard 3-way merge logic
			local := localMap[id]
			remote := remoteMap[id]
			base := baseMap[id]
			merged, _, _ := MergeIssue(base, local, remote)
			if merged != nil {
				mergedIssues = append(mergedIssues, merged)
			}
		}
	}

	// Clear resolved conflicts (keep skipped ones)
	if skipped == 0 {
		if err := ClearSyncConflictState(beadsDir); err != nil {
			return fmt.Errorf("clearing conflict state: %w", err)
		}
	} else {
		// Update conflict state to only keep skipped conflicts
		var remaining []SyncConflictRecord
		for _, c := range conflictState.Conflicts {
			if _, resolved := resolvedMap[c.IssueID]; !resolved {
				remaining = append(remaining, c)
			}
		}
		conflictState.Conflicts = remaining
		if err := SaveSyncConflictState(beadsDir, conflictState); err != nil {
			return fmt.Errorf("saving updated conflict state: %w", err)
		}
	}

	// Write merged state
	if err := writeMergedStateToJSONL(jsonlPath, mergedIssues); err != nil {
		return fmt.Errorf("writing merged state: %w", err)
	}

	// Import to database
	if err := importFromJSONLInline(ctx, jsonlPath, false, false, false); err != nil {
		return fmt.Errorf("importing merged state: %w", err)
	}

	// Export to ensure consistency
	if err := exportToJSONL(ctx, jsonlPath); err != nil {
		return fmt.Errorf("exporting: %w", err)
	}

	// Update base state
	finalIssues, err := loadIssuesFromJSONL(jsonlPath)
	if err != nil {
		return fmt.Errorf("reloading final state: %w", err)
	}
	if err := saveBaseState(beadsDir, finalIssues); err != nil {
		return fmt.Errorf("saving base state: %w", err)
	}

	resolvedCount := len(resolvedIssues)
	fmt.Printf("\n✓ Manual resolution complete (%d resolved, %d skipped)\n", resolvedCount, skipped)

	return nil
}

// bd-wn2g: RPC-based sync functions

// ensureDirectModeForSync checks that storage is available for sync operations.
// Some sync operations (--full, --import, --resolve, --set-mode) require direct
// database access and cannot run in daemon-only mode.
//
// gt-kfoy7h: When BD_DAEMON_HOST is set (remote daemon mode), sync operations that
// require direct database access are not supported. In remote daemon mode, sync is
// handled automatically by the daemon, so manual sync operations are unnecessary.
func ensureDirectModeForSync() error {
	// gt-kfoy7h: Check if we're connected to a remote daemon via BD_DAEMON_HOST.
	// Remote daemon mode doesn't support direct database access for sync operations.
	if isRemoteDaemon() {
		return fmt.Errorf("sync operations requiring direct database access are not available in remote daemon mode.\n" +
			"When BD_DAEMON_HOST is set, sync is handled automatically by the remote daemon.\n\n" +
			"Available options:\n" +
			"  bd sync --status    Show sync status (works with remote daemon)\n" +
			"  bd sync             Export changes (works with remote daemon)\n\n" +
			"Operations like --full, --import, --resolve, and --set-mode require local database access.\n" +
			"To use these operations, unset BD_DAEMON_HOST and run against a local database.")
	}

	if store == nil {
		return fmt.Errorf("sync operation requires database access; ensure daemon is running")
	}
	return nil
}

// doSyncExportViaDaemon performs sync export via the daemon RPC.
// Returns nil on success, error if it fails and should fall back to direct mode.
func doSyncExportViaDaemon(force, dryRun bool) error {
	args := &rpc.SyncExportArgs{
		Force:  force,
		DryRun: dryRun,
	}

	result, err := daemonClient.SyncExport(args)
	if err != nil {
		return fmt.Errorf("daemon sync export failed: %w", err)
	}

	// Display results
	if result.Skipped {
		fmt.Println("✓ Already synced (no changes)")
	} else if dryRun {
		fmt.Println(result.Message)
	} else {
		if result.ChangedCount > 0 && !force {
			fmt.Printf("✓ Exported %d issues (%d changed since last sync)\n", result.ExportedCount, result.ChangedCount)
		} else {
			fmt.Printf("✓ Exported %d issues\n", result.ExportedCount)
		}
		if result.JSONLPath != "" {
			fmt.Printf("✓ %s updated\n", result.JSONLPath)
		}
	}

	return nil
}

// doSyncStatusViaDaemon shows sync status via the daemon RPC.
// Returns nil on success, error if it fails and should fall back to direct mode.
func doSyncStatusViaDaemon() error {
	result, err := daemonClient.SyncStatus(&rpc.SyncStatusArgs{})
	if err != nil {
		return fmt.Errorf("daemon sync status failed: %w", err)
	}

	// Display status in the same format as showSyncStateStatus
	fmt.Printf("Sync mode: %s (%s)\n", result.SyncMode, result.SyncModeDesc)
	fmt.Printf("  Export on: %s, Import on: %s\n", result.ExportOn, result.ImportOn)
	fmt.Printf("Conflict strategy: %s\n", result.ConflictStrategy)

	if result.FederationRemote != "" {
		fmt.Printf("Federation remote: %s\n", result.FederationRemote)
	}

	if result.LastExport == "" {
		fmt.Println("Last export: never")
	} else {
		if result.LastExportCommit != "" {
			fmt.Printf("Last export: %s (commit %s)\n", result.LastExport, result.LastExportCommit)
		} else {
			fmt.Printf("Last export: %s\n", result.LastExport)
		}
	}

	if result.PendingChanges == 0 {
		fmt.Println("Pending changes: none")
	} else {
		fmt.Printf("Pending changes: %d issues modified since last export\n", result.PendingChanges)
	}

	if result.SyncBranch == "" {
		fmt.Println("Import branch: none")
	} else {
		fmt.Printf("Import branch: %s\n", result.SyncBranch)
	}

	if result.ConflictCount == 0 {
		fmt.Println("Conflicts: none")
	} else {
		fmt.Printf("Conflicts: %d unresolved\n", result.ConflictCount)
	}

	return nil
}

func init() {
	syncCmd.Flags().StringP("message", "m", "", "Commit message (default: auto-generated)")
	syncCmd.Flags().Bool("dry-run", false, "Preview sync without making changes")
	syncCmd.Flags().Bool("no-push", false, "Skip pushing to remote")
	syncCmd.Flags().Bool("no-pull", false, "Skip pulling from remote")
	syncCmd.Flags().Bool("rename-on-import", false, "Rename imported issues to match database prefix (updates all references)")
	syncCmd.Flags().Bool("flush-only", false, "Only export pending changes to JSONL (skip git operations)")
	syncCmd.Flags().Bool("squash", false, "Accumulate changes in JSONL without committing (run 'bd sync' later to commit all)")
	syncCmd.Flags().Bool("import-only", false, "Only import from JSONL (skip git operations, useful after git pull)")
	syncCmd.Flags().Bool("import", false, "Import from JSONL (shorthand for --import-only)")
	syncCmd.Flags().Bool("status", false, "Show sync state (pending changes, last export, conflicts)")
	syncCmd.Flags().Bool("merge", false, "Merge sync branch back to main branch")
	syncCmd.Flags().Bool("from-main", false, "One-way sync from main branch (for ephemeral branches without upstream)")
	syncCmd.Flags().Bool("no-git-history", false, "Skip git history backfill for deletions (use during JSONL filename migrations)")
	syncCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output sync statistics in JSON format")
	syncCmd.Flags().Bool("check", false, "Pre-sync integrity check: detect forced pushes, prefix mismatches, and orphaned issues")
	syncCmd.Flags().Bool("accept-rebase", false, "Accept remote sync branch history (use when force-push detected)")
	syncCmd.Flags().Bool("full", false, "Full sync: pull → merge → export → commit → push (legacy behavior)")
	syncCmd.Flags().Bool("resolve", false, "Resolve pending sync conflicts")
	syncCmd.Flags().Bool("ours", false, "Use 'ours' strategy for conflict resolution (with --resolve)")
	syncCmd.Flags().Bool("theirs", false, "Use 'theirs' strategy for conflict resolution (with --resolve)")
	syncCmd.Flags().Bool("manual", false, "Use interactive manual resolution for conflicts (with --resolve)")
	syncCmd.Flags().Bool("force", false, "Force full export/import (skip incremental optimization)")
	syncCmd.Flags().String("set-mode", "", "Set sync mode (git-portable, realtime, dolt-native, belt-and-suspenders)")
	rootCmd.AddCommand(syncCmd)
}

// Git helper functions moved to sync_git.go

// doSyncFromMain function moved to sync_import.go
// Export function moved to sync_export.go
// Sync branch functions moved to sync_branch.go
// Import functions moved to sync_import.go
// External beads dir functions moved to sync_branch.go
// Integrity check types and functions moved to sync_check.go
