// Package coop provides a Go HTTP client for the Coop terminal sidecar API.
//
// Coop (github.com/alfredjeanlab/coop) is a standalone Rust binary that spawns
// AI agents on PTYs, classifies agent state via structured detection, and exposes
// HTTP/WS/gRPC APIs. This client wraps Coop's REST API for use as a session
// backend in Gas Town, replacing tmux-based session management.
package coop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is an HTTP client for a single Coop sidecar instance.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithToken sets the Bearer auth token.
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithTimeout sets the default HTTP request timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// NewClient creates a Coop client for the sidecar at baseURL (e.g. "http://localhost:3000").
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// --- Session interface methods ---

// HasSession returns true if the Coop sidecar is healthy and the agent process
// has not exited. This replaces `tmux has-session -t <name>`.
func (c *Client) HasSession(ctx context.Context) (bool, error) {
	var resp HealthResponse
	if err := c.getJSON(ctx, "/api/v1/health", &resp); err != nil {
		return false, err
	}
	return resp.Status != ProcessExited, nil
}

// CapturePane returns the current terminal text. This replaces
// `tmux capture-pane -t <name> -p -S -100`.
func (c *Client) CapturePane(ctx context.Context) (string, error) {
	return c.getText(ctx, "/api/v1/screen/text")
}

