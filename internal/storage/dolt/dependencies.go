package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// issueColumns is the full column list for SELECT queries on the issues table.
// Used by SearchIssues and other queries that need to return full issue records.
// IMPORTANT: This must match the order expected by scanIssueRow.
const issueColumns = `id, content_hash, title, description, design, acceptance_criteria, notes,
       status, priority, issue_type, assignee, estimated_minutes,
       created_at, created_by, owner, updated_at, closed_at, external_ref,
       compaction_level, compacted_at, compacted_at_commit, original_size, source_repo, close_reason,
       deleted_at, deleted_by, delete_reason, original_type,
       sender, ephemeral, pinned, is_template, crystallizes,
       await_type, await_id, timeout_ns, waiters,
       hook_bead, role_bead, agent_state, last_activity, role_type, rig,
       pod_name, pod_ip, pod_node, pod_status, screen_session,
       mol_type,
       event_kind, actor, target, payload,
       due_at, defer_until,
       quality_score, work_type, source_system, metadata,
       advice_hook_command, advice_hook_trigger, advice_hook_timeout, advice_hook_on_failure`

// prefixColumns adds a table alias prefix to each column in the issueColumns list.
// Used for JOIN queries where columns need to be qualified with a table alias.
func prefixColumns(prefix, columns string) string {
	// Split by comma, trim whitespace, add prefix, rejoin
	cols := strings.Split(columns, ",")
	for i, col := range cols {
		cols[i] = prefix + strings.TrimSpace(col)
	}
	return strings.Join(cols, ", ")
}

// AddDependency adds a dependency between two issues.
// Delegates to the transaction method for single-source-of-truth logic.
func (s *DoltStore) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	return s.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.AddDependency(ctx, dep, actor)
	})
}

// RemoveDependency removes a dependency between two issues.
// Delegates to the transaction method for single-source-of-truth logic.
func (s *DoltStore) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	return s.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.RemoveDependency(ctx, issueID, dependsOnID, actor)
	})
}

