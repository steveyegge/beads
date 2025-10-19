package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
	"golang.org/x/mod/semver"
)

// ServerVersion is the version of this RPC server
// This should match the bd CLI version for proper compatibility checks
// It's set as a var so it can be initialized from main
var ServerVersion = "0.9.10"

// StorageCacheEntry holds a cached storage with metadata for eviction
type StorageCacheEntry struct {
	store      storage.Storage
	lastAccess time.Time
}

// Server represents the RPC server that runs in the daemon
type Server struct {
	socketPath   string
	storage      storage.Storage // Default storage (for backward compat)
	listener     net.Listener
	mu           sync.RWMutex
	shutdown     bool
	shutdownChan chan struct{}
	stopOnce     sync.Once
	// Per-request storage routing with eviction support
	storageCache   map[string]*StorageCacheEntry // repoRoot -> entry
	cacheMu        sync.RWMutex
	maxCacheSize   int
	cacheTTL       time.Duration
	cleanupTicker  *time.Ticker
	// Health and metrics
	startTime    time.Time
	cacheHits    int64
	cacheMisses  int64
}

// NewServer creates a new RPC server
func NewServer(socketPath string, store storage.Storage) *Server {
	// Parse config from env vars
	maxCacheSize := 50 // default
	if env := os.Getenv("BEADS_DAEMON_MAX_CACHE_SIZE"); env != "" {
		// Parse as integer
		var size int
		if _, err := fmt.Sscanf(env, "%d", &size); err == nil && size > 0 {
			maxCacheSize = size
		}
	}
	
	cacheTTL := 30 * time.Minute // default
	if env := os.Getenv("BEADS_DAEMON_CACHE_TTL"); env != "" {
		if ttl, err := time.ParseDuration(env); err == nil && ttl > 0 {
			cacheTTL = ttl
		}
	}

	return &Server{
		socketPath:   socketPath,
		storage:      store,
		storageCache: make(map[string]*StorageCacheEntry),
		maxCacheSize: maxCacheSize,
		cacheTTL:     cacheTTL,
		shutdownChan: make(chan struct{}),
		startTime:    time.Now(),
	}
}

// Start starts the RPC server and listens for connections
func (s *Server) Start(ctx context.Context) error {
	if err := s.ensureSocketDir(); err != nil {
		return fmt.Errorf("failed to ensure socket directory: %w", err)
	}

	if err := s.removeOldSocket(); err != nil {
		return fmt.Errorf("failed to remove old socket: %w", err)
	}

	var err error
	s.listener, err = net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}

	// Set socket permissions to 0600 for security (owner only)
	if err := os.Chmod(s.socketPath, 0600); err != nil {
		s.listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	go s.handleSignals()
	go s.runCleanupLoop()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			shutdown := s.shutdown
			s.mu.Unlock()
			if shutdown {
				return nil
			}
			return fmt.Errorf("failed to accept connection: %w", err)
		}

		go s.handleConnection(conn)
	}
}

// Stop stops the RPC server and cleans up resources
func (s *Server) Stop() error {
	var err error
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.shutdown = true
		s.mu.Unlock()

		// Signal cleanup goroutine to stop
		close(s.shutdownChan)

		// Close all cached storage connections outside lock
		s.cacheMu.Lock()
		stores := make([]storage.Storage, 0, len(s.storageCache))
		for _, entry := range s.storageCache {
			stores = append(stores, entry.store)
		}
		s.storageCache = make(map[string]*StorageCacheEntry)
		s.cacheMu.Unlock()

		// Close stores without holding lock
		for _, store := range stores {
			if closeErr := store.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to close storage: %v\n", closeErr)
			}
		}

		if s.listener != nil {
			if closeErr := s.listener.Close(); closeErr != nil {
				err = fmt.Errorf("failed to close listener: %w", closeErr)
				return
			}
		}

		if removeErr := s.removeOldSocket(); removeErr != nil {
			err = fmt.Errorf("failed to remove socket: %w", removeErr)
		}
	})
	return err
}

func (s *Server) ensureSocketDir() error {
	dir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	// Best-effort tighten permissions if directory already existed
	_ = os.Chmod(dir, 0700)
	return nil
}

