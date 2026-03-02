package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

const (
	importModeUpsert = "upsert"
	importModeStrict = "strict"
)

// ImportOptions configures import behavior.
type ImportOptions struct {
	DryRun                     bool
	SkipUpdate                 bool
	Strict                     bool
	Mode                       string
	IncludeWisps               bool
	RenameOnImport             bool
	ClearDuplicateExternalRefs bool
	OrphanHandling             string
	DeletionIDs                []string
	SkipPrefixValidation       bool
	ProtectLocalExportIDs      map[string]time.Time
}

// ImportResult describes what an import operation did.
type ImportResult struct {
	Created             int
	Updated             int
	Unchanged           int
	Skipped             int
	Deleted             int
	Collisions          int
	IDMapping           map[string]string
	CollisionIDs        []string
	PrefixMismatch      bool
	ExpectedPrefix      string
	MismatchPrefixes    map[string]int
	SkippedDependencies []string
}

// importIssuesCore imports issues into the Dolt store.
// Import semantics:
// - Default mode is upsert.
// - Strict mode fails if any imported ID already exists in the target table.
// - Upsert mode replaces labels/dependencies/comments for imported IDs (source-authoritative).
func importIssuesCore(ctx context.Context, _ string, store *dolt.DoltStore, issues []*types.Issue, opts ImportOptions) (*ImportResult, error) {
	if len(issues) == 0 {
		return &ImportResult{}, nil
	}

	mode, err := resolveImportMode(opts)
	if err != nil {
		return nil, err
	}
	orphanHandling, err := parseOrphanHandling(opts.OrphanHandling)
	if err != nil {
		return nil, err
	}

	filtered := make([]*types.Issue, 0, len(issues))
	skipped := 0
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		if !opts.IncludeWisps && issueRoutesToWisp(ctx, store, issue) {
			skipped++
			continue
		}
		filtered = append(filtered, issue)
	}

	if opts.DryRun {
		return &ImportResult{Skipped: skipped + len(filtered)}, nil
	}
	if len(filtered) == 0 {
		return &ImportResult{Skipped: skipped}, nil
	}

	existing, err := findExistingIssueIDs(ctx, store, filtered)
	if err != nil {
		return nil, err
	}
	if mode == importModeStrict {
		for _, issue := range filtered {
			if existing[issue.ID] {
				return nil, fmt.Errorf("strict mode: issue %s already exists", issue.ID)
			}
		}
	}

	issuesForCreate := make([]*types.Issue, 0, len(filtered))
	for _, issue := range filtered {
		clone := *issue
		clone.Labels = nil
		clone.Dependencies = nil
		clone.Comments = nil
		issuesForCreate = append(issuesForCreate, &clone)
	}

	err = store.CreateIssuesWithFullOptions(ctx, issuesForCreate, getActorWithGit(), storage.BatchCreateOptions{
		OrphanHandling:       orphanHandling,
		SkipPrefixValidation: opts.SkipPrefixValidation,
	})
	if err != nil {
		return nil, err
	}

	skippedDeps, err := reconcileIssueRelations(ctx, store, filtered, mode, orphanHandling)
	if err != nil {
		return nil, err
	}

	result := &ImportResult{
		Skipped:             skipped,
		SkippedDependencies: skippedDeps,
	}

	for _, issue := range filtered {
		if existing[issue.ID] {
			result.Updated++
		} else {
			result.Created++
		}
	}

	return result, nil
}

// importFromLocalJSONL imports issues from a local JSONL file on disk into the Dolt store.
// Unlike git-based import, this reads from the current working tree, preserving
// any manual cleanup done to the JSONL file (e.g., via bd compact --purge-tombstones).
// Returns the number of issues imported and any error.
func importFromLocalJSONL(ctx context.Context, store *dolt.DoltStore, localPath string) (int, error) {
	result, err := importFromLocalJSONLWithOptions(ctx, store, localPath, ImportOptions{
		Mode:                 importModeUpsert,
		IncludeWisps:         true,
		OrphanHandling:       string(storage.OrphanAllow),
		SkipPrefixValidation: true,
	})
	if err != nil {
		return 0, err
	}

	return result.Created + result.Updated, nil
}

