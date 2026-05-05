package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// ClaimIssue atomically transitions an open issue to in_progress and assigns
// it to the actor. Uses SELECT … FOR UPDATE to serialize concurrent claims.
// Returns ErrAlreadyClaimed when another actor already holds the issue, or
// ErrNotClaimable for issues in a non-claimable status.
func (s *PostgresStore) ClaimIssue(ctx context.Context, id, actor string) error {
	return s.RunInTransaction(ctx, "", func(tx storage.Transaction) error {
		ptx := tx.(*pgxTransaction)
		row := ptx.tx.QueryRow(ctx,
			`SELECT status, assignee FROM issues WHERE id = $1 FOR UPDATE`, id,
		)
		var status string
		var assignee *string
		if err := row.Scan(&status, &assignee); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: %s", storage.ErrNotFound, id)
			}
			return wrapErr("claim: select row", err)
		}
		switch types.Status(status) {
		case types.StatusOpen:
			// claimable
		case types.StatusInProgress:
			if assignee != nil && *assignee != "" && *assignee != actor {
				return fmt.Errorf("%w: held by %s", storage.ErrAlreadyClaimed, *assignee)
			}
			// re-claim by same actor — refresh metadata only
		default:
			return fmt.Errorf("%w: status=%s", storage.ErrNotClaimable, status)
		}
		now := time.Now().UTC()
		updates := map[string]interface{}{
			"status":     string(types.StatusInProgress),
			"assignee":   actor,
			"started_at": now,
			"updated_at": now,
		}
		return ptx.UpdateIssue(ctx, id, updates, actor)
	})
}

// ClaimReadyIssue picks the highest-priority ready issue matching the filter
// and claims it for the actor. Returns nil if no ready issue is available.
func (s *PostgresStore) ClaimReadyIssue(ctx context.Context, filter types.WorkFilter, actor string) (*types.Issue, error) {
	if filter.Limit == 0 {
		filter.Limit = 1
	}
	if filter.Assignee == nil {
		empty := ""
		filter.Assignee = &empty
	}
	candidates, err := s.GetReadyWork(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		if err := s.ClaimIssue(ctx, candidate.ID, actor); err != nil {
			if errors.Is(err, storage.ErrAlreadyClaimed) {
				continue
			}
			return nil, err
		}
		// Refresh after claim so caller sees the new status/assignee.
		fresh, err := s.GetIssue(ctx, candidate.ID)
		if err != nil {
			return nil, err
		}
		return fresh, nil
	}
	return nil, nil
}
