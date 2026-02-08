package rpc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/lockfile"
)

// rpcDebugEnabled returns true if BD_DEBUG_RPC environment variable is set
func rpcDebugEnabled() bool {
	val := os.Getenv("BD_DEBUG_RPC")
	return val == "1" || val == "true"
}

// rpcDebugLog logs to stderr if BD_DEBUG_RPC is enabled
func rpcDebugLog(format string, args ...interface{}) {
	if rpcDebugEnabled() {
		fmt.Fprintf(os.Stderr, "[RPC DEBUG] "+format+"\n", args...)
	}
}

// ClientVersion is the version of this RPC client
// This should match the bd CLI version for proper compatibility checks
// It's set dynamically by main.go from cmd/bd/version.go before making RPC calls
var ClientVersion = "0.0.0" // Placeholder; overridden at startup

// Client represents an RPC client that connects to the daemon
type Client struct {
	conn       net.Conn
	socketPath string
	timeout    time.Duration
	dbPath     string // Expected database path for validation
	actor      string // Actor for audit trail (who is performing operations)
	token      string // Authentication token for TCP connections
	isRemote         bool   // True if connected via TCP or HTTP to remote daemon
	httpClient       *HTTPClient // If set, delegates to HTTP client instead of socket/TCP
	requestTimeoutMs int    // Per-request timeout in ms (0 = server default)
}

// TryConnect attempts to connect to the daemon socket
// Returns nil if no daemon is running or unhealthy
func TryConnect(socketPath string) (*Client, error) {
	return TryConnectWithTimeout(socketPath, 200*time.Millisecond)
}

// TryConnectWithTimeout attempts to connect to the daemon socket using the provided dial timeout.
// Returns nil if no daemon is running or unhealthy.
func TryConnectWithTimeout(socketPath string, dialTimeout time.Duration) (*Client, error) {
	rpcDebugLog("attempting connection to socket: %s", socketPath)

	// Fast probe: check daemon lock before attempting RPC connection if socket doesn't exist
	// This eliminates unnecessary connection attempts when no daemon is running
	// If socket exists, we skip lock check for backwards compatibility and test scenarios
	socketExists := endpointExists(socketPath)
	rpcDebugLog("socket exists check: %v", socketExists)

	if !socketExists {
		beadsDir := filepath.Dir(socketPath)
		running, _ := lockfile.TryDaemonLock(beadsDir)
		if !running {
			debug.Logf("daemon lock not held and socket missing (no daemon running)")
			rpcDebugLog("daemon lock not held (no daemon running)")
			// Self-heal: clean up stale artifacts when lock is free and socket is missing
			cleanupStaleDaemonArtifacts(beadsDir)
			return nil, nil
		}
		// Lock is held but socket was missing - re-check socket existence atomically
		// to handle race where daemon just started between first check and lock check
		rpcDebugLog("daemon lock held but socket was missing - re-checking socket existence")
		socketExists = endpointExists(socketPath)
		if !socketExists {
			// Lock held but socket still missing after re-check - daemon startup or crash
			debug.Logf("daemon lock held but socket missing after re-check (startup race or crash): %s", socketPath)
			rpcDebugLog("connection aborted: socket still missing despite lock being held")
			return nil, nil
		}
		rpcDebugLog("socket now exists after re-check (daemon startup race resolved)")
	}

	if dialTimeout <= 0 {
		dialTimeout = 200 * time.Millisecond
	}
	
	rpcDebugLog("dialing socket (timeout: %v)", dialTimeout)
	dialStart := time.Now()
	conn, err := dialRPC(socketPath, dialTimeout)
	dialDuration := time.Since(dialStart)
	
	if err != nil {
		debug.Logf("failed to connect to RPC endpoint: %v", err)
		rpcDebugLog("dial failed after %v: %v", dialDuration, err)

		// Fast-fail: socket exists but dial failed - check if daemon actually alive
		// If lock is not held, daemon crashed and left stale socket - clean up immediately
		beadsDir := filepath.Dir(socketPath)
		running, _ := lockfile.TryDaemonLock(beadsDir)
		if !running {
			rpcDebugLog("daemon not running (lock free) - cleaning up stale socket")
			cleanupStaleDaemonArtifacts(beadsDir)
			_ = os.Remove(socketPath) // Also remove stale socket
		}
		return nil, nil
	}
	
	rpcDebugLog("dial succeeded in %v", dialDuration)

	client := &Client{
		conn:       conn,
		socketPath: socketPath,
		timeout:    30 * time.Second,
	}

	rpcDebugLog("performing health check")
	healthStart := time.Now()
	health, err := client.Health()
	healthDuration := time.Since(healthStart)
	
	if err != nil {
		debug.Logf("health check failed: %v", err)
		rpcDebugLog("health check failed after %v: %v", healthDuration, err)
		_ = conn.Close()
		return nil, nil
	}

	if health.Status == "unhealthy" {
		debug.Logf("daemon unhealthy: %s", health.Error)
		rpcDebugLog("daemon unhealthy (checked in %v): %s", healthDuration, health.Error)
		_ = conn.Close()
		return nil, nil
	}

	debug.Logf("connected to daemon (status: %s, uptime: %.1fs)",
		health.Status, health.Uptime)
	rpcDebugLog("connection successful (health check: %v, status: %s, uptime: %.1fs)",
		healthDuration, health.Status, health.Uptime)

	return client, nil
}

// GetDaemonHost returns the daemon host address.
// Priority: BD_DAEMON_HOST env var > daemon-host config key.
// When set, clients should connect to this address instead of Unix socket.
func GetDaemonHost() string {
	if host := os.Getenv("BD_DAEMON_HOST"); host != "" {
		return host
	}
	return config.GetString("daemon-host")
}

// GetDaemonToken returns the daemon authentication token.
// Priority: BD_DAEMON_TOKEN env var > daemon-token config key.
// This token is used for authentication when connecting to remote daemons.
func GetDaemonToken() string {
	if token := os.Getenv("BD_DAEMON_TOKEN"); token != "" {
		return token
	}
	return config.GetString("daemon-token")
}

// TryConnectTCP attempts to connect to a remote daemon via TCP.
// Returns nil if the daemon is not reachable or unhealthy.
// The token parameter is used for authentication with the remote daemon.
func TryConnectTCP(addr string, token string) (*Client, error) {
	return TryConnectTCPWithTimeout(addr, token, 2*time.Second)
}

