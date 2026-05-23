package db

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/storage/domain"
)

func NewEventsSQLRepository(runner Runner) domain.EventsSQLRepository {
	return &eventsSQLRepositoryImpl{runner: runner}
}

type eventsSQLRepositoryImpl struct {
	runner Runner
}

var _ domain.EventsSQLRepository = (*eventsSQLRepositoryImpl)(nil)

func (r *eventsSQLRepositoryImpl) Record(ctx context.Context, evt domain.Event, opts domain.RecordEventOpts) error {
	table := "events"
	if opts.UseWispsTable {
		table = "wisp_events"
	}
	//nolint:gosec // G201: table is one of two hardcoded constants
	_, err := r.runner.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (issue_id, event_type, actor, old_value, new_value)
		VALUES (?, ?, ?, ?, ?)
	`, table), evt.IssueID, string(evt.Type), evt.Actor, evt.OldValue, evt.NewValue)
	if err != nil {
		return fmt.Errorf("db: record event in %s: %w", table, err)
	}
	return nil
}
