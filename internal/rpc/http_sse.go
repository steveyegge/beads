//go:build !windows

package rpc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// handleSSEEvents handles GET /events for Server-Sent Events streaming.
// Supports query parameters:
//   - since: unix timestamp in ms, replays buffered events newer than this
//   - filter: server-side filter (e.g., "type:update", "issue:gt-abc")
//
// Requires Bearer token auth when configured.
func (h *HTTPServer) handleSSEEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Auth check (same as handleRPC)
	if h.token != "" {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			h.writeError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}
		if !strings.HasPrefix(authHeader, "Bearer ") {
			h.writeError(w, http.StatusUnauthorized, "invalid Authorization header format")
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != h.token {
			h.writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
	}

	// Check that response supports streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Parse query parameters
	var sinceMs int64
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		var err error
		sinceMs, err = strconv.ParseInt(sinceStr, 10, 64)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid 'since' parameter: must be unix ms")
			return
		}
	}

	filterStr := r.URL.Query().Get("filter")
	filter := parseSSEFilter(filterStr)

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Subscribe to live events
	ch, unsubscribe := h.rpcServer.Subscribe()
	defer unsubscribe()

	// Replay buffered events since the given timestamp
	if sinceMs > 0 {
		buffered := h.rpcServer.GetRecentMutations(sinceMs)
		for _, evt := range buffered {
			if !filter.matches(evt) {
				continue
			}
			writeSSEEvent(w, evt)
		}
		flusher.Flush()
	}

	// Stream live events until client disconnects
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				// Channel closed (unsubscribed)
				return
			}
			if !filter.matches(evt) {
				continue
			}
			writeSSEEvent(w, evt)
			flusher.Flush()
		}
	}
}

// sseFilter represents a parsed server-side filter for SSE events.
type sseFilter struct {
	mutationType string // filter by mutation type (e.g., "update")
	issueID      string // filter by exact issue ID
}

// parseSSEFilter parses a filter string like "type:update" or "issue:gt-abc".
func parseSSEFilter(s string) sseFilter {
	if s == "" {
		return sseFilter{}
	}
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return sseFilter{}
	}
	switch parts[0] {
	case "type":
		return sseFilter{mutationType: parts[1]}
	case "issue":
		return sseFilter{issueID: parts[1]}
	default:
		return sseFilter{}
	}
}

// matches returns true if the event passes this filter.
func (f sseFilter) matches(evt MutationEvent) bool {
	if f.mutationType != "" && evt.Type != f.mutationType {
		return false
	}
	if f.issueID != "" && evt.IssueID != f.issueID {
		return false
	}
	return true
}

// writeSSEEvent writes a single SSE event to the writer.
func writeSSEEvent(w http.ResponseWriter, evt MutationEvent) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	// id: field for Last-Event-ID reconnection
	fmt.Fprintf(w, "id: %d\n", evt.Timestamp.UnixMilli())
	fmt.Fprintf(w, "event: mutation\n")
	fmt.Fprintf(w, "data: %s\n\n", data)
}
