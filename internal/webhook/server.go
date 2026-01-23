package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// Server handles HTTP requests for decision response webhooks.
type Server struct {
	store      storage.Storage
	secret     []byte
	strictMode bool
	mux        *http.ServeMux
	httpServer *http.Server
}

// ServerConfig holds configuration for the webhook server.
type ServerConfig struct {
	Store      storage.Storage
	Secret     []byte // HMAC secret for token validation
	StrictMode bool   // If true, strictly validate respondent matches token
}

// NewServer creates a new webhook server.
func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		store:      cfg.Store,
		secret:     cfg.Secret,
		strictMode: cfg.StrictMode,
		mux:        http.NewServeMux(),
	}

	// Register routes
	s.mux.HandleFunc("/api/decisions/", s.handleDecisionResponse)
	s.mux.HandleFunc("/health", s.handleHealth)

	return s
}

// Start starts the HTTP server on the given address.
func (s *Server) Start(addr string) error {
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// Handler returns the HTTP handler for use with custom servers.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// RespondRequest is the JSON request body for decision responses.
type RespondRequest struct {
	Selected   string `json:"selected"`    // Option ID selected (optional if text provided)
	Text       string `json:"text"`        // Custom text response (optional if selected provided)
	Respondent string `json:"respondent"`  // Who is responding (email, user ID)
	AuthToken  string `json:"auth_token"`  // HMAC-signed token
}

// RespondResponse is the JSON response body.
type RespondResponse struct {
	Success     bool   `json:"success"`
	DecisionID  string `json:"decision_id,omitempty"`
	Selected    string `json:"selected,omitempty"`
	Text        string `json:"text,omitempty"`
	RespondedAt string `json:"responded_at,omitempty"`
	Error       string `json:"error,omitempty"`
	Warning     string `json:"warning,omitempty"`
}

// handleDecisionResponse handles POST /api/decisions/{id}/respond
func (s *Server) handleDecisionResponse(w http.ResponseWriter, r *http.Request) {
	// Set JSON content type for all responses
	w.Header().Set("Content-Type", "application/json")

	// Only allow POST
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed: use POST")
		return
	}

	// Extract decision ID from path: /api/decisions/{id}/respond
	path := strings.TrimPrefix(r.URL.Path, "/api/decisions/")
	if !strings.HasSuffix(path, "/respond") {
		s.writeError(w, http.StatusNotFound, "not found: expected /api/decisions/{id}/respond")
		return
	}
	decisionID := strings.TrimSuffix(path, "/respond")
	if decisionID == "" {
		s.writeError(w, http.StatusBadRequest, "missing decision ID in path")
		return
	}

	// Read and parse request body
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	var req RespondRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	// Validate request fields
	if req.Selected == "" && req.Text == "" {
		s.writeError(w, http.StatusBadRequest, "must provide 'selected' and/or 'text'")
		return
	}
	if req.AuthToken == "" {
		s.writeError(w, http.StatusUnauthorized, "missing auth_token")
		return
	}

	// Validate auth token
	claims, err := ValidateResponseToken(req.AuthToken, s.secret)
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, fmt.Sprintf("invalid auth_token: %v", err))
		return
	}

	// Verify token is for this decision
	if claims.DecisionID != decisionID {
		s.writeError(w, http.StatusForbidden, "token is for a different decision")
		return
	}

	// Validate respondent if configured
	var warning string
	if ok, msg := ValidateRespondent(claims.Respondent, req.Respondent, s.strictMode); !ok {
		s.writeError(w, http.StatusForbidden, msg)
		return
	} else if msg != "" {
		warning = msg
	}

	// Get the decision point
	ctx := r.Context()
	issue, err := s.store.GetIssue(ctx, decisionID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get issue: %v", err))
		return
	}
	if issue == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("decision %s not found", decisionID))
		return
	}

	// Verify it's a decision gate
	if issue.IssueType != types.TypeGate || issue.AwaitType != "decision" {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("%s is not a decision point", decisionID))
		return
	}

	// Get decision point data
	dp, err := s.store.GetDecisionPoint(ctx, decisionID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get decision point: %v", err))
		return
	}
	if dp == nil {
		s.writeError(w, http.StatusNotFound, fmt.Sprintf("no decision point data for %s", decisionID))
		return
	}

	// Check if already responded (rate limit: 1 response per decision)
	if dp.RespondedAt != nil {
		s.writeError(w, http.StatusConflict, fmt.Sprintf("decision %s already responded at %s by %s",
			decisionID, dp.RespondedAt.Format(time.RFC3339), dp.RespondedBy))
		return
	}

	// Validate selected option if provided
	if req.Selected != "" {
		options, err := dp.GetOptionsWithAccept()
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to parse options: %v", err))
			return
		}
		found := false
		for _, opt := range options {
			if opt.ID == req.Selected {
				found = true
				break
			}
		}
		if !found {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid option: %s", req.Selected))
			return
		}
	}

	// Update the decision point
	now := time.Now()
	dp.RespondedAt = &now
	dp.RespondedBy = req.Respondent
	if req.Selected != "" {
		dp.SelectedOption = req.Selected
	}
	if req.Text != "" {
		dp.ResponseText = req.Text
	}

	if err := s.store.UpdateDecisionPoint(ctx, dp); err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update decision point: %v", err))
		return
	}

	// Close the gate if an option was selected (or accept-as-is selected)
	if req.Selected != "" {
		reason := "Decision resolved via webhook"
		if req.Selected == types.DecisionAcceptOptionID {
			reason = "Guidance accepted as-is via webhook"
		}
		// Note: We use empty actor and session since this is webhook-based
		if err := s.store.CloseIssue(ctx, decisionID, reason, req.Respondent, ""); err != nil {
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to close gate: %v", err))
			return
		}
	}

	// Success response
	resp := RespondResponse{
		Success:     true,
		DecisionID:  decisionID,
		Selected:    req.Selected,
		Text:        req.Text,
		RespondedAt: now.Format(time.RFC3339),
		Warning:     warning,
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// handleHealth handles GET /health for load balancer checks.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// writeError writes a JSON error response.
func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(RespondResponse{
		Success: false,
		Error:   message,
	})
}
