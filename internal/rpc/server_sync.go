// Package rpc provides RPC server handlers for sync operations (bd-wn2g).
// These handlers enable bd sync to work in daemon mode without falling back to direct mode.
package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/export"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/syncbranch"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

// handleSyncExport handles the sync_export RPC operation (bd-wn2g).
// This is the daemon-side implementation of `bd sync` default behavior (export to JSONL).
// Uses single-flight guard to prevent concurrent exports from piling up
// slow IN-clause queries that crush Dolt CPU.
func (s *Server) handleSyncExport(req *Request) Response {
	// Single-flight guard: only one export at a time
	if !s.exportInProgress.CompareAndSwap(false, true) {
		data, _ := json.Marshal(SyncExportResult{
			Skipped: true,
			Message: "export already in progress",
		})
		return Response{Success: true, Data: data}
	}
	defer s.exportInProgress.Store(false)

	var args SyncExportArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	store := s.storage

	// Get sync mode configuration
	syncMode := getSyncModeFromStore(ctx, store)
	shouldExportJSONL := syncMode != string(config.SyncModeDoltNative)

	// Find JSONL path
	jsonlPath := s.findJSONLPath()
	if jsonlPath == "" {
		return Response{Success: false, Error: "not in a bd workspace (no .beads directory found)"}
	}

	// Cheap status check: skip export when there are no changes (unless forced)
	if !args.Force && !args.DryRun {
		hasChanges, err := hasUncommittedChangesInStore(ctx, store)
		if err != nil {
			// Log warning but continue with export
			fmt.Fprintf(os.Stderr, "Warning: status check failed: %v (proceeding with export)\n", err)
		} else if !hasChanges {
			result := SyncExportResult{
				ExportedCount: 0,
				ChangedCount:  0,
				JSONLPath:     jsonlPath,
				Skipped:       true,
				Message:       "Already synced (no changes)",
			}
			data, _ := json.Marshal(result)
			return Response{Success: true, Data: data}
		}
	}

	// Dry-run mode: just report what would happen
	if args.DryRun {
		dirtyCount := 0
		if dirtyIDs, err := store.GetDirtyIssues(ctx); err == nil {
			dirtyCount = len(dirtyIDs)
		}
		result := SyncExportResult{
			ExportedCount: 0,
			ChangedCount:  dirtyCount,
			JSONLPath:     jsonlPath,
			Message:       fmt.Sprintf("[DRY RUN] Would export %d changed issues to JSONL", dirtyCount),
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	// Handle Dolt remote operations for dolt-native and belt-and-suspenders modes
	shouldUseDolt := syncMode == string(config.SyncModeDoltNative) || syncMode == string(config.SyncModeBeltAndSuspenders)
	if shouldUseDolt {
		if err := s.handleDoltSync(ctx, store, syncMode); err != nil {
			return Response{Success: false, Error: err.Error()}
		}
	}

	// Export to JSONL for git-portable, realtime, and belt-and-suspenders modes
	if !shouldExportJSONL {
		// dolt-native mode: no JSONL export
		result := SyncExportResult{
			ExportedCount: 0,
			ChangedCount:  0,
			JSONLPath:     "",
			Message:       "Dolt-native mode: skipped JSONL export",
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	// Get count of dirty (changed) issues for incremental tracking
	var changedCount int
	if !args.Force {
		if dirtyIDs, err := store.GetDirtyIssues(ctx); err == nil {
			changedCount = len(dirtyIDs)
		}
	}

	// Perform the export
	exportedCount, err := s.performSyncExport(ctx, store, jsonlPath)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("export failed: %v", err)}
	}

	result := SyncExportResult{
		ExportedCount: exportedCount,
		ChangedCount:  changedCount,
		JSONLPath:     jsonlPath,
		Message:       fmt.Sprintf("Exported %d issues to JSONL", exportedCount),
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleSyncStatus handles the sync_status RPC operation (bd-wn2g).
// This is the daemon-side implementation of `bd sync --status`.
func (s *Server) handleSyncStatus(req *Request) Response {
	ctx, cancel := s.reqCtx(req)
	defer cancel()
	store := s.storage

	// Find JSONL path for conflict check
	jsonlPath := s.findJSONLPath()
	beadsDir := ""
	if jsonlPath != "" {
		beadsDir = filepath.Dir(jsonlPath)
	}

	// Get sync configuration
	syncCfg := config.GetSyncConfig()
	conflictCfg := config.GetConflictConfig()
	fedCfg := config.GetFederationConfig()

	// Get sync mode (check both config.yaml and database)
	syncMode := getSyncModeFromStore(ctx, store)
	syncModeDesc := getSyncModeDescription(syncMode)

	// Get last export time
	lastExport := ""
	lastExportCommit := ""
	if exportTime, err := store.GetMetadata(ctx, "last_import_time"); err == nil && exportTime != "" {
		if t, err := time.Parse(time.RFC3339Nano, exportTime); err == nil {
			lastExport = t.Format("2006-01-02 15:04:05")
		} else {
			lastExport = exportTime
		}
	}

	// Get pending changes (dirty issues)
	pendingChanges := 0
	if dirtyIDs, err := store.GetDirtyIssues(ctx); err == nil {
		pendingChanges = len(dirtyIDs)
	}

	// Get sync branch
	syncBranch := ""
	if sb, err := syncbranch.Get(ctx, store); err == nil {
		syncBranch = sb
	}

	// Check for conflicts
	conflictCount := 0
	if beadsDir != "" {
		if conflicts, err := loadSyncConflictCount(beadsDir); err == nil {
			conflictCount = conflicts
		}
	}

	result := SyncStatusResult{
		SyncMode:         string(syncCfg.Mode),
		SyncModeDesc:     syncModeDesc,
		ExportOn:         syncCfg.ExportOn,
		ImportOn:         syncCfg.ImportOn,
		ConflictStrategy: string(conflictCfg.Strategy),
		LastExport:       lastExport,
		LastExportCommit: lastExportCommit,
		PendingChanges:   pendingChanges,
		SyncBranch:       syncBranch,
		ConflictCount:    conflictCount,
		FederationRemote: fedCfg.Remote,
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// findJSONLPath finds the JSONL file path for the current workspace.
func (s *Server) findJSONLPath() string {
	// Use workspace path if available
	if s.workspacePath != "" {
		beadsDir := filepath.Join(s.workspacePath, ".beads")
		return utils.FindJSONLInDir(beadsDir)
	}

	// Fall back to database directory
	if s.dbPath != "" {
		dbDir := filepath.Dir(s.dbPath)
		return utils.FindJSONLInDir(dbDir)
	}

	return ""
}

// performSyncExport exports issues to JSONL and returns the count.
func (s *Server) performSyncExport(ctx context.Context, store storage.Storage, jsonlPath string) (int, error) {
	cycleStart := time.Now()

	// Load export configuration
	cfg, err := export.LoadConfig(ctx, store, false)
	if err != nil {
		return 0, fmt.Errorf("failed to load export config: %w", err)
	}

	// Get all issues including tombstones
	fetchStart := time.Now()
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{IncludeTombstones: true})
	if err != nil {
		return 0, fmt.Errorf("failed to get issues: %w", err)
	}
	debug.Logf("sync-export: fetched %d issues in %v", len(issues), time.Since(fetchStart))

	// Sort by ID for consistent output
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].ID < issues[j].ID
	})

	// Populate dependencies
	var allDeps map[string][]*types.Dependency
	depsStart := time.Now()
	result := export.FetchWithPolicy(ctx, cfg, export.DataTypeCore, "get dependencies", func() error {
		var err error
		allDeps, err = store.GetAllDependencyRecords(ctx)
		return err
	})
	if result.Err != nil {
		return 0, fmt.Errorf("failed to get dependencies: %w", result.Err)
	}
	debug.Logf("sync-export: fetched dependencies in %v", time.Since(depsStart))
	for _, issue := range issues {
		issue.Dependencies = allDeps[issue.ID]
	}

	// Populate labels (from in-memory cache)
	issueIDs := make([]string, len(issues))
	for i, issue := range issues {
		issueIDs[i] = issue.ID
	}
	var allLabels map[string][]string
	labelsStart := time.Now()
	result = export.FetchWithPolicy(ctx, cfg, export.DataTypeLabels, "get labels", func() error {
		var err error
		allLabels, err = s.labelCache.GetLabelsForIssues(ctx, issueIDs)
		return err
	})
	if result.Err != nil {
		return 0, fmt.Errorf("failed to get labels: %w", result.Err)
	}
	debug.Logf("sync-export: fetched labels for %d issues in %v (cache=%v)", len(issueIDs), time.Since(labelsStart), s.labelCache.IsPopulated())
	if !result.Success {
		allLabels = make(map[string][]string)
	}
	for _, issue := range issues {
		issue.Labels = allLabels[issue.ID]
	}

	// Populate comments
	var allComments map[string][]*types.Comment
	commentsStart := time.Now()
	result = export.FetchWithPolicy(ctx, cfg, export.DataTypeComments, "get comments", func() error {
		var err error
		allComments, err = store.GetCommentsForIssues(ctx, issueIDs)
		return err
	})
	if result.Err != nil {
		return 0, fmt.Errorf("failed to get comments: %w", result.Err)
	}
	debug.Logf("sync-export: fetched comments in %v", time.Since(commentsStart))
	if !result.Success {
		allComments = make(map[string][]*types.Comment)
	}
	for _, issue := range issues {
		issue.Comments = allComments[issue.ID]
	}

	// Create temp file for atomic write
	dir := filepath.Dir(jsonlPath)
	base := filepath.Base(jsonlPath)
	tempFile, err := os.CreateTemp(dir, base+".tmp.*")
	if err != nil {
		return 0, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}()

	// Write JSONL
	encoder := json.NewEncoder(tempFile)
	exportedIDs := make([]string, 0, len(issues))
	for _, issue := range issues {
		if err := encoder.Encode(issue); err != nil {
			return 0, fmt.Errorf("failed to encode issue %s: %w", issue.ID, err)
		}
		exportedIDs = append(exportedIDs, issue.ID)
	}

	// Close and rename
	if err := tempFile.Close(); err != nil {
		return 0, fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tempPath, jsonlPath); err != nil {
		return 0, fmt.Errorf("failed to replace JSONL file: %w", err)
	}

	// Clear dirty flags
	if err := store.ClearDirtyIssuesByID(ctx, exportedIDs); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to clear dirty flags: %v\n", err)
	}

	// Update export hashes in a single transaction (bd-8csx.1: reduces N Dolt commits to 1)
	exportHashes := make(map[string]string, len(issues))
	for _, issue := range issues {
		if issue.ContentHash != "" {
			exportHashes[issue.ID] = issue.ContentHash
		}
	}
	if len(exportHashes) > 0 {
		if err := store.BatchSetExportHashes(ctx, exportHashes); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to batch set export hashes: %v\n", err)
		}
	}

	debug.Logf("sync-export: complete cycle=%v issues=%d", time.Since(cycleStart), len(exportedIDs))
	return len(exportedIDs), nil
}

