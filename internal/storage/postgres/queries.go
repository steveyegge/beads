package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// GetReadyWork queries the ready_issues view, narrowed by the WorkFilter.
func (s *PostgresStore) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	clauses := []string{}
	args := []any{}
	next := 1
	add := func(clause string, vals ...any) {
		clauses = append(clauses, clause)
		args = append(args, vals...)
		next += len(vals)
	}
	if filter.Status != "" {
		add(fmt.Sprintf("status = $%d", next), string(filter.Status))
	}
	if filter.Type != "" {
		add(fmt.Sprintf("issue_type = $%d", next), filter.Type)
	}
	if filter.Priority != nil {
		add(fmt.Sprintf("priority = $%d", next), *filter.Priority)
	}
	if filter.Assignee != nil {
		add(fmt.Sprintf("assignee = $%d", next), *filter.Assignee)
	}
	if filter.Unassigned {
		clauses = append(clauses, "(assignee IS NULL OR assignee = '')")
	}
	for _, label := range filter.Labels {
		add(fmt.Sprintf("EXISTS (SELECT 1 FROM labels l WHERE l.issue_id = ready_issues.id AND l.label = $%d)", next), label)
	}
	if len(filter.LabelsAny) > 0 {
		ph := joinPlaceholders(next, len(filter.LabelsAny))
		labels := make([]any, len(filter.LabelsAny))
		for i, l := range filter.LabelsAny {
			labels[i] = l
		}
		add(fmt.Sprintf("EXISTS (SELECT 1 FROM labels l WHERE l.issue_id = ready_issues.id AND l.label IN (%s))", ph), labels...)
	}
	for _, label := range filter.ExcludeLabels {
		add(fmt.Sprintf("NOT EXISTS (SELECT 1 FROM labels l WHERE l.issue_id = ready_issues.id AND l.label = $%d)", next), label)
	}
	if !filter.IncludeEphemeral {
		clauses = append(clauses, "(ephemeral = FALSE OR ephemeral IS NULL)")
	}
	if filter.MetadataFields != nil {
		for k, v := range filter.MetadataFields {
			add(fmt.Sprintf("metadata->>%s = $%d", quoteJSONLit(k), next), v)
		}
	}
	if filter.HasMetadataKey != "" {
		add(fmt.Sprintf("metadata ? $%d", next), filter.HasMetadataKey)
	}
	if !filter.IncludeDeferred {
		clauses = append(clauses, "(defer_until IS NULL OR defer_until <= NOW())")
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	order := orderClause(filter.SortPolicy)
	limit := ""
	if filter.Limit > 0 {
		limit = fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	//nolint:gosec // placeholders bound, identifiers static
	q := fmt.Sprintf(`SELECT %s FROM ready_issues%s%s%s`, issueColumns, where, order, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapErr("get ready work", err)
	}
	issues, err := scanIssues(rows)
	if err != nil {
		return nil, wrapErr("scan ready work", err)
	}
	for _, issue := range issues {
		issue.Labels, _ = getLabelsFromTable(ctx, s.pool, "labels", issue.ID)
	}
	return issues, nil
}

func orderClause(p types.SortPolicy) string {
	switch p {
	case types.SortPolicyOldest:
		return ` ORDER BY created_at ASC`
	case types.SortPolicyPriority:
		return ` ORDER BY priority ASC, created_at ASC`
	default:
		// Hybrid: recent (within 48h) by priority, older by age.
		return ` ORDER BY
			CASE WHEN created_at >= NOW() - INTERVAL '48 hours' THEN 0 ELSE 1 END,
			priority ASC,
			created_at ASC`
	}
}

// quoteJSONLit returns a single-quoted SQL string literal for a JSON path
// component. Used only with column names like metadata->> followed by a key.
// We never accept untrusted input here — callers pass map keys that came from
// the API surface.
func quoteJSONLit(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// GetBlockedIssues queries the blocked_issues view.
func (s *PostgresStore) GetBlockedIssues(ctx context.Context, filter types.WorkFilter) ([]*types.BlockedIssue, error) {
	q := fmt.Sprintf(`SELECT %s, blocked_by_count FROM blocked_issues ORDER BY priority ASC, updated_at DESC`, issueColumns)
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, wrapErr("get blocked issues", err)
	}
	defer rows.Close()
	var out []*types.BlockedIssue
	for rows.Next() {
		var r issueScanRow
		var blockedBy int
		dest := append(r.dest(), &blockedBy)
		if err := rows.Scan(dest...); err != nil {
			return nil, wrapErr("scan blocked issues", err)
		}
		blocked := &types.BlockedIssue{Issue: *r.toIssue(), BlockedByCount: blockedBy}
		out = append(out, blocked)
	}
	return out, rows.Err()
}

// GetEpicsEligibleForClosure returns epic issues whose children are all closed.
func (s *PostgresStore) GetEpicsEligibleForClosure(ctx context.Context) ([]*types.EpicStatus, error) {
	q := fmt.Sprintf(`
		WITH epics AS (
			SELECT i.id FROM issues i
			WHERE i.issue_type = 'epic' AND i.status NOT IN ('closed', 'pinned')
		),
		children AS (
			SELECT d.depends_on_id AS epic_id, d.issue_id AS child_id, c.status AS child_status
			FROM dependencies d
			JOIN epics e ON e.id = d.depends_on_id
			JOIN issues c ON c.id = d.issue_id
			WHERE d.type = 'parent-child'
		),
		stats AS (
			SELECT epic_id,
				COUNT(*)::int AS total,
				COUNT(*) FILTER (WHERE child_status = 'closed')::int AS closed
			FROM children GROUP BY epic_id
		)
		SELECT %s, stats.total, stats.closed
		FROM issues i
		JOIN stats ON stats.epic_id = i.id
		WHERE stats.total > 0 AND stats.total = stats.closed
	`, prefixedIssueColumns("i"))
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, wrapErr("get epics eligible for closure", err)
	}
	defer rows.Close()
	var out []*types.EpicStatus
	for rows.Next() {
		var r issueScanRow
		var total, closed int
		dest := append(r.dest(), &total, &closed)
		if err := rows.Scan(dest...); err != nil {
			return nil, wrapErr("scan epic closure", err)
		}
		out = append(out, &types.EpicStatus{
			Epic:             r.toIssue(),
			TotalChildren:    total,
			ClosedChildren:   closed,
			EligibleForClose: total > 0 && total == closed,
		})
	}
	return out, rows.Err()
}