// GetDependencies retrieves issues that this issue depends on
func (s *DoltStore) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	// Direct SELECT * query - avoids the two-query anti-pattern
	query := fmt.Sprintf(`
		SELECT %s FROM issues i
		JOIN dependencies d ON i.id = d.depends_on_id
		WHERE d.issue_id = ?
		ORDER BY i.priority ASC, i.created_at DESC
	`, prefixColumns("i.", issueColumns))

	rows, err := s.queryContext(ctx, query, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependencies: %w", err)
	}
	defer rows.Close()

	var issues []*types.Issue
	for rows.Next() {
		issue, err := scanIssueRow(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetDependents retrieves issues that depend on this issue
func (s *DoltStore) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) {
	// Direct SELECT * query - avoids the two-query anti-pattern
	query := fmt.Sprintf(`
		SELECT %s FROM issues i
		JOIN dependencies d ON i.id = d.issue_id
		WHERE d.depends_on_id = ?
		ORDER BY i.priority ASC, i.created_at DESC
	`, prefixColumns("i.", issueColumns))

	rows, err := s.queryContext(ctx, query, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependents: %w", err)
	}
	defer rows.Close()

	var issues []*types.Issue
	for rows.Next() {
		issue, err := scanIssueRow(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetDependenciesWithMetadata returns dependencies with metadata
func (s *DoltStore) GetDependenciesWithMetadata(ctx context.Context, issueID string) ([]*types.IssueWithDependencyMetadata, error) {
	rows, err := s.queryContext(ctx, `
		SELECT d.depends_on_id, d.type, d.created_at, d.created_by, d.metadata, d.thread_id
		FROM dependencies d
		WHERE d.issue_id = ?
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependencies with metadata: %w", err)
	}

	// Collect scan data first, then close rows before making nested queries.
	// Dolt embedded mode can't handle multiple open queries on the same connection.
	type depRow struct {
		depID     string
		depType   string
		createdAt sql.NullTime
		createdBy string
		metadata  sql.NullString
		threadID  sql.NullString
	}
	var depRows []depRow
	for rows.Next() {
		var r depRow
		if err := rows.Scan(&r.depID, &r.depType, &r.createdAt, &r.createdBy, &r.metadata, &r.threadID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan dependency: %w", err)
		}
		depRows = append(depRows, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var results []*types.IssueWithDependencyMetadata
	for _, r := range depRows {
		issue, err := s.GetIssue(ctx, r.depID)
		if err != nil {
			return nil, err
		}
		if issue == nil {
			continue
		}

		result := &types.IssueWithDependencyMetadata{
			Issue:          *issue,
			DependencyType: types.DependencyType(r.depType),
		}
		results = append(results, result)
	}
	return results, nil
}

// GetDependentsWithMetadata returns dependents with metadata
func (s *DoltStore) GetDependentsWithMetadata(ctx context.Context, issueID string) ([]*types.IssueWithDependencyMetadata, error) {
	rows, err := s.queryContext(ctx, `
		SELECT d.issue_id, d.type, d.created_at, d.created_by, d.metadata, d.thread_id
		FROM dependencies d
		WHERE d.depends_on_id = ?
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependents with metadata: %w", err)
	}

	// Collect scan data first, then close rows before making nested queries.
	// Dolt embedded mode can't handle multiple open queries on the same connection.
	type depRow struct {
		depID     string
		depType   string
		createdAt sql.NullTime
		createdBy string
		metadata  sql.NullString
		threadID  sql.NullString
	}
	var depRows []depRow
	for rows.Next() {
		var r depRow
		if err := rows.Scan(&r.depID, &r.depType, &r.createdAt, &r.createdBy, &r.metadata, &r.threadID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan dependent: %w", err)
		}
		depRows = append(depRows, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var results []*types.IssueWithDependencyMetadata
	for _, r := range depRows {
		issue, err := s.GetIssue(ctx, r.depID)
		if err != nil {
			return nil, err
		}
		if issue == nil {
			continue
		}

		result := &types.IssueWithDependencyMetadata{
			Issue:          *issue,
			DependencyType: types.DependencyType(r.depType),
		}
		results = append(results, result)
	}
	return results, nil
}

// GetDependencyRecords returns raw dependency records for an issue
func (s *DoltStore) GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	rows, err := s.queryContext(ctx, `
		SELECT issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id
		FROM dependencies
		WHERE issue_id = ?
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependency records: %w", err)
	}
	defer rows.Close()

	return scanDependencyRows(rows)
}

// GetAllDependencyRecords returns all dependency records
func (s *DoltStore) GetAllDependencyRecords(ctx context.Context) (map[string][]*types.Dependency, error) {
	rows, err := s.queryContext(ctx, `
		SELECT issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id
		FROM dependencies
		ORDER BY issue_id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get all dependency records: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]*types.Dependency)
	for rows.Next() {
		dep, err := scanDependencyRow(rows)
		if err != nil {
			return nil, err
		}
		result[dep.IssueID] = append(result[dep.IssueID], dep)
	}
	return result, rows.Err()
}

// GetDependencyRecordsForIssues returns dependency records for specific issues.
// Uses batched IN clauses to avoid oversized queries that crush Dolt CPU.
func (s *DoltStore) GetDependencyRecordsForIssues(ctx context.Context, issueIDs []string) (map[string][]*types.Dependency, error) {
	return BatchIN(ctx, s.db, issueIDs, DefaultBatchSize,
		`SELECT issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id FROM dependencies WHERE issue_id IN (%s) ORDER BY issue_id`,
		func(rows *sql.Rows) (string, *types.Dependency, error) {
			dep, err := scanDependencyRow(rows)
			if err != nil {
				return "", nil, err
			}
			return dep.IssueID, dep, nil
		},
	)
}

// GetDependencyCounts returns dependency counts for multiple issues.
// Uses batched IN clauses to avoid oversized queries that crush Dolt CPU.
func (s *DoltStore) GetDependencyCounts(ctx context.Context, issueIDs []string) (map[string]*types.DependencyCounts, error) {
	if len(issueIDs) == 0 {
		return make(map[string]*types.DependencyCounts), nil
	}

	result := make(map[string]*types.DependencyCounts)
	for _, id := range issueIDs {
		result[id] = &types.DependencyCounts{}
	}

	// Query for dependencies (blockers) in batches
	depCounts, err := BatchIN(ctx, s.db, issueIDs, DefaultBatchSize,
		`SELECT issue_id, COUNT(*) as cnt FROM dependencies WHERE issue_id IN (%s) AND type = 'blocks' GROUP BY issue_id`,
		func(rows *sql.Rows) (string, int, error) {
			var id string
			var cnt int
			err := rows.Scan(&id, &cnt)
			return id, cnt, err
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependency counts: %w", err)
	}
	for id, counts := range depCounts {
		if c, ok := result[id]; ok && len(counts) > 0 {
			c.DependencyCount = counts[0]
		}
	}

	// Query for dependents (blocking) in batches
	blockingCounts, err := BatchIN(ctx, s.db, issueIDs, DefaultBatchSize,
		`SELECT depends_on_id, COUNT(*) as cnt FROM dependencies WHERE depends_on_id IN (%s) AND type = 'blocks' GROUP BY depends_on_id`,
		func(rows *sql.Rows) (string, int, error) {
			var id string
			var cnt int
			err := rows.Scan(&id, &cnt)
			return id, cnt, err
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get blocking counts: %w", err)
	}
	for id, counts := range blockingCounts {
		if c, ok := result[id]; ok && len(counts) > 0 {
			c.DependentCount = counts[0]
		}
	}

	return result, nil
}

// GetDependencyTree returns a dependency tree for visualization
func (s *DoltStore) GetDependencyTree(ctx context.Context, issueID string, maxDepth int, showAllPaths bool, reverse bool) ([]*types.TreeNode, error) {
	// Simple implementation - can be optimized with CTE
	visited := make(map[string]bool)
	return s.buildDependencyTree(ctx, issueID, 0, maxDepth, reverse, visited)
}

func (s *DoltStore) buildDependencyTree(ctx context.Context, issueID string, depth, maxDepth int, reverse bool, visited map[string]bool) ([]*types.TreeNode, error) {
	if depth >= maxDepth || visited[issueID] {
		return nil, nil
	}
	visited[issueID] = true

	issue, err := s.GetIssue(ctx, issueID)
	if err != nil || issue == nil {
		return nil, err
	}

	var childIDs []string
	var query string
	if reverse {
		query = "SELECT issue_id FROM dependencies WHERE depends_on_id = ?"
	} else {
		query = "SELECT depends_on_id FROM dependencies WHERE issue_id = ?"
	}

	rows, err := s.queryContext(ctx, query, issueID)
	if err != nil {
		return nil, err
	}

	// Collect IDs first, then close rows before making nested queries.
	// Dolt embedded mode can't handle multiple open queries on the same connection.
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		childIDs = append(childIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	node := &types.TreeNode{
		Issue: *issue,
		Depth: depth,
	}

	// TreeNode doesn't have Children field - return flat list
	nodes := []*types.TreeNode{node}
	for _, childID := range childIDs {
		children, err := s.buildDependencyTree(ctx, childID, depth+1, maxDepth, reverse, visited)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, children...)
	}

	return nodes, nil
}

// DetectCycles finds circular dependencies
func (s *DoltStore) DetectCycles(ctx context.Context) ([][]*types.Issue, error) {
	// Get all dependencies
	deps, err := s.GetAllDependencyRecords(ctx)
	if err != nil {
		return nil, err
	}

	// Build adjacency list
	graph := make(map[string][]string)
	for issueID, records := range deps {
		for _, dep := range records {
			if dep.Type == types.DepBlocks {
				graph[issueID] = append(graph[issueID], dep.DependsOnID)
			}
		}
	}

	// Find cycles using DFS
	var cycles [][]*types.Issue
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	path := make([]string, 0)

	var dfs func(node string) bool
	dfs = func(node string) bool {
		visited[node] = true
		recStack[node] = true
		path = append(path, node)

		for _, neighbor := range graph[node] {
			if !visited[neighbor] {
				if dfs(neighbor) {
					return true
				}
			} else if recStack[neighbor] {
				// Found cycle - extract it
				cycleStart := -1
				for i, n := range path {
					if n == neighbor {
						cycleStart = i
						break
					}
				}
				if cycleStart >= 0 {
					cyclePath := path[cycleStart:]
					var cycleIssues []*types.Issue
					for _, id := range cyclePath {
						issue, _ := s.GetIssue(ctx, id)
						if issue != nil {
							cycleIssues = append(cycleIssues, issue)
						}
					}
					if len(cycleIssues) > 0 {
						cycles = append(cycles, cycleIssues)
					}
				}
			}
		}

		path = path[:len(path)-1]
		recStack[node] = false
		return false
	}

	for node := range graph {
		if !visited[node] {
			dfs(node)
		}
	}

	return cycles, nil
}

// IsBlocked checks if an issue has open blockers
func (s *DoltStore) IsBlocked(ctx context.Context, issueID string) (bool, []string, error) {
	rows, err := s.queryContext(ctx, `
		SELECT d.depends_on_id
		FROM dependencies d
		JOIN issues i ON d.depends_on_id = i.id
		WHERE d.issue_id = ?
		  AND d.type = 'blocks'
		  AND i.status IN ('open', 'in_progress', 'blocked', 'deferred', 'hooked')
	`, issueID)
	if err != nil {
		return false, nil, fmt.Errorf("failed to check blockers: %w", err)
	}
	defer rows.Close()

	var blockers []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return false, nil, err
		}
		blockers = append(blockers, id)
	}

	return len(blockers) > 0, blockers, rows.Err()
}

// GetNewlyUnblockedByClose finds issues that become unblocked when an issue is closed
func (s *DoltStore) GetNewlyUnblockedByClose(ctx context.Context, closedIssueID string) ([]*types.Issue, error) {
	// Direct SELECT with full columns - avoids the two-query anti-pattern
	// (SELECT id then SELECT WHERE id IN) which creates massive IN clauses
	query := fmt.Sprintf(`
		SELECT DISTINCT %s
		FROM issues i
		JOIN dependencies d ON d.issue_id = i.id
		WHERE d.depends_on_id = ?
		  AND d.type = 'blocks'
		  AND i.status IN ('open', 'blocked')
		  AND NOT EXISTS (
			SELECT 1 FROM dependencies d2
			JOIN issues blocker ON d2.depends_on_id = blocker.id
			WHERE d2.issue_id = d.issue_id
			  AND d2.type = 'blocks'
			  AND d2.depends_on_id != ?
			  AND blocker.status IN ('open', 'in_progress', 'blocked', 'deferred', 'hooked')
		  )
	`, prefixColumns("i.", issueColumns))

	rows, err := s.queryContext(ctx, query, closedIssueID, closedIssueID)
	if err != nil {
		return nil, fmt.Errorf("failed to find newly unblocked: %w", err)
	}
	defer rows.Close()

	var issues []*types.Issue
	for rows.Next() {
		issue, err := scanIssueRow(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// GetIssuesByIDs retrieves multiple issues by ID in a single query.
// NOTE: This should only be used for small sets of IDs. For queries that could return
// large result sets, use SearchIssues with appropriate filters instead (it does direct SELECT).
func (s *DoltStore) GetIssuesByIDs(ctx context.Context, ids []string) ([]*types.Issue, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build IN clause
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	// nolint:gosec // G201: placeholders contains only ? markers, actual values passed via args
	query := fmt.Sprintf(`
		SELECT %s
		FROM issues
		WHERE id IN (%s)
	`, issueColumns, strings.Join(placeholders, ","))

	queryRows, err := s.queryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get issues by IDs: %w", err)
	}
	defer queryRows.Close()

	var issues []*types.Issue
	for queryRows.Next() {
		issue, err := scanIssueRow(queryRows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}

	return issues, queryRows.Err()
}

// scanIssueRow scans a single issue from a rows result
func scanIssueRow(rows rowScanner) (*types.Issue, error) {
	var issue types.Issue
	var createdAtStr, updatedAtStr sql.NullString // TEXT columns - must parse manually
	var closedAt, compactedAt, deletedAt, lastActivity, dueAt, deferUntil sql.NullTime
	var estimatedMinutes, originalSize, timeoutNs sql.NullInt64
	var assignee, externalRef, compactedAtCommit, owner, createdBy sql.NullString
	var contentHash, sourceRepo, closeReason, deletedBy, deleteReason, originalType sql.NullString
	var workType, sourceSystem sql.NullString
	var sender, molType, eventKind, actor, target, payload sql.NullString
	var awaitType, awaitID, waiters sql.NullString
	var hookBead, roleBead, agentState, roleType, rig sql.NullString
	var podName, podIP, podNode, podStatus, screenSession sql.NullString
	var ephemeral, pinned, isTemplate, crystallizes sql.NullInt64
	var qualityScore sql.NullFloat64
	var metadata sql.NullString
	// NOTE: advice_target_* fields removed - advice uses labels now
	// Advice hook fields (hq--uaim)
	var adviceHookCommand, adviceHookTrigger, adviceHookOnFailure sql.NullString
	var adviceHookTimeout sql.NullInt64

	if err := rows.Scan(
		&issue.ID, &contentHash, &issue.Title, &issue.Description, &issue.Design,
		&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
		&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
		&createdAtStr, &createdBy, &owner, &updatedAtStr, &closedAt, &externalRef,
		&issue.CompactionLevel, &compactedAt, &compactedAtCommit, &originalSize, &sourceRepo, &closeReason,
		&deletedAt, &deletedBy, &deleteReason, &originalType,
		&sender, &ephemeral, &pinned, &isTemplate, &crystallizes,
		&awaitType, &awaitID, &timeoutNs, &waiters,
		&hookBead, &roleBead, &agentState, &lastActivity, &roleType, &rig,
		&podName, &podIP, &podNode, &podStatus, &screenSession,
		&molType,
		&eventKind, &actor, &target, &payload,
		&dueAt, &deferUntil,
		&qualityScore, &workType, &sourceSystem, &metadata,
		&adviceHookCommand, &adviceHookTrigger, &adviceHookTimeout, &adviceHookOnFailure,
	); err != nil {
		return nil, fmt.Errorf("failed to scan issue row: %w", err)
	}

	// Parse timestamp strings (TEXT columns require manual parsing)
	if createdAtStr.Valid {
		issue.CreatedAt = parseTimeString(createdAtStr.String)
	}
	if updatedAtStr.Valid {
		issue.UpdatedAt = parseTimeString(updatedAtStr.String)
	}

	// Map nullable fields
	if contentHash.Valid {
		issue.ContentHash = contentHash.String
	}
	if closedAt.Valid {
		issue.ClosedAt = &closedAt.Time
	}
	if estimatedMinutes.Valid {
		mins := int(estimatedMinutes.Int64)
		issue.EstimatedMinutes = &mins
	}
	if assignee.Valid {
		issue.Assignee = assignee.String
	}
	if owner.Valid {
		issue.Owner = owner.String
	}
	if createdBy.Valid {
		issue.CreatedBy = createdBy.String
	}
	if externalRef.Valid {
		issue.ExternalRef = &externalRef.String
	}
	if compactedAt.Valid {
		issue.CompactedAt = &compactedAt.Time
	}
	if compactedAtCommit.Valid {
		issue.CompactedAtCommit = &compactedAtCommit.String
	}
	if originalSize.Valid {
		issue.OriginalSize = int(originalSize.Int64)
	}
	if sourceRepo.Valid {
		issue.SourceRepo = sourceRepo.String
	}
	if closeReason.Valid {
		issue.CloseReason = closeReason.String
	}
	if deletedAt.Valid {
		issue.DeletedAt = &deletedAt.Time
	}
	if deletedBy.Valid {
		issue.DeletedBy = deletedBy.String
	}
	if deleteReason.Valid {
		issue.DeleteReason = deleteReason.String
	}
	if originalType.Valid {
		issue.OriginalType = originalType.String
	}
	if sender.Valid {
		issue.Sender = sender.String
	}
	if ephemeral.Valid && ephemeral.Int64 != 0 {
		issue.Ephemeral = true
	}
	if pinned.Valid && pinned.Int64 != 0 {
		issue.Pinned = true
	}
	if isTemplate.Valid && isTemplate.Int64 != 0 {
		issue.IsTemplate = true
	}
	if crystallizes.Valid && crystallizes.Int64 != 0 {
		issue.Crystallizes = true
	}
	if awaitType.Valid {
		issue.AwaitType = awaitType.String
	}
	if awaitID.Valid {
		issue.AwaitID = awaitID.String
	}
	if timeoutNs.Valid {
		issue.Timeout = time.Duration(timeoutNs.Int64)
	}
	if waiters.Valid && waiters.String != "" {
		issue.Waiters = parseJSONStringArray(waiters.String)
	}
	if hookBead.Valid {
		issue.HookBead = hookBead.String
	}
	if roleBead.Valid {
		issue.RoleBead = roleBead.String
	}
	if agentState.Valid {
		issue.AgentState = types.AgentState(agentState.String)
	}
	if lastActivity.Valid {
		issue.LastActivity = &lastActivity.Time
	}
	if roleType.Valid {
		issue.RoleType = roleType.String
	}
	if rig.Valid {
		issue.Rig = rig.String
	}
	if podName.Valid {
		issue.PodName = podName.String
	}
	if podIP.Valid {
		issue.PodIP = podIP.String
	}
	if podNode.Valid {
		issue.PodNode = podNode.String
	}
	if podStatus.Valid {
		issue.PodStatus = podStatus.String
	}
	if screenSession.Valid {
		issue.ScreenSession = screenSession.String
	}
	if molType.Valid {
		issue.MolType = types.MolType(molType.String)
	}
	if eventKind.Valid {
		issue.EventKind = eventKind.String
	}
	if actor.Valid {
		issue.Actor = actor.String
	}
	if target.Valid {
		issue.Target = target.String
	}
	if payload.Valid {
		issue.Payload = payload.String
	}
	if dueAt.Valid {
		issue.DueAt = &dueAt.Time
	}
	if deferUntil.Valid {
		issue.DeferUntil = &deferUntil.Time
	}
	if qualityScore.Valid {
		qs := float32(qualityScore.Float64)
		issue.QualityScore = &qs
	}
	if workType.Valid {
		issue.WorkType = types.WorkType(workType.String)
	}
	if sourceSystem.Valid {
		issue.SourceSystem = sourceSystem.String
	}
	if metadata.Valid && metadata.String != "" && metadata.String != "{}" {
		issue.Metadata = []byte(metadata.String)
	}
	// NOTE: advice_target_* fields removed - advice uses labels now
	// Advice hook fields (hq--uaim)
	if adviceHookCommand.Valid {
		issue.AdviceHookCommand = adviceHookCommand.String
	}
	if adviceHookTrigger.Valid {
		issue.AdviceHookTrigger = adviceHookTrigger.String
	}
	if adviceHookTimeout.Valid {
		issue.AdviceHookTimeout = int(adviceHookTimeout.Int64)
	}
	if adviceHookOnFailure.Valid {
		issue.AdviceHookOnFailure = adviceHookOnFailure.String
	}

	return &issue, nil
}

func scanDependencyRows(rows rowIterator) ([]*types.Dependency, error) {
	var deps []*types.Dependency
	for rows.Next() {
		dep, err := scanDependencyRow(rows)
		if err != nil {
			return nil, err
		}
		deps = append(deps, dep)
	}
	return deps, rows.Err()
}

func scanDependencyRow(rows rowScanner) (*types.Dependency, error) {
	var dep types.Dependency
	var createdAt sql.NullTime
	var metadata, threadID sql.NullString

	if err := rows.Scan(&dep.IssueID, &dep.DependsOnID, &dep.Type, &createdAt, &dep.CreatedBy, &metadata, &threadID); err != nil {
		return nil, fmt.Errorf("failed to scan dependency: %w", err)
	}

	if createdAt.Valid {
		dep.CreatedAt = createdAt.Time
	}
	if threadID.Valid {
		dep.ThreadID = threadID.String
	}

	return &dep, nil
}
