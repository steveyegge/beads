package dolt

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// IsEphemeralID returns true if the ID belongs to an ephemeral issue.
func IsEphemeralID(id string) bool {
	return strings.Contains(id, "-wisp-")
}

// isActiveWisp checks if an ephemeral-looking ID still exists in the wisps table.
// Returns false if the ID is not ephemeral, or if the wisp was promoted/deleted.
// Used by CRUD methods to decide whether to route to wisp tables or fall through
// to permanent tables (handles promoted wisps correctly).
func (s *DoltStore) isActiveWisp(ctx context.Context, id string) bool {
	if !IsEphemeralID(id) {
		return false
	}
	wisp, _ := s.getWisp(ctx, id)
	return wisp != nil
}

// allEphemeral returns true if all IDs in the slice are ephemeral.
func allEphemeral(ids []string) bool {
	for _, id := range ids {
		if !IsEphemeralID(id) {
			return false
		}
	}
	return len(ids) > 0
}

// partitionIDs separates IDs into ephemeral and dolt groups.
func partitionIDs(ids []string) (ephIDs, doltIDs []string) {
	for _, id := range ids {
		if IsEphemeralID(id) {
			ephIDs = append(ephIDs, id)
		} else {
			doltIDs = append(doltIDs, id)
		}
	}
	return
}

// PromoteFromEphemeral copies an issue from the wisps table to the issues table,
// clearing the Ephemeral flag. Used by bd promote and mol squash to crystallize wisps.
//
// Uses direct SQL inserts to bypass IsEphemeralID routing, which would otherwise
// redirect label/dependency/event writes back to wisp tables.
func (s *DoltStore) PromoteFromEphemeral(ctx context.Context, id string, actor string) error {
	issue, err := s.getWisp(ctx, id)
	if errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("wisp %s not found", id)
	}
	if err != nil {
		return err
	}
	if issue == nil {
		return fmt.Errorf("wisp %s not found", id)
	}

	// Clear ephemeral flag for persistent storage
	issue.Ephemeral = false

	// Create in issues table (bypasses ephemeral routing since Ephemeral=false)
	if err := s.CreateIssue(ctx, issue, actor); err != nil {
		return fmt.Errorf("failed to promote wisp to issues: %w", err)
	}

	// Copy labels directly to permanent labels table (bypass IsEphemeralID routing)
	labels, err := s.getWispLabels(ctx, id)
	if err != nil {
		return err
	}
	for _, label := range labels {
		if _, err := s.execContext(ctx,
			`INSERT IGNORE INTO labels (issue_id, label) VALUES (?, ?)`,
			id, label); err != nil {
			return fmt.Errorf("failed to copy label %q: %w", label, err)
		}
	}

	// Copy dependencies directly to permanent dependencies table
	deps, err := s.getWispDependencyRecords(ctx, id)
	if err != nil {
		return err
	}
	for _, dep := range deps {
		metadata := dep.Metadata
		if metadata == "" {
			metadata = "{}"
		}
		if _, err := s.execContext(ctx, `
			INSERT IGNORE INTO dependencies (issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, dep.IssueID, dep.DependsOnID, dep.Type, dep.CreatedAt, dep.CreatedBy, metadata, dep.ThreadID); err != nil {
			// Skip if target doesn't exist (external ref to other wisp)
			if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "foreign key") {
				continue
			}
			return fmt.Errorf("failed to copy dependency: %w", err)
		}
	}

	// Copy events via INSERT...SELECT (best-effort: don't fail promotion over history)
	_, _ = s.execContext(ctx, `
		INSERT IGNORE INTO events (issue_id, event_type, actor, old_value, new_value, comment, created_at)
		SELECT issue_id, event_type, actor, old_value, new_value, comment, created_at
		FROM wisp_events WHERE issue_id = ?
	`, id)

	// Copy comments via INSERT...SELECT
	_, _ = s.execContext(ctx, `
		INSERT IGNORE INTO comments (issue_id, author, text, created_at)
		SELECT issue_id, author, text, created_at
		FROM wisp_comments WHERE issue_id = ?
	`, id)

	// Delete from wisps table (and all wisp_* auxiliary tables)
	return s.deleteWisp(ctx, id)
}

// getWispDependencyRecords returns raw dependency records for a wisp from wisp_dependencies.
func (s *DoltStore) getWispDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	rows, err := s.queryContext(ctx, `
		SELECT issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id
		FROM wisp_dependencies
		WHERE issue_id = ?
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get wisp dependency records: %w", err)
	}
	defer rows.Close()

	return scanDependencyRows(rows)
}