func (s *Server) removeOldSocket() error {
	if _, err := os.Stat(s.socketPath); err == nil {
		// Socket exists - check if it's stale before removing
		// Try to connect to see if a daemon is actually using it
		conn, err := net.DialTimeout("unix", s.socketPath, 500*time.Millisecond)
		if err == nil {
			// Socket is active - another daemon is running
			conn.Close()
			return fmt.Errorf("socket %s is in use by another daemon", s.socketPath)
		}
		
		// Socket is stale - safe to remove
		if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *Server) handleSignals() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	s.Stop()
}

// runCleanupLoop periodically evicts stale storage connections
func (s *Server) runCleanupLoop() {
	s.cleanupTicker = time.NewTicker(5 * time.Minute)
	defer s.cleanupTicker.Stop()

	for {
		select {
		case <-s.cleanupTicker.C:
			s.evictStaleStorage()
		case <-s.shutdownChan:
			return
		}
	}
}

// evictStaleStorage removes idle connections and enforces cache size limits
func (s *Server) evictStaleStorage() {
	now := time.Now()
	toClose := []storage.Storage{}
	
	s.cacheMu.Lock()

	// First pass: evict TTL-expired entries
	for path, entry := range s.storageCache {
		if now.Sub(entry.lastAccess) > s.cacheTTL {
			toClose = append(toClose, entry.store)
			delete(s.storageCache, path)
		}
	}

	// Second pass: enforce max cache size with LRU eviction
	if len(s.storageCache) > s.maxCacheSize {
		// Build sorted list of entries by lastAccess
		type cacheItem struct {
			path  string
			entry *StorageCacheEntry
		}
		items := make([]cacheItem, 0, len(s.storageCache))
		for path, entry := range s.storageCache {
			items = append(items, cacheItem{path, entry})
		}

		// Sort by lastAccess (oldest first) with sort.Slice
		sort.Slice(items, func(i, j int) bool {
			return items[i].entry.lastAccess.Before(items[j].entry.lastAccess)
		})

		// Evict oldest entries until we're under the limit
		numToEvict := len(s.storageCache) - s.maxCacheSize
		for i := 0; i < numToEvict && i < len(items); i++ {
			toClose = append(toClose, items[i].entry.store)
			delete(s.storageCache, items[i].path)
		}
	}

	// Unlock before closing to avoid holding lock during Close
	s.cacheMu.Unlock()

	// Close connections synchronously
	for _, store := range toClose {
		if err := store.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close evicted storage: %v\n", err)
		}
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			resp := Response{
				Success: false,
				Error:   fmt.Sprintf("invalid request: %v", err),
			}
			s.writeResponse(writer, resp)
			continue
		}

		resp := s.handleRequest(&req)
		s.writeResponse(writer, resp)
	}
}

// checkVersionCompatibility validates client version against server version
// Returns error if versions are incompatible
func (s *Server) checkVersionCompatibility(clientVersion string) error {
	// Allow empty client version (old clients before this feature)
	if clientVersion == "" {
		return nil
	}
	
	// Normalize versions to semver format (add 'v' prefix if missing)
	serverVer := ServerVersion
	if !strings.HasPrefix(serverVer, "v") {
		serverVer = "v" + serverVer
	}
	clientVer := clientVersion
	if !strings.HasPrefix(clientVer, "v") {
		clientVer = "v" + clientVer
	}
	
	// Validate versions are valid semver
	if !semver.IsValid(serverVer) || !semver.IsValid(clientVer) {
		// If either version is invalid, allow connection (dev builds, etc)
		return nil
	}
	
	// Extract major versions
	serverMajor := semver.Major(serverVer)
	clientMajor := semver.Major(clientVer)
	
	// Major version must match
	if serverMajor != clientMajor {
		cmp := semver.Compare(serverVer, clientVer)
		if cmp < 0 {
			// Daemon is older - needs upgrade
			return fmt.Errorf("incompatible major versions: client %s, daemon %s. Daemon is older; upgrade and restart daemon: 'bd daemon --stop && bd daemon'", 
				clientVersion, ServerVersion)
		}
		// Daemon is newer - client needs upgrade
		return fmt.Errorf("incompatible major versions: client %s, daemon %s. Client is older; upgrade the bd CLI to match the daemon's major version", 
			clientVersion, ServerVersion)
	}
	
	// Compare full versions - daemon should be >= client for backward compatibility
	cmp := semver.Compare(serverVer, clientVer)
	if cmp < 0 {
		// Server is older than client within same major version - may be missing features
		return fmt.Errorf("version mismatch: daemon %s is older than client %s. Upgrade and restart daemon: 'bd daemon --stop && bd daemon'", 
			ServerVersion, clientVersion)
	}
	
	// Client is same version or older - OK (daemon supports backward compat within major version)
	return nil
}

