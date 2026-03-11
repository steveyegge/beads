package dolt

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

// CreateMilestone creates a new milestone.
func (s *DoltStore) CreateMilestone(ctx context.Context, ms *types.Milestone, actor string) error {
	var targetDate interface{}
	if ms.TargetDate != nil {
		targetDate = ms.TargetDate.UTC()
	}

	if _, err := s.execContext(ctx, `
		INSERT INTO milestones (name, target_date, description, created_by)
		VALUES (?, ?, ?, ?)
	`, ms.Name, targetDate, ms.Description, actor); err != nil {
		return fmt.Errorf("failed to create milestone: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, "CALL DOLT_COMMIT('-Am', ?, '--author', ?)",
		"milestone: create "+ms.Name, s.commitAuthorString()); err != nil && !isDoltNothingToCommit(err) {
		return fmt.Errorf("dolt commit: %w", err)
	}
	return nil
}

// GetMilestone retrieves a milestone by name.
func (s *DoltStore) GetMilestone(ctx context.Context, name string) (*types.Milestone, error) {
	var ms types.Milestone
	var targetDate sql.NullTime

	if err := s.queryRowContext(ctx, func(row *sql.Row) error {
		return row.Scan(&ms.Name, &targetDate, &ms.Description, &ms.CreatedAt, &ms.CreatedBy)
	}, `SELECT name, target_date, description, created_at, created_by FROM milestones WHERE name = ?`, name); err != nil {
		return nil, fmt.Errorf("milestone %q not found: %w", name, err)
	}

	if targetDate.Valid {
		ms.TargetDate = &targetDate.Time
	}
	return &ms, nil
}

// ListMilestones returns all milestones ordered by target date.
func (s *DoltStore) ListMilestones(ctx context.Context) ([]*types.Milestone, error) {
	rows, err := s.queryContext(ctx, `
		SELECT name, target_date, description, created_at, created_by
		FROM milestones
		ORDER BY COALESCE(target_date, '9999-12-31') ASC, name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list milestones: %w", err)
	}
	defer rows.Close()

	var milestones []*types.Milestone
	for rows.Next() {
		var ms types.Milestone
		var targetDate sql.NullTime
		if err := rows.Scan(&ms.Name, &targetDate, &ms.Description, &ms.CreatedAt, &ms.CreatedBy); err != nil {
			return nil, fmt.Errorf("failed to scan milestone: %w", err)
		}
		if targetDate.Valid {
			ms.TargetDate = &targetDate.Time
		}
		milestones = append(milestones, &ms)
	}
	return milestones, rows.Err()
}

// DeleteMilestone deletes a milestone and clears it from all linked issues.
func (s *DoltStore) DeleteMilestone(ctx context.Context, name string, actor string) error {
	// Clear milestone from linked issues first
	if _, err := s.execContext(ctx, `UPDATE issues SET milestone = '' WHERE milestone = ?`, name); err != nil {
		return fmt.Errorf("failed to clear milestone from issues: %w", err)
	}
	// wisps table may not exist — ignore error
	_, _ = s.execContext(ctx, `UPDATE wisps SET milestone = '' WHERE milestone = ?`, name)

	if _, err := s.execContext(ctx, `DELETE FROM milestones WHERE name = ?`, name); err != nil {
		return fmt.Errorf("failed to delete milestone: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, "CALL DOLT_COMMIT('-Am', ?, '--author', ?)",
		"milestone: delete "+name, s.commitAuthorString()); err != nil && !isDoltNothingToCommit(err) {
		return fmt.Errorf("dolt commit: %w", err)
	}
	return nil
}