// TryConnectTCPWithTimeout attempts to connect to a remote daemon via TCP
// using the provided dial timeout.
// Returns nil if the daemon is not reachable or unhealthy.
func TryConnectTCPWithTimeout(addr string, token string, dialTimeout time.Duration) (*Client, error) {
	rpcDebugLog("attempting TCP connection to: %s", addr)

	if dialTimeout <= 0 {
		dialTimeout = 2 * time.Second
	}

	rpcDebugLog("dialing TCP (timeout: %v)", dialTimeout)
	dialStart := time.Now()
	conn, err := dialTCP(addr, dialTimeout)
	dialDuration := time.Since(dialStart)

	if err != nil {
		debug.Logf("failed to connect to remote daemon: %v", err)
		rpcDebugLog("TCP dial failed after %v: %v", dialDuration, err)
		return nil, fmt.Errorf("failed to connect to remote daemon at %s: %w", addr, err)
	}

	rpcDebugLog("TCP dial succeeded in %v", dialDuration)

	client := &Client{
		conn:     conn,
		timeout:  30 * time.Second,
		token:    token,
		isRemote: true,
	}

	rpcDebugLog("performing health check")
	healthStart := time.Now()
	health, err := client.Health()
	healthDuration := time.Since(healthStart)

	if err != nil {
		debug.Logf("health check failed: %v", err)
		rpcDebugLog("health check failed after %v: %v", healthDuration, err)
		_ = conn.Close()
		return nil, fmt.Errorf("health check failed for remote daemon at %s: %w", addr, err)
	}

	if health.Status == "unhealthy" {
		debug.Logf("remote daemon unhealthy: %s", health.Error)
		rpcDebugLog("remote daemon unhealthy (checked in %v): %s", healthDuration, health.Error)
		_ = conn.Close()
		return nil, fmt.Errorf("remote daemon at %s is unhealthy: %s", addr, health.Error)
	}

	debug.Logf("connected to remote daemon at %s (status: %s, uptime: %.1fs)",
		addr, health.Status, health.Uptime)
	rpcDebugLog("TCP connection successful (health check: %v, status: %s, uptime: %.1fs)",
		healthDuration, health.Status, health.Uptime)

	return client, nil
}

// TryConnectAuto attempts to connect to a daemon, automatically choosing
// between HTTP (if BD_DAEMON_HTTP_URL is set), TCP (if BD_DAEMON_HOST is set),
// or Unix socket (local).
// For remote connections, it uses BD_DAEMON_TOKEN for authentication if set.
// Returns nil if no daemon is available.
func TryConnectAuto(socketPath string) (*Client, error) {
	return TryConnectAutoWithTimeout(socketPath, 0)
}

// TryConnectAutoWithTimeout is like TryConnectAuto but with a custom timeout.
// For local connections, timeout defaults to 200ms if not specified.
// For TCP connections, timeout defaults to 2s if not specified.
// For HTTP connections, timeout defaults to 10s if not specified.
func TryConnectAutoWithTimeout(socketPath string, timeout time.Duration) (*Client, error) {
	// Check if HTTP URL is configured (highest priority)
	httpURL := GetDaemonHTTPURL()
	if httpURL != "" {
		rpcDebugLog("BD_DAEMON_HTTP_URL is set, attempting HTTP connection to: %s", httpURL)
		token := GetDaemonToken()
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		httpClient, err := TryConnectHTTPWithTimeout(httpURL, token, timeout)
		if err != nil {
			return nil, err
		}
		if httpClient == nil {
			return nil, nil
		}
		// Wrap HTTPClient in a Client-compatible wrapper
		return wrapHTTPClient(httpClient), nil
	}

	// Check if remote daemon host is configured (also check for HTTP URLs in BD_DAEMON_HOST)
	remoteHost := GetDaemonHost()
	if remoteHost != "" {
		// Check if it's an HTTP URL
		if IsHTTPURL(remoteHost) {
			rpcDebugLog("BD_DAEMON_HOST contains HTTP URL, attempting HTTP connection to: %s", remoteHost)
			token := GetDaemonToken()
			if timeout <= 0 {
				timeout = 10 * time.Second
			}
			httpClient, err := TryConnectHTTPWithTimeout(remoteHost, token, timeout)
			if err != nil {
				return nil, err
			}
			if httpClient == nil {
				return nil, nil
			}
			// Wrap HTTPClient in a Client-compatible wrapper
			return wrapHTTPClient(httpClient), nil
		}

		rpcDebugLog("BD_DAEMON_HOST is set, attempting TCP connection to: %s", remoteHost)
		token := GetDaemonToken()
		if timeout <= 0 {
			timeout = 2 * time.Second
		}
		return TryConnectTCPWithTimeout(remoteHost, token, timeout)
	}

	// Fall back to local Unix socket connection
	rpcDebugLog("BD_DAEMON_HOST not set, using local Unix socket")
	if timeout <= 0 {
		timeout = 200 * time.Millisecond
	}
	return TryConnectWithTimeout(socketPath, timeout)
}

// wrapHTTPClient wraps an HTTPClient in a Client struct that delegates to it.
// This allows HTTPClient to be used wherever *Client is expected.
func wrapHTTPClient(httpClient *HTTPClient) *Client {
	return &Client{
		httpClient: httpClient,
		isRemote:   true,
	}
}

