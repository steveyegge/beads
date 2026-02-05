//go:build !windows

package rpc

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/debug"
)

// HTTPClient represents an HTTP client that connects to the daemon via HTTP/Connect-RPC
type HTTPClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
	timeout    time.Duration
	dbPath     string
	actor      string
}

// GetDaemonHTTPURL returns the BD_DAEMON_HTTP_URL environment variable if set.
// When set, clients should connect via HTTP to this URL instead of Unix socket or TCP.
// Example: https://bd-daemon.app.e2e.dev.fics.ai
func GetDaemonHTTPURL() string {
	return os.Getenv("BD_DAEMON_HTTP_URL")
}

// IsHTTPURL returns true if the given address looks like an HTTP/HTTPS URL
func IsHTTPURL(addr string) bool {
	return strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://")
}

// TryConnectHTTP attempts to connect to a remote daemon via HTTP/Connect-RPC.
// Returns nil if the daemon is not reachable or unhealthy.
// The token parameter is used for Bearer authentication with the remote daemon.
func TryConnectHTTP(baseURL string, token string) (*HTTPClient, error) {
	return TryConnectHTTPWithTimeout(baseURL, token, 10*time.Second)
}

// TryConnectHTTPWithTimeout attempts to connect to a remote daemon via HTTP
// using the provided timeout.
// Returns nil if the daemon is not reachable or unhealthy.
func TryConnectHTTPWithTimeout(baseURL string, token string, timeout time.Duration) (*HTTPClient, error) {
	rpcDebugLog("attempting HTTP connection to: %s", baseURL)

	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	// Normalize URL - remove trailing slash
	baseURL = strings.TrimSuffix(baseURL, "/")

	client := &HTTPClient{
		baseURL: baseURL,
		token:   token,
		timeout: 30 * time.Second,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					// Allow self-signed certs for internal testing
					// In production, this should be configurable
					InsecureSkipVerify: os.Getenv("BD_INSECURE_SKIP_VERIFY") == "1",
				},
			},
		},
	}

	// Perform health check
	rpcDebugLog("performing HTTP health check")
	healthStart := time.Now()
	health, err := client.Health()
	healthDuration := time.Since(healthStart)

	if err != nil {
		debug.Logf("HTTP health check failed: %v", err)
		rpcDebugLog("HTTP health check failed after %v: %v", healthDuration, err)
		return nil, fmt.Errorf("health check failed for remote daemon at %s: %w", baseURL, err)
	}

	if health.Status == "unhealthy" {
		debug.Logf("remote daemon unhealthy: %s", health.Error)
		rpcDebugLog("remote daemon unhealthy (checked in %v): %s", healthDuration, health.Error)
		return nil, fmt.Errorf("remote daemon at %s is unhealthy: %s", baseURL, health.Error)
	}

	debug.Logf("connected to remote daemon via HTTP at %s (status: %s, uptime: %.1fs)",
		baseURL, health.Status, health.Uptime)
	rpcDebugLog("HTTP connection successful (health check: %v, status: %s, uptime: %.1fs)",
		healthDuration, health.Status, health.Uptime)

	return client, nil
}

// Close is a no-op for HTTP client (no persistent connection)
func (c *HTTPClient) Close() error {
	return nil
}

// SetTimeout sets the request timeout duration
func (c *HTTPClient) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

// SetDatabasePath sets the expected database path for validation
func (c *HTTPClient) SetDatabasePath(dbPath string) {
	c.dbPath = dbPath
}

// SetActor sets the actor for audit trail
func (c *HTTPClient) SetActor(actor string) {
	c.actor = actor
}

// SetToken sets the authentication token
func (c *HTTPClient) SetToken(token string) {
	c.token = token
}

// IsRemote returns true (HTTP is always remote)
func (c *HTTPClient) IsRemote() bool {
	return true
}

// Execute sends an RPC request via HTTP and waits for a response
func (c *HTTPClient) Execute(operation string, args interface{}) (*Response, error) {
	return c.ExecuteWithCwd(operation, args, "")
}

// ExecuteWithCwd sends an RPC request via HTTP with an explicit cwd
func (c *HTTPClient) ExecuteWithCwd(operation string, args interface{}, cwd string) (*Response, error) {
	// Map operation to HTTP method name
	methodName := operationToHTTPMethod(operation)
	if methodName == "" {
		return nil, fmt.Errorf("unsupported operation for HTTP: %s", operation)
	}

	// Marshal args to JSON
	var body []byte
	var err error
	if args != nil {
		body, err = json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal args: %w", err)
		}
	} else {
		body = []byte("{}")
	}

	// Build URL
	url := fmt.Sprintf("%s/bd.v1.BeadsService/%s", c.baseURL, methodName)

	// Create request
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.actor != "" {
		req.Header.Set("X-BD-Actor", c.actor)
	}
	if ClientVersion != "" {
		req.Header.Set("X-BD-Client-Version", ClientVersion)
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if cwd != "" {
		req.Header.Set("X-BD-Cwd", cwd)
	}
	if c.dbPath != "" {
		req.Header.Set("X-BD-Expected-DB", c.dbPath)
	}

	// Execute request
	rpcDebugLog("HTTP request: %s %s", req.Method, url)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	rpcDebugLog("HTTP response: status=%d, body=%s", resp.StatusCode, string(respBody))

	// Check for HTTP errors
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("authentication failed: unauthorized")
	}

	// For HTTP, successful responses return raw data, errors return {"error": "..."}
	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error != "" {
			return &Response{Success: false, Error: errResp.Error}, fmt.Errorf("operation failed: %s", errResp.Error)
		}
		return nil, fmt.Errorf("HTTP error: status %d", resp.StatusCode)
	}

	// Return successful response with data
	return &Response{
		Success: true,
		Data:    respBody,
	}, nil
}

