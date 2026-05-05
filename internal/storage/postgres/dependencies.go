package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// AddDependency inserts a dep edge atomically, also writing an audit event.
// Cycle prevention is delegated to the DB layer for Storage callers; the
// Transaction-scoped variant exposes an option to skip cycle checks.
func (s *PostgresStore) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	return s.RunInTransaction(ctx, "", func(tx storage.Transaction) error {
		return tx.AddDependency(ctx, dep, actor)
	})
}

func addDependencyRow(ctx context.Context, c pgxConn, dep *types.Dependency, skipCycleCheck bool) error {
	if dep == nil || dep.IssueID == "" || dep.DependsOnID == "" {
		return errors.New("postgres: AddDependency: issue_id and depends_on_id are required")
	}
	if dep.IssueID == dep.DependsOnID {
		return errors.New("postgres: AddDependency: self-dependency rejected")
	}
	if dep.Type == "" {
		dep.Type = types.DepBlocks
	}
	if dep.CreatedAt.IsZero() {
		dep.CreatedAt = time.Now().UTC()
	}
	depTable, _ := dependencyTablesForID(ctx, c, dep.IssueID)
	if !skipCycleCheck && dep.Type.AffectsReadyWork() {
		hasCycle, err := wouldCreateCycle(ctx, c, dep.IssueID, dep.DependsOnID)
		if err != nil {
			return err
		}
		if hasCycle {
			return fmt.Errorf("postgres: AddDependency: would create cycle (%s → %s)", dep.IssueID, dep.DependsOnID)
		}
	}
	depTable = guardTable(depTable)
	//nolint:gosec // depTable is allowlisted via dependencyTablesForID + guardTable
	stmt := fmt.Sprintf(`
		INSERT INTO %s (issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (issue_id, depends_on_id) DO UPDATE SET
			type = EXCLUDED.type,
			created_at = EXCLUDED.created_at,
			created_by = EXCLUDED.created_by,
			metadata = EXCLUDED.metadata,
			thread_id = EXCLUDED.thread_id
	`, depTable)
	if _, err := c.Exec(ctx, stmt,
		dep.IssueID, dep.DependsOnID, string(dep.Type), dep.CreatedAt,
		dep.CreatedBy, jsonbMetadata([]byte(dep.Metadata)), dep.ThreadID,
	); err != nil {
		return wrapErr("insert dependency", err)
	}
	return nil
}

// wouldCreateCycle walks the dependency graph (blocks/parent-child) from
// dependsOnID and reports whether issueID is reachable.
func wouldCreateCycle(ctx context.Context, c pgxConn, issueID, dependsOnID string) (bool, error) {
	q := `
		WITH RECURSIVE descendants(id) AS (
			SELECT depends_on_id FROM dependencies
			WHERE issue_id = $1 AND type IN ('blocks', 'parent-child', 'conditional-blocks', 'waits-for')
			UNION
			SELECT d.depends_on_id FROM dependencies d
			JOIN descendants ds ON d.issue_id = ds.id
			WHERE d.type IN ('blocks', 'parent-child', 'conditional-blocks', 'waits-for')
		)
		SELECT EXISTS(SELECT 1 FROM descendants WHERE id = $2)
	`
	var found bool
	if err := c.QueryRow(ctx, q, dependsOnID, issueID).Scan(&found); err != nil {
		return false, wrapErr("cycle detection", err)
	}
	return found, nil
}

// RemoveDependency deletes a dep edge.
func (s *PostgresStore) RemoveDependency(ctx context.Context, issueID, dependsOnID, actor string) error {
	return s.RunInTransaction(ctx, "", func(tx storage.Transaction) error {
		return tx.RemoveDependency(ctx, issueID, dependsOnID, actor)
	})
}

