package ui_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
	ui "github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/ui/api"
	"github.com/steveyegge/beads/internal/ui/templates"
	"github.com/steveyegge/beads/ui/static"
)

type stubEventSource struct {
	ch chan api.IssueEvent
}

func (s *stubEventSource) Subscribe(context.Context) (<-chan api.IssueEvent, error) {
	return s.ch, nil
}

func TestEventStreamEmitsServerSentEvents(t *testing.T) {
	t.Parallel()

	eventCh := make(chan api.IssueEvent, 4)
	source := &stubEventSource{ch: eventCh}

	indexHTML, err := templates.RenderBasePage(templates.BasePageData{
		AppTitle:           "Beads",
		InitialFiltersJSON: mustDefaultFiltersJSON(t),
		EventStreamURL:     "/events",
		StaticPrefix:       "/.assets",
	})
	if err != nil {
		t.Fatalf("RenderBasePage: %v", err)
	}

	handler, err := ui.NewHandler(ui.HandlerConfig{
		StaticFS:  static.Files,
		IndexHTML: indexHTML,
		Register: func(mux *http.ServeMux) {
			mux.Handle("/events", api.NewEventStreamHandler(source,
				api.WithHeartbeatInterval(24*time.Hour),
			))
		},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/events", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("unexpected content type %q", ct)
	}

	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("unexpected cache control %q", cc)
	}

	base := time.Date(2025, time.October, 22, 15, 4, 5, 0, time.UTC)
	expected := []api.IssueEvent{
		{
			Type: api.EventTypeCreated,
			Issue: api.IssueSummary{
				ID:        "ui-06",
				Title:     "initial creation",
				Status:    string(types.StatusOpen),
				IssueType: string(types.TypeFeature),
				Priority:  1,
				UpdatedAt: base.Format(time.RFC3339),
			},
		},
		{
			Type: api.EventTypeUpdated,
			Issue: api.IssueSummary{
				ID:        "ui-06",
				Title:     "updated summary",
				Status:    string(types.StatusInProgress),
				IssueType: string(types.TypeFeature),
				Priority:  1,
				UpdatedAt: base.Add(10 * time.Second).Format(time.RFC3339),
			},
		},
		{
			Type: api.EventTypeClosed,
			Issue: api.IssueSummary{
				ID:        "ui-06",
				Title:     "done",
				Status:    string(types.StatusClosed),
				IssueType: string(types.TypeFeature),
				Priority:  1,
				UpdatedAt: base.Add(20 * time.Second).Format(time.RFC3339),
			},
		},
	}

	for _, evt := range expected {
		eventCh <- evt
	}
	close(eventCh)

	received := readSSE(resp.Body, len(expected))
	if len(received) != len(expected) {
		t.Fatalf("expected %d events, got %d", len(expected), len(received))
	}

	for i, chunk := range received {
		if chunk.Event != string(expected[i].Type) {
			t.Fatalf("event[%d] type = %q, want %q", i, chunk.Event, expected[i].Type)
		}
		var got api.IssueEvent
		if err := json.Unmarshal([]byte(chunk.Data), &got); err != nil {
			t.Fatalf("decode event[%d]: %v (raw=%s)", i, err, chunk.Data)
		}
		if got.Type != expected[i].Type {
			t.Fatalf("event[%d] payload type = %q, want %q", i, got.Type, expected[i].Type)
		}
		if got.Issue.ID != expected[i].Issue.ID {
			t.Fatalf("event[%d] issue id = %q, want %q", i, got.Issue.ID, expected[i].Issue.ID)
		}
		if got.Issue.Status != expected[i].Issue.Status {
			t.Fatalf("event[%d] issue status = %q, want %q", i, got.Issue.Status, expected[i].Issue.Status)
		}
		if got.Issue.Title != expected[i].Issue.Title {
			t.Fatalf("event[%d] issue title = %q, want %q", i, got.Issue.Title, expected[i].Issue.Title)
		}
	}
}

type sseChunk struct {
	Event string
	Data  string
}

func readSSE(r io.Reader, want int) []sseChunk {
	reader := bufio.NewReader(r)
	chunks := make([]sseChunk, 0, want)

	for len(chunks) < want {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "event:") {
			event := strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			chunk := sseChunk{Event: event}

			// Read until blank line to gather data.
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					break
				}
				dataLine = strings.TrimRight(dataLine, "\r\n")
				if dataLine == "" {
					break
				}
				if strings.HasPrefix(dataLine, "data:") {
					if chunk.Data != "" {
						chunk.Data += "\n"
					}
					chunk.Data += strings.TrimSpace(strings.TrimPrefix(dataLine, "data:"))
				}
			}

			chunks = append(chunks, chunk)
		}
	}

	return chunks
}
