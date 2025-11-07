package api

import (
	"context"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
)

type watchEventsClient interface {
	WatchEvents(ctx context.Context, args *rpc.WatchEventsArgs) (<-chan rpc.IssueEvent, func(), error)
}

type daemonEventSource struct {
	client watchEventsClient
}

// NewDaemonEventSource adapts an RPC client into an SSE event source.
func NewDaemonEventSource(client watchEventsClient) EventSource {
	if client == nil {
		return nil
	}
	return &daemonEventSource{client: client}
}

func (s *daemonEventSource) Subscribe(ctx context.Context) (<-chan IssueEvent, error) {
	stream, cancel, err := s.client.WatchEvents(ctx, nil)
	if err != nil {
		return nil, err
	}

	out := make(chan IssueEvent, 32)

	go func() {
		defer cancel()
		defer close(out)

		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-stream:
				if !ok {
					return
				}

				record := evt.Issue
				updatedAt := strings.TrimSpace(record.UpdatedAt)
				if updatedAt == "" {
					updatedAt = time.Now().UTC().Format(time.RFC3339)
				}

				summary := IssueSummary{
					ID:        record.ID,
					Title:     record.Title,
					Status:    string(record.Status),
					IssueType: string(record.IssueType),
					Priority:  record.Priority,
					Assignee:  record.Assignee,
					Labels:    append([]string(nil), record.Labels...),
					UpdatedAt: updatedAt,
				}

				select {
				case out <- IssueEvent{
					Type:  EventType(evt.Type),
					Issue: summary,
				}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out, nil
}
