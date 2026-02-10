package sqlite

// TODO(dolt-parity): These DecisionPoint methods need corresponding implementations
// in the dolt storage backend when it's added. The interface is defined in
// storage/storage.go and must be implemented for all backends.

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// CreateDecisionPoint creates a new decision point for an issue.
func (s *SQLiteStorage) CreateDecisionPoint(ctx context.Context, dp *types.DecisionPoint) error {
	// Verify issue exists
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM issues WHERE id = ?)`, dp.IssueID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check issue existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("issue %s not found", dp.IssueID)
	}

	// Convert empty strings to NULL for optional FK fields
	var priorID interface{}
	if dp.PriorID != "" {
		priorID = dp.PriorID
	}
	var parentBeadID interface{}
	if dp.ParentBeadID != "" {
		parentBeadID = dp.ParentBeadID
	}

	// Insert decision point
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO decision_points (
			issue_id, prompt, context, options, default_option, selected_option,
			response_text, rationale, responded_at, responded_by, iteration, max_iterations,
			prior_id, guidance, urgency, requested_by, parent_bead_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, dp.IssueID, dp.Prompt, dp.Context, dp.Options, dp.DefaultOption, dp.SelectedOption,
		dp.ResponseText, dp.Rationale, dp.RespondedAt, dp.RespondedBy, dp.Iteration, dp.MaxIterations,
		priorID, dp.Guidance, dp.Urgency, dp.RequestedBy, parentBeadID)
	if err != nil {
		return fmt.Errorf("failed to insert decision point: %w", err)
	}

	return nil
}

// GetDecisionPoint retrieves the decision point for an issue.
func (s *SQLiteStorage) GetDecisionPoint(ctx context.Context, issueID string) (*types.DecisionPoint, error) {
	// Hold read lock during database operations to prevent reconnect() from
	// closing the connection mid-query (GH#607 race condition fix)
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	dp := &types.DecisionPoint{}
	err := s.db.QueryRowContext(ctx, `
		SELECT issue_id, prompt, COALESCE(context, ''), options,
			COALESCE(default_option, ''), COALESCE(selected_option, ''),
			COALESCE(response_text, ''), COALESCE(rationale, ''), responded_at, COALESCE(responded_by, ''),
			iteration, max_iterations,
			COALESCE(prior_id, ''), COALESCE(guidance, ''), COALESCE(urgency, ''), COALESCE(requested_by, ''),
			COALESCE(parent_bead_id, ''), created_at
		FROM decision_points
		WHERE issue_id = ?
	`, issueID).Scan(
		&dp.IssueID, &dp.Prompt, &dp.Context, &dp.Options,
		&dp.DefaultOption, &dp.SelectedOption,
		&dp.ResponseText, &dp.Rationale, &dp.RespondedAt, &dp.RespondedBy,
		&dp.Iteration, &dp.MaxIterations,
		&dp.PriorID, &dp.Guidance, &dp.Urgency, &dp.RequestedBy,
		&dp.ParentBeadID, &dp.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query decision point: %w", err)
	}

	return dp, nil
}

// UpdateDecisionPoint updates an existing decision point.
func (s *SQLiteStorage) UpdateDecisionPoint(ctx context.Context, dp *types.DecisionPoint) error {
	// Convert empty strings to NULL for optional FK fields
	var priorID interface{}
	if dp.PriorID != "" {
		priorID = dp.PriorID
	}
	var parentBeadID interface{}
	if dp.ParentBeadID != "" {
		parentBeadID = dp.ParentBeadID
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE decision_points SET
			prompt = ?,
			context = ?,
			options = ?,
			default_option = ?,
			selected_option = ?,
			response_text = ?,
			rationale = ?,
			responded_at = ?,
			responded_by = ?,
			iteration = ?,
			max_iterations = ?,
			prior_id = ?,
			guidance = ?,
			urgency = ?,
			parent_bead_id = ?,
			reminder_count = ?
		WHERE issue_id = ?
	`, dp.Prompt, dp.Context, dp.Options, dp.DefaultOption, dp.SelectedOption,
		dp.ResponseText, dp.Rationale, dp.RespondedAt, dp.RespondedBy,
		dp.Iteration, dp.MaxIterations, priorID, dp.Guidance, dp.Urgency, parentBeadID,
		dp.ReminderCount, dp.IssueID)
	if err != nil {
		return fmt.Errorf("failed to update decision point: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("decision point not found for issue %s", dp.IssueID)
	}

	return nil
}