// handleDoltSync handles Dolt commit/push for dolt-native and belt-and-suspenders modes.
func (s *Server) handleDoltSync(ctx context.Context, store storage.Storage, syncMode string) error {
	rs, ok := storage.AsRemote(store)
	if !ok {
		if syncMode == string(config.SyncModeDoltNative) {
			return fmt.Errorf("dolt-native sync mode requires Dolt backend")
		}
		// belt-and-suspenders: warn but continue with JSONL
		fmt.Fprintln(os.Stderr, "Warning: Dolt remote not available, falling back to JSONL-only")
		return nil
	}

	// Commit to Dolt
	if err := rs.Commit(ctx, "bd sync: auto-commit"); err != nil {
		// Ignore "nothing to commit" errors
		if err.Error() != "nothing to commit" {
			return fmt.Errorf("dolt commit failed: %w", err)
		}
	}

	// Push to Dolt remote
	if err := rs.Push(ctx); err != nil {
		// Don't fail if no remote configured
		if err.Error() != "remote" {
			return fmt.Errorf("dolt push failed: %w", err)
		}
		fmt.Fprintln(os.Stderr, "Warning: No Dolt remote configured, skipping push")
	}

	return nil
}

// Helper functions

// getSyncModeFromStore gets the sync mode, checking config.yaml first then database.
func getSyncModeFromStore(ctx context.Context, s storage.Storage) string {
	// First check config.yaml
	yamlMode := config.GetSyncMode()
	if yamlMode != "" && yamlMode != config.SyncModeGitPortable {
		return string(yamlMode)
	}

	// Fall back to database
	mode, err := s.GetConfig(ctx, "sync.mode")
	if err == nil && mode != "" {
		if config.IsValidSyncMode(mode) {
			return mode
		}
	}

	// Check storage-backend for Dolt default
	storageBackend := config.GetString("storage-backend")
	if storageBackend == "dolt" {
		return string(config.SyncModeDoltNative)
	}

	return string(config.SyncModeGitPortable)
}

