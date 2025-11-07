package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

const watcherBufferSize = 64

func (s *Server) registerWatcher() (int64, chan IssueEvent) {
	id := atomic.AddInt64(&s.watcherSeq, 1)
	ch := make(chan IssueEvent, watcherBufferSize)

	s.watchersMu.Lock()
	s.watchers[id] = ch
	s.watchersMu.Unlock()

	return id, ch
}

func (s *Server) unregisterWatcher(id int64) {
	s.watchersMu.Lock()
	ch, ok := s.watchers[id]
	if ok {
		delete(s.watchers, id)
	}
	s.watchersMu.Unlock()

	if ok {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "[rpc] watcher %d close panic: %v\n", id, r)
			}
		}()
		close(ch)
	}
}

func (s *Server) hasWatchers() bool {
	s.watchersMu.RLock()
	defer s.watchersMu.RUnlock()
	return len(s.watchers) > 0
}

func (s *Server) dispatchIssueEvent(evt IssueEvent) {
	type watcherEntry struct {
		id int64
		ch chan IssueEvent
	}

	entries := make([]watcherEntry, 0)
	s.watchersMu.RLock()
	for id, ch := range s.watchers {
		entries = append(entries, watcherEntry{id: id, ch: ch})
	}
	s.watchersMu.RUnlock()

	for _, entry := range entries {
		func(id int64, ch chan IssueEvent) {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "[rpc] watcher %d removed after panic dispatching %q event: %v\n", id, evt.Type, r)
					s.unregisterWatcher(id)
				}
			}()

			select {
			case ch <- evt:
			default:
				// Drop event if subscriber is slow; they'll reconcile on next poll.
			}
		}(entry.id, entry.ch)
	}
}

func (s *Server) buildIssueEventRecord(ctx context.Context, issue *types.Issue) (IssueEventRecord, error) {
	if issue == nil {
		return IssueEventRecord{}, fmt.Errorf("nil issue snapshot")
	}

	labels := issue.Labels
	if len(labels) == 0 {
		if fetched, err := s.storage.GetLabels(ctx, issue.ID); err == nil {
			labels = fetched
		}
	}

	labelsCopy := append([]string(nil), labels...)

	updatedAt := issue.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}

	return IssueEventRecord{
		ID:        issue.ID,
		Title:     issue.Title,
		Status:    issue.Status,
		IssueType: issue.IssueType,
		Priority:  issue.Priority,
		Assignee:  issue.Assignee,
		Labels:    labelsCopy,
		UpdatedAt: updatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func (s *Server) publishIssueEvent(ctx context.Context, eventType IssueEventType, issue *types.Issue) {
	if !s.hasWatchers() {
		return
	}

	record, err := s.buildIssueEventRecord(ctx, issue)
	if err != nil {
		return
	}

	s.dispatchIssueEvent(IssueEvent{
		Type:  eventType,
		Issue: record,
	})
}

func (s *Server) publishIssueEventByID(ctx context.Context, eventType IssueEventType, issueID string) {
	if !s.hasWatchers() {
		return
	}

	issue, err := s.storage.GetIssue(ctx, issueID)
	if err != nil || issue == nil {
		return
	}

	s.publishIssueEvent(ctx, eventType, issue)
}

func (s *Server) handleWatchEvents(conn net.Conn, writer *bufio.Writer, _ *Request) {
	id, stream := s.registerWatcher()
	defer s.unregisterWatcher(id)

	ack := Response{Success: true}
	s.writeResponse(writer, ack)

	for {
		select {
		case evt, ok := <-stream:
			if !ok {
				return
			}

			payload, err := json.Marshal(evt)
			if err != nil {
				continue
			}

			if err := conn.SetWriteDeadline(time.Now().Add(s.requestTimeout)); err != nil {
				return
			}

			if _, err := writer.Write(payload); err != nil {
				return
			}
			if err := writer.WriteByte('\n'); err != nil {
				return
			}
			if err := writer.Flush(); err != nil {
				return
			}

		case <-s.shutdownChan:
			return
		}
	}
}