func importFromLocalJSONLWithOptions(ctx context.Context, store *dolt.DoltStore, localPath string, opts ImportOptions) (*ImportResult, error) {
	issues, err := parseIssuesFromImportJSONL(localPath)
	if err != nil {
		return nil, err
	}

	if len(issues) == 0 {
		return &ImportResult{}, nil
	}

	// Auto-detect prefix from first issue if not already configured
	configuredPrefix, err := store.GetConfig(ctx, "issue_prefix")
	if err == nil && strings.TrimSpace(configuredPrefix) == "" {
		firstPrefix := utils.ExtractIssuePrefix(issues[0].ID)
		if firstPrefix != "" {
			if err := store.SetConfig(ctx, "issue_prefix", firstPrefix); err != nil {
				return nil, fmt.Errorf("failed to set issue_prefix from imported issues: %w", err)
			}
		}
	}

	result, err := importIssuesCore(ctx, "", store, issues, opts)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func parseIssuesFromImportJSONL(localPath string) ([]*types.Issue, error) {
	//nolint:gosec // G304: path from user-provided CLI argument
	f, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read JSONL file %s: %w", localPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)

	var issues []*types.Issue
	seen := make(map[string]int)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		issue, err := parseIssueJSONLLine([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("failed to parse issue at line %d: %w", lineNum, err)
		}

		if firstLine, ok := seen[issue.ID]; ok {
			return nil, fmt.Errorf("duplicate issue id %q at line %d (first seen at line %d)", issue.ID, lineNum, firstLine)
		}
		seen[issue.ID] = lineNum
		issues = append(issues, issue)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan JSONL: %w", err)
	}

	return issues, nil
}

func parseIssueJSONLLine(line []byte) (*types.Issue, error) {
	var issue types.Issue
	if err := json.Unmarshal(line, &issue); err != nil {
		return nil, err
	}
	if strings.TrimSpace(issue.ID) == "" {
		return nil, fmt.Errorf("missing required field id")
	}

	issue.SetDefaults()
	if err := normalizeIssueMetadata(&issue, line); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	for _, dep := range issue.Dependencies {
		if dep == nil {
			continue
		}
		dep.IssueID = issue.ID
		if dep.Type == "" {
			dep.Type = types.DepBlocks
		}
		if dep.CreatedAt.IsZero() {
			if !issue.UpdatedAt.IsZero() {
				dep.CreatedAt = issue.UpdatedAt.UTC()
			} else if !issue.CreatedAt.IsZero() {
				dep.CreatedAt = issue.CreatedAt.UTC()
			} else {
				dep.CreatedAt = now
			}
		} else {
			dep.CreatedAt = dep.CreatedAt.UTC()
		}
		dep.Metadata = normalizeDependencyMetadata(dep.Metadata)
	}

	for _, comment := range issue.Comments {
		if comment == nil {
			continue
		}
		comment.IssueID = issue.ID
		if comment.CreatedAt.IsZero() {
			if !issue.UpdatedAt.IsZero() {
				comment.CreatedAt = issue.UpdatedAt.UTC()
			} else if !issue.CreatedAt.IsZero() {
				comment.CreatedAt = issue.CreatedAt.UTC()
			} else {
				comment.CreatedAt = now
			}
		} else {
			comment.CreatedAt = comment.CreatedAt.UTC()
		}
	}

	return &issue, nil
}

func normalizeIssueMetadata(issue *types.Issue, line []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return err
	}

	rawMetadata, ok := raw["metadata"]
	if !ok || len(strings.TrimSpace(string(rawMetadata))) == 0 || string(rawMetadata) == "null" {
		issue.Metadata = json.RawMessage("{}")
		return nil
	}

	var decoded interface{}
	if err := json.Unmarshal(rawMetadata, &decoded); err != nil {
		return fmt.Errorf("invalid metadata: %w", err)
	}

	switch value := decoded.(type) {
	case string:
		normalized, err := storage.NormalizeMetadataValue(value)
		if err != nil {
			return fmt.Errorf("invalid metadata: %w", err)
		}
		issue.Metadata = json.RawMessage(normalized)
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("invalid metadata: %w", err)
		}
		normalized, err := storage.NormalizeMetadataValue(json.RawMessage(b))
		if err != nil {
			return fmt.Errorf("invalid metadata: %w", err)
		}
		issue.Metadata = json.RawMessage(normalized)
	}

	return nil
}