func (s *Server) handleRequest(req *Request) Response {
	// Check version compatibility (skip for ping/health to allow version checks)
	if req.Operation != OpPing && req.Operation != OpHealth {
		if err := s.checkVersionCompatibility(req.ClientVersion); err != nil {
			return Response{
				Success: false,
				Error:   err.Error(),
			}
		}
	}
	
	switch req.Operation {
	case OpPing:
		return s.handlePing(req)
	case OpHealth:
		return s.handleHealth(req)
	case OpCreate:
		return s.handleCreate(req)
	case OpUpdate:
		return s.handleUpdate(req)
	case OpClose:
		return s.handleClose(req)
	case OpList:
		return s.handleList(req)
	case OpShow:
		return s.handleShow(req)
	case OpReady:
		return s.handleReady(req)
	case OpStats:
		return s.handleStats(req)
	case OpDepAdd:
		return s.handleDepAdd(req)
	case OpDepRemove:
		return s.handleDepRemove(req)
	case OpLabelAdd:
		return s.handleLabelAdd(req)
	case OpLabelRemove:
		return s.handleLabelRemove(req)
	case OpBatch:
		return s.handleBatch(req)
	case OpReposList:
		return s.handleReposList(req)
	case OpReposReady:
		return s.handleReposReady(req)
	case OpReposStats:
		return s.handleReposStats(req)
	case OpReposClearCache:
		return s.handleReposClearCache(req)
	default:
		return Response{
			Success: false,
			Error:   fmt.Sprintf("unknown operation: %s", req.Operation),
		}
	}
}

// Adapter helpers
func (s *Server) reqCtx(_ *Request) context.Context {
	return context.Background()
}

func (s *Server) reqActor(req *Request) string {
	if req != nil && req.Actor != "" {
		return req.Actor
	}
	return "daemon"
}

func strValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func updatesFromArgs(a UpdateArgs) map[string]interface{} {
	u := map[string]interface{}{}
	if a.Title != nil {
		u["title"] = *a.Title
	}
	if a.Status != nil {
		u["status"] = *a.Status
	}
	if a.Priority != nil {
		u["priority"] = *a.Priority
	}
	if a.Design != nil {
		u["design"] = a.Design
	}
	if a.AcceptanceCriteria != nil {
		u["acceptance_criteria"] = a.AcceptanceCriteria
	}
	if a.Notes != nil {
		u["notes"] = a.Notes
	}
	if a.Assignee != nil {
		u["assignee"] = a.Assignee
	}
	return u
}

// Handler implementations

