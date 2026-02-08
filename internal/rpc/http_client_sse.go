//go:build !windows

package rpc

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// SSEEvent represents a parsed Server-Sent Event.
type SSEEvent struct {
	ID    string         // Last-Event-ID
	Event string         // event type (e.g., "mutation")
	Data  MutationEvent  // parsed mutation event
	Raw   string         // raw data line
}

// SSEClientOptions configures the SSE client connection.
type SSEClientOptions struct {
	BaseURL string // e.g., "https://daemon.example.com"
	Token   string // Bearer auth token
	Since   int64  // unix ms timestamp for replay
	Filter  string // server-side filter (e.g., "issue:gt-abc")
}

// ConnectSSE connects to the daemon's SSE /events endpoint and returns a channel
// of parsed events. The channel is closed when the context is canceled or the
// connection drops. Errors are sent to the returned error channel.
func ConnectSSE(ctx context.Context, opts SSEClientOptions) (<-chan SSEEvent, <-chan error) {
	events := make(chan SSEEvent, 64)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		url := fmt.Sprintf("%s/events", strings.TrimSuffix(opts.BaseURL, "/"))
		sep := "?"
		if opts.Since > 0 {
			url += fmt.Sprintf("%ssince=%d", sep, opts.Since)
			sep = "&"
		}
		if opts.Filter != "" {
			url += fmt.Sprintf("%sfilter=%s", sep, opts.Filter)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			errs <- fmt.Errorf("creating SSE request: %w", err)
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		if opts.Token != "" {
			req.Header.Set("Authorization", "Bearer "+opts.Token)
		}

		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: os.Getenv("BD_INSECURE_SKIP_VERIFY") == "1",
				},
			},
		}

		resp, err := client.Do(req)
		if err != nil {
			errs <- fmt.Errorf("SSE connection failed: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errs <- fmt.Errorf("SSE endpoint returned status %d", resp.StatusCode)
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		// Allow large SSE events (1MB)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		var currentID, currentEvent, currentData string

		for scanner.Scan() {
			line := scanner.Text()

			if line == "" {
				// Empty line = event boundary, dispatch if we have data
				if currentData != "" {
					evt := SSEEvent{
						ID:    currentID,
						Event: currentEvent,
						Raw:   currentData,
					}
					// Try to parse as MutationEvent
					if err := json.Unmarshal([]byte(currentData), &evt.Data); err == nil {
						// parsed successfully
					}
					select {
					case events <- evt:
					case <-ctx.Done():
						return
					}
				}
				currentID = ""
				currentEvent = ""
				currentData = ""
				continue
			}

			if strings.HasPrefix(line, "id: ") || strings.HasPrefix(line, "id:") {
				currentID = strings.TrimPrefix(line, "id: ")
				currentID = strings.TrimPrefix(currentID, "id:")
				currentID = strings.TrimSpace(currentID)
			} else if strings.HasPrefix(line, "event: ") || strings.HasPrefix(line, "event:") {
				currentEvent = strings.TrimPrefix(line, "event: ")
				currentEvent = strings.TrimPrefix(currentEvent, "event:")
				currentEvent = strings.TrimSpace(currentEvent)
			} else if strings.HasPrefix(line, "data: ") || strings.HasPrefix(line, "data:") {
				data := strings.TrimPrefix(line, "data: ")
				data = strings.TrimPrefix(data, "data:")
				if currentData != "" {
					currentData += "\n" + data
				} else {
					currentData = data
				}
			}
			// Ignore comment lines (starting with :) and unknown fields
		}

		if err := scanner.Err(); err != nil {
			// Only report if context wasn't canceled
			if ctx.Err() == nil {
				errs <- fmt.Errorf("SSE stream error: %w", err)
			}
		}
	}()

	return events, errs
}

// BaseURL returns the base URL of the HTTP client.
func (c *HTTPClient) BaseURL() string {
	return c.baseURL
}

// Token returns the auth token of the HTTP client.
func (c *HTTPClient) Token() string {
	return c.token
}

// HTTPClient returns the underlying HTTP client, or nil if not using HTTP transport.
func (c *Client) HTTPClient() *HTTPClient {
	return c.httpClient
}
