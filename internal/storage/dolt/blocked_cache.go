// Package dolt implements the blocked_issues_cache for Dolt, mirroring the SQLite
// optimization in sqlite/blocked_cache.go. The cache materializes the recursive CTE
// from the ready_issues view into a simple table, converting O(N*M*depth) recursive
// joins on every read into a simple NOT EXISTS check. (bd-b2ts)
//
// The cache is rebuilt from scratch on invalidation. For Dolt, invalidation is
// triggered by the daemon after mutations (via the mutation event channel) and
// periodically on health checks as a safety net.
package dolt

import (
	"context"
	"database/sql"
	"fmt"
)

// RebuildBlockedCache completely rebuilds the blocked_issues_cache table.
// This replaces the expensive recursive CTE in the ready_issues view with
// a materialized table that can be queried with a simple NOT EXISTS.
//
// The rebuild handles four blocking types:
//   - 'blocks': B is blocked until A is closed
//   - 'conditional-blocks': B is blocked until A is closed with failure reason
//   - 'waits-for': B is blocked until all children of spawner A are closed
//   - 'parent-child': Propagates blockage to children (up to 50 levels deep)
func (s *DoltStore) RebuildBlockedCache(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.rebuildBlockedCacheInternal(ctx, s.db)
	if err == nil {
		s.blockedCacheBuilt.Store(true)
	}
	return err
}

// rebuildBlockedCacheTx rebuilds the cache within an existing transaction.
func (s *DoltStore) rebuildBlockedCacheTx(ctx context.Context, tx *sql.Tx) error {
	return s.rebuildBlockedCacheInternal(ctx, tx)
}

// dbExecer abstracts *sql.DB and *sql.Tx for executing queries.
type dbExecer interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

func (s *DoltStore) rebuildBlockedCacheInternal(ctx context.Context, exec dbExecer) error {
	// Clear the cache
	if _, err := exec.ExecContext(ctx, "DELETE FROM blocked_issues_cache"); err != nil {
		return fmt.Errorf("failed to clear blocked_issues_cache: %w", err)
	}

	// Rebuild using recursive CTE matching the SQLite blocked_cache.go logic.
	// Dolt supports recursive CTEs and JSON_EXTRACT natively.
	//
	// Note: Dolt uses JSON_EXTRACT (MySQL syntax) vs json_extract (SQLite).
	// COALESCE+JSON_EXTRACT pattern works identically on both.
	query := `
		INSERT INTO blocked_issues_cache (issue_id)
		WITH RECURSIVE
		  blocked_directly AS (
		    -- Regular 'blocks' dependencies: B blocked if A not closed
		    SELECT DISTINCT d.issue_id
		    FROM dependencies d
		    JOIN issues blocker ON d.depends_on_id = blocker.id
		    WHERE d.type = 'blocks'
		      AND blocker.status IN ('open', 'in_progress', 'blocked', 'deferred', 'hooked')

		    UNION

		    -- 'conditional-blocks': B blocked unless A closed with failure
		    SELECT DISTINCT d.issue_id
		    FROM dependencies d
		    JOIN issues blocker ON d.depends_on_id = blocker.id
		    WHERE d.type = 'conditional-blocks'
		      AND (
		        blocker.status IN ('open', 'in_progress', 'blocked', 'deferred')
		        OR
		        (blocker.status = 'closed' AND NOT (
		          LOWER(COALESCE(blocker.close_reason, '')) LIKE '%failed%'
		          OR LOWER(COALESCE(blocker.close_reason, '')) LIKE '%rejected%'
		          OR LOWER(COALESCE(blocker.close_reason, '')) LIKE '%wontfix%'
		          OR LOWER(COALESCE(blocker.close_reason, '')) LIKE '%canceled%'
		          OR LOWER(COALESCE(blocker.close_reason, '')) LIKE '%cancelled%'
		          OR LOWER(COALESCE(blocker.close_reason, '')) LIKE '%abandoned%'
		          OR LOWER(COALESCE(blocker.close_reason, '')) LIKE '%blocked%'
		          OR LOWER(COALESCE(blocker.close_reason, '')) LIKE '%error%'
		          OR LOWER(COALESCE(blocker.close_reason, '')) LIKE '%timeout%'
		          OR LOWER(COALESCE(blocker.close_reason, '')) LIKE '%aborted%'
		        ))
		      )

		    UNION

		    -- 'waits-for': B blocked until all children of spawner closed
		    SELECT DISTINCT d.issue_id
		    FROM dependencies d
		    WHERE d.type = 'waits-for'
		      AND (
		        COALESCE(JSON_EXTRACT(d.metadata, '$.gate'), 'all-children') = 'all-children'
		        AND EXISTS (
		          SELECT 1 FROM dependencies child_dep
		          JOIN issues child ON child_dep.issue_id = child.id
		          WHERE child_dep.type = 'parent-child'
		            AND child_dep.depends_on_id = COALESCE(
		              JSON_EXTRACT(d.metadata, '$.spawner_id'),
		              d.depends_on_id
		            )
		            AND child.status NOT IN ('closed', 'tombstone')
		        )
		        OR
		        COALESCE(JSON_EXTRACT(d.metadata, '$.gate'), 'all-children') = 'any-children'
		        AND NOT EXISTS (
		          SELECT 1 FROM dependencies child_dep
		          JOIN issues child ON child_dep.issue_id = child.id
		          WHERE child_dep.type = 'parent-child'
		            AND child_dep.depends_on_id = COALESCE(
		              JSON_EXTRACT(d.metadata, '$.spawner_id'),
		              d.depends_on_id
		            )
		            AND child.status IN ('closed', 'tombstone')
		        )
		      )
		  ),

		  blocked_transitively AS (
		    SELECT issue_id, 0 as depth
		    FROM blocked_directly

		    UNION ALL

		    SELECT d.issue_id, bt.depth + 1
		    FROM blocked_transitively bt
		    JOIN dependencies d ON d.depends_on_id = bt.issue_id
		    WHERE d.type = 'parent-child'
		      AND bt.depth < 50
		  )
		SELECT DISTINCT issue_id FROM blocked_transitively
	`

	if _, err := exec.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to rebuild blocked_issues_cache: %w", err)
	}

	return nil
}