// Close closes the connection to the daemon
func (c *Client) Close() error {
	if c.httpClient != nil {
		return c.httpClient.Close()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// SetTimeout sets the request timeout duration
func (c *Client) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
	if c.httpClient != nil {
		c.httpClient.SetTimeout(timeout)
	}
}

// SetDatabasePath sets the expected database path for validation
func (c *Client) SetDatabasePath(dbPath string) {
	c.dbPath = dbPath
	if c.httpClient != nil {
		c.httpClient.SetDatabasePath(dbPath)
	}
}

// SetActor sets the actor for audit trail (who is performing operations)
func (c *Client) SetActor(actor string) {
	c.actor = actor
	if c.httpClient != nil {
		c.httpClient.SetActor(actor)
	}
}

// SetToken sets the authentication token for TCP connections
func (c *Client) SetToken(token string) {
	c.token = token
	if c.httpClient != nil {
		c.httpClient.SetToken(token)
	}
}

// SetRequestTimeout sets a per-request timeout in milliseconds.
// This is sent to the server in the Request and also extends the client socket deadline.
// Use 0 to revert to the server/client default.
func (c *Client) SetRequestTimeout(ms int) {
	c.requestTimeoutMs = ms
}

// IsRemote returns true if this client is connected to a remote daemon via TCP
func (c *Client) IsRemote() bool {
	return c.isRemote
}

// Execute sends an RPC request and waits for a response
func (c *Client) Execute(operation string, args interface{}) (*Response, error) {
	if c.httpClient != nil {
		return c.httpClient.Execute(operation, args)
	}
	return c.ExecuteWithCwd(operation, args, "")
}

// ExecuteWithCwd sends an RPC request with an explicit cwd (or current dir if empty string)
func (c *Client) ExecuteWithCwd(operation string, args interface{}, cwd string) (*Response, error) {
	if c.httpClient != nil {
		return c.httpClient.ExecuteWithCwd(operation, args, cwd)
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal args: %w", err)
	}

	// Use provided cwd, or get current working directory for database routing
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	req := Request{
		Operation:     operation,
		Args:          argsJSON,
		Actor:         c.actor, // Who is performing this operation
		ClientVersion: ClientVersion,
		Cwd:           cwd,
		ExpectedDB:    c.dbPath, // Send expected database path for validation
		Token:         c.token,  // Authentication token for TCP connections
		TimeoutMs:     c.requestTimeoutMs,
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Use the longer of c.timeout and requestTimeoutMs for the socket deadline.
	socketDeadline := c.timeout
	if c.requestTimeoutMs > 0 {
		requested := time.Duration(c.requestTimeoutMs) * time.Millisecond
		if requested > socketDeadline {
			socketDeadline = requested
		}
	}
	if socketDeadline > 0 {
		deadline := time.Now().Add(socketDeadline)
		if err := c.conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("failed to set deadline: %w", err)
		}
	}

	writer := bufio.NewWriter(c.conn)
	if _, err := writer.Write(reqJSON); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}
	if err := writer.WriteByte('\n'); err != nil {
		return nil, fmt.Errorf("failed to write newline: %w", err)
	}
	if err := writer.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush: %w", err)
	}

	reader := bufio.NewReader(c.conn)
	respLine, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(respLine, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !resp.Success {
		return &resp, fmt.Errorf("operation failed: %s", resp.Error)
	}

	return &resp, nil
}

// Ping sends a ping request to verify the daemon is alive
func (c *Client) Ping() error {
	resp, err := c.Execute(OpPing, nil)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("ping failed: %s", resp.Error)
	}

	return nil
}

// Status retrieves daemon status metadata
func (c *Client) Status() (*StatusResponse, error) {
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

// Health sends a health check request to verify the daemon is healthy
func (c *Client) Health() (*HealthResponse, error) {
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

// Shutdown sends a graceful shutdown request to the daemon
func (c *Client) Shutdown() error {
	_, err := c.Execute(OpShutdown, nil)
	return err
}

// Metrics retrieves daemon metrics
func (c *Client) Metrics() (*MetricsSnapshot, error) {
	resp, err := c.Execute(OpMetrics, nil)
	if err != nil {
		return nil, err
	}

	var metrics MetricsSnapshot
	if err := json.Unmarshal(resp.Data, &metrics); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics response: %w", err)
	}

	return &metrics, nil
}

// Create creates a new issue via the daemon
func (c *Client) Create(args *CreateArgs) (*Response, error) {
	return c.Execute(OpCreate, args)
}

// Update updates an issue via the daemon
func (c *Client) Update(args *UpdateArgs) (*Response, error) {
	return c.Execute(OpUpdate, args)
}

// UpdateWithComment updates an issue and optionally adds a comment atomically via the daemon.
// This performs both operations in a single transaction, ensuring consistency.
func (c *Client) UpdateWithComment(args *UpdateWithCommentArgs) (*Response, error) {
	return c.Execute(OpUpdateWithComment, args)
}

// CloseIssue marks an issue as closed via the daemon.
func (c *Client) CloseIssue(args *CloseArgs) (*Response, error) {
	return c.Execute(OpClose, args)
}

// Delete deletes one or more issues via the daemon.
func (c *Client) Delete(args *DeleteArgs) (*Response, error) {
	return c.Execute(OpDelete, args)
}

// Rename renames an issue ID via the daemon.
func (c *Client) Rename(args *RenameArgs) (*RenameResult, error) {
	resp, err := c.Execute(OpRename, args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var result RenameResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse rename result: %w", err)
	}
	return &result, nil
}

// RenamePrefix renames the issue prefix for all issues via the daemon.
func (c *Client) RenamePrefix(args *RenamePrefixArgs) (*RenamePrefixResult, error) {
	resp, err := c.Execute(OpRenamePrefix, args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var result RenamePrefixResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse rename-prefix result: %w", err)
	}
	return &result, nil
}

// Move moves an issue to a different rig via the daemon.
func (c *Client) Move(args *MoveArgs) (*MoveResult, error) {
	resp, err := c.Execute(OpMove, args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var result MoveResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse move result: %w", err)
	}
	return &result, nil
}

// Refile refiles an issue to a different rig via the daemon.
func (c *Client) Refile(args *RefileArgs) (*RefileResult, error) {
	resp, err := c.Execute(OpRefile, args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var result RefileResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse refile result: %w", err)
	}
	return &result, nil
}

// Cook creates a protomolecule from a formula via the daemon.
func (c *Client) Cook(args *CookArgs) (*CookResult, error) {
	resp, err := c.Execute(OpCook, args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var result CookResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse cook result: %w", err)
	}
	return &result, nil
}

// Pour instantiates a proto as a persistent mol via the daemon.
func (c *Client) Pour(args *PourArgs) (*PourResult, error) {
	resp, err := c.Execute(OpPour, args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var result PourResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse pour result: %w", err)
	}
	return &result, nil
}

// List lists issues via the daemon
func (c *Client) List(args *ListArgs) (*Response, error) {
	return c.Execute(OpList, args)
}

// ListWatch is a long-polling endpoint for watch mode (bd-la75).
// It blocks until mutations occur after the given Since timestamp, then returns the updated issue list.
func (c *Client) ListWatch(args *ListWatchArgs) (*ListWatchResult, error) {
	resp, err := c.Execute(OpListWatch, args)
	if err != nil {
		return nil, err
	}

	var result ListWatchResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal list watch response: %w", err)
	}

	return &result, nil
}

// Count counts issues via the daemon
func (c *Client) Count(args *CountArgs) (*Response, error) {
	return c.Execute(OpCount, args)
}

// Show shows an issue via the daemon
func (c *Client) Show(args *ShowArgs) (*Response, error) {
	return c.Execute(OpShow, args)
}