// ListWisps returns the ephemeral issues matching the filter.
func (s *PostgresStore) ListWisps(ctx context.Context, filter types.WispFilter) ([]*types.Issue, error) {
	clauses := []string{}
	args := []any{}
	next := 1
	add := func(clause string, vals ...any) {
		clauses = append(clauses, clause)
		args = append(args, vals...)
		next += len(vals)
	}
	if filter.Type != nil {
		add(fmt.Sprintf("issue_type = $%d", next), string(*filter.Type))
	}
	if filter.Status != nil {
		add(fmt.Sprintf("status = $%d", next), string(*filter.Status))
	} else if !filter.IncludeClosed {
		clauses = append(clauses, "status NOT IN ('closed', 'pinned')")
	}
	if filter.UpdatedAfter != nil {
		add(fmt.Sprintf("updated_at > $%d", next), *filter.UpdatedAfter)
	}
	if filter.UpdatedBefore != nil {
		add(fmt.Sprintf("updated_at < $%d", next), *filter.UpdatedBefore)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	limit := ""
	if filter.Limit > 0 {
		limit = fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	//nolint:gosec // placeholders bound, identifiers static
	q := fmt.Sprintf(`SELECT %s FROM wisps%s ORDER BY updated_at DESC%s`, issueColumns, where, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapErr("list wisps", err)
	}
	return scanIssues(rows)
}

// GetStatistics returns simple counts. Sufficient for `bd stats` v1.
func (s *PostgresStore) GetStatistics(ctx context.Context) (*types.Statistics, error) {
	q := `
		SELECT
			COUNT(*) FILTER (WHERE status = 'open')::int AS open,
			COUNT(*) FILTER (WHERE status = 'in_progress')::int AS in_progress,
			COUNT(*) FILTER (WHERE status = 'blocked')::int AS blocked,
			COUNT(*) FILTER (WHERE status = 'closed')::int AS closed,
			COUNT(*)::int AS total
		FROM issues
	`
	stats := &types.Statistics{}
	if err := s.pool.QueryRow(ctx, q).Scan(&stats.OpenIssues, &stats.InProgressIssues, &stats.BlockedIssues, &stats.ClosedIssues, &stats.TotalIssues); err != nil {
		return nil, wrapErr("get statistics", err)
	}
	return stats, nil
}

// GetMoleculeProgress + GetMoleculeLastActivity require dependency rows that
// the v1 smoke path does not exercise. We return ErrNotFound stubs so callers
// can detect "no data" and fall through.
func (s *PostgresStore) GetMoleculeProgress(ctx context.Context, moleculeID string) (*types.MoleculeProgressStats, error) {
	if _, err := s.GetIssue(ctx, moleculeID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, moleculeID)
		}
		return nil, err
	}
	return &types.MoleculeProgressStats{}, nil
}