func (s *Server) handlePing(_ *Request) Response {
	data, _ := json.Marshal(PingResponse{
		Message: "pong",
		Version: ServerVersion,
	})
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleHealth(req *Request) Response {
	start := time.Now()
	
	store, err := s.getStorageForRequest(req)
	if err != nil {
		data, _ := json.Marshal(HealthResponse{
			Status:  "unhealthy",
			Version: ServerVersion,
			Uptime:  time.Since(s.startTime).Seconds(),
			Error:   fmt.Sprintf("storage error: %v", err),
		})
		return Response{
			Success: false,
			Data:    data,
			Error:   fmt.Sprintf("storage error: %v", err),
		}
	}

	healthCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	status := "healthy"
	dbError := ""
	
	_, pingErr := store.GetStatistics(healthCtx)
	dbResponseMs := time.Since(start).Seconds() * 1000
	
	if pingErr != nil {
		status = "unhealthy"
		dbError = pingErr.Error()
	} else if dbResponseMs > 500 {
		status = "degraded"
	}

	s.cacheMu.RLock()
	cacheSize := len(s.storageCache)
	s.cacheMu.RUnlock()

	// Check version compatibility
	compatible := true
	if req.ClientVersion != "" {
		if err := s.checkVersionCompatibility(req.ClientVersion); err != nil {
			compatible = false
		}
	}

	health := HealthResponse{
		Status:         status,
		Version:        ServerVersion,
		ClientVersion:  req.ClientVersion,
		Compatible:     compatible,
		Uptime:         time.Since(s.startTime).Seconds(),
		CacheSize:      cacheSize,
		CacheHits:      atomic.LoadInt64(&s.cacheHits),
		CacheMisses:    atomic.LoadInt64(&s.cacheMisses),
		DBResponseTime: dbResponseMs,
	}
	
	if dbError != "" {
		health.Error = dbError
	}

	data, _ := json.Marshal(health)
	return Response{
		Success: status != "unhealthy",
		Data:    data,
		Error:   dbError,
	}
}

func (s *Server) handleCreate(req *Request) Response {
	var createArgs CreateArgs
	if err := json.Unmarshal(req.Args, &createArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid create args: %v", err),
		}
	}

	store, err := s.getStorageForRequest(req)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("storage error: %v", err),
		}
	}

	var design, acceptance, assignee *string
	if createArgs.Design != "" {
		design = &createArgs.Design
	}
	if createArgs.AcceptanceCriteria != "" {
		acceptance = &createArgs.AcceptanceCriteria
	}
	if createArgs.Assignee != "" {
		assignee = &createArgs.Assignee
	}

	issue := &types.Issue{
		ID:                 createArgs.ID,
		Title:              createArgs.Title,
		Description:        createArgs.Description,
		IssueType:          types.IssueType(createArgs.IssueType),
		Priority:           createArgs.Priority,
		Design:             strValue(design),
		AcceptanceCriteria: strValue(acceptance),
		Assignee:           strValue(assignee),
		Status:             types.StatusOpen,
	}

	ctx := s.reqCtx(req)
	if err := store.CreateIssue(ctx, issue, s.reqActor(req)); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to create issue: %v", err),
		}
	}

	data, _ := json.Marshal(issue)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleUpdate(req *Request) Response {
	var updateArgs UpdateArgs
	if err := json.Unmarshal(req.Args, &updateArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid update args: %v", err),
		}
	}

	store, err := s.getStorageForRequest(req)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("storage error: %v", err),
		}
	}

	ctx := s.reqCtx(req)
	updates := updatesFromArgs(updateArgs)
	if len(updates) == 0 {
		return Response{Success: true}
	}

	if err := store.UpdateIssue(ctx, updateArgs.ID, updates, s.reqActor(req)); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to update issue: %v", err),
		}
	}

	issue, err := store.GetIssue(ctx, updateArgs.ID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get updated issue: %v", err),
		}
	}

	data, _ := json.Marshal(issue)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleClose(req *Request) Response {
	var closeArgs CloseArgs
	if err := json.Unmarshal(req.Args, &closeArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid close args: %v", err),
		}
	}

	store, err := s.getStorageForRequest(req)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("storage error: %v", err),
		}
	}

	ctx := s.reqCtx(req)
	if err := store.CloseIssue(ctx, closeArgs.ID, closeArgs.Reason, s.reqActor(req)); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to close issue: %v", err),
		}
	}

	issue, _ := store.GetIssue(ctx, closeArgs.ID)
	data, _ := json.Marshal(issue)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleList(req *Request) Response {
	var listArgs ListArgs
	if err := json.Unmarshal(req.Args, &listArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid list args: %v", err),
		}
	}

	store, err := s.getStorageForRequest(req)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("storage error: %v", err),
		}
	}

	filter := types.IssueFilter{
		Limit: listArgs.Limit,
	}
	if listArgs.Status != "" {
		status := types.Status(listArgs.Status)
		filter.Status = &status
	}
	if listArgs.IssueType != "" {
		issueType := types.IssueType(listArgs.IssueType)
		filter.IssueType = &issueType
	}
	if listArgs.Assignee != "" {
		filter.Assignee = &listArgs.Assignee
	}
	if listArgs.Priority != nil {
		filter.Priority = listArgs.Priority
	}

	ctx := s.reqCtx(req)
	issues, err := store.SearchIssues(ctx, listArgs.Query, filter)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to list issues: %v", err),
		}
	}

	data, _ := json.Marshal(issues)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleShow(req *Request) Response {
	var showArgs ShowArgs
	if err := json.Unmarshal(req.Args, &showArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid show args: %v", err),
		}
	}

	store, err := s.getStorageForRequest(req)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("storage error: %v", err),
		}
	}

	ctx := s.reqCtx(req)
	issue, err := store.GetIssue(ctx, showArgs.ID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get issue: %v", err),
		}
	}

	data, _ := json.Marshal(issue)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleReady(req *Request) Response {
	var readyArgs ReadyArgs
	if err := json.Unmarshal(req.Args, &readyArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid ready args: %v", err),
		}
	}

	store, err := s.getStorageForRequest(req)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("storage error: %v", err),
		}
	}

	wf := types.WorkFilter{
		Status:   types.StatusOpen,
		Priority: readyArgs.Priority,
		Limit:    readyArgs.Limit,
	}
	if readyArgs.Assignee != "" {
		wf.Assignee = &readyArgs.Assignee
	}

	ctx := s.reqCtx(req)
	issues, err := store.GetReadyWork(ctx, wf)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get ready work: %v", err),
		}
	}

	data, _ := json.Marshal(issues)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleStats(req *Request) Response {
	store, err := s.getStorageForRequest(req)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("storage error: %v", err),
		}
	}

	ctx := s.reqCtx(req)
	stats, err := store.GetStatistics(ctx)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to get statistics: %v", err),
		}
	}

	data, _ := json.Marshal(stats)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleDepAdd(req *Request) Response {
	var depArgs DepAddArgs
	if err := json.Unmarshal(req.Args, &depArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid dep add args: %v", err),
		}
	}

	store, err := s.getStorageForRequest(req)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("storage error: %v", err),
		}
	}

	dep := &types.Dependency{
		IssueID:     depArgs.FromID,
		DependsOnID: depArgs.ToID,
		Type:        types.DependencyType(depArgs.DepType),
	}

	ctx := s.reqCtx(req)
	if err := store.AddDependency(ctx, dep, s.reqActor(req)); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to add dependency: %v", err),
		}
	}

	return Response{Success: true}
}

