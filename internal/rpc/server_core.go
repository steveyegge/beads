package rpc

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/steveyegge/beads/internal/eventbus"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// WispStore is the interface for in-memory wisp storage.
// This interface is defined here to avoid circular imports with the daemon package.
// The daemon.memoryWispStore implementation satisfies this interface.
type WispStore interface {
	// Create adds a new wisp to the store.
	Create(ctx context.Context, issue *types.Issue) error

	// Get retrieves a wisp by ID.
	Get(ctx context.Context, id string) (*types.Issue, error)

	// List returns wisps matching the filter.
	List(ctx context.Context, filter types.IssueFilter) ([]*types.Issue, error)

	// Update modifies an existing wisp.
	Update(ctx context.Context, issue *types.Issue) error

	// Delete removes a wisp by ID.
	Delete(ctx context.Context, id string) error

	// Count returns the number of wisps in the store.
	Count() int

	// Close releases any resources held by the store.
	Close() error
}

// ServerVersion is the version of this RPC server
// This should match the bd CLI version for proper compatibility checks
// It's set dynamically by daemon.go from cmd/bd/version.go before starting the server
var ServerVersion = "0.0.0" // Placeholder; overridden by daemon startup

const (
	statusUnhealthy = "unhealthy"
)

// Server represents the RPC server that runs in the daemon
type Server struct {
	socketPath    string
	workspacePath string          // Absolute path to workspace root
	dbPath        string          // Absolute path to database file
	storage       storage.Storage // Default storage (for backward compat)
	wispStore     WispStore       // In-memory store for ephemeral wisps
	listener      net.Listener
	tcpAddr       string           // TCP address to listen on (e.g., ":9876")
	tcpListener   net.Listener     // TCP listener (in addition to Unix socket)
	tlsConfig     *tls.Config      // TLS config for TCP connections (nil = no TLS)
	tcpToken      string           // Token for TCP authentication (empty = no auth required)
	httpAddr      string           // HTTP address to listen on (e.g., ":9080")
	httpServer    *HTTPServer      // HTTP server (wraps RPC in HTTP POST endpoints)
	mu            sync.RWMutex
	shutdown      bool
	shutdownChan  chan struct{}
	stopOnce      sync.Once
	doneChan      chan struct{} // closed when Start() cleanup is complete
	// Health and metrics
	startTime        time.Time
	lastActivityTime atomic.Value // time.Time - last request timestamp
	metrics          *Metrics
	// Connection limiting
	maxConns      int
	activeConns   int32 // atomic counter
	connSemaphore chan struct{}
	// Request timeout
	requestTimeout time.Duration
	// Ready channel signals when server is listening
	readyChan chan struct{}
	// Auto-import single-flight guard
	importInProgress atomic.Bool
	// Auto-export single-flight guard (prevents concurrent exports piling up stuck queries)
	exportInProgress atomic.Bool
	// Mutation events for event-driven daemon
	mutationChan    chan MutationEvent
	droppedEvents   atomic.Int64 // Counter for dropped mutation events
	// Recent mutations buffer for polling (circular buffer, configurable size)
	recentMutations   []MutationEvent
	recentMutationsMu sync.RWMutex
	maxMutationBuffer int
	// SSE fan-out subscribers
	subscribersMu sync.RWMutex
	subscribers   []*sseSubscriber
	nextSubID     uint64
	// Query result cache for read operations
	queryCache *QueryCache
	// Daemon configuration (set via SetConfig after creation)
	autoCommit   bool
	autoPush     bool
	autoPull     bool
	localMode    bool
	syncInterval string
	daemonMode   string
	// Query deduplication for coalescing identical in-flight queries
	queryDedup *QueryDeduplicator
	// In-memory label cache (eliminates expensive batch label queries to Dolt)
	labelCache *LabelCache
	// Event bus for hook dispatch (bd-66fp)
	bus *eventbus.Bus
	// NATS health provider (returns status for bus_status RPC)
	natsHealthFn func() NATSHealthInfo
}

