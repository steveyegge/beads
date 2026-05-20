package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

func NewDependencySQLRepository(runner Runner) domain.DependencySQLRepository {
	return &dependencySQLRepositoryImpl{runner: runner}
}

type dependencySQLRepositoryImpl struct {
	runner Runner
}

var _ domain.DependencySQLRepository = (*dependencySQLRepositoryImpl)(nil)

const depTargetExpr = "COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external)"
const depSelectColumns = "issue_id, " + depTargetExpr + " AS depends_on_id, type, created_at, created_by, metadata, thread_id"

func (r *dependencySQLRepositoryImpl) Insert(ctx context.Context, dep *types.Dependency, actor string) error {
	if dep == nil {
		return errors.New("db: DependencySQLRepository.Insert: dep must not be nil")
	}
	if dep.IssueID == "" {
		return errors.New("db: DependencySQLRepository.Insert: IssueID must not be empty")
	}
	if dep.DependsOnID == "" {
		return errors.New("db: DependencySQLRepository.Insert: DependsOnID must not be empty")
	}
	if dep.IssueID == dep.DependsOnID {
		return fmt.Errorf("db: DependencySQLRepository.Insert: %s cannot depend on itself", dep.IssueID)
	}

	metadata := dep.Metadata
	if metadata == "" {
		metadata = "{}"
	}

	var existingType string
	err := r.runner.QueryRowContext(ctx,
		fmt.Sprintf("SELECT type FROM dependencies WHERE issue_id = ? AND %s = ?", depTargetExpr),
		dep.IssueID, dep.DependsOnID,
	).Scan(&existingType)
	switch {
	case err == nil:
		if existingType == string(dep.Type) {
			if _, err := r.runner.ExecContext(ctx,
				fmt.Sprintf("UPDATE dependencies SET metadata = ? WHERE issue_id = ? AND %s = ?", depTargetExpr),
				metadata, dep.IssueID, dep.DependsOnID,
			); err != nil {
				return fmt.Errorf("db: DependencySQLRepository.Insert: refresh metadata: %w", err)
			}
			return nil
		}
		return fmt.Errorf("db: DependencySQLRepository.Insert: %s -> %s already exists with type %q (requested %q)",
			dep.IssueID, dep.DependsOnID, existingType, dep.Type)
	case errors.Is(err, sql.ErrNoRows):
	default:
		return fmt.Errorf("db: DependencySQLRepository.Insert: check existing: %w", err)
	}

	if _, err := r.runner.ExecContext(ctx, `
		INSERT INTO dependencies (issue_id, depends_on_issue_id, type, created_at, created_by, metadata, thread_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		dep.IssueID, dep.DependsOnID, string(dep.Type),
		time.Now().UTC(), actor, metadata, dep.ThreadID,
	); err != nil {
		return fmt.Errorf("db: DependencySQLRepository.Insert: %w", err)
	}
	return nil
}

func (r *dependencySQLRepositoryImpl) HasCycle(ctx context.Context, issueID, dependsOnID string) (bool, error) {
	if issueID == "" || dependsOnID == "" {
		return false, errors.New("db: DependencySQLRepository.HasCycle: issueID and dependsOnID must not be empty")
	}
	var one int
	err := r.runner.QueryRowContext(ctx, `
		SELECT 1 FROM dependencies
		WHERE issue_id = ? AND depends_on_issue_id = ?
		  AND type IN ('blocks', 'conditional-blocks')
		LIMIT 1
	`, dependsOnID, issueID).Scan(&one)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("db: DependencySQLRepository.HasCycle: direct probe: %w", err)
	}
	var count int
	err = r.runner.QueryRowContext(ctx, `
		WITH RECURSIVE reachable(node) AS (
			SELECT ?
			UNION
			SELECT d.depends_on_issue_id
			FROM dependencies d
			JOIN reachable r ON d.issue_id = r.node
			WHERE d.type IN ('blocks', 'conditional-blocks')
			  AND d.depends_on_issue_id IS NOT NULL
		)
		SELECT COUNT(*) FROM reachable WHERE node = ?
	`, dependsOnID, issueID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("db: DependencySQLRepository.HasCycle: %w", err)
	}
	return count > 0, nil
}

func (r *dependencySQLRepositoryImpl) ListByIssueIDs(ctx context.Context, issueIDs []string, opts domain.DepListOpts) (domain.DepBulkResult, error) {
	result := domain.DepBulkResult{
		Outgoing: make(map[string][]*types.Dependency),
		Incoming: make(map[string][]*types.Dependency),
	}
	if len(issueIDs) == 0 {
		return result, nil
	}

	idPlaceholders, idArgs := buildInPlaceholders(issueIDs)
	typeWhere, typeArgs := buildTypeFilter(opts.Types)

	if opts.Direction == domain.DepDirectionBoth || opts.Direction == domain.DepDirectionOut {
		q := fmt.Sprintf(
			`SELECT %s FROM dependencies WHERE issue_id IN (%s)%s ORDER BY issue_id`,
			depSelectColumns, idPlaceholders, typeWhere,
		)
		args := combineArgs(idArgs, typeArgs)
		if err := r.queryDeps(ctx, q, args, result.Outgoing, true); err != nil {
			return domain.DepBulkResult{}, fmt.Errorf("db: DependencySQLRepository.ListByIssueIDs (out): %w", err)
		}
	}

	if opts.Direction == domain.DepDirectionBoth || opts.Direction == domain.DepDirectionIn {
		q := fmt.Sprintf(
			`SELECT %s FROM dependencies WHERE %s IN (%s)%s ORDER BY issue_id`,
			depSelectColumns, depTargetExpr, idPlaceholders, typeWhere,
		)
		args := combineArgs(idArgs, typeArgs)
		if err := r.queryDeps(ctx, q, args, result.Incoming, false); err != nil {
			return domain.DepBulkResult{}, fmt.Errorf("db: DependencySQLRepository.ListByIssueIDs (in): %w", err)
		}
	}

	return result, nil
}

func (r *dependencySQLRepositoryImpl) CountsByIssueIDs(ctx context.Context, issueIDs []string) (map[string]*types.DependencyCounts, error) {
	result := make(map[string]*types.DependencyCounts)
	if len(issueIDs) == 0 {
		return result, nil
	}
	for _, id := range issueIDs {
		result[id] = &types.DependencyCounts{}
	}

	idPlaceholders, idArgs := buildInPlaceholders(issueIDs)

	outQ := fmt.Sprintf(
		`SELECT issue_id, COUNT(*) FROM dependencies WHERE issue_id IN (%s) AND type = 'blocks' GROUP BY issue_id`,
		idPlaceholders,
	)
	if err := scanCounts(ctx, r.runner, outQ, idArgs, result, func(c *types.DependencyCounts, n int) { c.DependencyCount = n }); err != nil {
		return nil, fmt.Errorf("db: DependencySQLRepository.CountsByIssueIDs (out): %w", err)
	}

	inQ := fmt.Sprintf(
		`SELECT %s AS depends_on_id, COUNT(*) FROM dependencies WHERE %s IN (%s) AND type = 'blocks' GROUP BY %s`,
		depTargetExpr, depTargetExpr, idPlaceholders, depTargetExpr,
	)
	if err := scanCounts(ctx, r.runner, inQ, idArgs, result, func(c *types.DependencyCounts, n int) { c.DependentCount = n }); err != nil {
		return nil, fmt.Errorf("db: DependencySQLRepository.CountsByIssueIDs (in): %w", err)
	}

	return result, nil
}

func (r *dependencySQLRepositoryImpl) queryDeps(ctx context.Context, q string, args []any, into map[string][]*types.Dependency, keyByIssueID bool) error {
	rows, err := r.runner.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var d types.Dependency
		var typ string
		var createdBy, metadata, threadID sql.NullString
		var createdAt sql.NullTime
		if err := rows.Scan(&d.IssueID, &d.DependsOnID, &typ, &createdAt, &createdBy, &metadata, &threadID); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		d.Type = types.DependencyType(typ)
		if createdAt.Valid {
			d.CreatedAt = createdAt.Time
		}
		if createdBy.Valid {
			d.CreatedBy = createdBy.String
		}
		if metadata.Valid && metadata.String != "" && metadata.String != "{}" {
			d.Metadata = metadata.String
		}
		if threadID.Valid {
			d.ThreadID = threadID.String
		}
		dd := d
		var key string
		if keyByIssueID {
			key = d.IssueID
		} else {
			key = d.DependsOnID
		}
		into[key] = append(into[key], &dd)
	}
	return rows.Err()
}

func scanCounts(ctx context.Context, runner Runner, q string, args []any, into map[string]*types.DependencyCounts, assign func(c *types.DependencyCounts, n int)) error {
	rows, err := runner.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		if c, ok := into[id]; ok {
			assign(c, n)
		}
	}
	return rows.Err()
}

func buildInPlaceholders(values []string) (string, []any) {
	ph := make([]string, len(values))
	args := make([]any, len(values))
	for i, v := range values {
		ph[i] = "?"
		args[i] = v
	}
	return strings.Join(ph, ","), args
}

func buildTypeFilter(depTypes []types.DependencyType) (string, []any) {
	if len(depTypes) == 0 {
		return "", nil
	}
	ph := make([]string, len(depTypes))
	args := make([]any, len(depTypes))
	for i, t := range depTypes {
		ph[i] = "?"
		args[i] = string(t)
	}
	return " AND type IN (" + strings.Join(ph, ",") + ")", args
}

func combineArgs(a, b []any) []any {
	out := make([]any, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}