func (s *Server) handleDepRemove(req *Request) Response {
	var depArgs DepRemoveArgs
	if err := json.Unmarshal(req.Args, &depArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid dep remove args: %v", err),
		}
	}

	store, err := s.getStorageForRequest(req)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("storage error: %v", err),
		}
	}

	ctx := s.reqCtx(req)
	if err := store.RemoveDependency(ctx, depArgs.FromID, depArgs.ToID, s.reqActor(req)); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to remove dependency: %v", err),
		}
	}

	return Response{Success: true}
}

func (s *Server) handleLabelAdd(req *Request) Response {
	var labelArgs LabelAddArgs
	if err := json.Unmarshal(req.Args, &labelArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid label add args: %v", err),
		}
	}

	store, err := s.getStorageForRequest(req)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("storage error: %v", err),
		}
	}

	ctx := s.reqCtx(req)
	if err := store.AddLabel(ctx, labelArgs.ID, labelArgs.Label, s.reqActor(req)); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to add label: %v", err),
		}
	}

	return Response{Success: true}
}

func (s *Server) handleLabelRemove(req *Request) Response {
	var labelArgs LabelRemoveArgs
	if err := json.Unmarshal(req.Args, &labelArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid label remove args: %v", err),
		}
	}

	store, err := s.getStorageForRequest(req)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("storage error: %v", err),
		}
	}

	ctx := s.reqCtx(req)
	if err := store.RemoveLabel(ctx, labelArgs.ID, labelArgs.Label, s.reqActor(req)); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to remove label: %v", err),
		}
	}

	return Response{Success: true}
}

