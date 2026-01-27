// Package dialog provides a client for sending dialog requests to a remote UI.
// The UI client (dialog-client) runs on the user's machine and establishes an
// SSH reverse tunnel. This package connects through that tunnel.
package dialog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// Client sends dialog requests to a remote UI client
type Client struct {
	addr    string
	conn    net.Conn
	reader  *bufio.Reader
	mu      sync.Mutex
	timeout time.Duration
}

// Request is sent to the UI client
type Request struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"` // "entry", "choice", "confirm"
	Title   string   `json:"title"`
	Prompt  string   `json:"prompt"`
	Options []Option `json:"options,omitempty"`
	Default string   `json:"default,omitempty"`
}

// Option for choice dialogs
type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Response from the UI client
type Response struct {
	ID        string `json:"id"`
	Canceled bool   `json:"cancelled"` //nolint:misspell // JSON API compatibility
	Text      string `json:"text,omitempty"`
	Selected  string `json:"selected,omitempty"`
	Error     string `json:"error,omitempty"`
}

// DefaultPort is the default port for dialog communication
const DefaultPort = 9876

// NewClient creates a new dialog client
func NewClient(addr string) *Client {
	if addr == "" {
		addr = fmt.Sprintf("127.0.0.1:%d", DefaultPort)
	}
	return &Client{
		addr:    addr,
		timeout: 5 * time.Minute, // dialogs can take a while
	}
}

// SetTimeout sets the response timeout
func (c *Client) SetTimeout(d time.Duration) {
	c.timeout = d
}

// Connect establishes connection to the UI client
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return nil // already connected
	}

	conn, err := net.DialTimeout("tcp", c.addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to dialog client at %s: %w", c.addr, err)
	}

	c.conn = conn
	c.reader = bufio.NewReader(conn)
	return nil
}

// Close closes the connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.reader = nil
		return err
	}
	return nil
}

// IsConnected returns true if connected
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// Send sends a dialog request and waits for response
func (c *Client) Send(req *Request) (*Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	// Set deadline for this request
	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))
	defer func() { _ = c.conn.SetDeadline(time.Time{}) }()

	// Send request
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if _, err := c.conn.Write(append(reqJSON, '\n')); err != nil {
		c.conn = nil // mark as disconnected
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Read response
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		c.conn = nil
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.Error != "" {
		return &resp, fmt.Errorf("dialog error: %s", resp.Error)
	}

	return &resp, nil
}

// ShowEntry shows a text entry dialog
func (c *Client) ShowEntry(id, title, prompt, defaultValue string) (string, bool, error) {
	resp, err := c.Send(&Request{
		ID:      id,
		Type:    "entry",
		Title:   title,
		Prompt:  prompt,
		Default: defaultValue,
	})
	if err != nil {
		return "", false, err
	}
	return resp.Text, resp.Canceled, nil
}

// ShowChoice shows a choice dialog
func (c *Client) ShowChoice(id, title, prompt string, options []Option) (string, bool, error) {
	resp, err := c.Send(&Request{
		ID:      id,
		Type:    "choice",
		Title:   title,
		Prompt:  prompt,
		Options: options,
	})
	if err != nil {
		return "", false, err
	}
	return resp.Selected, resp.Canceled, nil
}

// ShowConfirm shows a yes/no confirmation dialog
func (c *Client) ShowConfirm(id, title, prompt string) (bool, bool, error) {
	resp, err := c.Send(&Request{
		ID:      id,
		Type:    "confirm",
		Title:   title,
		Prompt:  prompt,
	})
	if err != nil {
		return false, false, err
	}
	return resp.Selected == "Yes", resp.Canceled, nil
}

// Global client for convenience
var defaultClient *Client
var defaultClientOnce sync.Once

// DefaultClient returns a shared client instance
func DefaultClient() *Client {
	defaultClientOnce.Do(func() {
		defaultClient = NewClient("")
	})
	return defaultClient
}

// Quick helpers using default client

// Entry shows a text entry dialog using the default client
func Entry(id, title, prompt, defaultValue string) (string, bool, error) {
	client := DefaultClient()
	if err := client.Connect(); err != nil {
		return "", false, err
	}
	return client.ShowEntry(id, title, prompt, defaultValue)
}

// Choice shows a choice dialog using the default client
func Choice(id, title, prompt string, options []Option) (string, bool, error) {
	client := DefaultClient()
	if err := client.Connect(); err != nil {
		return "", false, err
	}
	return client.ShowChoice(id, title, prompt, options)
}

// Confirm shows a confirmation dialog using the default client
func Confirm(id, title, prompt string) (bool, bool, error) {
	client := DefaultClient()
	if err := client.Connect(); err != nil {
		return false, false, err
	}
	return client.ShowConfirm(id, title, prompt)
}
