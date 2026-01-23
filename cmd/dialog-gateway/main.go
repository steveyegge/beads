// dialog-gateway is an HTTP server that bridges webhook notifications from
// bd decision create to the dialog-client running on MacOS.
//
// Flow:
// 1. bd decision create dispatches webhook to localhost:8090/api/decisions/webhook
// 2. Gateway converts DecisionPayload to dialog.Request
// 3. Sends to dialog-client via TCP (through SSH tunnel on port 9876)
// 4. User responds via MacOS dialog
// 5. Gateway calls `bd decision respond <id> --select=<opt>` to record response
//
// Usage:
//   go run ./cmd/dialog-gateway [-port 8090] [-dialog-addr 127.0.0.1:9876]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/steveyegge/beads/internal/dialog"
	"github.com/steveyegge/beads/internal/notification"
)

var (
	port       = flag.Int("port", 8090, "HTTP port to listen on")
	dialogAddr = flag.String("dialog-addr", "127.0.0.1:9876", "Address of dialog-client (via SSH tunnel)")
	bdPath     = flag.String("bd-path", "bd", "Path to bd binary")
	verbose    = flag.Bool("verbose", false, "Enable verbose logging")
)

// Gateway handles webhook notifications and dialog interactions
type Gateway struct {
	dialogClient *dialog.Client
	bdPath       string
	mu           sync.Mutex
	connected    bool
}

// NewGateway creates a new gateway instance
func NewGateway(dialogAddr, bdPath string) *Gateway {
	client := dialog.NewClient(dialogAddr)
	client.SetTimeout(10 * time.Minute) // Dialogs can take a while
	return &Gateway{
		dialogClient: client,
		bdPath:       bdPath,
	}
}

// Connect establishes connection to dialog-client
func (g *Gateway) Connect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.connected {
		return nil
	}

	if err := g.dialogClient.Connect(); err != nil {
		return err
	}
	g.connected = true
	return nil
}

// Close closes the dialog-client connection
func (g *Gateway) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.connected = false
	return g.dialogClient.Close()
}

// IsConnected returns true if connected to dialog-client
func (g *Gateway) IsConnected() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.connected
}

// Health check response
type HealthResponse struct {
	Status         string `json:"status"`
	DialogClient   bool   `json:"dialog_client"`
	BdAvailable    bool   `json:"bd_available"`
	BdPath         string `json:"bd_path"`
}

// WebhookResponse is returned after processing a webhook
type WebhookResponse struct {
	Success      bool   `json:"success"`
	DecisionID   string `json:"decision_id,omitempty"`
	Selected     string `json:"selected,omitempty"`
	Cancelled    bool   `json:"cancelled,omitempty"`
	Error        string `json:"error,omitempty"`
	ResponseTime string `json:"response_time,omitempty"`
}