// Mutation event types
const (
	MutationCreate  = "create"
	MutationUpdate  = "update"
	MutationDelete  = "delete"
	MutationComment = "comment"
	// Molecule-specific event types for activity feed
	MutationBonded   = "bonded"   // Molecule bonded to parent (dynamic bond)
	MutationSquashed = "squashed" // Wisp squashed to digest
	MutationBurned   = "burned"   // Wisp discarded without digest
	MutationStatus   = "status"   // Status change (in_progress, completed, failed)
)

// MutationEvent represents a database mutation for event-driven sync
type MutationEvent struct {
	Type      string    // One of the Mutation* constants
	IssueID   string    // e.g., "bd-42"
	Title     string    // Issue title for display context (may be empty for some operations)
	Assignee  string    // Issue assignee for display context (may be empty)
	Actor     string    // Who performed the action (may differ from assignee)
	Timestamp time.Time
	// Optional metadata for richer events (used by status, bonded, etc.)
	OldStatus string `json:"old_status,omitempty"` // Previous status (for status events)
	NewStatus string `json:"new_status,omitempty"` // New status (for status events)
	ParentID  string `json:"parent_id,omitempty"`  // Parent molecule (for bonded events)
	StepCount int    `json:"step_count,omitempty"` // Number of steps (for bonded events)
	// Enrichment fields for event matching (bd-pg90)
	IssueType string   `json:"issue_type,omitempty"` // task, bug, feature, gate, etc.
	Labels    []string `json:"labels,omitempty"`      // Issue labels at time of mutation
	AwaitType string   `json:"await_type,omitempty"`  // decision, timer, human, etc. (gates only)
}

// isWisp checks if an issue should be stored in the in-memory WispStore.
// Returns true if the issue is ephemeral (Ephemeral=true) or has -wisp- in its ID.
func isWisp(issue *types.Issue) bool {
	if issue == nil {
		return false
	}
	if issue.Ephemeral {
		return true
	}
	// Check for -wisp- pattern in ID (legacy wisp ID format)
	if strings.Contains(issue.ID, "-wisp-") {
		return true
	}
	return false
}

// isWispID checks if an issue ID indicates it's a wisp.
func isWispID(id string) bool {
	return strings.Contains(id, "-wisp-")
}

// NewServer creates a new RPC server
func NewServer(socketPath string, store storage.Storage, workspacePath string, dbPath string) *Server {
	return NewServerWithWispStore(socketPath, store, nil, workspacePath, dbPath)
}

// NewServerWithWispStore creates a new RPC server with an optional WispStore for ephemeral issues.
func NewServerWithWispStore(socketPath string, store storage.Storage, wispStore WispStore, workspacePath string, dbPath string) *Server {
	// Parse config from env vars
	maxConns := 100 // default
	if env := os.Getenv("BEADS_DAEMON_MAX_CONNS"); env != "" {
		var conns int
		if _, err := fmt.Sscanf(env, "%d", &conns); err == nil && conns > 0 {
			maxConns = conns
		}
	}

	requestTimeout := 60 * time.Second // default (increased from 30s to accommodate slow Dolt operations)
	if env := os.Getenv("BEADS_DAEMON_REQUEST_TIMEOUT"); env != "" {
		if timeout, err := time.ParseDuration(env); err == nil && timeout > 0 {
			requestTimeout = timeout
		}
	}

	mutationBufferSize := 512 // default (increased from 100 for better burst handling)
	if env := os.Getenv("BEADS_MUTATION_BUFFER"); env != "" {
		var bufSize int
		if _, err := fmt.Sscanf(env, "%d", &bufSize); err == nil && bufSize > 0 {
			mutationBufferSize = bufSize
		}
	}

	// Query cache configuration
	cacheTTL := 10 * time.Second // default 10 seconds
	if env := os.Getenv("BEADS_CACHE_TTL"); env != "" {
		if ttl, err := time.ParseDuration(env); err == nil && ttl > 0 {
			cacheTTL = ttl
		}
	}
	cacheMaxSize := 1000 // default max entries
	if env := os.Getenv("BEADS_CACHE_MAX_SIZE"); env != "" {
		var maxSize int
		if _, err := fmt.Sscanf(env, "%d", &maxSize); err == nil && maxSize > 0 {
			cacheMaxSize = maxSize
		}
	}

	// Slow query threshold configuration
	slowQueryThreshold := DefaultSlowQueryThreshold // 100ms default
	if env := os.Getenv("BEADS_SLOW_QUERY_THRESHOLD"); env != "" {
		if threshold, err := time.ParseDuration(env); err == nil && threshold >= 0 {
			slowQueryThreshold = threshold
		}
	}

	metrics := NewMetrics()
	metrics.SetSlowQueryThreshold(slowQueryThreshold)
	s := &Server{
		socketPath:        socketPath,
		workspacePath:     workspacePath,
		dbPath:            dbPath,
		storage:           store,
		wispStore:         wispStore,
		shutdownChan:      make(chan struct{}),
		doneChan:          make(chan struct{}),
		startTime:         time.Now(),
		metrics:           metrics,
		maxConns:          maxConns,
		connSemaphore:     make(chan struct{}, maxConns),
		requestTimeout:    requestTimeout,
		readyChan:         make(chan struct{}),
		mutationChan:      make(chan MutationEvent, mutationBufferSize), // Configurable buffer
		recentMutations:   make([]MutationEvent, 0, 1000),
		maxMutationBuffer: 1000,
		queryCache:        NewQueryCache(cacheTTL, cacheMaxSize),
		queryDedup:        NewQueryDeduplicator(500 * time.Millisecond), // 500ms dedup window
		labelCache:        NewLabelCache(store),
	}
	s.lastActivityTime.Store(time.Now())

	// Set up slow query logging callback
	s.metrics.SetSlowQueryCallback(func(operation string, latency time.Duration, timestamp time.Time) {
		fmt.Fprintf(os.Stderr, "SLOW QUERY: operation=%s latency=%s time=%s\n",
			operation, latency.Round(time.Millisecond), timestamp.Format(time.RFC3339))
	})

	return s
}

