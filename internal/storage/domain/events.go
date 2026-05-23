package domain

import (
	"context"

	"github.com/steveyegge/beads/internal/types"
)

type Event struct {
	IssueID  string
	Type     types.EventType
	Actor    string
	OldValue string
	NewValue string
}

type RecordEventOpts struct {
	UseWispsTable bool
}

type EventsSQLRepository interface {
	Record(ctx context.Context, evt Event, opts RecordEventOpts) error
}
