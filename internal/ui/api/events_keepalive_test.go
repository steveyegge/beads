package api

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type idleEventSource struct {
	subscribed chan struct{}
}

func newIdleEventSource() *idleEventSource {
	return &idleEventSource{
		subscribed: make(chan struct{}),
	}
}

func (s *idleEventSource) Subscribe(ctx context.Context) (<-chan IssueEvent, error) {
	ch := make(chan IssueEvent)

	go func() {
		close(s.subscribed)
		<-ctx.Done()
	}()

	return ch, nil
}

func TestEventStreamKeepaliveScaledMinute(t *testing.T) {
	t.Parallel()

	const (
		scaledSecond       = 25 * time.Millisecond
		heartbeatInterval  = 30 * scaledSecond
		requiredHeartbeats = 2
		waitDuration       = 65 * scaledSecond
	)

	source := newIdleEventSource()
	handler := NewEventStreamHandler(
		source,
		WithHeartbeatInterval(heartbeatInterval),
	)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	server := &http.Server{
		Handler: handler,
	}

	serverErr := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	reqCtx, reqCancel := context.WithTimeout(context.Background(), waitDuration+2*scaledSecond)
	defer reqCancel()

	url := fmt.Sprintf("http://%s/events", listener.Addr().String())
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	select {
	case <-source.subscribed:
	case <-time.After(2 * scaledSecond):
		t.Fatalf("timed out waiting for event source subscription")
	}

	reader := bufio.NewReader(resp.Body)
	errCh := make(chan error, 1)
	heartbeatCh := make(chan int, 1)

	go func() {
		defer close(errCh)

		count := 0
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}

			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, ":") {
				continue
			}

			if strings.HasPrefix(trimmed, "event:") {
				event := strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
				if event == "heartbeat" {
					count++
					if count >= requiredHeartbeats {
						select {
						case heartbeatCh <- count:
						default:
						}
					}
				}
			}
		}
	}()

	select {
	case <-time.After(waitDuration):
		t.Fatalf("timed out waiting for %d heartbeats", requiredHeartbeats)
	case err := <-errCh:
		if err != nil {
			t.Fatalf("event stream terminated early: %v", err)
		}
		t.Fatalf("event stream ended unexpectedly without meeting heartbeat target")
	case <-heartbeatCh:
		// Success; connection stayed alive long enough to emit required heartbeats.
	}

	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("server error: %v", err)
		}
	default:
	}
}