// ResolveID resolves a partial issue ID to a full ID via the daemon
func (c *Client) ResolveID(args *ResolveIDArgs) (*Response, error) {
	return c.Execute(OpResolveID, args)
}

// Ready gets ready work via the daemon
func (c *Client) Ready(args *ReadyArgs) (*Response, error) {
	return c.Execute(OpReady, args)
}

// Blocked gets blocked issues via the daemon
func (c *Client) Blocked(args *BlockedArgs) (*Response, error) {
	return c.Execute(OpBlocked, args)
}

// Stale gets stale issues via the daemon
func (c *Client) Stale(args *StaleArgs) (*Response, error) {
	return c.Execute(OpStale, args)
}

// Stats gets statistics via the daemon
func (c *Client) Stats() (*Response, error) {
	return c.Execute(OpStats, nil)
}

// GetMutations retrieves recent mutations from the daemon
func (c *Client) GetMutations(args *GetMutationsArgs) (*Response, error) {
	return c.Execute(OpGetMutations, args)
}

// AddDependency adds a dependency via the daemon
func (c *Client) AddDependency(args *DepAddArgs) (*Response, error) {
	return c.Execute(OpDepAdd, args)
}

// RemoveDependency removes a dependency via the daemon
func (c *Client) RemoveDependency(args *DepRemoveArgs) (*Response, error) {
	return c.Execute(OpDepRemove, args)
}

// AddBidirectionalRelation adds a bidirectional relation atomically via the daemon.
// Both directions (id1->id2 and id2->id1) are added in a single transaction.
func (c *Client) AddBidirectionalRelation(args *DepAddBidirectionalArgs) (*Response, error) {
	return c.Execute(OpDepAddBidirectional, args)
}

// RemoveBidirectionalRelation removes a bidirectional relation atomically via the daemon.
// Both directions (id1->id2 and id2->id1) are removed in a single transaction.
func (c *Client) RemoveBidirectionalRelation(args *DepRemoveBidirectionalArgs) (*Response, error) {
	return c.Execute(OpDepRemoveBidirectional, args)
}

// AddLabel adds a label via the daemon
func (c *Client) AddLabel(args *LabelAddArgs) (*Response, error) {
	return c.Execute(OpLabelAdd, args)
}

// RemoveLabel removes a label via the daemon
func (c *Client) RemoveLabel(args *LabelRemoveArgs) (*Response, error) {
	return c.Execute(OpLabelRemove, args)
}

// BatchAddLabels adds multiple labels to an issue atomically in a single transaction.
// Returns the number of labels actually added (excludes duplicates).
func (c *Client) BatchAddLabels(args *BatchAddLabelsArgs) (*BatchAddLabelsResult, error) {
	resp, err := c.Execute(OpBatchAddLabels, args)
	if err != nil {
		return nil, err
	}

	var result BatchAddLabelsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal batch_add_labels response: %w", err)
	}

	return &result, nil
}

// ListComments retrieves comments for an issue via the daemon
func (c *Client) ListComments(args *CommentListArgs) (*Response, error) {
	return c.Execute(OpCommentList, args)
}

// AddComment adds a comment to an issue via the daemon
func (c *Client) AddComment(args *CommentAddArgs) (*Response, error) {
	return c.Execute(OpCommentAdd, args)
}

// Batch executes multiple operations atomically
func (c *Client) Batch(args *BatchArgs) (*Response, error) {
	return c.Execute(OpBatch, args)
}



// Export exports the database to JSONL format
func (c *Client) Export(args *ExportArgs) (*Response, error) {
	return c.Execute(OpExport, args)
}

// EpicStatus gets epic completion status via the daemon
func (c *Client) EpicStatus(args *EpicStatusArgs) (*Response, error) {
	return c.Execute(OpEpicStatus, args)
}

// Gate operations

// GateCreate creates a gate via the daemon
func (c *Client) GateCreate(args *GateCreateArgs) (*Response, error) {
	return c.Execute(OpGateCreate, args)
}

// GateList lists gates via the daemon
func (c *Client) GateList(args *GateListArgs) (*Response, error) {
	return c.Execute(OpGateList, args)
}

// GateShow shows a gate via the daemon
func (c *Client) GateShow(args *GateShowArgs) (*Response, error) {
	return c.Execute(OpGateShow, args)
}

// GateClose closes a gate via the daemon
func (c *Client) GateClose(args *GateCloseArgs) (*Response, error) {
	return c.Execute(OpGateClose, args)
}

// GateWait adds waiters to a gate via the daemon
func (c *Client) GateWait(args *GateWaitArgs) (*Response, error) {
	return c.Execute(OpGateWait, args)
}

// Decision point operations

// DecisionCreate creates a decision point via the daemon
func (c *Client) DecisionCreate(args *DecisionCreateArgs) (*DecisionResponse, error) {
	resp, err := c.Execute(OpDecisionCreate, args)
	if err != nil {
		return nil, err
	}

	var result DecisionResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal decision create response: %w", err)
	}

	return &result, nil
}

// DecisionGet retrieves a decision point via the daemon
func (c *Client) DecisionGet(args *DecisionGetArgs) (*DecisionResponse, error) {
	resp, err := c.Execute(OpDecisionGet, args)
	if err != nil {
		return nil, err
	}

	var result DecisionResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal decision get response: %w", err)
	}

	return &result, nil
}

// DecisionResolve resolves a decision point via the daemon
func (c *Client) DecisionResolve(args *DecisionResolveArgs) (*DecisionResponse, error) {
	resp, err := c.Execute(OpDecisionResolve, args)
	if err != nil {
		return nil, err
	}

	var result DecisionResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal decision resolve response: %w", err)
	}

	return &result, nil
}

// DecisionList lists decision points via the daemon
func (c *Client) DecisionList(args *DecisionListArgs) (*DecisionListResponse, error) {
	resp, err := c.Execute(OpDecisionList, args)
	if err != nil {
		return nil, err
	}

	var result DecisionListResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal decision list response: %w", err)
	}

	return &result, nil
}

// DecisionRemind sends a reminder for a pending decision via the daemon
func (c *Client) DecisionRemind(args *DecisionRemindArgs) (*DecisionRemindResult, error) {
	resp, err := c.Execute(OpDecisionRemind, args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var result DecisionRemindResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse decision remind result: %w", err)
	}
	return &result, nil
}