func (s *Server) handleBatch(req *Request) Response {
	var batchArgs BatchArgs
	if err := json.Unmarshal(req.Args, &batchArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid batch args: %v", err),
		}
	}

	results := make([]BatchResult, 0, len(batchArgs.Operations))

	for _, op := range batchArgs.Operations {
		subReq := &Request{
			Operation:     op.Operation,
			Args:          op.Args,
			Actor:         req.Actor,
			RequestID:     req.RequestID,
			Cwd:           req.Cwd,           // Pass through context
			ClientVersion: req.ClientVersion, // Pass through version for compatibility checks
		}

		resp := s.handleRequest(subReq)

		results = append(results, BatchResult{
			Success: resp.Success,
			Data:    resp.Data,
			Error:   resp.Error,
		})

		if !resp.Success {
			break
		}
	}

	batchResp := BatchResponse{Results: results}
	data, _ := json.Marshal(batchResp)

	return Response{
		Success: true,
		Data:    data,
	}
}

// getStorageForRequest returns the appropriate storage for the request
// If req.Cwd is set, it finds the database for that directory
// Otherwise, it uses the default storage
func (s *Server) getStorageForRequest(req *Request) (storage.Storage, error) {
	// If no cwd specified, use default storage
	if req.Cwd == "" {
		return s.storage, nil
	}

	// Find database for this cwd (to get the canonical repo root)
	dbPath := s.findDatabaseForCwd(req.Cwd)
	if dbPath == "" {
		return nil, fmt.Errorf("no .beads database found for path: %s", req.Cwd)
	}

	// Canonicalize key to repo root (parent of .beads directory)
	beadsDir := filepath.Dir(dbPath)
	repoRoot := filepath.Dir(beadsDir)

	// Check cache first with write lock (to avoid race on lastAccess update)
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	
	if entry, ok := s.storageCache[repoRoot]; ok {
		// Update last access time (safe under Lock)
		entry.lastAccess = time.Now()
		atomic.AddInt64(&s.cacheHits, 1)
		return entry.store, nil
	}
	
	atomic.AddInt64(&s.cacheMisses, 1)

	// Open storage
	store, err := sqlite.New(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}

	// Cache it with current timestamp
	s.storageCache[repoRoot] = &StorageCacheEntry{
		store:      store,
		lastAccess: time.Now(),
	}

	// Enforce LRU immediately to prevent FD spikes between cleanup ticks
	needEvict := len(s.storageCache) > s.maxCacheSize
	s.cacheMu.Unlock()
	
	if needEvict {
		s.evictStaleStorage()
	}

	// Re-acquire lock for defer
	s.cacheMu.Lock()

	return store, nil
}

