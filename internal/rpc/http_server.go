//go:build !windows

package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HTTPServer wraps the RPC server with HTTP endpoints
type HTTPServer struct {
	rpcServer  *Server
	httpServer *http.Server
	listener   net.Listener
	addr       string
	token      string // Bearer token for authentication
	mu         sync.RWMutex
}

// NewHTTPServer creates a new HTTP wrapper around an RPC server
func NewHTTPServer(rpcServer *Server, addr string, token string) *HTTPServer {
	return &HTTPServer{
		rpcServer: rpcServer,
		addr:      addr,
		token:     token,
	}
}

// Start starts the HTTP server
func (h *HTTPServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Health endpoints (no auth required)
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/healthz", h.handleHealth)
	mux.HandleFunc("/readyz", h.handleReadiness)
	mux.HandleFunc("/metrics", h.handleMetrics)

	// Connect-RPC style endpoints (auth required)
	mux.HandleFunc("/bd.v1.BeadsService/", h.handleRPC)

	h.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	var err error
	h.listener, err = net.Listen("tcp", h.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", h.addr, err)
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.httpServer.Shutdown(shutdownCtx)
	}()

	return h.httpServer.Serve(h.listener)
}

// Addr returns the address the HTTP server is listening on
func (h *HTTPServer) Addr() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.listener != nil {
		return h.listener.Addr().String()
	}
	return h.addr
}

// Listener returns the underlying listener
func (h *HTTPServer) Listener() net.Listener {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.listener
}

// handleHealth handles GET /health and /healthz
func (h *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Call RPC server's health handler with empty request
	req := &Request{Operation: OpHealth}
	resp := h.rpcServer.handleRequest(req)

	var health HealthResponse
	if resp.Success && len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, &health); err != nil {
			// Log error but don't fail - treat as degraded
			health.Error = fmt.Sprintf("failed to parse health response: %v", err)
		}
	}

	// Default empty status to "healthy" when response was successful
	// This prevents 503 when the RPC handler returns success with empty/missing status
	if health.Status == "" {
		if resp.Success {
			health.Status = "healthy"
		} else {
			health.Status = "unhealthy"
			if health.Error == "" {
				health.Error = resp.Error
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if health.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	result := map[string]interface{}{
		"status":  health.Status,
		"version": health.Version,
		"uptime":  fmt.Sprintf("%.0fs", health.Uptime),
	}
	if health.Error != "" {
		result["error"] = health.Error
	}

	_ = json.NewEncoder(w).Encode(result)
}

// handleReadiness handles GET /readyz
func (h *HTTPServer) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Call RPC server's health handler
	req := &Request{Operation: OpHealth}
	resp := h.rpcServer.handleRequest(req)

	var health HealthResponse
	if resp.Success && len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, &health); err != nil {
			// Log error but don't fail - treat as degraded
			health.Error = fmt.Sprintf("failed to parse health response: %v", err)
		}
	}

	// Default empty status to "healthy" when response was successful
	// This prevents 503 when the RPC handler returns success with empty/missing status
	if health.Status == "" {
		if resp.Success {
			health.Status = "healthy"
		} else {
			health.Status = "unhealthy"
			if health.Error == "" {
				health.Error = resp.Error
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if health.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "not ready",
			"reason": health.Error,
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ready",
	})
}

// handleMetrics handles GET /metrics
func (h *HTTPServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Call RPC server's metrics handler
	req := &Request{Operation: OpMetrics}
	resp := h.rpcServer.handleRequest(req)

	w.Header().Set("Content-Type", "application/json")
	if resp.Success && len(resp.Data) > 0 {
		w.Write(resp.Data)
	} else {
		w.Write([]byte("{}"))
	}
}

// handleRPC handles POST /bd.v1.BeadsService/{method}
func (h *HTTPServer) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check authentication if token is configured
	if h.token != "" {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			h.writeError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}
		if !strings.HasPrefix(authHeader, "Bearer ") {
			h.writeError(w, http.StatusUnauthorized, "invalid Authorization header format")
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != h.token {
			h.writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
	}

	// Extract method from path: /bd.v1.BeadsService/List -> List
	path := strings.TrimPrefix(r.URL.Path, "/bd.v1.BeadsService/")
	if path == "" || path == r.URL.Path {
		h.writeError(w, http.StatusNotFound, "invalid endpoint")
		return
	}

	// Map HTTP method name to RPC operation
	operation := httpMethodToOperation(path)
	if operation == "" {
		h.writeError(w, http.StatusNotFound, fmt.Sprintf("unknown method: %s", path))
		return
	}

	// Read request body
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	// Build RPC request
	req := &Request{
		Operation:     operation,
		Args:          body,
		Actor:         r.Header.Get("X-BD-Actor"),
		ClientVersion: r.Header.Get("X-BD-Client-Version"),
		Cwd:           r.Header.Get("X-BD-Cwd"),
		ExpectedDB:    r.Header.Get("X-BD-Expected-DB"),
	}

	// Execute via RPC server
	resp := h.rpcServer.handleRequest(req)

	// Write response
	w.Header().Set("Content-Type", "application/json")
	if !resp.Success {
		w.WriteHeader(http.StatusInternalServerError)
	}

	// For HTTP, we return just the data (or error), not wrapped in Response
	if resp.Success {
		if len(resp.Data) > 0 {
			w.Write(resp.Data)
		} else {
			w.Write([]byte("{}"))
		}
	} else {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": resp.Error,
		})
	}
}

