package dolt

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// SearchIssues finds issues matching query and filters.
// Delegates to issueops.SearchIssuesInTx for shared query logic.
//
// Q5: When the wisps table is known empty (cached), SkipWisps is set on the
// filter automatically, avoiding the full-table scan regardless of caller intent.
func (s *DoltStore) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	// Q5: short-circuit wisps merge if the table is known empty.
	if !filter.SkipWisps {
		filter.SkipWisps = s.isWispTableEmpty(ctx)
	}

	var result []*types.Issue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.SearchIssuesInTx(ctx, tx, query, filter)
		return err
	})
	return result, err
}

func (s *DoltStore) SearchIssuesWithCounts(ctx context.Context, query string, filter types.IssueFilter) ([]*types.IssueWithCounts, error) {
	var result []*types.IssueWithCounts
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.SearchIssuesWithCountsInTx(ctx, tx, query, filter)
		return err
	})
	return result, err
}

// isWispTableEmpty returns true when the wisps table is known to have zero rows
// (Q5 per-process cache). A false return does NOT mean wisps are non-empty; it
// means the count hasn't been fetched yet or the cache was invalidated.
func (s *DoltStore) isWispTableEmpty(ctx context.Context) bool {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.wispCountCached {
		return s.wispCountZero
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM wisps`).Scan(&count)
	if err != nil {
		// Table may not exist or query failed; don't cache, don't skip.
		return false
	}
	s.wispCountCached = true
	s.wispCountZero = count == 0
	return s.wispCountZero
}

// invalidateWispCountCache clears the Q5 wisp row-count cache.
// Must be called after any write that creates or deletes wisps.
func (s *DoltStore) invalidateWispCountCache() {
	s.cacheMu.Lock()
	s.wispCountCached = false
	s.wispCountZero = false
	s.cacheMu.Unlock()
}

// CountIssuesByGroup returns per-group issue counts. groupBy is one of:
// status, priority, type, assignee, label.
func (s *DoltStore) CountIssuesByGroup(ctx context.Context, filter types.IssueFilter, groupBy string) (map[string]int, error) {
	if !filter.SkipWisps {
		filter.SkipWisps = s.isWispTableEmpty(ctx)
	}
	var result map[string]int
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.CountIssuesByGroupInTx(ctx, tx, filter, groupBy)
		return err
	})
	return result, err
}

// GetReadyWork returns issues that are ready to work on (not blocked).
//
// Blocking semantics are unified through issueops.GetReadyWorkInTx.
func (s *DoltStore) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	var result []*types.Issue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetReadyWorkInTx(ctx, tx, filter)
		return err
	})
	return result, err
}

func (s *DoltStore) GetReadyWorkWithCounts(ctx context.Context, filter types.WorkFilter) ([]*types.IssueWithCounts, error) {
	var result []*types.IssueWithCounts
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetReadyWorkWithCountsInTx(ctx, tx, filter)
		return err
	})
	return result, err
}

func (s *DoltStore) GetBlockedIssues(ctx context.Context, filter types.WorkFilter) ([]*types.BlockedIssue, error) {
	var result []*types.BlockedIssue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetBlockedIssuesInTx(ctx, tx, filter)
		return err
	})
	return result, err
}

// GetEpicsEligibleForClosure returns epics whose children are all closed
func (s *DoltStore) GetEpicsEligibleForClosure(ctx context.Context) ([]*types.EpicStatus, error) {
	var result []*types.EpicStatus
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetEpicsEligibleForClosureInTx(ctx, tx)
		return err
	})
	return result, err
}

// GetStaleIssues returns issues that haven't been updated recently
func (s *DoltStore) GetStaleIssues(ctx context.Context, filter types.StaleFilter) ([]*types.Issue, error) {
	var result []*types.Issue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetStaleIssuesInTx(ctx, tx, filter)
		return err
	})
	return result, err
}

// GetStatistics returns summary statistics
func (s *DoltStore) GetStatistics(ctx context.Context) (*types.Statistics, error) {
	stats := &types.Statistics{}

	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		return issueops.ScanIssueCountsInTx(ctx, tx, stats)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get statistics: %w", err)
	}

	var blockedCount int
	if err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM issues
			WHERE is_blocked = 1 AND status <> 'closed' AND status <> 'pinned'
		`).Scan(&blockedCount)
	}); err != nil {
		return nil, fmt.Errorf("failed to count blocked issues: %w", err)
	}
	stats.BlockedIssues = blockedCount

	stats.ReadyIssues = stats.OpenIssues - blockedCount
	if stats.ReadyIssues < 0 {
		stats.ReadyIssues = 0
	}

	return stats, nil
}

// invalidateBlockedIDsCache clears the blocked IDs cache.
// Called after writes that may change blocking status.
func (s *DoltStore) invalidateBlockedIDsCache() {
	s.cacheMu.Lock()
	s.blockedIDsCached = false
	s.blockedIDsCache = nil
	s.blockedIDsCacheMap = nil
	s.blockedIDsCacheIncludesWisps = false
	s.cacheMu.Unlock()
}