// DecisionCancel cancels a pending decision via the daemon
func (c *Client) DecisionCancel(args *DecisionCancelArgs) (*DecisionCancelResult, error) {
	resp, err := c.Execute(OpDecisionCancel, args)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	var result DecisionCancelResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse decision cancel result: %w", err)
	}
	return &result, nil
}

// GetWorkerStatus retrieves worker status via the daemon
func (c *Client) GetWorkerStatus(args *GetWorkerStatusArgs) (*GetWorkerStatusResponse, error) {
	resp, err := c.Execute(OpGetWorkerStatus, args)
	if err != nil {
		return nil, err
	}

	var result GetWorkerStatusResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal worker status response: %w", err)
	}

	return &result, nil
}

// GetConfig retrieves a config value from the daemon's database
func (c *Client) GetConfig(args *GetConfigArgs) (*GetConfigResponse, error) {
	resp, err := c.Execute(OpGetConfig, args)
	if err != nil {
		return nil, err
	}

	var result GetConfigResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config response: %w", err)
	}

	return &result, nil
}

// Config operations (bd-wmil)

// ConfigSet sets a config value via the daemon
func (c *Client) ConfigSet(args *ConfigSetArgs) (*ConfigSetResponse, error) {
	resp, err := c.Execute(OpConfigSet, args)
	if err != nil {
		return nil, err
	}

	var result ConfigSetResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config set response: %w", err)
	}

	return &result, nil
}

// ConfigList lists all config values via the daemon
func (c *Client) ConfigList() (*ConfigListResponse, error) {
	resp, err := c.Execute(OpConfigList, nil)
	if err != nil {
		return nil, err
	}

	var result ConfigListResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config list response: %w", err)
	}

	return &result, nil
}

// ConfigUnset deletes a config value via the daemon
func (c *Client) ConfigUnset(args *ConfigUnsetArgs) (*ConfigUnsetResponse, error) {
	resp, err := c.Execute(OpConfigUnset, args)
	if err != nil {
		return nil, err
	}

	var result ConfigUnsetResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config unset response: %w", err)
	}

	return &result, nil
}

// Mol operations (gt-as9kdm)

// MolBond executes a mol bond operation via the daemon
func (c *Client) MolBond(args *MolBondArgs) (*MolBondResult, error) {
	resp, err := c.Execute(OpMolBond, args)
	if err != nil {
		return nil, err
	}

	var result MolBondResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mol bond response: %w", err)
	}

	return &result, nil
}

// MolSquash executes a mol squash operation via the daemon
func (c *Client) MolSquash(args *MolSquashArgs) (*MolSquashResult, error) {
	resp, err := c.Execute(OpMolSquash, args)
	if err != nil {
		return nil, err
	}

	var result MolSquashResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mol squash response: %w", err)
	}

	return &result, nil
}

// MolBurn executes a mol burn operation via the daemon
func (c *Client) MolBurn(args *MolBurnArgs) (*MolBurnResult, error) {
	resp, err := c.Execute(OpMolBurn, args)
	if err != nil {
		return nil, err
	}

	var result MolBurnResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mol burn response: %w", err)
	}

	return &result, nil
}

// MolCurrent retrieves current molecule progress via the daemon
func (c *Client) MolCurrent(args *MolCurrentArgs) (*MolCurrentResult, error) {
	resp, err := c.Execute(OpMolCurrent, args)
	if err != nil {
		return nil, err
	}

	var result MolCurrentResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mol current response: %w", err)
	}

	return &result, nil
}

// MolProgressStats gets efficient progress stats for a molecule via the daemon
func (c *Client) MolProgressStats(moleculeID string) (*MolProgressStatsResult, error) {
	args := &MolProgressStatsArgs{MoleculeID: moleculeID}
	resp, err := c.Execute(OpMolProgressStats, args)
	if err != nil {
		return nil, err
	}

	var result MolProgressStatsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mol progress stats response: %w", err)
	}

	return &result, nil
}

// MolReadyGated retrieves molecules ready for gate-resume dispatch via the daemon (bd-2n56)
func (c *Client) MolReadyGated(args *MolReadyGatedArgs) (*MolReadyGatedResult, error) {
	resp, err := c.Execute(OpMolReadyGated, args)
	if err != nil {
		return nil, err
	}

	var result MolReadyGatedResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mol ready gated response: %w", err)
	}

	return &result, nil
}

// Types retrieves available issue types via the daemon (bd-s091)
func (c *Client) Types(args *TypesArgs) (*TypesResult, error) {
	resp, err := c.Execute(OpTypes, args)
	if err != nil {
		return nil, err
	}

	var result TypesResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal types response: %w", err)
	}

	return &result, nil
}

// CloseContinue executes close --continue via the daemon (bd-ympw)
// This walks the parent-child chain to advance to the next step in a molecule
func (c *Client) CloseContinue(args *CloseContinueArgs) (*CloseContinueResult, error) {
	resp, err := c.Execute(OpCloseContinue, args)
	if err != nil {
		return nil, err
	}

	var result CloseContinueResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal close continue response: %w", err)
	}

	return &result, nil
}

// Sync operations (bd-wn2g)

// SyncExport exports the database to JSONL via the daemon
func (c *Client) SyncExport(args *SyncExportArgs) (*SyncExportResult, error) {
	resp, err := c.Execute(OpSyncExport, args)
	if err != nil {
		return nil, err
	}

	var result SyncExportResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sync export response: %w", err)
	}

	return &result, nil
}

// SyncStatus retrieves sync status via the daemon
func (c *Client) SyncStatus(args *SyncStatusArgs) (*SyncStatusResult, error) {
	resp, err := c.Execute(OpSyncStatus, args)
	if err != nil {
		return nil, err
	}

	var result SyncStatusResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sync status response: %w", err)
	}

	return &result, nil
}

// SetState atomically sets a state dimension on an issue via the daemon.
// This creates an event bead and updates labels in a single transaction.
func (c *Client) SetState(args *SetStateArgs) (*SetStateResult, error) {
	resp, err := c.Execute(OpSetState, args)
	if err != nil {
		return nil, err
	}

	var result SetStateResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal set_state response: %w", err)
	}

	return &result, nil
}

// CreateWithDependencies creates multiple issues with their labels and dependencies
// in a single atomic transaction via the daemon.
func (c *Client) CreateWithDependencies(args *CreateWithDepsArgs) (*CreateWithDepsResult, error) {
	resp, err := c.Execute(OpCreateWithDeps, args)
	if err != nil {
		return nil, err
	}

	var result CreateWithDepsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal create_with_deps response: %w", err)
	}

	return &result, nil
}