// getSyncModeDescription returns a human-readable description.
func getSyncModeDescription(mode string) string {
	switch mode {
	case string(config.SyncModeGitPortable):
		return "JSONL exported on push, imported on pull"
	case string(config.SyncModeRealtime):
		return "JSONL exported on every change"
	case string(config.SyncModeDoltNative):
		return "Dolt remotes only, no JSONL"
	case string(config.SyncModeBeltAndSuspenders):
		return "Both Dolt remotes and JSONL"
	default:
		return "unknown mode"
	}
}

// hasUncommittedChangesInStore checks if there are uncommitted changes in the store.
func hasUncommittedChangesInStore(ctx context.Context, s storage.Storage) (bool, error) {
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

// loadSyncConflictCount loads the number of unresolved sync conflicts.
func loadSyncConflictCount(beadsDir string) (int, error) {
	conflictPath := filepath.Join(beadsDir, "sync_conflicts.json")
	data, err := os.ReadFile(conflictPath) // #nosec G304 - trusted path
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var state struct {
		Conflicts []struct{} `json:"conflicts"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return 0, err
	}

	return len(state.Conflicts), nil
}

// Compile-time check that SQLiteStorage implements storage.Storage
var _ storage.Storage = (*sqlite.SQLiteStorage)(nil)
