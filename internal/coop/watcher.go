package coop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Watcher subscribes to Coop's WebSocket state_change events and delivers
// them to a channel. It replaces polling tmux + screen parsing for agent
// state detection.
type Watcher struct {
	wsURL string
	token string

	mu   sync.Mutex
	conn *websocket.Conn
	done chan struct{}
}

// NewWatcher creates a WebSocket watcher. baseURL is the Coop HTTP base
// (e.g. "http://localhost:3000") which gets converted to ws://.
func NewWatcher(baseURL string, opts ...Option) *Watcher {
	// Convert http(s) to ws(s)
	u := strings.TrimRight(baseURL, "/")
	u = strings.Replace(u, "https://", "wss://", 1)
	u = strings.Replace(u, "http://", "ws://", 1)

	w := &Watcher{
		wsURL: u + "/ws",
		done:  make(chan struct{}),
	}

	// Apply options to extract token
	dummy := &Client{}
	for _, o := range opts {
		o(dummy)
	}
	w.token = dummy.token

	return w
}

// Watch connects to the WebSocket and streams StateChangeEvents until the
// context is cancelled. Reconnects on connection loss with exponential backoff.
// The returned channel is closed when the context is cancelled.
func (w *Watcher) Watch(ctx context.Context) (<-chan StateChangeEvent, error) {
	ch := make(chan StateChangeEvent, 64)

	go func() {
		defer close(ch)

		backoff := time.Second
		maxBackoff := 30 * time.Second

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			err := w.connect(ctx, ch)
			if err == nil || ctx.Err() != nil {
				return
			}

			// Backoff before reconnect
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff = min(backoff*2, maxBackoff)
			}
		}
	}()

	return ch, nil
}

func (w *Watcher) connect(ctx context.Context, ch chan<- StateChangeEvent) error {
	u, err := url.Parse(w.wsURL)
	if err != nil {
		return fmt.Errorf("coop: parse ws url: %w", err)
	}

	q := u.Query()
	q.Set("mode", "state")
	if w.token != "" {
		q.Set("token", w.token)
	}
	u.RawQuery = q.Encode()

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return fmt.Errorf("coop: ws dial: %w", err)
	}

	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.conn = nil
		w.mu.Unlock()
		conn.Close()
	}()

	// Read loop — context cancellation closes the conn
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("coop: ws read: %w", err)
		}

		// Peek at the type field
		var envelope struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			continue
		}

		if envelope.Type != WSTypeStateChange {
			continue
		}

		var event StateChangeEvent
		if json.Unmarshal(data, &event) != nil {
			continue
		}

		select {
		case ch <- event:
		case <-ctx.Done():
			return nil
		}
	}
}

// Close shuts down the watcher. Safe to call multiple times.
func (w *Watcher) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
}