// BatchAddDependencies adds multiple dependencies atomically in a single transaction via the daemon.
// This is more efficient than making multiple AddDependency calls and ensures atomicity.
func (c *Client) BatchAddDependencies(args *BatchAddDependenciesArgs) (*BatchAddDependenciesResult, error) {
	resp, err := c.Execute(OpBatchAddDependencies, args)
	if err != nil {
		return nil, err
	}

	var result BatchAddDependenciesResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal batch_add_dependencies response: %w", err)
	}

	return &result, nil
}

// BatchQueryWorkers queries worker assignments for multiple issues at once via the daemon.
// This is more efficient than making multiple GetIssue calls when querying worker assignments.
func (c *Client) BatchQueryWorkers(args *BatchQueryWorkersArgs) (*BatchQueryWorkersResult, error) {
	resp, err := c.Execute(OpBatchQueryWorkers, args)
	if err != nil {
		return nil, err
	}

	var result BatchQueryWorkersResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal batch_query_workers response: %w", err)
	}

	return &result, nil
}

// CreateConvoyWithTracking creates a convoy issue and tracking dependencies atomically via the daemon.
// This ensures the convoy and all its tracking relations are created in a single transaction.
func (c *Client) CreateConvoyWithTracking(args *CreateConvoyWithTrackingArgs) (*CreateConvoyWithTrackingResult, error) {
	resp, err := c.Execute(OpCreateConvoyWithTracking, args)
	if err != nil {
		return nil, err
	}

	var result CreateConvoyWithTrackingResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal create_convoy_with_tracking response: %w", err)
	}

	return &result, nil
}

// AtomicClosureChain closes multiple related issues and updates an agent atomically via the daemon.
// This is used for MR completion where we need to close the MR, close its source issue,
// and optionally update the agent bead (e.g., clear hook_bead) in a single transaction.
func (c *Client) AtomicClosureChain(args *AtomicClosureChainArgs) (*AtomicClosureChainResult, error) {
	resp, err := c.Execute(OpAtomicClosureChain, args)
	if err != nil {
		return nil, err
	}

	var result AtomicClosureChainResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal atomic_closure_chain response: %w", err)
	}

	return &result, nil
}

// Init initializes a beads database remotely via the daemon.
// This creates a new database, sets the issue prefix, and optionally imports from JSONL.
func (c *Client) Init(args *InitArgs) (*InitResult, error) {
	resp, err := c.Execute(OpInit, args)
	if err != nil {
		return nil, err
	}

	var result InitResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal init response: %w", err)
	}

	return &result, nil
}

// Migrate runs database migrations remotely via the daemon.
// This detects schema version, migrates old databases, and updates version metadata.
func (c *Client) Migrate(args *MigrateArgs) (*MigrateResult, error) {
	resp, err := c.Execute(OpMigrate, args)
	if err != nil {
		return nil, err
	}

	var result MigrateResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal migrate response: %w", err)
	}

	return &result, nil
}

// Formula CRUD operations (gt-pozvwr.24.9)

// FormulaList lists available formulas via the daemon.
func (c *Client) FormulaList(args *FormulaListArgs) (*FormulaListResult, error) {
	resp, err := c.Execute(OpFormulaList, args)
	if err != nil {
		return nil, err
	}

	var result FormulaListResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal formula list response: %w", err)
	}

	return &result, nil
}

// FormulaGet retrieves a formula by ID or name via the daemon.
func (c *Client) FormulaGet(args *FormulaGetArgs) (*FormulaGetResult, error) {
	resp, err := c.Execute(OpFormulaGet, args)
	if err != nil {
		return nil, err
	}

	var result FormulaGetResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal formula get response: %w", err)
	}

	return &result, nil
}

// FormulaSave creates or updates a formula via the daemon.
func (c *Client) FormulaSave(args *FormulaSaveArgs) (*FormulaSaveResult, error) {
	resp, err := c.Execute(OpFormulaSave, args)
	if err != nil {
		return nil, err
	}

	var result FormulaSaveResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal formula save response: %w", err)
	}

	return &result, nil
}

// FormulaDelete soft-deletes a formula via the daemon.
func (c *Client) FormulaDelete(args *FormulaDeleteArgs) (*FormulaDeleteResult, error) {
	resp, err := c.Execute(OpFormulaDelete, args)
	if err != nil {
		return nil, err
	}

	var result FormulaDeleteResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal formula delete response: %w", err)
	}

	return &result, nil
}

// Runbook CRUD operations

// RunbookList lists available runbooks via the daemon.
func (c *Client) RunbookList(args *RunbookListArgs) (*RunbookListResult, error) {
	resp, err := c.Execute(OpRunbookList, args)
	if err != nil {
		return nil, err
	}

	var result RunbookListResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal runbook list response: %w", err)
	}

	return &result, nil
}

// RunbookGet retrieves a runbook by ID or name via the daemon.
func (c *Client) RunbookGet(args *RunbookGetArgs) (*RunbookGetResult, error) {
	resp, err := c.Execute(OpRunbookGet, args)
	if err != nil {
		return nil, err
	}

	var result RunbookGetResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal runbook get response: %w", err)
	}

	return &result, nil
}

// RunbookSave creates or updates a runbook via the daemon.
func (c *Client) RunbookSave(args *RunbookSaveArgs) (*RunbookSaveResult, error) {
	resp, err := c.Execute(OpRunbookSave, args)
	if err != nil {
		return nil, err
	}

	var result RunbookSaveResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal runbook save response: %w", err)
	}

	return &result, nil
}

// Agent pod operations (gt-el7sxq.7)

// AgentPodRegister sets pod fields on an agent bead.
func (c *Client) AgentPodRegister(args *AgentPodRegisterArgs) (*AgentPodRegisterResult, error) {
	resp, err := c.Execute(OpAgentPodRegister, args)
	if err != nil {
		return nil, err
	}

	var result AgentPodRegisterResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent_pod_register response: %w", err)
	}

	return &result, nil
}

// AgentPodDeregister clears all pod fields on an agent bead.
func (c *Client) AgentPodDeregister(args *AgentPodDeregisterArgs) (*AgentPodDeregisterResult, error) {
	resp, err := c.Execute(OpAgentPodDeregister, args)
	if err != nil {
		return nil, err
	}

	var result AgentPodDeregisterResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent_pod_deregister response: %w", err)
	}

	return &result, nil
}

