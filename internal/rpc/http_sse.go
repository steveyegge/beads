//go:build !windows

package rpc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/steveyegge/beads/internal/eventbus"
)

// handleSSEEvents handles GET /events for Server-Sent Events streaming.
// Supports query parameters:
//   - since: unix timestamp in ms, replays buffered events newer than this
//   - filter: server-side filter (e.g., "type:update", "issue:gt-abc")
//
// When JetStream is available, events are sourced from the MUTATION_EVENTS
// stream for durability and cross-pod visibility. Falls back to in-memory
// fan-out when JetStream is not configured. (bd-7l2u6)
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

	// Try JetStream-backed streaming first, fall back to in-memory fan-out.
	if bus := h.rpcServer.GetBus(); bus != nil {
		if js := bus.JetStream(); js != nil {
			h.streamFromJetStream(w, r, flusher, js, sinceMs, filter)
			return
		}
	}

	// Fallback: in-memory fan-out (no JetStream available)
	h.streamFromMemory(w, r, flusher, sinceMs, filter)
}

// streamFromJetStream creates an ephemeral JetStream consumer on the
// MUTATION_EVENTS stream and relays events to the SSE response. (bd-7l2u6)
func (h *HTTPServer) streamFromJetStream(w http.ResponseWriter, r *http.Request, flusher http.Flusher, js nats.JetStreamContext, sinceMs int64, filter sseFilter) {
	// Build subject filter for NATS subscription.
	subject := eventbus.SubjectMutationPrefix + ">"
	if filter.mutationType != "" {
		if suffix := mutationTypeToNATSSubject(filter.mutationType); suffix != "" {
			subject = eventbus.SubjectMutationPrefix + suffix
		}
	}

	// Choose delivery policy based on since parameter.
	var opts []nats.SubOpt
	opts = append(opts, nats.AckExplicit())
	if sinceMs > 0 {
		startTime := time.UnixMilli(sinceMs)
		opts = append(opts, nats.StartTime(startTime))
	} else {
		opts = append(opts, nats.DeliverNew())
	}

	sub, err := js.Subscribe(subject, func(_ *nats.Msg) {}, opts...)
	if err != nil {
		// JetStream subscribe failed, fall back to in-memory
		h.streamFromMemory(w, r, flusher, sinceMs, filter)
		return
	}
	defer func() { _ = sub.Unsubscribe() }()

	ctx := r.Context()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()

		default:
			msg, err := sub.NextMsg(1 * time.Second)
			if err != nil {
				// Timeout or other transient error — continue loop
				continue
			}

			var payload eventbus.MutationEventPayload
			if err := json.Unmarshal(msg.Data, &payload); err != nil {
				_ = msg.Ack()
				continue
			}

			evt := payloadToMutationEventSSE(payload)

			// Apply client-side filter (issue ID filter can't be done at NATS subject level)
			if !filter.matches(evt) {
				_ = msg.Ack()
				continue
			}

			writeSSEEvent(w, evt)
			flusher.Flush()
			_ = msg.Ack()
		}
	}
}

// streamFromMemory uses the in-memory fan-out for SSE streaming.
// This is the original implementation, used when JetStream is not available.
func (h *HTTPServer) streamFromMemory(w http.ResponseWriter, r *http.Request, flusher http.Flusher, sinceMs int64, filter sseFilter) {
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
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case evt, ok := <-ch:
			if !ok {
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

// payloadToMutationEventSSE converts a NATS MutationEventPayload to an rpc MutationEvent.
func payloadToMutationEventSSE(p eventbus.MutationEventPayload) MutationEvent {
	ts, _ := time.Parse(time.RFC3339Nano, p.Timestamp)
	return MutationEvent{
		Type:      p.Type,
		IssueID:   p.IssueID,
		Title:     p.Title,
		Assignee:  p.Assignee,
		Actor:     p.Actor,
		Timestamp: ts,
		OldStatus: p.OldStatus,
		NewStatus: p.NewStatus,
		ParentID:  p.ParentID,
		IssueType: p.IssueType,
		Labels:    p.Labels,
		AwaitType: p.AwaitType,
	}
}

// mutationTypeToNATSSubject maps a mutation type string to the NATS subject suffix.
func mutationTypeToNATSSubject(mutType string) string {
	switch mutType {
	case MutationCreate:
		return string(eventbus.EventMutationCreate)
	case MutationUpdate:
		return string(eventbus.EventMutationUpdate)
	case MutationDelete:
		return string(eventbus.EventMutationDelete)
	case MutationComment:
		return string(eventbus.EventMutationComment)
	case MutationStatus:
		return string(eventbus.EventMutationStatus)
	default:
		return ""
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
