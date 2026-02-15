package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/validation"
)

// resolveNoGitHistoryForFromMain returns the effective noGitHistory flag.
// When fromMain is true, noGitHistory defaults to true (fixes #417).
func resolveNoGitHistoryForFromMain(fromMain, noGitHistory bool) bool {
	if fromMain && !noGitHistory {
		return true
	}
	return noGitHistory
}

// exportToJSONL exports the current database state to the JSONL file.
// Delegates to exportToJSONLWithStore using the global store.
func exportToJSONL(ctx context.Context, jsonlPath string) error {
	if err := ensureStoreActive(); err != nil {
		return fmt.Errorf("activating store: %w", err)
	}
	if err := exportToJSONLWithStore(ctx, store, jsonlPath); err != nil {
		return err
	}
	// Update content hash metadata after successful export
	repoKey := getRepoKeyForPath(jsonlPath)
	updateExportMetadata(ctx, store, jsonlPath, slog.Default(), repoKey)
	return nil
}

// importFromJSONL imports issues from a JSONL file into the database.
// Parameters renameOnImport and noGitHistory are accepted for API compatibility
// but are no longer used in the Dolt-only world.
func importFromJSONL(ctx context.Context, jsonlPath string, _, _ bool) error {
	if err := ensureStoreActive(); err != nil {
		return fmt.Errorf("activating store: %w", err)
	}
	return importToJSONLWithStore(ctx, store, jsonlPath)
}

// importFromJSONLInline imports issues from JSONL inline (without subprocess).
// This was the inline variant of importFromJSONL to avoid subprocess path resolution
// issues. In the Dolt-only world, both functions are equivalent.
func importFromJSONLInline(ctx context.Context, jsonlPath string, renameOnImport, noGitHistory, _ bool) error {
	return importFromJSONL(ctx, jsonlPath, renameOnImport, noGitHistory)
}

// loadIssuesFromJSONL reads a JSONL file and returns the parsed issues.
func loadIssuesFromJSONL(jsonlPath string) ([]*types.Issue, error) {
	file, err := os.Open(jsonlPath) // #nosec G304 - controlled path
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening JSONL: %w", err)
	}
	defer file.Close()

	var issues []*types.Issue
	scanner := bufio.NewScanner(file)
	const maxScannerBuffer = 1024 * 1024 // 1MB
	scanner.Buffer(make([]byte, 0, maxScannerBuffer), maxScannerBuffer)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}

		var issue types.Issue
		if err := json.Unmarshal([]byte(line), &issue); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse JSONL line %d: %v\n", lineNum, err)
			continue
		}
		issues = append(issues, &issue)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading JSONL: %w", err)
	}

	return issues, nil
}

// doSyncFromMain performs a one-way sync from the main branch.
// Reads the JSONL from main and imports it into the current database.
func doSyncFromMain(ctx context.Context, jsonlPath string, renameOnImport, dryRun, noGitHistory bool) error {
	if dryRun {
		fmt.Println("→ [DRY RUN] Would sync from main branch")
		return nil
	}

	// Read JSONL from main branch via git
	relPath, err := getRelativeGitPath(jsonlPath)
	if err != nil {
		return fmt.Errorf("getting relative path: %w", err)
	}

	mainData, err := readFromGitRef(relPath, "main")
	if err != nil {
		// Try "master" as fallback
		mainData, err = readFromGitRef(relPath, "master")
		if err != nil {
			return fmt.Errorf("reading JSONL from main branch: %w", err)
		}
	}

	// Write main branch content to a temp file for import
	tmpFile, err := os.CreateTemp("", "bd-sync-from-main-*.jsonl")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(mainData); err != nil {
		tmpFile.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	tmpFile.Close()

	// Import from the temp file
	fmt.Println("→ Importing from main branch...")
	if err := importFromJSONL(ctx, tmpPath, renameOnImport, noGitHistory); err != nil {
		return fmt.Errorf("importing from main: %w", err)
	}

	// Export back to update local JSONL
	fmt.Println("→ Exporting to JSONL...")
	if err := exportToJSONL(ctx, jsonlPath); err != nil {
		return fmt.Errorf("exporting: %w", err)
	}

	fmt.Println("✓ Synced from main branch")
	return nil
}

// getRelativeGitPath converts an absolute path to a git-relative path.
func getRelativeGitPath(absPath string) (string, error) {
	// Use the beads dir to find the git root
	beadsDir := findJSONLPath()
	if beadsDir == "" {
		return absPath, nil // fallback
	}
	// Git show needs repo-relative paths
	// Simple approach: try the path as-is and let git figure it out
	return absPath, nil
}

// ExportResult describes the outcome of an incremental export operation.
type ExportResult struct {
	ExportedIDs []string
}

// exportToJSONLIncrementalDeferred performs an export and returns the result.
// In the Dolt-only world, this delegates to the standard export.
func exportToJSONLIncrementalDeferred(ctx context.Context, jsonlPath string) (*ExportResult, error) {
	if err := ensureStoreActive(); err != nil {
		return nil, fmt.Errorf("activating store: %w", err)
	}

	// Get all issues for the result
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return nil, fmt.Errorf("getting issues: %w", err)
	}

	// Perform the export
	if err := exportToJSONLWithStore(ctx, store, jsonlPath); err != nil {
		return nil, err
	}

	// Build result
	result := &ExportResult{
		ExportedIDs: make([]string, 0, len(issues)),
	}
	for _, issue := range issues {
		result.ExportedIDs = append(result.ExportedIDs, issue.ID)
	}

	return result, nil
}

// validateOpenIssuesForSync checks that open issues pass template validation before sync.
// Controlled by the "validation.on-sync" config key:
//   - "none" or "": skip validation (default)
//   - "warn": validate and warn but don't block
//   - "error": validate and block sync if issues fail
func validateOpenIssuesForSync(ctx context.Context) error {
	mode := config.GetString("validation.on-sync")
	if mode == "" || mode == "none" {
		return nil
	}

	if err := ensureStoreActive(); err != nil {
		return nil // Can't validate without store, skip
	}

	openStatus := types.StatusOpen
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{Status: &openStatus})
	if err != nil {
		return nil // Skip validation on error
	}

	var invalidCount int
	for _, issue := range issues {
		// Skip chores - they don't have required sections
		if issue.IssueType == types.TypeChore {
			continue
		}
		if err := validation.ValidateTemplate(issue.IssueType, issue.Description); err != nil {
			invalidCount++
			if mode == "warn" || mode == "error" {
				fmt.Fprintf(os.Stderr, "Warning: %s fails template validation: %v\n", issue.ID, err)
			}
		}
	}

	if invalidCount > 0 && mode == "error" {
		return fmt.Errorf("%d open issue(s) fail template validation (set validation.on-sync=warn to allow)", invalidCount)
	}

	return nil
}

// finalizeExport updates metadata after a successful export.
func finalizeExport(ctx context.Context, result *ExportResult) {
	if result == nil || store == nil {
		return
	}
	jsonlPath := findJSONLPath()
	if jsonlPath == "" {
		return
	}
	repoKey := getRepoKeyForPath(jsonlPath)
	updateExportMetadata(ctx, store, jsonlPath, slog.Default(), repoKey)
}