func resolveImportMode(opts ImportOptions) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "" {
		if opts.Strict {
			mode = importModeStrict
		} else {
			mode = importModeUpsert
		}
	}
	switch mode {
	case importModeStrict, importModeUpsert:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid import mode %q (expected strict or upsert)", opts.Mode)
	}
}

func parseOrphanHandling(raw string) (storage.OrphanHandling, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return storage.OrphanAllow, nil
	}
	switch storage.OrphanHandling(value) {
	case storage.OrphanStrict, storage.OrphanAllow, storage.OrphanSkip, storage.OrphanResurrect:
		return storage.OrphanHandling(value), nil
	default:
		return "", fmt.Errorf("invalid orphan handling mode %q (expected strict|allow|skip|resurrect)", raw)
	}
}

func issueRoutesToWisp(ctx context.Context, store *dolt.DoltStore, issue *types.Issue) bool {
	if issue == nil {
		return false
	}
	if issue.Ephemeral || dolt.IsEphemeralID(issue.ID) {
		return true
	}
	return store.IsInfraTypeCtx(ctx, issue.IssueType)
}

func issueTableForImport(ctx context.Context, store *dolt.DoltStore, issue *types.Issue) string {
	if issueRoutesToWisp(ctx, store, issue) {
		return "wisps"
	}
	return "issues"
}

func findExistingIssueIDs(ctx context.Context, store *dolt.DoltStore, issues []*types.Issue) (map[string]bool, error) {
	existing := make(map[string]bool, len(issues))
	db := store.DB()

	for _, issue := range issues {
		table := issueTableForImport(ctx, store, issue)
		query := fmt.Sprintf("SELECT 1 FROM %s WHERE id = ? LIMIT 1", table) //nolint:gosec // table is fixed by issueTableForImport
		var one int
		err := db.QueryRowContext(ctx, query, issue.ID).Scan(&one)
		if err == nil {
			existing[issue.ID] = true
			continue
		}
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		return nil, fmt.Errorf("failed checking existing issue %s: %w", issue.ID, err)
	}

	return existing, nil
}

type relationTables struct {
	labels       string
	dependencies string
	comments     string
}

func tablesForIssueImport(ctx context.Context, store *dolt.DoltStore, issue *types.Issue) relationTables {
	if issueRoutesToWisp(ctx, store, issue) {
		return relationTables{
			labels:       "wisp_labels",
			dependencies: "wisp_dependencies",
			comments:     "wisp_comments",
		}
	}
	return relationTables{
		labels:       "labels",
		dependencies: "dependencies",
		comments:     "comments",
	}
}