// Screen returns structured screen data including cursor position and sequence.
func (c *Client) Screen(ctx context.Context) (*ScreenResponse, error) {
	var resp ScreenResponse
	if err := c.getJSON(ctx, "/api/v1/screen", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// NudgeSession sends a message to an idle agent. This replaces
// `tmux send-keys -t <name> "<message>" Enter`.
func (c *Client) NudgeSession(ctx context.Context, message string) (*NudgeResponse, error) {
	var resp NudgeResponse
	err := c.postJSON(ctx, "/api/v1/agent/nudge", NudgeRequest{Message: message}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// RespondToPrompt responds to an active permission, plan, or ask_user prompt.
func (c *Client) RespondToPrompt(ctx context.Context, req RespondRequest) (*RespondResponse, error) {
	var resp RespondResponse
	err := c.postJSON(ctx, "/api/v1/agent/respond", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// AcceptPrompt accepts the current permission or plan prompt.
func (c *Client) AcceptPrompt(ctx context.Context) (*RespondResponse, error) {
	accept := true
	return c.RespondToPrompt(ctx, RespondRequest{Accept: &accept})
}

// DenyPrompt denies the current permission or plan prompt.
func (c *Client) DenyPrompt(ctx context.Context) (*RespondResponse, error) {
	deny := false
	return c.RespondToPrompt(ctx, RespondRequest{Accept: &deny})
}

// SelectOption selects an option for an ask_user prompt (0-indexed).
func (c *Client) SelectOption(ctx context.Context, option int) (*RespondResponse, error) {
	return c.RespondToPrompt(ctx, RespondRequest{Option: &option})
}

// RespondText sends a freeform text response to an ask_user prompt.
func (c *Client) RespondText(ctx context.Context, text string) (*RespondResponse, error) {
	return c.RespondToPrompt(ctx, RespondRequest{Text: text})
}

// AgentState returns the current structured agent state.
func (c *Client) AgentState(ctx context.Context) (*AgentStateResponse, error) {
	var resp AgentStateResponse
	if err := c.getJSON(ctx, "/api/v1/agent/state", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Health returns the Coop sidecar health status.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	var resp HealthResponse
	if err := c.getJSON(ctx, "/api/v1/health", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Status returns process status including exit code.
func (c *Client) Status(ctx context.Context) (*StatusResponse, error) {
	var resp StatusResponse
	if err := c.getJSON(ctx, "/api/v1/status", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Signal sends a signal to the agent process (e.g. "SIGINT", "SIGTERM").
func (c *Client) Signal(ctx context.Context, signal string) error {
	return c.postJSON(ctx, "/api/v1/signal", SignalRequest{Signal: signal}, nil)
}

// SendInput sends text input to the terminal.
func (c *Client) SendInput(ctx context.Context, text string, enter bool) (*InputResponse, error) {
	var resp InputResponse
	err := c.postJSON(ctx, "/api/v1/input", InputRequest{Text: text, Enter: enter}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// Shutdown requests the Coop sidecar to shut down gracefully.
// This replaces tmux kill-session for Coop-backed agents.
func (c *Client) Shutdown(ctx context.Context) error {
	return c.postJSON(ctx, "/api/v1/shutdown", nil, nil)
}

// IsAgentRunning returns true if the agent process is actively running
// (not exited or crashed). Uses the agent state endpoint.
func (c *Client) IsAgentRunning(ctx context.Context) (bool, error) {
	resp, err := c.AgentState(ctx)
	if err != nil {
		// If Coop returns EXITED error, the agent is not running.
		if cerr, ok := err.(*CoopError); ok && cerr.IsExited() {
			return false, nil
		}
		return false, err
	}
	return resp.State != StateExited && resp.State != "crashed", nil
}

// --- Environment & CWD ---

// GetEnvironment reads a single environment variable. Checks pending
// overrides first, then falls back to the child process /proc environ.
func (c *Client) GetEnvironment(ctx context.Context, key string) (string, error) {
	var resp EnvGetResponse
	if err := c.getJSON(ctx, "/api/v1/env/"+key, &resp); err != nil {
		return "", err
	}
	if resp.Value == nil {
		return "", nil
	}
	return *resp.Value, nil
}

// SetEnvironment stores a pending environment variable override. The value
// is applied on the next session switch.
func (c *Client) SetEnvironment(ctx context.Context, key, value string) error {
	return c.putJSON(ctx, "/api/v1/env/"+key, EnvPutRequest{Value: value}, nil)
}

// DeleteEnvironment removes a pending environment variable override.
func (c *Client) DeleteEnvironment(ctx context.Context, key string) error {
	return c.doJSON(ctx, http.MethodDelete, "/api/v1/env/"+key, nil, nil)
}

// ListEnvironment returns all child process env vars and pending overrides.
func (c *Client) ListEnvironment(ctx context.Context) (*EnvListResponse, error) {
	var resp EnvListResponse
	if err := c.getJSON(ctx, "/api/v1/env", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetWorkingDirectory returns the child process working directory.
func (c *Client) GetWorkingDirectory(ctx context.Context) (string, error) {
	var resp CwdResponse
	if err := c.getJSON(ctx, "/api/v1/session/cwd", &resp); err != nil {
		return "", err
	}
	return resp.Cwd, nil
}

// --- HTTP helpers ---

// CoopError is returned when the Coop API returns an error status code.
type CoopError struct {
	StatusCode int
	ErrorCode  string
	Message    string
}

func (e *CoopError) Error() string {
	if e.ErrorCode != "" {
		return fmt.Sprintf("coop: %s (%d): %s", e.ErrorCode, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("coop: HTTP %d: %s", e.StatusCode, e.Message)
}

// IsNotReady returns true if the error indicates the agent process hasn't started yet.
func (e *CoopError) IsNotReady() bool { return e.ErrorCode == "NOT_READY" }

// IsExited returns true if the error indicates the agent process has exited.
func (e *CoopError) IsExited() bool { return e.ErrorCode == "EXITED" }

// IsAgentBusy returns true if the error indicates the agent is busy (not idle).
func (e *CoopError) IsAgentBusy() bool { return e.ErrorCode == "AGENT_BUSY" }

// IsNoPrompt returns true if the error indicates no active prompt to respond to.
func (e *CoopError) IsNoPrompt() bool { return e.ErrorCode == "NO_PROMPT" }

// IsWriterBusy returns true if the error indicates another client holds the write lock.
func (e *CoopError) IsWriterBusy() bool { return e.ErrorCode == "WRITER_BUSY" }

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out interface{}) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("coop: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("coop: GET %s: decode: %w", path, err)
		}
	}
	return nil
}

func (c *Client) getText(ctx context.Context, path string) (string, error) {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("coop: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", c.parseError(resp)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("coop: GET %s: read: %w", path, err)
	}
	return string(data), nil
}

func (c *Client) putJSON(ctx context.Context, path string, body interface{}, out interface{}) error {
	return c.doJSON(ctx, http.MethodPut, path, body, out)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("coop: marshal: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := c.newRequest(ctx, method, path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("coop: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("coop: %s %s: decode: %w", method, path, err)
		}
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, path string, body interface{}, out interface{}) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("coop: marshal: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := c.newRequest(ctx, http.MethodPost, path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("coop: POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("coop: POST %s: decode: %w", path, err)
		}
	}
	return nil
}

func (c *Client) parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	cerr := &CoopError{StatusCode: resp.StatusCode}

	var errResp ErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Code != "" {
		cerr.ErrorCode = errResp.Code
		cerr.Message = errResp.Message
	} else {
		cerr.Message = strings.TrimSpace(string(body))
	}
	return cerr
}
