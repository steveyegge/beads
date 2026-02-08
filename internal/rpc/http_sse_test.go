//go:build !windows

package rpc

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSSEFilter_Matches(t *testing.T) {
	evt := MutationEvent{
		Type:    MutationUpdate,
		IssueID: "gt-abc",
		Title:   "Test issue",
	}

	tests := []struct {
		name   string
		filter sseFilter
		want   bool
	}{
		{"empty filter matches all", sseFilter{}, true},
		{"type match", sseFilter{mutationType: "update"}, true},
		{"type mismatch", sseFilter{mutationType: "create"}, false},
		{"issue match", sseFilter{issueID: "gt-abc"}, true},
		{"issue mismatch", sseFilter{issueID: "gt-xyz"}, false},
		{"both match", sseFilter{mutationType: "update", issueID: "gt-abc"}, true},
		{"type matches but issue doesn't", sseFilter{mutationType: "update", issueID: "gt-xyz"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.matches(evt); got != tt.want {
				t.Errorf("matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseSSEFilter(t *testing.T) {
	tests := []struct {
		input string
		want  sseFilter
	}{
		{"", sseFilter{}},
		{"type:update", sseFilter{mutationType: "update"}},
		{"issue:gt-abc", sseFilter{issueID: "gt-abc"}},
		{"invalid", sseFilter{}},
		{"unknown:value", sseFilter{}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseSSEFilter(tt.input)
			if got != tt.want {
				t.Errorf("parseSSEFilter(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestWriteSSEEvent(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	evt := MutationEvent{
		Type:      MutationCreate,
		IssueID:   "gt-123",
		Title:     "Test",
		Timestamp: now,
	}

	recorder := httptest.NewRecorder()
	writeSSEEvent(recorder, evt)

	body := recorder.Body.String()

	// Check id field
	expectedID := fmt.Sprintf("id: %d\n", now.UnixMilli())
	if !strings.Contains(body, expectedID) {
		t.Errorf("missing or wrong id field.\nGot: %s\nExpected to contain: %s", body, expectedID)
	}

	// Check event type
	if !strings.Contains(body, "event: mutation\n") {
		t.Errorf("missing event type in SSE output: %s", body)
	}

	// Check data contains issue ID (Go default JSON key is "IssueID")
	if !strings.Contains(body, `"IssueID":"gt-123"`) {
		t.Errorf("missing IssueID in SSE data: %s", body)
	}

	// Check trailing double newline
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("SSE event should end with double newline: %q", body)
	}
}

func TestSubscribeFanOut(t *testing.T) {
	s := NewServer("/tmp/test.sock", nil, "/tmp/test", "/tmp/test.db")

	// Create two subscribers
	ch1, unsub1 := s.Subscribe()
	ch2, unsub2 := s.Subscribe()
	defer unsub1()
	defer unsub2()

	// Emit an event
	s.emitMutation(MutationCreate, "bd-42", "Test Issue", "user1")

	// Both should receive it
	select {
	case evt := <-ch1:
		if evt.IssueID != "bd-42" {
			t.Errorf("sub1 got IssueID=%s, want bd-42", evt.IssueID)
		}
	case <-time.After(time.Second):
		t.Fatal("sub1 timeout waiting for event")
	}

	select {
	case evt := <-ch2:
		if evt.IssueID != "bd-42" {
			t.Errorf("sub2 got IssueID=%s, want bd-42", evt.IssueID)
		}
	case <-time.After(time.Second):
		t.Fatal("sub2 timeout waiting for event")
	}
}

func TestSubscribeUnsubscribe(t *testing.T) {
	s := NewServer("/tmp/test.sock", nil, "/tmp/test", "/tmp/test.db")

	ch, unsub := s.Subscribe()

	// Verify subscription exists
	s.subscribersMu.RLock()
	if len(s.subscribers) != 1 {
		t.Fatalf("expected 1 subscriber, got %d", len(s.subscribers))
	}
	s.subscribersMu.RUnlock()

	// Unsubscribe
	unsub()

	s.subscribersMu.RLock()
	if len(s.subscribers) != 0 {
		t.Fatalf("expected 0 subscribers after unsub, got %d", len(s.subscribers))
	}
	s.subscribersMu.RUnlock()

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestRecentMutationsBufferSize(t *testing.T) {
	s := NewServer("/tmp/test.sock", nil, "/tmp/test", "/tmp/test.db")

	// Buffer size should be 1000 now
	if s.maxMutationBuffer != 1000 {
		t.Errorf("maxMutationBuffer = %d, want 1000", s.maxMutationBuffer)
	}

	// Emit more than buffer size
	for i := 0; i < 1100; i++ {
		s.emitMutation(MutationCreate, fmt.Sprintf("bd-%d", i), "Test", "user")
	}

	s.recentMutationsMu.RLock()
	count := len(s.recentMutations)
	s.recentMutationsMu.RUnlock()

	if count > 1000 {
		t.Errorf("recent mutations buffer overflow: got %d, max should be 1000", count)
	}
}

func TestSSEEndpointAuth(t *testing.T) {
	s := NewServer("/tmp/test.sock", nil, "/tmp/test", "/tmp/test.db")
	h := NewHTTPServer(s, ":0", "test-token")

	// Test without auth
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	w := httptest.NewRecorder()
	h.handleSSEEvents(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}

	// Test with wrong token
	req = httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w = httptest.NewRecorder()
	h.handleSSEEvents(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", w.Code)
	}

	// Test with wrong method
	req = httptest.NewRequest(http.MethodPost, "/events", nil)
	w = httptest.NewRecorder()
	h.handleSSEEvents(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", w.Code)
	}
}

func TestSSEEndpointNoAuth(t *testing.T) {
	s := NewServer("/tmp/test.sock", nil, "/tmp/test", "/tmp/test.db")
	h := NewHTTPServer(s, ":0", "") // no auth

	// Emit an event before connecting so there's something to replay
	now := time.Now()
	s.emitMutation(MutationCreate, "bd-99", "Replay Test", "user1")

	// Start a test server and connect
	ts := httptest.NewServer(http.HandlerFunc(h.handleSSEEvents))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sinceMs := now.Add(-time.Second).UnixMilli()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s?since=%d", ts.URL, sinceMs), nil)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Should get Content-Type: text/event-stream
	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %s, want text/event-stream", ct)
	}

	// Read the first event (the replayed one)
	scanner := bufio.NewScanner(resp.Body)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)
		// After we see the data line, we have enough
		if strings.HasPrefix(line, "data:") {
			break
		}
	}

	// Verify we got SSE-formatted data
	found := false
	for _, line := range lines {
		if strings.Contains(line, "bd-99") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("replayed event not found in SSE output. Lines: %v", lines)
	}
}