// ListPendingDecisions returns all decision points that haven't been responded to.
func (s *SQLiteStorage) ListPendingDecisions(ctx context.Context) ([]*types.DecisionPoint, error) {
	// Hold read lock during database operations to prevent reconnect() from
	// closing the connection mid-query (GH#607 race condition fix)
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT issue_id, prompt, COALESCE(context, ''), options,
			COALESCE(default_option, ''), COALESCE(selected_option, ''),
			COALESCE(response_text, ''), COALESCE(rationale, ''), responded_at, COALESCE(responded_by, ''),
			iteration, max_iterations,
			COALESCE(prior_id, ''), COALESCE(guidance, ''), COALESCE(urgency, ''), COALESCE(requested_by, ''),
			COALESCE(parent_bead_id, ''), created_at
		FROM decision_points
		WHERE responded_at IS NULL
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []*types.DecisionPoint
	for rows.Next() {
		dp := &types.DecisionPoint{}
		err := rows.Scan(
			&dp.IssueID, &dp.Prompt, &dp.Context, &dp.Options,
			&dp.DefaultOption, &dp.SelectedOption,
			&dp.ResponseText, &dp.Rationale, &dp.RespondedAt, &dp.RespondedBy,
			&dp.Iteration, &dp.MaxIterations,
			&dp.PriorID, &dp.Guidance, &dp.Urgency, &dp.RequestedBy,
			&dp.ParentBeadID, &dp.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan decision point: %w", err)
		}
		results = append(results, dp)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating decision points: %w", err)
	}

	return results, nil
}

// ListRecentlyRespondedDecisions returns decisions that were responded to
// within the given time window, optionally filtered by requesting agent.
func (s *SQLiteStorage) ListRecentlyRespondedDecisions(ctx context.Context, since time.Time, requestedBy string) ([]*types.DecisionPoint, error) {
	// Hold read lock during database operations to prevent reconnect() from
	// closing the connection mid-query (GH#607 race condition fix)
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	var rows *sql.Rows
	var err error

	if requestedBy != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT issue_id, prompt, COALESCE(context, ''), options,
				COALESCE(default_option, ''), COALESCE(selected_option, ''),
				COALESCE(response_text, ''), COALESCE(rationale, ''), responded_at, COALESCE(responded_by, ''),
				iteration, max_iterations,
				COALESCE(prior_id, ''), COALESCE(guidance, ''), COALESCE(urgency, ''), COALESCE(requested_by, ''),
				COALESCE(parent_bead_id, ''), created_at
			FROM decision_points
			WHERE responded_at IS NOT NULL
			  AND responded_at >= ?
			  AND requested_by = ?
			ORDER BY responded_at DESC
		`, since, requestedBy)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT issue_id, prompt, COALESCE(context, ''), options,
				COALESCE(default_option, ''), COALESCE(selected_option, ''),
				COALESCE(response_text, ''), COALESCE(rationale, ''), responded_at, COALESCE(responded_by, ''),
				iteration, max_iterations,
				COALESCE(prior_id, ''), COALESCE(guidance, ''), COALESCE(urgency, ''), COALESCE(requested_by, ''),
				COALESCE(parent_bead_id, ''), created_at
			FROM decision_points
			WHERE responded_at IS NOT NULL
			  AND responded_at >= ?
			ORDER BY responded_at DESC
		`, since)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query recently responded decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []*types.DecisionPoint
	for rows.Next() {
		dp := &types.DecisionPoint{}
		err := rows.Scan(
			&dp.IssueID, &dp.Prompt, &dp.Context, &dp.Options,
			&dp.DefaultOption, &dp.SelectedOption,
			&dp.ResponseText, &dp.Rationale, &dp.RespondedAt, &dp.RespondedBy,
			&dp.Iteration, &dp.MaxIterations,
			&dp.PriorID, &dp.Guidance, &dp.Urgency, &dp.RequestedBy,
			&dp.ParentBeadID, &dp.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan decision point: %w", err)
		}
		results = append(results, dp)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating decision points: %w", err)
	}

	return results, nil
}
