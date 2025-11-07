package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type stubEventSource struct {
	events      chan IssueEvent
	subscribed  chan struct{}
	returnError error
}

func newStubEventSource(buffer int) *stubEventSource {
	return &stubEventSource{
		events:     make(chan IssueEvent, buffer),
		subscribed: make(chan struct{}),
	}
}

func (s *stubEventSource) Subscribe(ctx context.Context) (<-chan IssueEvent, error) {
	if s.returnError != nil {
		return nil, s.returnError
	}
	close(s.subscribed)
	return s.events, nil
}

func TestEventStreamCustomClock(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2030, time.January, 12, 8, 30, 0, 0, time.UTC)
	source := newStubEventSource(1)
	handler := NewEventStreamHandler(
		source,
		WithHeartbeatInterval(0),
		WithNowFunc(func() time.Time { return fixed }),
	)

	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	select {
	case <-source.subscribed:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("event source was not subscribed")
	}

	source.events <- IssueEvent{
		Type: EventTypeCreated,
		Issue: IssueSummary{
			ID:        "ui-77",
			Title:     "New issue",
			Status:    string(EventTypeCreated),
			UpdatedAt: fixed.Format(time.RFC3339),
		},
	}
	close(source.events)
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("event stream handler did not terminate")
	}
	cancel()

	body := rr.Body.String()
	if !strings.Contains(body, fixed.Format(time.RFC3339)) {
		t.Fatalf("expected custom clock timestamp in output: %s", body)
	}
	if !strings.Contains(body, "event: created") {
		t.Fatalf("expected created event in output: %s", body)
	}
	if !strings.Contains(body, "\"id\":\"ui-77\"") {
		t.Fatalf("expected issue payload in stream: %s", body)
	}
}

func TestEventStreamHandlerRejectsNonGet(t *testing.T) {
	handler := NewEventStreamHandler(newStubEventSource(1))
	req := httptest.NewRequest(http.MethodPost, "/events", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestEventStreamHandlerUnavailable(t *testing.T) {
	handler := NewEventStreamHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestEventStreamHandlerRequiresFlusher(t *testing.T) {
	source := newStubEventSource(1)
	handler := NewEventStreamHandler(source)

	writer := &nonFlushingWriter{header: make(http.Header)}
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	handler.ServeHTTP(writer, req)
	if writer.status != http.StatusInternalServerError {
		t.Fatalf("expected 500 for non-flusher response writer, got %d", writer.status)
	}
}

func TestEventStreamHandlerSubscribeError(t *testing.T) {
	source := newStubEventSource(1)
	source.returnError = errors.New("subscribe failed")
	handler := NewEventStreamHandler(source)

	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "subscribe failed") {
		t.Fatalf("expected error body, got %s", rec.Body.String())
	}
}

type failingWriter struct{}

func (failingWriter) Header() http.Header { return http.Header{} }
func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("io boom")
}
func (failingWriter) WriteHeader(int) {}

func TestWriteSSEEventError(t *testing.T) {
	if err := writeSSEEvent(failingWriter{}, "event", map[string]string{"foo": "bar"}); err == nil {
		t.Fatalf("expected write error")
	}
}

type nonFlushingWriter struct {
	header http.Header
	status int
}

func (w *nonFlushingWriter) Header() http.Header {
	return w.header
}

func (w *nonFlushingWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (w *nonFlushingWriter) WriteHeader(status int) {
	w.status = status
}
