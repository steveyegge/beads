package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// CreateDecisionPoint creates a new decision point for an issue.
func (s *DoltStore) CreateDecisionPoint(ctx context.Context, dp *types.DecisionPoint) error {
	if dp.CreatedAt.IsZero() {
		dp.CreatedAt = time.Now()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO decision_points (
			issue_id, prompt, options, default_option, selected_option,
			response_text, responded_at, responded_by, iteration,
			max_iterations, prior_id, guidance, reminder_count, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		dp.IssueID, dp.Prompt, dp.Options, dp.DefaultOption, dp.SelectedOption,
		dp.ResponseText, dp.RespondedAt, dp.RespondedBy, dp.Iteration,
		dp.MaxIterations, dp.PriorID, dp.Guidance, dp.ReminderCount, dp.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create decision point: %w", err)
	}
	return nil
}

// GetDecisionPoint retrieves the decision point for an issue.
func (s *DoltStore) GetDecisionPoint(ctx context.Context, issueID string) (*types.DecisionPoint, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT issue_id, prompt, options, default_option, selected_option,
			response_text, responded_at, responded_by, iteration,
			max_iterations, prior_id, guidance, reminder_count, created_at
		FROM decision_points WHERE issue_id = ?
	`, issueID)

	dp := &types.DecisionPoint{}
	var defaultOption, selectedOption, responseText, respondedBy, priorID, guidance sql.NullString
	var respondedAt sql.NullTime

	err := row.Scan(
		&dp.IssueID, &dp.Prompt, &dp.Options, &defaultOption, &selectedOption,
		&responseText, &respondedAt, &respondedBy, &dp.Iteration,
		&dp.MaxIterations, &priorID, &guidance, &dp.ReminderCount, &dp.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get decision point: %w", err)
	}

	if defaultOption.Valid {
		dp.DefaultOption = defaultOption.String
	}
	if selectedOption.Valid {
		dp.SelectedOption = selectedOption.String
	}
	if responseText.Valid {
		dp.ResponseText = responseText.String
	}
	if respondedAt.Valid {
		dp.RespondedAt = &respondedAt.Time
	}
	if respondedBy.Valid {
		dp.RespondedBy = respondedBy.String
	}
	if priorID.Valid {
		dp.PriorID = priorID.String
	}
	if guidance.Valid {
		dp.Guidance = guidance.String
	}

	return dp, nil
}

// UpdateDecisionPoint updates an existing decision point.
func (s *DoltStore) UpdateDecisionPoint(ctx context.Context, dp *types.DecisionPoint) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE decision_points SET
			prompt = ?, options = ?, default_option = ?, selected_option = ?,
			response_text = ?, responded_at = ?, responded_by = ?, iteration = ?,
			max_iterations = ?, prior_id = ?, guidance = ?, reminder_count = ?
		WHERE issue_id = ?
	`,
		dp.Prompt, dp.Options, dp.DefaultOption, dp.SelectedOption,
		dp.ResponseText, dp.RespondedAt, dp.RespondedBy, dp.Iteration,
		dp.MaxIterations, dp.PriorID, dp.Guidance, dp.ReminderCount,
		dp.IssueID,
	)
	if err != nil {
		return fmt.Errorf("failed to update decision point: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("decision point not found for issue %s", dp.IssueID)
	}

	return nil
}

// ListPendingDecisions returns all decision points that haven't been responded to.
func (s *DoltStore) ListPendingDecisions(ctx context.Context) ([]*types.DecisionPoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT issue_id, prompt, options, default_option, selected_option,
			response_text, responded_at, responded_by, iteration,
			max_iterations, prior_id, guidance, reminder_count, created_at
		FROM decision_points
		WHERE responded_at IS NULL
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending decisions: %w", err)
	}
	defer rows.Close()

	return scanDecisionPoints(rows)
}

// ListAllDecisionPoints returns all decision points (for JSONL export).
func (s *DoltStore) ListAllDecisionPoints(ctx context.Context) ([]*types.DecisionPoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT issue_id, prompt, options, default_option, selected_option,
			response_text, responded_at, responded_by, iteration,
			max_iterations, prior_id, guidance, reminder_count, created_at
		FROM decision_points
		ORDER BY issue_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list all decision points: %w", err)
	}
	defer rows.Close()

	return scanDecisionPoints(rows)
}

// scanDecisionPoints scans rows into a slice of DecisionPoint structs.
func scanDecisionPoints(rows *sql.Rows) ([]*types.DecisionPoint, error) {
	var results []*types.DecisionPoint
	for rows.Next() {
		dp := &types.DecisionPoint{}
		var defaultOption, selectedOption, responseText, respondedBy, priorID, guidance sql.NullString
		var respondedAt sql.NullTime

		err := rows.Scan(
			&dp.IssueID, &dp.Prompt, &dp.Options, &defaultOption, &selectedOption,
			&responseText, &respondedAt, &respondedBy, &dp.Iteration,
			&dp.MaxIterations, &priorID, &guidance, &dp.ReminderCount, &dp.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan decision point: %w", err)
		}

		if defaultOption.Valid {
			dp.DefaultOption = defaultOption.String
		}
		if selectedOption.Valid {
			dp.SelectedOption = selectedOption.String
		}
		if responseText.Valid {
			dp.ResponseText = responseText.String
		}
		if respondedAt.Valid {
			dp.RespondedAt = &respondedAt.Time
		}
		if respondedBy.Valid {
			dp.RespondedBy = respondedBy.String
		}
		if priorID.Valid {
			dp.PriorID = priorID.String
		}
		if guidance.Valid {
			dp.Guidance = guidance.String
		}

		results = append(results, dp)
	}
	return results, rows.Err()
}
