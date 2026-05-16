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
func (s *DoltStore) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	var result []*types.Issue
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.SearchIssuesInTx(ctx, tx, query, filter)
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
		result, err = issueops.GetReadyWorkInTx(ctx, tx, filter, s.computeBlockedIDsForReadyWork)
		return err
	})
	return result, err
}

// GetBlockedIssues returns issues that are blocked by other issues.
// Uses separate single-table queries with Go-level filtering to avoid
// correlated EXISTS subqueries that trigger Dolt's joinIter panic
// (slice bounds out of range at join_iters.go:192).
// Same fix pattern as GetStatistics blocked count (fc16065c, a4a21958).
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

	// Blocked count: reuse computeBlockedIDs which caches the result across
	// GetReadyWork and GetStatistics calls within the same CLI invocation.
	var blockedCount int
	blockedIDs, err := s.computeBlockedIDs(ctx, true)
	if err == nil {
		blockedCount = len(blockedIDs)
	}
	stats.BlockedIssues = blockedCount

	// Ready count: compute without using the ready_issues view to avoid
	// recursive CTE join that triggers the same Dolt panic.
	// Ready = open, non-ephemeral, not blocked (directly or transitively).
	stats.ReadyIssues = stats.OpenIssues - blockedCount
	if stats.ReadyIssues < 0 {
		stats.ReadyIssues = 0
	}

	return stats, nil
}

// computeBlockedIDs returns the set of issue IDs that are blocked by active issues.
// Uses separate single-table queries with Go-level filtering to avoid Dolt's
// joinIter panic (slice bounds out of range at join_iters.go:192).
// Results are cached per DoltStore lifetime and invalidated when dependencies
// change (AddDependency, RemoveDependency).
//
// When includeWisps is false, only the issues/dependencies tables are scanned,
// skipping the wisps/wisp_dependencies tables. This is safe when the caller only
// needs blocked status for non-ephemeral issues (no cross-table blocking deps exist).
// A cached result from includeWisps=true satisfies includeWisps=false requests.
//
// Caller must hold s.mu (at least RLock).
func (s *DoltStore) computeBlockedIDs(ctx context.Context, includeWisps bool) ([]string, error) {
	var result []string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = s.computeBlockedIDsForReadyWork(ctx, tx, includeWisps)
		return err
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *DoltStore) computeBlockedIDsForReadyWork(ctx context.Context, tx *sql.Tx, includeWisps bool) ([]string, error) {
	s.cacheMu.Lock()
	if s.blockedIDsCached && (s.blockedIDsCacheIncludesWisps || !includeWisps) {
		result := s.blockedIDsCache
		s.cacheMu.Unlock()
		return result, nil
	}
	s.cacheMu.Unlock()

	result, _, err := issueops.ComputeBlockedIDsInTx(ctx, tx, includeWisps)
	if err != nil {
		return nil, err
	}
	blockedSet := make(map[string]bool, len(result))
	for _, id := range result {
		blockedSet[id] = true
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.blockedIDsCached && (s.blockedIDsCacheIncludesWisps || !includeWisps) {
		return s.blockedIDsCache, nil
	}
	s.blockedIDsCache = result
	s.blockedIDsCacheMap = blockedSet
	s.blockedIDsCached = true
	s.blockedIDsCacheIncludesWisps = includeWisps
	return result, nil
}

// invalidateBlockedIDsCache clears the blocked IDs cache so the next call
// to computeBlockedIDs will recompute from the database.
func (s *DoltStore) invalidateBlockedIDsCache() {
	s.cacheMu.Lock()
	s.blockedIDsCached = false
	s.blockedIDsCache = nil
	s.blockedIDsCacheMap = nil
	s.blockedIDsCacheIncludesWisps = false
	s.cacheMu.Unlock()
}

// getChildrenOfDeferredParents returns IDs of issues whose parent has a future
// defer_until date. Uses separate single-table queries to avoid correlated
// cross-table JOIN subqueries that trigger Dolt joinIter hangs (GH#1190).
// Caller must hold s.mu (at least RLock).
func (s *DoltStore) getChildrenOfDeferredParents(ctx context.Context) ([]string, error) {
	// Step 1: Get IDs of issues with future defer_until
	deferredRows, err := s.queryContext(ctx, `
		SELECT id FROM issues
		WHERE defer_until IS NOT NULL AND defer_until > UTC_TIMESTAMP()
	`)
	if err != nil {
		return nil, wrapQueryError("deferred parents: get deferred issues", err)
	}
	var deferredIDs []string
	for deferredRows.Next() {
		var id string
		if err := deferredRows.Scan(&id); err != nil {
			_ = deferredRows.Close()
			return nil, wrapScanError("deferred parents: scan deferred issue", err)
		}
		deferredIDs = append(deferredIDs, id)
	}
	_ = deferredRows.Close()
	if err := deferredRows.Err(); err != nil {
		return nil, wrapQueryError("deferred parents: deferred rows", err)
	}
	if len(deferredIDs) == 0 {
		return nil, nil
	}

	// Step 2: Get children of those deferred parents
	return s.getChildrenOfIssues(ctx, deferredIDs)
}

// getChildrenOfIssues returns IDs of direct children (parent-child deps) of the given issue IDs.
func (s *DoltStore) getChildrenOfIssues(ctx context.Context, parentIDs []string) ([]string, error) {
	var result []string
	err := s.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetChildrenOfIssuesInTx(ctx, tx, parentIDs)
		return err
	})
	return result, err
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
	if s.isActiveWisp(ctx, moleculeID) {
		issueTable = "wisps"
		depTable = "wisp_dependencies"
	}

	// Get molecule title
	var title sql.NullString
	//nolint:gosec // G201: issueTable is hardcoded to "issues" or "wisps"
	err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT title FROM %s WHERE id = ?", issueTable), moleculeID).Scan(&title)
	if err == nil && title.Valid {
		stats.MoleculeTitle = title.String
	}

	// Use separate single-table queries to avoid Dolt's joinIter panic
	// (join_iters.go:192) which triggers on JOIN between issues and dependencies.

	// Step 1: Get child issue IDs from dependencies table (single-table scan)
	//nolint:gosec // G201: depTable is hardcoded to "dependencies" or "wisp_dependencies"
	depRows, err := s.queryContext(ctx, fmt.Sprintf(`
		SELECT issue_id FROM %s
		WHERE depends_on_id = ? AND type = 'parent-child'
	`, depTable), moleculeID)
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
