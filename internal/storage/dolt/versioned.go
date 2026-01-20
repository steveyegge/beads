package dolt

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// Ensure DoltStore implements VersionedStorage at compile time.
var _ storage.VersionedStorage = (*DoltStore)(nil)

// History returns the complete version history for an issue.
// Implements storage.VersionedStorage.
func (s *DoltStore) History(ctx context.Context, issueID string) ([]*storage.HistoryEntry, error) {
	internal, err := s.GetIssueHistory(ctx, issueID)
	if err != nil {
		return nil, err
	}

	// Convert internal representation to interface type
	entries := make([]*storage.HistoryEntry, len(internal))
	for i, h := range internal {
		entries[i] = &storage.HistoryEntry{
			CommitHash: h.CommitHash,
			Committer:  h.Committer,
			CommitDate: h.CommitDate,
			Issue:      h.Issue,
		}
	}
	return entries, nil
}

// AsOf returns the state of an issue at a specific commit hash or branch ref.
// Implements storage.VersionedStorage.
func (s *DoltStore) AsOf(ctx context.Context, issueID string, ref string) (*types.Issue, error) {
	return s.GetIssueAsOf(ctx, issueID, ref)
}

// Diff returns changes between two commits/branches.
// Implements storage.VersionedStorage.
func (s *DoltStore) Diff(ctx context.Context, fromRef, toRef string) ([]*storage.DiffEntry, error) {
	// Validate refs to prevent SQL injection
	if err := validateRef(fromRef); err != nil {
		return nil, fmt.Errorf("invalid fromRef: %w", err)
	}
	if err := validateRef(toRef); err != nil {
		return nil, fmt.Errorf("invalid toRef: %w", err)
	}

	db, err := s.getDB(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	// Query issue-level diffs directly
	// Note: refs are validated above
	// nolint:gosec // G201: refs validated by validateRef()
	query := fmt.Sprintf(`
		SELECT
			COALESCE(from_id, '') as from_id,
			COALESCE(to_id, '') as to_id,
			diff_type,
			from_title, to_title,
			from_description, to_description,
			from_status, to_status,
			from_priority, to_priority
		FROM dolt_diff_issues('%s', '%s')
	`, fromRef, toRef)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get diff: %w", err)
	}
	defer rows.Close()

	var entries []*storage.DiffEntry
	for rows.Next() {
		var fromID, toID, diffType string
		var fromTitle, toTitle, fromDesc, toDesc, fromStatus, toStatus *string
		var fromPriority, toPriority *int

		if err := rows.Scan(&fromID, &toID, &diffType,
			&fromTitle, &toTitle,
			&fromDesc, &toDesc,
			&fromStatus, &toStatus,
			&fromPriority, &toPriority); err != nil {
			return nil, fmt.Errorf("failed to scan diff: %w", err)
		}

		entry := &storage.DiffEntry{
			DiffType: diffType,
		}

		// Determine issue ID (use to_id for added, from_id for removed, either for modified)
		if toID != "" {
			entry.IssueID = toID
		} else {
			entry.IssueID = fromID
		}

		// Build old value for modified/removed
		if diffType != "added" && fromID != "" {
			entry.OldValue = &types.Issue{
				ID: fromID,
			}
			if fromTitle != nil {
				entry.OldValue.Title = *fromTitle
			}
			if fromDesc != nil {
				entry.OldValue.Description = *fromDesc
			}
			if fromStatus != nil {
				entry.OldValue.Status = types.Status(*fromStatus)
			}
			if fromPriority != nil {
				entry.OldValue.Priority = *fromPriority
			}
		}

		// Build new value for modified/added
		if diffType != "removed" && toID != "" {
			entry.NewValue = &types.Issue{
				ID: toID,
			}
			if toTitle != nil {
				entry.NewValue.Title = *toTitle
			}
			if toDesc != nil {
				entry.NewValue.Description = *toDesc
			}
			if toStatus != nil {
				entry.NewValue.Status = types.Status(*toStatus)
			}
			if toPriority != nil {
				entry.NewValue.Priority = *toPriority
			}
		}

		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// ListBranches returns the names of all branches.
// Implements storage.VersionedStorage.
func (s *DoltStore) ListBranches(ctx context.Context) ([]string, error) {
	db, err := s.getDB(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	rows, err := db.QueryContext(ctx, "SELECT name FROM dolt_branches ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}
	defer rows.Close()

	var branches []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan branch: %w", err)
		}
		branches = append(branches, name)
	}
	return branches, rows.Err()
}

// GetCurrentCommit returns the hash of the current HEAD commit.
// Implements storage.VersionedStorage.
func (s *DoltStore) GetCurrentCommit(ctx context.Context) (string, error) {
	db, err := s.getDB(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get database connection: %w", err)
	}

	var hash string
	err = db.QueryRowContext(ctx, "SELECT DOLT_HASHOF('HEAD')").Scan(&hash)
	if err != nil {
		return "", fmt.Errorf("failed to get current commit: %w", err)
	}
	return hash, nil
}

// GetConflicts returns any merge conflicts in the current state.
// Implements storage.VersionedStorage.
func (s *DoltStore) GetConflicts(ctx context.Context) ([]storage.Conflict, error) {
	internal, err := s.GetInternalConflicts(ctx)
	if err != nil {
		return nil, err
	}

	conflicts := make([]storage.Conflict, 0, len(internal))
	for _, c := range internal {
		conflicts = append(conflicts, storage.Conflict{
			Field: c.TableName,
		})
	}
	return conflicts, nil
}