// AgentPodStatus updates the pod_status field on an agent bead.
func (c *Client) AgentPodStatus(args *AgentPodStatusArgs) (*AgentPodStatusResult, error) {
	resp, err := c.Execute(OpAgentPodStatus, args)
	if err != nil {
		return nil, err
	}

	var result AgentPodStatusResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent_pod_status response: %w", err)
	}

	return &result, nil
}

// AgentPodList returns agents with active pods.
func (c *Client) AgentPodList(args *AgentPodListArgs) (*AgentPodListResult, error) {
	resp, err := c.Execute(OpAgentPodList, args)
	if err != nil {
		return nil, err
	}

	var result AgentPodListResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent_pod_list response: %w", err)
	}

	return &result, nil
}

// VCS operations (bd-ma0s.2)

// VcsCommit creates a Dolt commit with the given message.
func (c *Client) VcsCommit(args *VcsCommitArgs) (*VcsCommitResult, error) {
	resp, err := c.Execute(OpVcsCommit, args)
	if err != nil {
		return nil, err
	}
	var result VcsCommitResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_commit response: %w", err)
	}
	return &result, nil
}

// VcsPush pushes commits to the configured remote.
func (c *Client) VcsPush() (*VcsPushResult, error) {
	resp, err := c.Execute(OpVcsPush, struct{}{})
	if err != nil {
		return nil, err
	}
	var result VcsPushResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_push response: %w", err)
	}
	return &result, nil
}

// VcsPull pulls changes from the configured remote.
func (c *Client) VcsPull() (*VcsPullResult, error) {
	resp, err := c.Execute(OpVcsPull, struct{}{})
	if err != nil {
		return nil, err
	}
	var result VcsPullResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_pull response: %w", err)
	}
	return &result, nil
}

// VcsMerge merges a branch into the current branch.
func (c *Client) VcsMerge(args *VcsMergeArgs) (*VcsMergeResult, error) {
	resp, err := c.Execute(OpVcsMerge, args)
	if err != nil {
		return nil, err
	}
	var result VcsMergeResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_merge response: %w", err)
	}
	return &result, nil
}

// VcsBranchCreate creates a new branch.
func (c *Client) VcsBranchCreate(args *VcsBranchCreateArgs) (*VcsBranchCreateResult, error) {
	resp, err := c.Execute(OpVcsBranchCreate, args)
	if err != nil {
		return nil, err
	}
	var result VcsBranchCreateResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_branch_create response: %w", err)
	}
	return &result, nil
}

// VcsBranchDelete deletes a branch.
func (c *Client) VcsBranchDelete(args *VcsBranchDeleteArgs) (*VcsBranchDeleteResult, error) {
	resp, err := c.Execute(OpVcsBranchDelete, args)
	if err != nil {
		return nil, err
	}
	var result VcsBranchDeleteResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_branch_delete response: %w", err)
	}
	return &result, nil
}

// VcsCheckout switches to the specified branch.
func (c *Client) VcsCheckout(args *VcsCheckoutArgs) (*VcsCheckoutResult, error) {
	resp, err := c.Execute(OpVcsCheckout, args)
	if err != nil {
		return nil, err
	}
	var result VcsCheckoutResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_checkout response: %w", err)
	}
	return &result, nil
}

// VcsActiveBranch returns the current branch name.
func (c *Client) VcsActiveBranch() (*VcsActiveBranchResult, error) {
	resp, err := c.Execute(OpVcsActiveBranch, struct{}{})
	if err != nil {
		return nil, err
	}
	var result VcsActiveBranchResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_active_branch response: %w", err)
	}
	return &result, nil
}

// VcsStatus returns the current Dolt status (staged/unstaged changes).
func (c *Client) VcsStatus() (*VcsStatusResult, error) {
	resp, err := c.Execute(OpVcsStatus, struct{}{})
	if err != nil {
		return nil, err
	}
	var result VcsStatusResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_status response: %w", err)
	}
	return &result, nil
}

// VcsHasUncommitted checks if there are any uncommitted changes.
func (c *Client) VcsHasUncommitted() (*VcsHasUncommittedResult, error) {
	resp, err := c.Execute(OpVcsHasUncommitted, struct{}{})
	if err != nil {
		return nil, err
	}
	var result VcsHasUncommittedResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_has_uncommitted response: %w", err)
	}
	return &result, nil
}

// VcsBranches returns the names of all branches.
func (c *Client) VcsBranches() (*VcsBranchesResult, error) {
	resp, err := c.Execute(OpVcsBranches, struct{}{})
	if err != nil {
		return nil, err
	}
	var result VcsBranchesResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_branches response: %w", err)
	}
	return &result, nil
}

// VcsCurrentCommit returns the hash of the current HEAD commit.
func (c *Client) VcsCurrentCommit() (*VcsCurrentCommitResult, error) {
	resp, err := c.Execute(OpVcsCurrentCommit, struct{}{})
	if err != nil {
		return nil, err
	}
	var result VcsCurrentCommitResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_current_commit response: %w", err)
	}
	return &result, nil
}

// VcsCommitExists checks if a commit hash exists in the repository.
func (c *Client) VcsCommitExists(args *VcsCommitExistsArgs) (*VcsCommitExistsResult, error) {
	resp, err := c.Execute(OpVcsCommitExists, args)
	if err != nil {
		return nil, err
	}
	var result VcsCommitExistsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_commit_exists response: %w", err)
	}
	return &result, nil
}

// VcsLog returns recent commit history.
func (c *Client) VcsLog(args *VcsLogArgs) (*VcsLogResult, error) {
	resp, err := c.Execute(OpVcsLog, args)
	if err != nil {
		return nil, err
	}
	var result VcsLogResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vcs_log response: %w", err)
	}
	return &result, nil
}

// AdminGC runs dolt garbage collection on the server-side Dolt repository.
func (c *Client) AdminGC(args *AdminGCArgs) (*AdminGCResult, error) {
	resp, err := c.Execute(OpAdminGC, args)
	if err != nil {
		return nil, err
	}

	var result AdminGCResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal admin_gc response: %w", err)
	}
	return &result, nil
}

// ===== Federation Client Methods (bd-ma0s.4) =====

// FedListRemotes lists configured federation remotes.
func (c *Client) FedListRemotes(args *FedListRemotesArgs) (*FedListRemotesResult, error) {
	resp, err := c.Execute(OpFedListRemotes, args)
	if err != nil {
		return nil, err
	}

	var result FedListRemotesResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fed_list_remotes response: %w", err)
	}

	return &result, nil
}

