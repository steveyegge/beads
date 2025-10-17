package rpc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Client is an RPC client that communicates with the bd daemon.
type Client struct {
	sockPath string
	mu       sync.Mutex
	conn     net.Conn
}

// TryConnect attempts to connect to the daemon and returns a client if successful.
// Returns nil if the daemon is not running or socket doesn't exist.
func TryConnect(sockPath string) *Client {
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		return nil
	}

	conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		return nil
	}

	client := &Client{
		sockPath: sockPath,
		conn:     conn,
	}

	if !client.ping() {
		conn.Close()
		return nil
	}

	return client
}

// ping sends a test request to verify the daemon is responsive.
func (c *Client) ping() bool {
	req, _ := NewRequest(OpStats, nil)
	_, err := c.Execute(req)
	return err == nil
}

// Execute sends a request to the daemon and returns the response.
func (c *Client) Execute(req *Request) (*Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil, fmt.Errorf("client not connected")
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	reqJSON = append(reqJSON, '\n')

	if _, err := c.conn.Write(reqJSON); err != nil {
		_ = c.reconnect()
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	scanner := bufio.NewScanner(c.conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			_ = c.reconnect()
			return nil, fmt.Errorf("failed to read response: %w", err)
		}
		_ = c.reconnect()
		return nil, fmt.Errorf("connection closed")
	}

	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp, nil
}

// reconnect attempts to reconnect to the daemon.
func (c *Client) reconnect() error {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	var err error
	backoff := 100 * time.Millisecond

	for i := 0; i < 3; i++ {
		c.conn, err = net.DialTimeout("unix", c.sockPath, 2*time.Second)
		if err == nil {
			return nil
		}
		time.Sleep(backoff)
		backoff *= 2
	}

	return fmt.Errorf("failed to reconnect after 3 attempts: %w", err)
}

// Close closes the client connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// SocketPath returns the default socket path for the given beads directory.
func SocketPath(beadsDir string) string {
	return filepath.Join(beadsDir, "bd.sock")
}

// DefaultSocketPath returns the socket path in the current working directory's .beads folder.
func DefaultSocketPath() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	beadsDir := filepath.Join(wd, ".beads")
	if _, err := os.Stat(beadsDir); os.IsNotExist(err) {
		return "", fmt.Errorf(".beads directory not found")
	}

	return SocketPath(beadsDir), nil
}