func removeDependencyRow(ctx context.Context, c pgxConn, issueID, dependsOnID string) error {
	depTable, _ := dependencyTablesForID(ctx, c, issueID)
	depTable = guardTable(depTable)
	//nolint:gosec // depTable is allowlisted via dependencyTablesForID + guardTable
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE issue_id = $1 AND depends_on_id = $2`, depTable)
	if _, err := c.Exec(ctx, stmt, issueID, dependsOnID); err != nil {
		return wrapErr("remove dependency", err)
	}
	return nil
}

// GetDependencies returns the issues that issueID depends on.
func (s *PostgresStore) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	q := fmt.Sprintf(`
		SELECT %s FROM issues i
		WHERE id IN (SELECT depends_on_id FROM dependencies WHERE issue_id = $1)
		ORDER BY i.created_at ASC
	`, prefixedIssueColumns("i"))
	rows, err := s.pool.Query(ctx, q, issueID)
	if err != nil {
		return nil, wrapErr("get dependencies", err)
	}
	return scanIssues(rows)
}

// GetDependents returns the issues that depend on issueID.
func (s *PostgresStore) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) {
	q := fmt.Sprintf(`
		SELECT %s FROM issues i
		WHERE id IN (SELECT issue_id FROM dependencies WHERE depends_on_id = $1)
		ORDER BY i.created_at ASC
	`, prefixedIssueColumns("i"))
	rows, err := s.pool.Query(ctx, q, issueID)
	if err != nil {
		return nil, wrapErr("get dependents", err)
	}
	return scanIssues(rows)
}

// GetDependenciesWithMetadata returns Issues + the dep type linking them.
func (s *PostgresStore) GetDependenciesWithMetadata(ctx context.Context, issueID string) ([]*types.IssueWithDependencyMetadata, error) {
	q := fmt.Sprintf(`
		SELECT %s, d.type
		FROM issues i
		JOIN dependencies d ON d.depends_on_id = i.id
		WHERE d.issue_id = $1
		ORDER BY i.created_at ASC
	`, prefixedIssueColumns("i"))
	return scanIssuesWithDepType(ctx, s.pool, q, issueID)
}

// GetDependentsWithMetadata mirrors GetDependenciesWithMetadata, reversing the join.
func (s *PostgresStore) GetDependentsWithMetadata(ctx context.Context, issueID string) ([]*types.IssueWithDependencyMetadata, error) {
	q := fmt.Sprintf(`
		SELECT %s, d.type
		FROM issues i
		JOIN dependencies d ON d.issue_id = i.id
		WHERE d.depends_on_id = $1
		ORDER BY i.created_at ASC
	`, prefixedIssueColumns("i"))
	return scanIssuesWithDepType(ctx, s.pool, q, issueID)
}

func scanIssuesWithDepType(ctx context.Context, c pgxConn, q string, args ...any) ([]*types.IssueWithDependencyMetadata, error) {
	rows, err := c.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapErr("query issues with dep type", err)
	}
	defer rows.Close()
	var out []*types.IssueWithDependencyMetadata
	for rows.Next() {
		var r issueScanRow
		var depType string
		dest := append(r.dest(), &depType)
		if err := rows.Scan(dest...); err != nil {
			return nil, wrapErr("scan issues with dep type", err)
		}
		entry := &types.IssueWithDependencyMetadata{
			Issue:          *r.toIssue(),
			DependencyType: types.DependencyType(depType),
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

// GetDependencyRecords returns the raw Dependency rows for one issue.
func (s *PostgresStore) GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	return getDependencyRecords(ctx, s.pool, []string{issueID})
}

// GetDependencyRecordsForIssues batches GetDependencyRecords for many issues.
func (s *PostgresStore) GetDependencyRecordsForIssues(ctx context.Context, issueIDs []string) (map[string][]*types.Dependency, error) {
	if len(issueIDs) == 0 {
		return map[string][]*types.Dependency{}, nil
	}
	deps, err := getDependencyRecords(ctx, s.pool, issueIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]*types.Dependency, len(issueIDs))
	for _, d := range deps {
		out[d.IssueID] = append(out[d.IssueID], d)
	}
	return out, nil
}

// GetAllDependencyRecords reads every persistent dependency edge.
func (s *PostgresStore) GetAllDependencyRecords(ctx context.Context) (map[string][]*types.Dependency, error) {
	deps, err := readAllDependencies(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]*types.Dependency)
	for _, d := range deps {
		out[d.IssueID] = append(out[d.IssueID], d)
	}
	return out, nil
}

func getDependencyRecords(ctx context.Context, c pgxConn, issueIDs []string) ([]*types.Dependency, error) {
	if len(issueIDs) == 0 {
		return nil, nil
	}
	args := make([]any, len(issueIDs))
	for i, id := range issueIDs {
		args[i] = id
	}
	ph := joinPlaceholders(1, len(issueIDs))
	//nolint:gosec // placeholders bound
	q := fmt.Sprintf(`
		SELECT issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id
		FROM dependencies
		WHERE issue_id IN (%s)
		ORDER BY issue_id, created_at ASC
	`, ph)
	rows, err := c.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapErr("get dependency records", err)
	}
	return scanDependencyRows(rows)
}

func readAllDependencies(ctx context.Context, c pgxConn) ([]*types.Dependency, error) {
	q := `SELECT issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id FROM dependencies ORDER BY issue_id, created_at ASC`
	rows, err := c.Query(ctx, q)
	if err != nil {
		return nil, wrapErr("read all dependencies", err)
	}
	return scanDependencyRows(rows)
}

func scanDependencyRows(rows pgx.Rows) ([]*types.Dependency, error) {
	defer rows.Close()
	var out []*types.Dependency
	for rows.Next() {
		d := &types.Dependency{}
		var metadata []byte
		var depType, threadID string
		if err := rows.Scan(&d.IssueID, &d.DependsOnID, &depType, &d.CreatedAt, &d.CreatedBy, &metadata, &threadID); err != nil {
			return nil, wrapErr("scan dependency", err)
		}
		d.Type = types.DependencyType(depType)
		d.ThreadID = threadID
		if len(metadata) > 0 && string(metadata) != "{}" {
			d.Metadata = string(metadata)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetDependencyCounts returns counts of dependencies and dependents per issue.
func (s *PostgresStore) GetDependencyCounts(ctx context.Context, issueIDs []string) (map[string]*types.DependencyCounts, error) {
	out := make(map[string]*types.DependencyCounts, len(issueIDs))
	if len(issueIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(issueIDs))
	for i, id := range issueIDs {
		args[i] = id
	}
	ph := joinPlaceholders(1, len(issueIDs))
	//nolint:gosec // placeholders bound
	q1 := fmt.Sprintf(`SELECT issue_id, COUNT(*)::int FROM dependencies WHERE issue_id IN (%s) GROUP BY issue_id`, ph)
	rows, err := s.pool.Query(ctx, q1, args...)
	if err != nil {
		return nil, wrapErr("count dependencies", err)
	}
	if err := func() error {
		defer rows.Close()
		for rows.Next() {
			var id string
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				return err
			}
			if out[id] == nil {
				out[id] = &types.DependencyCounts{}
			}
			out[id].DependencyCount = n
		}
		return rows.Err()
	}(); err != nil {
		return nil, wrapErr("scan dep counts", err)
	}
	//nolint:gosec // placeholders bound
	q2 := fmt.Sprintf(`SELECT depends_on_id, COUNT(*)::int FROM dependencies WHERE depends_on_id IN (%s) GROUP BY depends_on_id`, ph)
	rows2, err := s.pool.Query(ctx, q2, args...)
	if err != nil {
		return nil, wrapErr("count dependents", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var id string
		var n int
		if err := rows2.Scan(&id, &n); err != nil {
			return nil, wrapErr("scan dependent counts", err)
		}
		if out[id] == nil {
			out[id] = &types.DependencyCounts{}
		}
		out[id].DependentCount = n
	}
	return out, rows2.Err()
}

// IsBlocked returns whether the issue has any unsatisfied blocking dep.
func (s *PostgresStore) IsBlocked(ctx context.Context, issueID string) (bool, []string, error) {
	q := `
		SELECT d.depends_on_id FROM dependencies d
		JOIN issues blocker ON blocker.id = d.depends_on_id
		WHERE d.issue_id = $1
		  AND d.type = 'blocks'
		  AND blocker.status NOT IN ('closed', 'pinned')
		ORDER BY d.depends_on_id
	`
	rows, err := s.pool.Query(ctx, q, issueID)
	if err != nil {
		return false, nil, wrapErr("is blocked", err)
	}
	defer rows.Close()
	var blockers []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return false, nil, wrapErr("scan blockers", err)
		}
		blockers = append(blockers, id)
	}
	return len(blockers) > 0, blockers, rows.Err()
}

// GetDependencyTree builds a lightweight tree of (issue, depth) entries
// rooted at issueID. v1: walks blocks + parent-child to maxDepth, no cycle
// awareness beyond depth limiting.
func (s *PostgresStore) GetDependencyTree(ctx context.Context, issueID string, maxDepth int, showAllPaths, reverse bool) ([]*types.TreeNode, error) {
	if maxDepth <= 0 {
		maxDepth = 50
	}
	root, err := s.GetIssue(ctx, issueID)
	if err != nil {
		return nil, err
	}
	out := []*types.TreeNode{{Issue: *root, Depth: 0}}
	visited := map[string]bool{issueID: true}
	queue := []*types.TreeNode{out[0]}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.Depth >= maxDepth {
			continue
		}
		var children []*types.Issue
		if reverse {
			children, err = s.GetDependents(ctx, cur.Issue.ID)
		} else {
			children, err = s.GetDependencies(ctx, cur.Issue.ID)
		}
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			if !showAllPaths && visited[child.ID] {
				continue
			}
			visited[child.ID] = true
			node := &types.TreeNode{Issue: *child, Depth: cur.Depth + 1}
			out = append(out, node)
			queue = append(queue, node)
		}
	}
	return out, nil
}

// GetBlockingInfoForIssues returns three maps used by `bd ready` /
// `bd blocked` to enrich list output.
func (s *PostgresStore) GetBlockingInfoForIssues(ctx context.Context, issueIDs []string) (
	blockedBy map[string][]string,
	blocks map[string][]string,
	parents map[string]string,
	err error,
) {
	blockedBy = make(map[string][]string)
	blocks = make(map[string][]string)
	parents = make(map[string]string)
	if len(issueIDs) == 0 {
		return blockedBy, blocks, parents, nil
	}
	args := make([]any, len(issueIDs))
	for i, id := range issueIDs {
		args[i] = id
	}
	ph := joinPlaceholders(1, len(issueIDs))
	//nolint:gosec // placeholders bound
	q := fmt.Sprintf(`
		SELECT issue_id, depends_on_id, type
		FROM dependencies
		WHERE issue_id IN (%s) OR depends_on_id IN (%s)
	`, ph, ph)
	rows, err := s.pool.Query(ctx, q, append(args, args...)...)
	if err != nil {
		return blockedBy, blocks, parents, wrapErr("get blocking info", err)
	}
	defer rows.Close()
	wanted := make(map[string]bool, len(issueIDs))
	for _, id := range issueIDs {
		wanted[id] = true
	}
	for rows.Next() {
		var issueID, dep, typ string
		if err := rows.Scan(&issueID, &dep, &typ); err != nil {
			return blockedBy, blocks, parents, wrapErr("scan blocking info", err)
		}
		switch types.DependencyType(typ) {
		case types.DepBlocks:
			if wanted[issueID] {
				blockedBy[issueID] = append(blockedBy[issueID], dep)
			}
			if wanted[dep] {
				blocks[dep] = append(blocks[dep], issueID)
			}
		case types.DepParentChild:
			if wanted[issueID] {
				parents[issueID] = dep
			}
		}
	}
	return blockedBy, blocks, parents, rows.Err()
}

// GetNewlyUnblockedByClose returns the issues that become ready when
// closedIssueID transitions to closed.
func (s *PostgresStore) GetNewlyUnblockedByClose(ctx context.Context, closedIssueID string) ([]*types.Issue, error) {
	q := fmt.Sprintf(`
		SELECT %s FROM issues i
		WHERE EXISTS (
			SELECT 1 FROM dependencies d
			WHERE d.issue_id = i.id AND d.depends_on_id = $1 AND d.type = 'blocks'
		)
		AND i.status = 'open'
		AND NOT EXISTS (
			SELECT 1 FROM dependencies d2
			JOIN issues other ON other.id = d2.depends_on_id
			WHERE d2.issue_id = i.id
			  AND d2.depends_on_id <> $1
			  AND d2.type = 'blocks'
			  AND other.status NOT IN ('closed', 'pinned')
		)
	`, prefixedIssueColumns("i"))
	rows, err := s.pool.Query(ctx, q, closedIssueID)
	if err != nil {
		return nil, wrapErr("get newly unblocked", err)
	}
	return scanIssues(rows)
}

// DetectCycles walks the graph looking for blocks/parent-child cycles. The
// implementation is a straightforward DFS — the v1 expectation is occasional
// admin use, not hot-path validation.
func (s *PostgresStore) DetectCycles(ctx context.Context) ([][]*types.Issue, error) {
	deps, err := readAllDependencies(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	adj := map[string][]string{}
	for _, d := range deps {
		if !d.Type.AffectsReadyWork() {
			continue
		}
		adj[d.IssueID] = append(adj[d.IssueID], d.DependsOnID)
	}
	var cycles [][]string
	state := map[string]int{} // 0=unseen, 1=on stack, 2=done
	var stack []string
	var visit func(id string)
	visit = func(id string) {
		state[id] = 1
		stack = append(stack, id)
		for _, next := range adj[id] {
			switch state[next] {
			case 0:
				visit(next)
			case 1:
				start := -1
				for i, s := range stack {
					if s == next {
						start = i
						break
					}
				}
				if start >= 0 {
					cycle := append([]string{}, stack[start:]...)
					cycles = append(cycles, cycle)
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = 2
	}
	for node := range adj {
		if state[node] == 0 {
			visit(node)
		}
	}
	if len(cycles) == 0 {
		return nil, nil
	}
	out := make([][]*types.Issue, 0, len(cycles))
	for _, c := range cycles {
		issues := make([]*types.Issue, 0, len(c))
		for _, id := range c {
			if issue, err := s.GetIssue(ctx, id); err == nil {
				issues = append(issues, issue)
			}
		}
		out = append(out, issues)
	}
	return out, nil
}

// FindWispDependentsRecursive walks both regular and wisp dep tables to find
// every wisp transitively dependent on the given IDs.
func (s *PostgresStore) FindWispDependentsRecursive(ctx context.Context, ids []string) (map[string]bool, error) {
	out := make(map[string]bool)
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	ph := joinPlaceholders(1, len(ids))
	//nolint:gosec // placeholders bound
	q := fmt.Sprintf(`
		WITH RECURSIVE seen(id) AS (
			SELECT issue_id FROM wisp_dependencies WHERE depends_on_id IN (%s)
			UNION
			SELECT wd.issue_id FROM wisp_dependencies wd
			JOIN seen s ON wd.depends_on_id = s.id
		)
		SELECT id FROM seen
	`, ph)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapErr("find wisp dependents", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, wrapErr("scan wisp dependents", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

// RenameDependencyPrefix updates issue/depends_on IDs that share the old prefix.
// v1 issues a single UPDATE — counters and orphan handling are out of scope.
func (s *PostgresStore) RenameDependencyPrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	old := strings.TrimSuffix(oldPrefix, "-") + "-"
	rep := strings.TrimSuffix(newPrefix, "-") + "-"
	stmt := `
		UPDATE dependencies SET
			issue_id      = REGEXP_REPLACE(issue_id, '^' || $1, $2),
			depends_on_id = REGEXP_REPLACE(depends_on_id, '^' || $1, $2)
		WHERE issue_id LIKE $3 OR depends_on_id LIKE $3
	`
	if _, err := s.pool.Exec(ctx, stmt, old, rep, old+"%"); err != nil {
		return wrapErr("rename dependency prefix", err)
	}
	return nil
}

// importDependencyRow is exposed for the migration command to bulk-load deps
// without cycle checks.
func importDependencyRow(ctx context.Context, c pgxConn, dep *types.Dependency) error {
	return addDependencyRow(ctx, c, dep, true)
}

// metadataMustBeJSON ensures dep metadata is valid JSON before going to JSONB.
func metadataMustBeJSON(s string) error {
	if s == "" {
		return nil
	}
	if !json.Valid([]byte(s)) {
		return errors.New("dependency metadata is not valid JSON")
	}
	return nil
}
