package coop

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestAttachEchoSession(t *testing.T) {
	// Server echoes input back as output messages
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mode") != "raw" {
			t.Errorf("mode = %q, want 'raw'", r.URL.Query().Get("mode"))
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				Type string `json:"type"`
				Data string `json:"data"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.Type != "input" {
				continue
			}
			// Echo back as output
			reply, _ := json.Marshal(map[string]string{
				"type": "output",
				"data": msg.Data,
			})
			conn.WriteMessage(websocket.TextMessage, reply)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Use a pipe for stdin so we control input
	stdinR, stdinW := io.Pipe()
	var stdout bytes.Buffer

	c := NewClient(srv.URL)
	opts := AttachOptions{
		DetachKey: 0x1d,
		Stdin:     stdinR,
		Stdout:    &stdout,
		Stderr:    io.Discard,
	}

	done := make(chan error, 1)
	go func() {
		done <- c.Attach(ctx, opts)
	}()

	// Send some input
	stdinW.Write([]byte("hello"))
	time.Sleep(100 * time.Millisecond)

	// Send detach key
	stdinW.Write([]byte{0x1d})
	time.Sleep(100 * time.Millisecond)

	err := <-done
	if err != nil {
		t.Fatalf("Attach error: %v", err)
	}

	if !strings.Contains(stdout.String(), "hello") {
		t.Errorf("stdout = %q, want to contain 'hello'", stdout.String())
	}
}

func TestAttachDetachKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Hold connection open
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stdinR, stdinW := io.Pipe()
	var stderr bytes.Buffer

	c := NewClient(srv.URL)
	opts := AttachOptions{
		DetachKey: 0x1d,
		Stdin:     stdinR,
		Stdout:    io.Discard,
		Stderr:    &stderr,
	}

	done := make(chan error, 1)
	go func() {
		done <- c.Attach(ctx, opts)
	}()

	// Give it time to connect
	time.Sleep(100 * time.Millisecond)

	// Send some data then detach
	stdinW.Write([]byte("abc"))
	time.Sleep(50 * time.Millisecond)
	stdinW.Write([]byte{0x1d})

	err := <-done
	if err != nil {
		t.Fatalf("Attach error: %v", err)
	}

	if !strings.Contains(stderr.String(), "Detached") {
		t.Errorf("stderr = %q, want 'Detached'", stderr.String())
	}
}

func TestAttachServerExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Send exit message after a short delay
		time.Sleep(50 * time.Millisecond)
		exitMsg, _ := json.Marshal(map[string]interface{}{
			"type":   "exit",
			"code":   0,
			"signal": "",
		})
		conn.WriteMessage(websocket.TextMessage, exitMsg)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// stdin that blocks forever (never sends detach)
	stdinR, _ := io.Pipe()

	c := NewClient(srv.URL)
	opts := AttachOptions{
		DetachKey: 0x1d,
		Stdin:     stdinR,
		Stdout:    io.Discard,
		Stderr:    io.Discard,
	}

	err := c.Attach(ctx, opts)
	if err != nil {
		t.Fatalf("Attach error: %v", err)
	}
}

func TestAttachAuthToken(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.URL.Query().Get("token")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn.Close()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stdinR, _ := io.Pipe()

	c := NewClient(srv.URL, WithToken("my-secret"))
	opts := AttachOptions{
		Stdin:  stdinR,
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	c.Attach(ctx, opts)

	if gotToken != "my-secret" {
		t.Errorf("token = %q, want 'my-secret'", gotToken)
	}
}

func TestAttachContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	stdinR, _ := io.Pipe()

	c := NewClient(srv.URL)
	opts := AttachOptions{
		Stdin:  stdinR,
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	// Should return when context expires
	start := time.Now()
	c.Attach(ctx, opts)
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("Attach took %v, expected < 3s (context should cancel)", elapsed)
	}
}

func TestAttachBinaryOutput(t *testing.T) {
	binaryData := []byte{0x00, 0x01, 0x02, 0xff, 0xfe}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Send binary data
		conn.WriteMessage(websocket.BinaryMessage, binaryData)
		time.Sleep(50 * time.Millisecond)
		conn.Close()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stdinR, _ := io.Pipe()
	var stdout bytes.Buffer

	c := NewClient(srv.URL)
	opts := AttachOptions{
		Stdin:  stdinR,
		Stdout: &stdout,
		Stderr: io.Discard,
	}

	c.Attach(ctx, opts)

	if !bytes.Contains(stdout.Bytes(), binaryData) {
		t.Errorf("stdout = %v, want to contain %v", stdout.Bytes(), binaryData)
	}
}

func TestAttachResizeMessage(t *testing.T) {
	var receivedMsgs []map[string]interface{}
	var mu = &sync.Mutex{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if json.Unmarshal(data, &msg) == nil {
				mu.Lock()
				receivedMsgs = append(receivedMsgs, msg)
				mu.Unlock()
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stdinR, stdinW := io.Pipe()

	c := NewClient(srv.URL)
	opts := AttachOptions{
		DetachKey: 0x1d,
		Stdin:     stdinR,
		Stdout:    io.Discard,
		Stderr:    io.Discard,
	}

	done := make(chan error, 1)
	go func() {
		done <- c.Attach(ctx, opts)
	}()

	time.Sleep(200 * time.Millisecond)

	// Detach
	stdinW.Write([]byte{0x1d})
	<-done

	// stdin is a pipe, not a TTY, so no resize message expected.
	// This test just verifies attach doesn't crash when stdin isn't a TTY.
}

