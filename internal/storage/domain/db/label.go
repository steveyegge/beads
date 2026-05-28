package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

func NewLabelSQLRepository(runner Runner) domain.LabelSQLRepository {
	return &labelSQLRepositoryImpl{
		runner: runner,
		events: NewEventsSQLRepository(runner),
	}
}

type labelSQLRepositoryImpl struct {
	runner Runner
	events domain.EventsSQLRepository
}

var _ domain.LabelSQLRepository = (*labelSQLRepositoryImpl)(nil)

func pickLabelTable(useWisps bool) string {
	if useWisps {
		return "wisp_labels"
	}
	return "labels"
}

func (r *labelSQLRepositoryImpl) Insert(ctx context.Context, issueID, label, actor string, opts domain.LabelOpts) error {
	if issueID == "" {
		return fmt.Errorf("db: LabelSQLRepository.Insert: issueID must not be empty")
	}
	if label == "" {
		return fmt.Errorf("db: LabelSQLRepository.Insert: label must not be empty")
	}
	table := pickLabelTable(opts.UseWispsTable)
	//nolint:gosec // G201: table is one of two hardcoded constants
	if _, err := r.runner.ExecContext(ctx,
		fmt.Sprintf("INSERT IGNORE INTO %s (issue_id, label) VALUES (?, ?)", table),
		issueID, label,
	); err != nil {
		return fmt.Errorf("db: LabelSQLRepository.Insert %s/%s: %w", issueID, label, err)
	}
	return r.events.Record(ctx, domain.Event{
		IssueID:  issueID,
		Type:     types.EventLabelAdded,
		Actor:    actor,
		NewValue: label,
	}, domain.RecordEventOpts{UseWispsTable: opts.UseWispsTable})
}

func (r *labelSQLRepositoryImpl) List(ctx context.Context, issueID string, opts domain.LabelOpts) ([]string, error) {
	if issueID == "" {
		return nil, fmt.Errorf("db: LabelSQLRepository.List: issueID must not be empty")
	}
	table := pickLabelTable(opts.UseWispsTable)
	//nolint:gosec // G201: table is one of two hardcoded constants
	rows, err := r.runner.QueryContext(ctx,
		fmt.Sprintf("SELECT label FROM %s WHERE issue_id = ? ORDER BY label", table),
		issueID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: LabelSQLRepository.List %s: %w", issueID, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, fmt.Errorf("db: LabelSQLRepository.List: scan: %w", err)
		}
		out = append(out, label)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: LabelSQLRepository.List: rows: %w", err)
	}
	return out, nil
}

func (r *labelSQLRepositoryImpl) ListByIssueIDs(ctx context.Context, issueIDs []string, opts domain.LabelOpts) (map[string][]string, error) {
	result := make(map[string][]string)
	if len(issueIDs) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(issueIDs))
	args := make([]any, len(issueIDs))
	for i, id := range issueIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	table := pickLabelTable(opts.UseWispsTable)
	//nolint:gosec // G201: table is one of two hardcoded constants
	q := fmt.Sprintf(
		"SELECT issue_id, label FROM %s WHERE issue_id IN (%s) ORDER BY issue_id, label",
		table, strings.Join(placeholders, ","),
	)
	rows, err := r.runner.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("db: LabelSQLRepository.ListByIssueIDs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var issueID, label string
		if err := rows.Scan(&issueID, &label); err != nil {
			return nil, fmt.Errorf("db: LabelSQLRepository.ListByIssueIDs: scan: %w", err)
		}
		result[issueID] = append(result[issueID], label)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: LabelSQLRepository.ListByIssueIDs: rows: %w", err)
	}
	return result, nil
}