// getChildrenWithParents returns a map of childID -> parentID for direct children
// (parent-child deps) of the given parent IDs.
func (s *DoltStore) getChildrenWithParents(ctx context.Context, parentIDs []string) (map[string]string, error) {
	var result map[string]string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetChildrenWithParentsInTx(ctx, tx, parentIDs)
		return err
	})
	return result, err
}

func (s *DoltStore) getDescendantIDs(ctx context.Context, rootID string) ([]string, error) {
	var result []string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetDescendantIDsInTx(ctx, tx, rootID, 0)
		return err
	})
	return result, err
}

// GetMoleculeProgress returns progress stats for a molecule
func (s *DoltStore) GetMoleculeProgress(ctx context.Context, moleculeID string) (*types.MoleculeProgressStats, error) {
	stats := &types.MoleculeProgressStats{
		MoleculeID: moleculeID,
	}

	// Route to correct table based on whether molecule is a wisp (bd-w2w)
	issueTable := "issues"
	depTable := "dependencies"
	parentCol := "depends_on_issue_id"
	if s.isActiveWisp(ctx, moleculeID) {
		issueTable = "wisps"
		depTable = "wisp_dependencies"
		parentCol = "depends_on_wisp_id"
	}

	// Get molecule title
	var title sql.NullString
	//nolint:gosec // G201: issueTable is hardcoded to "issues" or "wisps"
	err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT title FROM %s WHERE id = ?", issueTable), moleculeID).Scan(&title)
	if err == nil && title.Valid {
		stats.MoleculeTitle = title.String
	}

	// Step 1: Get child issue IDs from dependencies table (single-table scan)
	//nolint:gosec // G201: depTable and parentCol are hardcoded
	depRows, err := s.queryContext(ctx, fmt.Sprintf(`
		SELECT issue_id FROM %s
		WHERE %s = ? AND type = 'parent-child'
	`, depTable, parentCol), moleculeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get molecule children: %w", err)
	}
	var childIDs []string
	for depRows.Next() {
		var id string
		if err := depRows.Scan(&id); err != nil {
			_ = depRows.Close() // Best effort cleanup on error path
			return nil, wrapScanError("get molecule progress: scan child", err)
		}
		childIDs = append(childIDs, id)
	}
	_ = depRows.Close() // Redundant close for safety (rows already iterated)

	// Step 2: Batch-fetch status for all children (batched IN clauses to avoid full table scans).
	// Children of a wisp molecule are also wisps, so use the same table.
	if len(childIDs) > 0 {
		type childInfo struct {
			status string
		}
		childMap := make(map[string]childInfo)
		for start := 0; start < len(childIDs); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(childIDs) {
				end = len(childIDs)
			}
			batch := childIDs[start:end]
			placeholders, args := doltBuildSQLInClause(batch)
			// nolint:gosec // G201: issueTable is hardcoded, placeholders contains only ? markers
			query := fmt.Sprintf("SELECT id, status FROM %s WHERE id IN (%s)", issueTable, placeholders)
			statusRows, err := s.queryContext(ctx, query, args...)
			if err != nil {
				return nil, fmt.Errorf("failed to batch-fetch child statuses: %w", err)
			}
			for statusRows.Next() {
				var id, status string
				if err := statusRows.Scan(&id, &status); err != nil {
					_ = statusRows.Close()
					return nil, wrapScanError("get molecule progress: scan status", err)
				}
				childMap[id] = childInfo{status: status}
			}
			_ = statusRows.Close()
		}

		for _, childID := range childIDs {
			info, ok := childMap[childID]
			if !ok {
				continue
			}
			stats.Total++
			switch types.Status(info.status) {
			case types.StatusClosed:
				stats.Completed++
			case types.StatusInProgress:
				stats.InProgress++
				if stats.CurrentStepID == "" {
					stats.CurrentStepID = childID
				}
			}
		}
	}

	return stats, nil
}

// GetMoleculeLastActivity returns the most recent activity timestamp for a molecule.
func (s *DoltStore) GetMoleculeLastActivity(ctx context.Context, moleculeID string) (*types.MoleculeLastActivity, error) {
	var result *types.MoleculeLastActivity
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetMoleculeLastActivityInTx(ctx, tx, moleculeID)
		return err
	})
	return result, err
}

// GetNextChildID returns the next available child ID for a parent.
// Delegates SQL work to issueops.GetNextChildIDTx.
func (s *DoltStore) GetNextChildID(ctx context.Context, parentID string) (string, error) {
	var childID string
	err := s.withRetryTx(ctx, func(tx *sql.Tx) error {
		var err error
		childID, err = issueops.GetNextChildIDTx(ctx, tx, parentID)
		return err
	})
	return childID, err
}