// handleHealth responds to health check requests
func (g *Gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Check bd availability
	bdAvailable := false
	if _, err := exec.LookPath(g.bdPath); err == nil {
		bdAvailable = true
	}

	health := HealthResponse{
		Status:       "ok",
		DialogClient: g.IsConnected(),
		BdAvailable:  bdAvailable,
		BdPath:       g.bdPath,
	}

	if !health.DialogClient {
		health.Status = "degraded"
	}
	if !health.BdAvailable {
		health.Status = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

// handleWebhook processes incoming decision notifications
func (g *Gateway) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	start := time.Now()

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		sendError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Parse decision payload
	var payload notification.DecisionPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		sendError(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if *verbose {
		log.Printf("Received webhook for decision %s: %s", payload.ID, payload.Prompt)
	}

	// Ensure connected to dialog-client
	if err := g.Connect(); err != nil {
		sendError(w, fmt.Sprintf("Dialog client not available: %v", err), http.StatusServiceUnavailable)
		return
	}

	// Convert to dialog request
	dialogReq := convertToDialogRequest(&payload)

	// Send dialog request and wait for response
	if *verbose {
		log.Printf("Showing dialog for %s", payload.ID)
	}

	resp, err := g.dialogClient.Send(dialogReq)
	if err != nil {
		// Connection may have been lost, mark as disconnected
		g.mu.Lock()
		g.connected = false
		g.mu.Unlock()
		sendError(w, fmt.Sprintf("Dialog error: %v", err), http.StatusInternalServerError)
		return
	}

	if *verbose {
		log.Printf("Dialog response for %s: cancelled=%v selected=%q", payload.ID, resp.Cancelled, resp.Selected)
	}

	// Handle response
	result := WebhookResponse{
		DecisionID:   payload.ID,
		Cancelled:    resp.Cancelled,
		Selected:     resp.Selected,
		ResponseTime: time.Since(start).String(),
	}

	if resp.Cancelled {
		// User cancelled - don't record response
		result.Success = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
		return
	}

	if resp.Error != "" {
		sendError(w, fmt.Sprintf("Dialog error: %s", resp.Error), http.StatusInternalServerError)
		return
	}

	// Record response via bd CLI
	if err := g.recordResponse(payload.ID, resp.Selected, resp.Text); err != nil {
		result.Success = false
		result.Error = err.Error()
	} else {
		result.Success = true
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleDialog allows direct dialog testing without webhook
func (g *Gateway) handleDialog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		sendError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Parse dialog request
	var req dialog.Request
	if err := json.Unmarshal(body, &req); err != nil {
		sendError(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Ensure connected
	if err := g.Connect(); err != nil {
		sendError(w, fmt.Sprintf("Dialog client not available: %v", err), http.StatusServiceUnavailable)
		return
	}

	// Send and wait
	resp, err := g.dialogClient.Send(&req)
	if err != nil {
		g.mu.Lock()
		g.connected = false
		g.mu.Unlock()
		sendError(w, fmt.Sprintf("Dialog error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// convertToDialogRequest converts a DecisionPayload to a dialog.Request
func convertToDialogRequest(payload *notification.DecisionPayload) *dialog.Request {
	req := &dialog.Request{
		ID:     payload.ID,
		Type:   "choice",
		Title:  "Decision Required",
		Prompt: payload.Prompt,
	}

	// Convert options
	for _, opt := range payload.Options {
		req.Options = append(req.Options, dialog.Option{
			ID:    opt.ID,
			Label: opt.Label,
		})
	}

	// Set default if present
	if payload.Default != "" {
		req.Default = payload.Default
	}

	// Add timeout info to prompt if present
	if payload.TimeoutAt != nil {
		remaining := time.Until(*payload.TimeoutAt)
		if remaining > 0 {
			req.Prompt += fmt.Sprintf("\n\n(Timeout in %s, default: %s)",
				remaining.Round(time.Minute), payload.Default)
		}
	}

	// If only one option and it looks like a text entry, use entry type
	if len(payload.Options) == 0 {
		req.Type = "entry"
	} else if len(payload.Options) == 2 {
		// Check for yes/no pattern
		ids := make(map[string]bool)
		for _, opt := range payload.Options {
			ids[strings.ToLower(opt.ID)] = true
		}
		if ids["yes"] && ids["no"] {
			req.Type = "confirm"
		}
	}

	return req
}

// recordResponse calls bd decision respond to record the selection
func (g *Gateway) recordResponse(decisionID, selected, text string) error {
	args := []string{"decision", "respond", decisionID}

	if selected != "" {
		args = append(args, fmt.Sprintf("--select=%s", selected))
	}
	if text != "" {
		args = append(args, fmt.Sprintf("--text=%s", text))
	}

	if *verbose {
		log.Printf("Executing: %s %s", g.bdPath, strings.Join(args, " "))
	}

	cmd := exec.Command(g.bdPath, args...)
	cmd.Env = os.Environ() // Inherit environment

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bd command failed: %v\nOutput: %s", err, string(output))
	}

	if *verbose {
		log.Printf("bd response: %s", string(output))
	}

	return nil
}

// sendError sends a JSON error response
func sendError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func main() {
	flag.Parse()

	log.Printf("Starting dialog-gateway on :%d", *port)
	log.Printf("Dialog client address: %s", *dialogAddr)
	log.Printf("bd path: %s", *bdPath)

	gateway := NewGateway(*dialogAddr, *bdPath)

	// Try initial connection
	if err := gateway.Connect(); err != nil {
		log.Printf("Warning: Could not connect to dialog-client: %v", err)
		log.Printf("Gateway will retry on each request")
	} else {
		log.Printf("Connected to dialog-client")
	}

	// Setup HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", gateway.handleHealth)
	mux.HandleFunc("/api/decisions/webhook", gateway.handleWebhook)
	mux.HandleFunc("/api/dialogs", gateway.handleDialog)

	// Simple index page
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Dialog Gateway</title></head>
<body>
<h1>Dialog Gateway</h1>
<p>Endpoints:</p>
<ul>
  <li><a href="/api/health">GET /api/health</a> - Health check</li>
  <li>POST /api/decisions/webhook - Receive decision notifications</li>
  <li>POST /api/dialogs - Direct dialog testing</li>
</ul>
</body>
</html>`)
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 15 * time.Minute, // Long timeout for dialogs
	}

	// Handle shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("Shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
		gateway.Close()
	}()

	log.Printf("Ready - listening on http://localhost:%d", *port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
