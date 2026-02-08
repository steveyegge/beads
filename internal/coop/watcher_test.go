package coop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func TestWatcherStateChanges(t *testing.T) {
	events := []StateChangeEvent{
		{Type: WSTypeStateChange, Prev: StateWorking, Next: StateWaitingForInput, Seq: 100},
		{Type: WSTypeStateChange, Prev: StateWaitingForInput, Next: StatePermissionPrompt, Seq: 101,
			Prompt: &PromptContext{Type: "permission", Tool: "Bash", InputPreview: "rm -rf /"}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify mode=state query param
		if r.URL.Query().Get("mode") != "state" {
			t.Errorf("mode = %q, want 'state'", r.URL.Query().Get("mode"))
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		for _, ev := range events {
			data, _ := json.Marshal(ev)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Send a non-state-change message (should be filtered)
		conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`))
		time.Sleep(10 * time.Millisecond)

		// Close cleanly
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"))
	}))
	defer srv.Close()

	// Convert http://... to ws://...
	wsURL := strings.Replace(srv.URL, "http://", "http://", 1)
	w := NewWatcher(wsURL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := w.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch error: %v", err)
	}

	var received []StateChangeEvent
	timeout := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				goto done
			}
			received = append(received, ev)
			if len(received) >= len(events) {
				cancel()
				// Drain remaining
				for range ch {
				}
				goto done
			}
		case <-timeout:
			t.Fatal("timed out waiting for events")
			goto done
		}
	}
done:

	if len(received) != len(events) {
		t.Fatalf("got %d events, want %d", len(received), len(events))
	}

	if received[0].Prev != StateWorking || received[0].Next != StateWaitingForInput {
		t.Errorf("event[0] = %+v", received[0])
	}
	if received[1].Prompt == nil || received[1].Prompt.Tool != "Bash" {
		t.Errorf("event[1] prompt = %+v", received[1].Prompt)
	}
}

func TestWatcherAuthToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token != "secret-token" {
			t.Errorf("token = %q, want 'secret-token'", token)
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn.Close()
	}))
	defer srv.Close()

	w := NewWatcher(srv.URL, WithToken("secret-token"))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := w.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch error: %v", err)
	}

	// Just drain
	cancel()
	for range ch {
	}
}

func TestWatcherClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Hold connection open
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	w := NewWatcher(srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := w.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch error: %v", err)
	}

	// Give it time to connect
	time.Sleep(100 * time.Millisecond)

	w.Close()
	cancel()

	// Channel should close
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel closed after Close()")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}