// Health sends a health check request via HTTP
func (c *HTTPClient) Health() (*HealthResponse, error) {
	resp, err := c.Execute(OpHealth, nil)
	if err != nil {
		return nil, err
	}

	var health HealthResponse
	if err := json.Unmarshal(resp.Data, &health); err != nil {
		return nil, fmt.Errorf("failed to unmarshal health response: %w", err)
	}

	return &health, nil
}

// operationToHTTPMethod maps RPC operation names to Connect-RPC HTTP method names
func operationToHTTPMethod(operation string) string {
	methodMap := map[string]string{
		// Core CRUD
		OpList:      "List",
		OpShow:      "Show",
		OpCreate:    "Create",
		OpUpdate:    "Update",
		OpDelete:    "Delete",
		OpRename:    "Rename",
		OpClose:     "Close",
		OpCount:     "Count",
		OpResolveID: "ResolveID",

		// Status
		OpHealth:  "Health",
		OpStatus:  "Status",
		OpPing:    "Ping",
		OpMetrics: "Metrics",

		// Queries
		OpReady:   "Ready",
		OpBlocked: "Blocked",
		OpStale:   "Stale",
		OpStats:   "Stats",

		// Dependencies
		OpDepAdd:    "DepAdd",
		OpDepRemove: "DepRemove",
		OpDepTree:   "DepTree",

		// Labels
		OpLabelAdd:    "LabelAdd",
		OpLabelRemove: "LabelRemove",

		// Comments
		OpCommentList: "CommentList",
		OpCommentAdd:  "CommentAdd",

		// Batch
		OpBatch: "Batch",

		// Sync
		OpExport:       "Export",
		OpImport:       "Import",
		OpCompact:      "Compact",
		OpCompactStats: "CompactStats",

		// Epic
		OpEpicStatus: "EpicStatus",

		// Mutations
		OpGetMutations:        "GetMutations",
		OpGetMoleculeProgress: "GetMoleculeProgress",
		OpGetWorkerStatus:     "GetWorkerStatus",
		OpGetConfig:           "GetConfig",

		// Gates
		OpGateCreate: "GateCreate",
		OpGateList:   "GateList",
		OpGateShow:   "GateShow",
		OpGateClose:  "GateClose",
		OpGateWait:   "GateWait",

		// Decisions
		OpDecisionCreate:  "DecisionCreate",
		OpDecisionGet:     "DecisionGet",
		OpDecisionResolve: "DecisionResolve",
		OpDecisionList:    "DecisionList",
		OpDecisionRemind:  "DecisionRemind",
		OpDecisionCancel:  "DecisionCancel",

		// Mol operations
		OpMolBond:   "MolBond",
		OpMolSquash: "MolSquash",
		OpMolBurn:   "MolBurn",

		// Atomic operations
		OpCreateWithDeps:           "CreateWithDeps",
		OpBatchAddLabels:           "BatchAddLabels",
		OpCreateMolecule:           "CreateMolecule",
		OpBatchAddDependencies:     "BatchAddDependencies",
		OpBatchQueryWorkers:        "BatchQueryWorkers",
		OpCreateConvoyWithTracking: "CreateConvoyWithTracking",
		OpAtomicClosureChain:       "AtomicClosureChain",

		// Remote database management
		OpInit:    "Init",
		OpMigrate: "Migrate",

		// Additional write operations (bd-wj80)
		OpRenamePrefix: "RenamePrefix",
		OpMove:         "Move",
		OpRefile:       "Refile",
		OpCook:         "Cook",
		OpPour:         "Pour",

		// Admin
		OpShutdown: "Shutdown",
	}

	return methodMap[operation]
}

// Implement remaining Client methods for HTTPClient to satisfy interface needs
// These delegate to Execute which handles HTTP transport

// Ping sends a ping request
func (c *HTTPClient) Ping() error {
	_, err := c.Execute(OpPing, nil)
	return err
}

// Status retrieves daemon status
func (c *HTTPClient) Status() (*StatusResponse, error) {
	resp, err := c.Execute(OpStatus, nil)
	if err != nil {
		return nil, err
	}
	var status StatusResponse
	if err := json.Unmarshal(resp.Data, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal status response: %w", err)
	}
	return &status, nil
}

// List lists issues
func (c *HTTPClient) List(args *ListArgs) (*Response, error) {
	return c.Execute(OpList, args)
}

// Show shows an issue
func (c *HTTPClient) Show(args *ShowArgs) (*Response, error) {
	return c.Execute(OpShow, args)
}

// Create creates a new issue
func (c *HTTPClient) Create(args *CreateArgs) (*Response, error) {
	return c.Execute(OpCreate, args)
}

// Update updates an issue
func (c *HTTPClient) Update(args *UpdateArgs) (*Response, error) {
	return c.Execute(OpUpdate, args)
}

// CloseIssue closes an issue
func (c *HTTPClient) CloseIssue(args *CloseArgs) (*Response, error) {
	return c.Execute(OpClose, args)
}

// Delete deletes issues
func (c *HTTPClient) Delete(args *DeleteArgs) (*Response, error) {
	return c.Execute(OpDelete, args)
}

// Count counts issues
func (c *HTTPClient) Count(args *CountArgs) (*Response, error) {
	return c.Execute(OpCount, args)
}
