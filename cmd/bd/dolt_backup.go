package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// maybeBackup exports all issues to JSONL if enough time has elapsed since the last backup.
// This is a check-on-write pattern: called after each successful auto-commit, not a daemon.
// Failures are warnings only (stderr) — a failed backup must never block local work.
func maybeBackup(ctx context.Context) {
	interval, err := getDoltBackupInterval()
	if err != nil || interval <= 0 {
		return
	}

	st := getStore()
	if st == nil {
		return
	}

	// Check when last backup ran.
	lastStr, err := st.GetMetadata(ctx, "last_backup_at")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: dolt backup: failed to read last_backup_at: %v\n", err)
		return
	}
	if lastStr != "" {
		lastTime, parseErr := time.Parse(time.RFC3339, lastStr)
		if parseErr == nil && time.Since(lastTime) < interval {
			return // not yet time
		}
	}

	if err := exportBackupJSONL(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: dolt backup failed: %v\n", err)
		return
	}

	// Update timestamp after successful backup.
	if err := st.SetMetadata(ctx, "last_backup_at", time.Now().Format(time.RFC3339)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: dolt backup: failed to update last_backup_at: %v\n", err)
	}
}

// exportBackupJSONL writes all issues (with labels, dependencies, comments) to
// .beads/backup/issues.jsonl. Uses atomic write (temp file + rename).
func exportBackupJSONL(ctx context.Context) error {
	st := getStore()
	if st == nil {
		return fmt.Errorf("no store available")
	}

	// 1. Fetch all issues.
	issues, err := st.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return fmt.Errorf("fetching issues: %w", err)
	}
	if len(issues) == 0 {
		return nil // nothing to back up
	}

	// 2. Collect IDs for batch lookups.
	ids := make([]string, len(issues))
	issueMap := make(map[string]*types.Issue, len(issues))
	for i, iss := range issues {
		ids[i] = iss.ID
		issueMap[iss.ID] = iss
	}

	// 3. Batch-fetch labels.
	labelsMap, err := st.GetLabelsForIssues(ctx, ids)
	if err != nil {
		return fmt.Errorf("fetching labels: %w", err)
	}
	for id, labels := range labelsMap {
		if iss, ok := issueMap[id]; ok {
			iss.Labels = labels
		}
	}

	// 4. Batch-fetch dependencies.
	depsMap, err := st.GetAllDependencyRecords(ctx)
	if err != nil {
		return fmt.Errorf("fetching dependencies: %w", err)
	}
	for id, deps := range depsMap {
		if iss, ok := issueMap[id]; ok {
			iss.Dependencies = deps
		}
	}

	// 5. Batch-fetch comments.
	commentsMap, err := st.GetCommentsForIssues(ctx, ids)
	if err != nil {
		return fmt.Errorf("fetching comments: %w", err)
	}
	for id, comments := range commentsMap {
		if iss, ok := issueMap[id]; ok {
			iss.Comments = comments
		}
	}

	// 6. Write JSONL atomically.
	beadsDir := filepath.Dir(dbPath)
	backupDir := filepath.Join(beadsDir, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return fmt.Errorf("creating backup dir: %w", err)
	}

	targetPath := filepath.Join(backupDir, "issues.jsonl")
	tmpPath := targetPath + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}

	enc := json.NewEncoder(f)
	for _, iss := range issues {
		if encErr := enc.Encode(iss); encErr != nil {
			_ = f.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("encoding issue %s: %w", iss.ID, encErr)
		}
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming backup file: %w", err)
	}

	return nil
}