// findDatabaseForCwd walks up from cwd to find .beads/*.db
func (s *Server) findDatabaseForCwd(cwd string) string {
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return ""
	}

	// Walk up directory tree
	for {
		beadsDir := filepath.Join(dir, ".beads")
		if info, err := os.Stat(beadsDir); err == nil && info.IsDir() {
			// Found .beads/ directory, look for *.db files
			matches, err := filepath.Glob(filepath.Join(beadsDir, "*.db"))
			if err == nil && len(matches) > 0 {
				return matches[0]
			}
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return ""
}

func (s *Server) writeResponse(writer *bufio.Writer, resp Response) {
	data, _ := json.Marshal(resp)
	writer.Write(data)
	writer.WriteByte('\n')
	writer.Flush()
}

// Multi-repo handlers

func (s *Server) handleReposList(_ *Request) Response {
	// Keep read lock during iteration to prevent stores from being closed mid-query
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	repos := make([]RepoInfo, 0, len(s.storageCache))
	for path, entry := range s.storageCache {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		stats, err := entry.store.GetStatistics(ctx)
		cancel()
		if err != nil {
			continue
		}

		// Extract prefix from a sample issue
		filter := types.IssueFilter{Limit: 1}
		ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
		issues, err := entry.store.SearchIssues(ctx2, "", filter)
		cancel2()
		prefix := ""
		if err == nil && len(issues) > 0 && len(issues[0].ID) > 0 {
			// Extract prefix (everything before the last hyphen and number)
			id := issues[0].ID
			for i := len(id) - 1; i >= 0; i-- {
				if id[i] == '-' {
					prefix = id[:i+1]
					break
				}
			}
		}

		repos = append(repos, RepoInfo{
			Path:       path,
			Prefix:     prefix,
			IssueCount: stats.TotalIssues,
			LastAccess: entry.lastAccess.Format(time.RFC3339),
		})
	}

	data, _ := json.Marshal(repos)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleReposReady(req *Request) Response {
	var args ReposReadyArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid args: %v", err),
		}
	}

	// Keep read lock during iteration to prevent stores from being closed mid-query
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	if args.GroupByRepo {
		result := make([]RepoReadyWork, 0, len(s.storageCache))
		for path, entry := range s.storageCache {
			filter := types.WorkFilter{
				Status: types.StatusOpen,
				Limit:  args.Limit,
			}
			if args.Priority != nil {
				filter.Priority = args.Priority
			}
			if args.Assignee != "" {
				filter.Assignee = &args.Assignee
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			issues, err := entry.store.GetReadyWork(ctx, filter)
			cancel()
			if err != nil || len(issues) == 0 {
				continue
			}

			result = append(result, RepoReadyWork{
				RepoPath: path,
				Issues:   issues,
			})
		}

		data, _ := json.Marshal(result)
		return Response{
			Success: true,
			Data:    data,
		}
	}

	// Flat list of all ready issues across all repos
	allIssues := make([]ReposReadyIssue, 0)
	for path, entry := range s.storageCache {
		filter := types.WorkFilter{
			Status: types.StatusOpen,
			Limit:  args.Limit,
		}
		if args.Priority != nil {
			filter.Priority = args.Priority
		}
		if args.Assignee != "" {
			filter.Assignee = &args.Assignee
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		issues, err := entry.store.GetReadyWork(ctx, filter)
		cancel()
		if err != nil {
			continue
		}

		for _, issue := range issues {
			allIssues = append(allIssues, ReposReadyIssue{
				RepoPath: path,
				Issue:    issue,
			})
		}
	}

	data, _ := json.Marshal(allIssues)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleReposStats(_ *Request) Response {
	// Keep read lock during iteration to prevent stores from being closed mid-query
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	total := types.Statistics{}
	perRepo := make(map[string]types.Statistics)
	errors := make(map[string]string)

	for path, entry := range s.storageCache {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		stats, err := entry.store.GetStatistics(ctx)
		cancel()
		if err != nil {
			errors[path] = err.Error()
			continue
		}

		perRepo[path] = *stats

		// Aggregate totals
		total.TotalIssues += stats.TotalIssues
		total.OpenIssues += stats.OpenIssues
		total.InProgressIssues += stats.InProgressIssues
		total.ClosedIssues += stats.ClosedIssues
		total.BlockedIssues += stats.BlockedIssues
		total.ReadyIssues += stats.ReadyIssues
		total.EpicsEligibleForClosure += stats.EpicsEligibleForClosure
	}

	result := ReposStatsResponse{
		Total:   total,
		PerRepo: perRepo,
	}
	if len(errors) > 0 {
		result.Errors = errors
	}

	data, _ := json.Marshal(result)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleReposClearCache(_ *Request) Response {
	// Copy stores under write lock, clear cache, then close outside lock
	// to avoid holding lock during potentially slow Close() operations
	s.cacheMu.Lock()
	stores := make([]storage.Storage, 0, len(s.storageCache))
	for _, entry := range s.storageCache {
		stores = append(stores, entry.store)
	}
	s.storageCache = make(map[string]*StorageCacheEntry)
	s.cacheMu.Unlock()

	// Close all storage connections without holding lock
	for _, store := range stores {
		if err := store.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close storage: %v\n", err)
		}
	}

	return Response{
		Success: true,
		Data:    json.RawMessage(`{"message":"Cache cleared successfully"}`),
	}
}