func (s *PostgresStore) GetMoleculeLastActivity(ctx context.Context, moleculeID string) (*types.MoleculeLastActivity, error) {
	if _, err := s.GetIssue(ctx, moleculeID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, moleculeID)
		}
		return nil, err
	}
	return &types.MoleculeLastActivity{}, nil
}

// GetStaleIssues returns issues unchanged for `filter.Days` days.
func (s *PostgresStore) GetStaleIssues(ctx context.Context, filter types.StaleFilter) ([]*types.Issue, error) {
	if filter.Days <= 0 {
		filter.Days = 7
	}
	args := []any{filter.Days}
	clauses := []string{
		"status NOT IN ('closed', 'pinned')",
		"updated_at < NOW() - ($1 || ' days')::interval",
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	limit := ""
	if filter.Limit > 0 {
		limit = fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	//nolint:gosec // placeholders bound, identifiers static
	q := fmt.Sprintf(`SELECT %s FROM issues WHERE %s ORDER BY updated_at ASC%s`,
		issueColumns, strings.Join(clauses, " AND "), limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, wrapErr("get stale issues", err)
	}
	return scanIssues(rows)
}

// AdvancedQueryStore: repo-mtime tracking. Stored in repo_mtimes (clone-local
// in spirit, but PG keeps the table empty in fresh installs).
func (s *PostgresStore) GetRepoMtime(ctx context.Context, repoPath string) (int64, error) {
	var mtime int64
	err := s.pool.QueryRow(ctx, `SELECT mtime_ns FROM repo_mtimes WHERE repo_path = $1`, repoPath).Scan(&mtime)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, wrapErr("get repo mtime", err)
	}
	return mtime, nil
}

func (s *PostgresStore) SetRepoMtime(ctx context.Context, repoPath, jsonlPath string, mtimeNs int64) error {
	stmt := `
		INSERT INTO repo_mtimes (repo_path, jsonl_path, mtime_ns, last_checked)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (repo_path) DO UPDATE SET
			jsonl_path = EXCLUDED.jsonl_path,
			mtime_ns = EXCLUDED.mtime_ns,
			last_checked = NOW()
	`
	if _, err := s.pool.Exec(ctx, stmt, repoPath, jsonlPath, mtimeNs); err != nil {
		return wrapErr("set repo mtime", err)
	}
	return nil
}

func (s *PostgresStore) ClearRepoMtime(ctx context.Context, repoPath string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM repo_mtimes WHERE repo_path = $1`, repoPath); err != nil {
		return wrapErr("clear repo mtime", err)
	}
	return nil
}