// emitMutation sends a mutation event to the daemon's event-driven loop.
// Non-blocking: drops event if channel is full (sync will happen eventually).
// Also stores in recent mutations buffer for polling.
// Title and assignee provide context for activity feeds; pass empty strings if unknown.
func (s *Server) emitMutation(eventType, issueID, title, assignee string) {
	s.emitRichMutation(MutationEvent{
		Type:     eventType,
		IssueID:  issueID,
		Title:    title,
		Assignee: assignee,
	})
}

// enrichEvent populates IssueType, Labels, and AwaitType on a MutationEvent
// from an issue object. Safe to call with nil issue (fields left empty).
func enrichEvent(evt *MutationEvent, issue *types.Issue) {
	if issue == nil {
		return
	}
	evt.IssueType = string(issue.IssueType)
	evt.Labels = issue.Labels
	evt.AwaitType = issue.AwaitType
}

// emitMutationFor is like emitMutation but enriches the event with issue metadata.
func (s *Server) emitMutationFor(eventType string, issue *types.Issue) {
	if issue == nil {
		s.emitMutation(eventType, "", "", "")
		return
	}
	evt := MutationEvent{
		Type:     eventType,
		IssueID:  issue.ID,
		Title:    issue.Title,
		Assignee: issue.Assignee,
	}
	enrichEvent(&evt, issue)
	s.emitRichMutation(evt)
}

// emitRichMutation sends a pre-built mutation event with optional metadata.
// Use this for events that include additional context (status changes, bonded events, etc.)
// Non-blocking: drops event if channel is full (sync will happen eventually).
// Also invalidates the query cache since data has changed.
func (s *Server) emitRichMutation(event MutationEvent) {
	// Always set timestamp if not provided
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Invalidate query cache on any mutation
	// This is a conservative approach - we clear the entire cache on any write.
	// A more sophisticated approach would track which queries are affected by
	// which mutations, but the simple approach is safer and still provides
	// significant benefit for repeated identical queries within the TTL window.
	if s.queryCache != nil {
		s.queryCache.Invalidate()
	}

	// Send to mutation channel for daemon
	select {
	case s.mutationChan <- event:
		// Event sent successfully
	default:
		// Channel full, increment dropped events counter
		s.droppedEvents.Add(1)
	}

	// Store in recent mutations buffer for polling
	s.recentMutationsMu.Lock()
	s.recentMutations = append(s.recentMutations, event)
	// Keep buffer size limited (circular buffer behavior)
	if len(s.recentMutations) > s.maxMutationBuffer {
		s.recentMutations = s.recentMutations[1:]
	}
	s.recentMutationsMu.Unlock()

	// Fan out to SSE subscribers (non-blocking per subscriber)
	s.subscribersMu.RLock()
	for _, sub := range s.subscribers {
		select {
		case sub.ch <- event:
		default:
			// Slow consumer, drop event
		}
	}
	s.subscribersMu.RUnlock()
}

