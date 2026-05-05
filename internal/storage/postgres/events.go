package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// GetEvents returns the audit-trail entries for one issue, newest first.
func (s *PostgresStore) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `
		SELECT id::text, issue_id, event_type, actor, old_value, new_value, comment, created_at
		FROM events
		WHERE issue_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := s.pool.Query(ctx, q, issueID, limit)
	if err != nil {
		return nil, wrapErr("get events", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

// GetAllEventsSince returns every audit row produced after `since`.
func (s *PostgresStore) GetAllEventsSince(ctx context.Context, since time.Time) ([]*types.Event, error) {
	q := `
		SELECT id::text, issue_id, event_type, actor, old_value, new_value, comment, created_at
		FROM events
		WHERE created_at > $1
		ORDER BY created_at ASC
	`
	rows, err := s.pool.Query(ctx, q, since)
	if err != nil {
		return nil, wrapErr("get events since", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
},
) ([]*types.Event, error) {
	var out []*types.Event
	for rows.Next() {
		e := &types.Event{}
		var oldVal, newVal, comment sql.NullString
		var typ string
		if err := rows.Scan(&e.ID, &e.IssueID, &typ, &e.Actor, &oldVal, &newVal, &comment, &e.CreatedAt); err != nil {
			return nil, wrapErr("scan events", err)
		}
		e.EventType = types.EventType(typ)
		if oldVal.Valid {
			s := oldVal.String
			e.OldValue = &s
		}
		if newVal.Valid {
			s := newVal.String
			e.NewValue = &s
		}
		if comment.Valid {
			s := comment.String
			e.Comment = &s
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// formatEventValue lifts a value into a serializable string form for events.
// time.Time is canonicalized to RFC3339 UTC.
func formatEventValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	case string:
		return t
	default:
		return fmt.Sprintf("%v", v)
	}
}