func reconcileIssueRelations(ctx context.Context, store *dolt.DoltStore, issues []*types.Issue, mode string, orphanHandling storage.OrphanHandling) ([]string, error) {
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin relation reconciliation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if mode == importModeUpsert {
		for _, issue := range issues {
			for _, query := range []string{
				`DELETE FROM labels WHERE issue_id = ?`,
				`DELETE FROM comments WHERE issue_id = ?`,
				`DELETE FROM dependencies WHERE issue_id = ?`,
				`DELETE FROM wisp_labels WHERE issue_id = ?`,
				`DELETE FROM wisp_comments WHERE issue_id = ?`,
				`DELETE FROM wisp_dependencies WHERE issue_id = ?`,
			} {
				if _, err := tx.ExecContext(ctx, query, issue.ID); err != nil {
					return nil, fmt.Errorf("failed to reset relations for %s: %w", issue.ID, err)
				}
			}
		}
	}

	actor := getActorWithGit()
	var skippedDeps []string

	for _, issue := range issues {
		tables := tablesForIssueImport(ctx, store, issue)
		seenLabels := make(map[string]struct{}, len(issue.Labels))

		for _, label := range issue.Labels {
			if _, ok := seenLabels[label]; ok {
				continue
			}
			seenLabels[label] = struct{}{}

			//nolint:gosec // table name comes from fixed relation table mapping
			query := fmt.Sprintf(`INSERT INTO %s (issue_id, label) VALUES (?, ?) ON DUPLICATE KEY UPDATE label = VALUES(label)`, tables.labels)
			if _, err := tx.ExecContext(ctx, query, issue.ID, label); err != nil {
				return nil, fmt.Errorf("failed inserting label %q for %s: %w", label, issue.ID, err)
			}
		}

		for _, comment := range issue.Comments {
			if comment == nil {
				continue
			}
			author := strings.TrimSpace(comment.Author)
			if author == "" {
				author = actor
			}
			createdAt := comment.CreatedAt
			if createdAt.IsZero() {
				createdAt = time.Now().UTC()
			} else {
				createdAt = createdAt.UTC()
			}

			//nolint:gosec // table name comes from fixed relation table mapping
			query := fmt.Sprintf(`INSERT INTO %s (issue_id, author, text, created_at) VALUES (?, ?, ?, ?)`, tables.comments)
			if _, err := tx.ExecContext(ctx, query, issue.ID, author, comment.Text, createdAt); err != nil {
				return nil, fmt.Errorf("failed inserting comment for %s: %w", issue.ID, err)
			}
		}

		for _, dep := range issue.Dependencies {
			if dep == nil {
				continue
			}
			depID := strings.TrimSpace(dep.DependsOnID)
			if depID == "" {
				continue
			}

			if !strings.HasPrefix(depID, "external:") {
				exists, err := dependencyTargetExists(ctx, tx, depID)
				if err != nil {
					return nil, fmt.Errorf("failed checking dependency target %s for %s: %w", depID, issue.ID, err)
				}
				if !exists {
					switch orphanHandling {
					case storage.OrphanStrict:
						return nil, fmt.Errorf("dependency target %s for issue %s does not exist (strict mode)", depID, issue.ID)
					case storage.OrphanSkip:
						skippedDeps = append(skippedDeps, issue.ID+"->"+depID)
						continue
					case storage.OrphanAllow, storage.OrphanResurrect:
						// Preserve the edge as-is (no depends_on FK exists).
					}
				}
			}

			depType := dep.Type
			if depType == "" {
				depType = types.DepBlocks
			}
			createdBy := strings.TrimSpace(dep.CreatedBy)
			if createdBy == "" {
				createdBy = actor
			}
			createdAt := dep.CreatedAt
			if createdAt.IsZero() {
				createdAt = time.Now().UTC()
			} else {
				createdAt = createdAt.UTC()
			}

			//nolint:gosec // table name comes from fixed relation table mapping
			query := fmt.Sprintf(`
				INSERT INTO %s (issue_id, depends_on_id, type, created_by, created_at, metadata, thread_id)
				VALUES (?, ?, ?, ?, ?, ?, ?)
				ON DUPLICATE KEY UPDATE
					type = VALUES(type),
					created_by = VALUES(created_by),
					created_at = VALUES(created_at),
					metadata = VALUES(metadata),
					thread_id = VALUES(thread_id)
			`, tables.dependencies)

			if _, err := tx.ExecContext(ctx, query,
				issue.ID, depID, depType, createdBy, createdAt, normalizeDependencyMetadata(dep.Metadata), dep.ThreadID,
			); err != nil {
				return nil, fmt.Errorf("failed inserting dependency %s -> %s: %w", issue.ID, depID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit relation reconciliation: %w", err)
	}

	return skippedDeps, nil
}

func dependencyTargetExists(ctx context.Context, tx *sql.Tx, targetID string) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM (
			SELECT id FROM issues WHERE id = ?
			UNION ALL
			SELECT id FROM wisps WHERE id = ?
		) AS targets
		LIMIT 1
	`, targetID, targetID).Scan(&one)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}