// MutationChan returns the mutation event channel for the daemon to consume
func (s *Server) MutationChan() <-chan MutationEvent {
	return s.mutationChan
}

// sseSubscriber represents an SSE client subscribing to mutation events.
type sseSubscriber struct {
	id uint64
	ch chan MutationEvent
}

// Subscribe registers a new SSE subscriber and returns a channel of events
// plus an unsubscribe function. The channel is buffered to absorb short bursts;
// slow consumers will have events dropped.
func (s *Server) Subscribe() (<-chan MutationEvent, func()) {
	sub := &sseSubscriber{
		ch: make(chan MutationEvent, 64),
	}

	s.subscribersMu.Lock()
	s.nextSubID++
	sub.id = s.nextSubID
	s.subscribers = append(s.subscribers, sub)
	s.subscribersMu.Unlock()

	unsubscribe := func() {
		s.subscribersMu.Lock()
		defer s.subscribersMu.Unlock()
		for i, existing := range s.subscribers {
			if existing.id == sub.id {
				s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
				close(sub.ch)
				break
			}
		}
	}

	return sub.ch, unsubscribe
}

// PeriodicStatsSummary returns a human-readable summary of metrics for periodic logging
func (s *Server) PeriodicStatsSummary() string {
	return s.metrics.PeriodicStatsSummary(int(atomic.LoadInt32(&s.activeConns)))
}

// GetMetrics returns the server's metrics collector (for direct access)
func (s *Server) GetMetrics() *Metrics {
	return s.metrics
}

// SetConfig sets the daemon configuration for status reporting
func (s *Server) SetConfig(autoCommit, autoPush, autoPull, localMode bool, syncInterval, daemonMode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoCommit = autoCommit
	s.autoPush = autoPush
	s.autoPull = autoPull
	s.localMode = localMode
	s.syncInterval = syncInterval
	s.daemonMode = daemonMode
}

// SetBus sets the event bus for hook dispatch.
// Must be called before Start() or after server creation.
func (s *Server) SetBus(bus *eventbus.Bus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bus = bus
}

// NATSHealthInfo contains NATS health data for bus_status reporting.
// This avoids importing the daemon package from rpc (no circular deps).
type NATSHealthInfo struct {
	Enabled     bool
	Status      string
	Port        int
	Connections int
	JetStream   bool
	Streams     int
}

// SetNATSHealthFn sets a callback that provides NATS health information.
// The callback is invoked on each bus_status request.
func (s *Server) SetNATSHealthFn(fn func() NATSHealthInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.natsHealthFn = fn
}

// SetTCPAddr sets the TCP address to listen on (e.g., ":9876" or "0.0.0.0:9876").
// Must be called before Start(). When set, the server will listen on both the
// Unix socket AND the TCP address.
func (s *Server) SetTCPAddr(addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tcpAddr = addr
}

// TCPAddr returns the configured TCP address, or empty string if not set.
func (s *Server) TCPAddr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tcpAddr
}

// TCPListener returns the active TCP listener, or nil if not configured.
// Used for testing and diagnostics.
func (s *Server) TCPListener() net.Listener {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tcpListener
}

// SetTCPToken sets the token required for TCP connection authentication.
// When set, TCP clients must include this token in their requests.
// Unix socket connections are not affected (local connections are trusted).
func (s *Server) SetTCPToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tcpToken = token
}

// TCPToken returns the configured TCP authentication token, or empty string if not set.
func (s *Server) TCPToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tcpToken
}