// FedSync syncs with a federation peer (fetch + merge + push).
func (c *Client) FedSync(args *FedSyncArgs) (*FedSyncResult, error) {
	resp, err := c.Execute(OpFedSync, args)
	if err != nil {
		// Try to extract partial result from error response
		if resp != nil && len(resp.Data) > 0 {
			var result FedSyncResult
			if unmarshalErr := json.Unmarshal(resp.Data, &result); unmarshalErr == nil {
				return &result, err
			}
		}
		return nil, err
	}

	var result FedSyncResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fed_sync response: %w", err)
	}

	return &result, nil
}

// FedSyncStatus returns the sync status with a federation peer.
func (c *Client) FedSyncStatus(args *FedSyncStatusArgs) (*FedSyncStatusResult, error) {
	resp, err := c.Execute(OpFedSyncStatus, args)
	if err != nil {
		return nil, err
	}

	var result FedSyncStatusResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fed_sync_status response: %w", err)
	}

	return &result, nil
}

// FedFetch fetches refs from a peer without merging.
func (c *Client) FedFetch(args *FedFetchArgs) (*FedFetchResult, error) {
	resp, err := c.Execute(OpFedFetch, args)
	if err != nil {
		return nil, err
	}

	var result FedFetchResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fed_fetch response: %w", err)
	}

	return &result, nil
}

// FedPushTo pushes to a specific peer.
func (c *Client) FedPushTo(args *FedPushToArgs) (*FedPushToResult, error) {
	resp, err := c.Execute(OpFedPushTo, args)
	if err != nil {
		return nil, err
	}

	var result FedPushToResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fed_push_to response: %w", err)
	}

	return &result, nil
}

// FedPullFrom pulls from a specific peer.
func (c *Client) FedPullFrom(args *FedPullFromArgs) (*FedPullFromResult, error) {
	resp, err := c.Execute(OpFedPullFrom, args)
	if err != nil {
		return nil, err
	}

	var result FedPullFromResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fed_pull_from response: %w", err)
	}

	return &result, nil
}

// FedAddRemote adds a federation remote.
func (c *Client) FedAddRemote(args *FedAddRemoteArgs) (*FedAddRemoteResult, error) {
	resp, err := c.Execute(OpFedAddRemote, args)
	if err != nil {
		return nil, err
	}

	var result FedAddRemoteResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fed_add_remote response: %w", err)
	}

	return &result, nil
}

// FedRemoveRemote removes a federation remote.
func (c *Client) FedRemoveRemote(args *FedRemoveRemoteArgs) (*FedRemoveRemoteResult, error) {
	resp, err := c.Execute(OpFedRemoveRemote, args)
	if err != nil {
		return nil, err
	}

	var result FedRemoveRemoteResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fed_remove_remote response: %w", err)
	}

	return &result, nil
}

// FedAddPeer adds an authenticated federation peer with credentials.
func (c *Client) FedAddPeer(args *FedAddPeerArgs) (*FedAddPeerResult, error) {
	resp, err := c.Execute(OpFedAddPeer, args)
	if err != nil {
		return nil, err
	}

	var result FedAddPeerResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fed_add_peer response: %w", err)
	}

	return &result, nil
}

// History query operations (bd-ma0s.3)

// HistoryIssue returns the complete version history for an issue.
func (c *Client) HistoryIssue(args *HistoryIssueArgs) (*HistoryIssueResult, error) {
	resp, err := c.Execute(OpHistoryIssue, args)
	if err != nil {
		return nil, err
	}

	var result HistoryIssueResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal history_issue response: %w", err)
	}

	return &result, nil
}

// HistoryDiff returns low-level table-level diffs between two commits.
func (c *Client) HistoryDiff(args *HistoryDiffArgs) (*HistoryDiffResult, error) {
	resp, err := c.Execute(OpHistoryDiff, args)
	if err != nil {
		return nil, err
	}

	var result HistoryDiffResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal history_diff response: %w", err)
	}

	return &result, nil
}

// HistoryIssueDiff returns detailed changes to a specific issue between two commits.
func (c *Client) HistoryIssueDiff(args *HistoryIssueDiffArgs) (*HistoryIssueDiffResult, error) {
	resp, err := c.Execute(OpHistoryIssueDiff, args)
	if err != nil {
		return nil, err
	}

	var result HistoryIssueDiffResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal history_issue_diff response: %w", err)
	}

	return &result, nil
}

// HistoryConflicts returns merge conflicts in the current state.
func (c *Client) HistoryConflicts(args *HistoryConflictsArgs) (*HistoryConflictsResult, error) {
	resp, err := c.Execute(OpHistoryConflicts, args)
	if err != nil {
		return nil, err
	}

	var result HistoryConflictsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal history_conflicts response: %w", err)
	}

	return &result, nil
}

// HistoryResolveConflicts resolves merge conflicts using the specified strategy.
func (c *Client) HistoryResolveConflicts(args *HistoryResolveConflictsArgs) (*HistoryResolveConflictsResult, error) {
	resp, err := c.Execute(OpHistoryResolveConflicts, args)
	if err != nil {
		return nil, err
	}

	var result HistoryResolveConflictsResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal history_resolve_conflicts response: %w", err)
	}

	return &result, nil
}

// VersionedDiff returns issue-level diffs with full Issue data between two commits.
func (c *Client) VersionedDiff(args *VersionedDiffArgs) (*VersionedDiffResult, error) {
	resp, err := c.Execute(OpVersionedDiff, args)
	if err != nil {
		return nil, err
	}

	var result VersionedDiffResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal versioned_diff response: %w", err)
	}

	return &result, nil
}

// cleanupStaleDaemonArtifacts removes stale daemon.pid file when socket is missing and lock is free.
// This prevents stale artifacts from accumulating after daemon crashes.
// Only removes pid file - lock file is managed by OS (released on process exit).
func cleanupStaleDaemonArtifacts(beadsDir string) {
	pidFile := filepath.Join(beadsDir, "daemon.pid")
	
	// Check if pid file exists
	if _, err := os.Stat(pidFile); err != nil {
		// No pid file to clean up
		return
	}
	
	// Remove stale pid file
	if err := os.Remove(pidFile); err != nil {
		debug.Logf("failed to remove stale pid file: %v", err)
		return
	}
	
	debug.Logf("removed stale daemon.pid file (lock free, socket missing)")
}