func (h *HTTPServer) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

// httpMethodToOperation maps Connect-RPC style method names to RPC operations
func httpMethodToOperation(method string) string {
	methodMap := map[string]string{
		// Core CRUD
		"List":      OpList,
		"Show":      OpShow,
		"Create":    OpCreate,
		"Update":    OpUpdate,
		"Delete":    OpDelete,
		"Rename":    OpRename,
		"Close":     OpClose,
		"Count":     OpCount,
		"ResolveID": OpResolveID,

		// Status
		"Health":  OpHealth,
		"Status":  OpStatus,
		"Ping":    OpPing,
		"Metrics": OpMetrics,

		// Queries
		"Ready":   OpReady,
		"Blocked": OpBlocked,
		"Stale":   OpStale,
		"Stats":   OpStats,

		// Dependencies
		"DepAdd":                 OpDepAdd,
		"DepRemove":              OpDepRemove,
		"DepTree":                OpDepTree,
		"DepAddBidirectional":    OpDepAddBidirectional,
		"DepRemoveBidirectional": OpDepRemoveBidirectional,

		// Labels
		"LabelAdd":    OpLabelAdd,
		"LabelRemove": OpLabelRemove,

		// Comments
		"CommentList": OpCommentList,
		"CommentAdd":  OpCommentAdd,

		// Batch
		"Batch": OpBatch,

		// Sync
		"Export":       OpExport,
		"Import":       OpImport,
		"Compact":      OpCompact,
		"CompactStats": OpCompactStats,

		// Epic
		"EpicStatus": OpEpicStatus,

		// Mutations
		"GetMutations":        OpGetMutations,
		"GetMoleculeProgress": OpGetMoleculeProgress,
		"GetWorkerStatus":     OpGetWorkerStatus,
		"GetConfig":           OpGetConfig,

		// Gates
		"GateCreate": OpGateCreate,
		"GateList":   OpGateList,
		"GateShow":   OpGateShow,
		"GateClose":  OpGateClose,
		"GateWait":   OpGateWait,

		// Decisions
		"DecisionCreate":  OpDecisionCreate,
		"DecisionGet":     OpDecisionGet,
		"DecisionResolve": OpDecisionResolve,
		"DecisionList":    OpDecisionList,
		"DecisionRemind":  OpDecisionRemind,
		"DecisionCancel":  OpDecisionCancel,

		// Mol operations
		"MolBond":          OpMolBond,
		"MolSquash":        OpMolSquash,
		"MolBurn":          OpMolBurn,
		"MolCurrent":       OpMolCurrent,
		"MolProgressStats": OpMolProgressStats,
		"MolReadyGated":    OpMolReadyGated,

		// Close operations
		"CloseContinue": OpCloseContinue,

		// Atomic operations
		"CreateWithDeps":           OpCreateWithDeps,
		"BatchAddLabels":           OpBatchAddLabels,
		"CreateMolecule":           OpCreateMolecule,
		"BatchAddDependencies":     OpBatchAddDependencies,
		"BatchQueryWorkers":        OpBatchQueryWorkers,
		"CreateConvoyWithTracking": OpCreateConvoyWithTracking,
		"AtomicClosureChain":       OpAtomicClosureChain,
		"UpdateWithComment":        OpUpdateWithComment,

		// Remote database management
		"Init":    OpInit,
		"Migrate": OpMigrate,

		// Additional write operations (bd-wj80)
		"RenamePrefix": OpRenamePrefix,
		"Move":         OpMove,
		"Refile":       OpRefile,
		"Cook":         OpCook,
		"Pour":         OpPour,

		// Formula CRUD operations (gt-pozvwr.24.9)
		"FormulaList":   OpFormulaList,
		"FormulaGet":    OpFormulaGet,
		"FormulaSave":   OpFormulaSave,
		"FormulaDelete": OpFormulaDelete,

		// Config
		"ConfigSet":  OpConfigSet,
		"ConfigList": OpConfigList,

		// Watch operations
		"ListWatch": OpListWatch,

		// Types
		"Types": OpTypes,

		// Sync operations
		"SyncExport": OpSyncExport,
		"SyncStatus": OpSyncStatus,

		// State operations
		"SetState": OpSetState,

		// Config (additional)
		"ConfigUnset": OpConfigUnset,

		// Admin
		"Shutdown": OpShutdown,
	}

	return methodMap[method]
}