// SetHTTPAddr sets the HTTP address to listen on (e.g., ":9080" or "0.0.0.0:9080").
// Must be called before Start(). When set, the server will start an HTTP endpoint
// that wraps RPC operations in Connect-RPC style HTTP POST requests.
func (s *Server) SetHTTPAddr(addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.httpAddr = addr
}

// HTTPAddr returns the configured HTTP address, or empty string if not set.
func (s *Server) HTTPAddr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.httpAddr
}

// HTTPServer returns the HTTP server instance, or nil if not configured.
func (s *Server) HTTPServer() *HTTPServer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.httpServer
}

// ResetDroppedEventsCount resets the dropped events counter and returns the previous value
func (s *Server) ResetDroppedEventsCount() int64 {
	return s.droppedEvents.Swap(0)
}

// GetRecentMutations returns mutations since the given timestamp
func (s *Server) GetRecentMutations(sinceMillis int64) []MutationEvent {
	s.recentMutationsMu.RLock()
	defer s.recentMutationsMu.RUnlock()

	var result []MutationEvent
	for _, m := range s.recentMutations {
		if m.Timestamp.UnixMilli() > sinceMillis {
			result = append(result, m)
		}
	}
	return result
}

// handleGetMutations handles the get_mutations RPC operation
func (s *Server) handleGetMutations(req *Request) Response {
	var args GetMutationsArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid arguments: %v", err),
		}
	}

	mutations := s.GetRecentMutations(args.Since)
	data, _ := json.Marshal(mutations)

	return Response{
		Success: true,
		Data:    data,
	}
}

// handleGetMoleculeProgress handles the get_molecule_progress RPC operation
// Returns detailed progress for a molecule (parent issue with child steps)
func (s *Server) handleGetMoleculeProgress(req *Request) Response {
	var args GetMoleculeProgressArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid arguments: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	// Get the molecule (parent issue)
	molecule, err := store.GetIssue(ctx, args.MoleculeID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get molecule: %v", err),
		}
	}
	if molecule == nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("molecule not found: %s", args.MoleculeID),
		}
	}

	// Get children (issues that have parent-child dependency on this molecule)
	var children []*types.IssueWithDependencyMetadata
	if sqliteStore, ok := store.(interface {
		GetDependentsWithMetadata(ctx context.Context, issueID string) ([]*types.IssueWithDependencyMetadata, error)
	}); ok {
		allDependents, err := sqliteStore.GetDependentsWithMetadata(ctx, args.MoleculeID)
		if err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to get molecule children: %v", err),
			}
		}
		// Filter for parent-child relationships only
		for _, dep := range allDependents {
			if dep.DependencyType == types.DepParentChild {
				children = append(children, dep)
			}
		}
	}

	// Get blocked issue IDs for status computation
	blockedIDs := make(map[string]bool)
	if sqliteStore, ok := store.(interface {
		GetBlockedIssueIDs(ctx context.Context) ([]string, error)
	}); ok {
		ids, err := sqliteStore.GetBlockedIssueIDs(ctx)
		if err == nil {
			for _, id := range ids {
				blockedIDs[id] = true
			}
		}
	}

	// Build steps from children
	steps := make([]MoleculeStep, 0, len(children))
	for _, child := range children {
		step := MoleculeStep{
			ID:    child.ID,
			Title: child.Title,
		}

		// Compute step status
		switch child.Status {
		case types.StatusClosed:
			step.Status = "done"
		case types.StatusInProgress:
			step.Status = "current"
		default: // open, blocked, etc.
			if blockedIDs[child.ID] {
				step.Status = "blocked"
			} else {
				step.Status = "ready"
			}
		}

		// Set timestamps
		startTime := child.CreatedAt.Format(time.RFC3339)
		step.StartTime = &startTime

		if child.ClosedAt != nil {
			closeTime := child.ClosedAt.Format(time.RFC3339)
			step.CloseTime = &closeTime
		}

		steps = append(steps, step)
	}

	progress := MoleculeProgress{
		MoleculeID: molecule.ID,
		Title:      molecule.Title,
		Assignee:   molecule.Assignee,
		Steps:      steps,
	}

	data, _ := json.Marshal(progress)
	return Response{
		Success: true,
		Data:    data,
	}
}
