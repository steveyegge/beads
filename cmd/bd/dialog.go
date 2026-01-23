package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// Dialog command flags
var (
	// Client flags
	dialogClientHost    string
	dialogClientPort    int
	dialogClientCLI     bool
	dialogClientKeyPath string

	// Server flags
	dialogServerPort       int
	dialogServerDialogAddr string
	dialogServerBdPath     string
	dialogServerVerbose    bool
)

var dialogCmd = &cobra.Command{
	Use:     "dialog",
	GroupID: "issues",
	Short:   "Dialog UI for decision points",
	Long: `Dialog system for human decision points.

The dialog system allows decision points to be presented to humans via
native UI dialogs (MacOS) or CLI prompts (Linux).

ARCHITECTURE:
  MacOS Laptop              EC2 Server                    bd daemon
  +---------------+   SSH   +------------------+   RPC    +------------+
  | dialog client |<------->| dialog server    |<-------->| (bd.sock)  |
  | (port 9876)   | tunnel  | (HTTP :8090)     |  socket  |            |
  +---------------+         +------------------+          +------------+

COMMANDS:
  client    Run dialog client (on local machine)
  server    Run dialog server/gateway (on EC2)`,
}

var dialogClientCmd = &cobra.Command{
	Use:   "client",
	Short: "Run dialog client (receives and displays dialogs)",
	Long: `Run the dialog client to receive and display decision dialogs.

On MacOS, dialogs are displayed using native osascript.
On Linux (or with --cli), prompts are shown in the terminal.

The client listens on a TCP port. When run with -host, it establishes
an SSH reverse tunnel so the EC2 server can connect to it.

Examples:
  bd dialog client -host ubuntu@ec2-host    # MacOS with SSH tunnel
  bd dialog client --cli                    # CLI mode for Linux testing
  bd dialog client --local                  # Local only, no SSH tunnel`,
	RunE: runDialogClient,
}

var dialogServerCmd = &cobra.Command{
	Use:     "server",
	Aliases: []string{"gateway"},
	Short:   "Run dialog server/gateway (HTTP webhook handler)",
	Long: `Run the dialog gateway HTTP server.

The server receives webhook notifications from 'bd decision create',
converts them to dialog requests, sends to the dialog client, and
records responses via 'bd decision respond'.

Endpoints:
  GET  /api/health               Health check
  POST /api/decisions/webhook    Receive decision notifications
  POST /api/dialogs              Direct dialog testing

Examples:
  bd dialog server                          # Default settings
  bd dialog server -v                       # Verbose logging
  bd dialog server --port 8090 --bd bd      # Custom port and bd path`,
	RunE: runDialogServer,
}

func init() {
	// Client flags
	dialogClientCmd.Flags().StringVarP(&dialogClientHost, "host", "H", "", "SSH host (user@hostname)")
	dialogClientCmd.Flags().IntVarP(&dialogClientPort, "port", "p", 9876, "Port for dialog requests")
	dialogClientCmd.Flags().BoolVar(&dialogClientCLI, "cli", false, "Use CLI prompts instead of osascript")
	dialogClientCmd.Flags().BoolVar(&dialogClientCLI, "local", false, "Run without SSH tunnel (alias for --cli)")
	dialogClientCmd.Flags().StringVarP(&dialogClientKeyPath, "key", "k", "", "Path to SSH private key")

	// Server flags
	dialogServerCmd.Flags().IntVarP(&dialogServerPort, "port", "p", 8090, "HTTP port to listen on")
	dialogServerCmd.Flags().StringVar(&dialogServerDialogAddr, "dialog-addr", "127.0.0.1:9876", "Address of dialog client")
	dialogServerCmd.Flags().StringVar(&dialogServerBdPath, "bd", "bd", "Path to bd binary")
	dialogServerCmd.Flags().BoolVarP(&dialogServerVerbose, "verbose", "v", false, "Enable verbose logging")

	dialogCmd.AddCommand(dialogClientCmd)
	dialogCmd.AddCommand(dialogServerCmd)
	rootCmd.AddCommand(dialogCmd)
}

// =============================================================================
// Dialog Client Implementation
// =============================================================================

// DialogRequest is sent from server to request a dialog
type DialogRequest struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"` // "entry", "choice", "confirm"
	Title   string         `json:"title"`
	Prompt  string         `json:"prompt"`
	Options []DialogOption `json:"options,omitempty"`
	Default string         `json:"default,omitempty"`
}

// DialogOption for choice dialogs
type DialogOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// DialogResponse is sent back to server
type DialogResponse struct {
	ID        string `json:"id"`
	Cancelled bool   `json:"cancelled"`
	Text      string `json:"text,omitempty"`
	Selected  string `json:"selected,omitempty"`
	Error     string `json:"error,omitempty"`
}

var dialogStdinReader *bufio.Reader

func runDialogClient(cmd *cobra.Command, args []string) error {
	if dialogClientHost == "" && !dialogClientCLI {
		return fmt.Errorf("requires -host or --cli flag")
	}

	// In CLI mode, always run local
	if dialogClientCLI {
		dialogStdinReader = bufio.NewReader(os.Stdin)
		fmt.Println("Running in CLI mode (terminal prompts)")
	}

	// Start listener
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", dialogClientPort))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", dialogClientPort, err)
	}
	defer listener.Close()
	fmt.Printf("Listening on 127.0.0.1:%d\n", dialogClientPort)

	var sshCmd *exec.Cmd
	if dialogClientHost != "" && !dialogClientCLI {
		// Establish SSH reverse tunnel
		sshArgs := []string{
			"-N", "-T",
			"-o", "ExitOnForwardFailure=yes",
			"-o", "ServerAliveInterval=30",
			"-o", "ServerAliveCountMax=3",
			"-R", fmt.Sprintf("%d:127.0.0.1:%d", dialogClientPort, dialogClientPort),
		}
		if dialogClientKeyPath != "" {
			sshArgs = append(sshArgs, "-i", dialogClientKeyPath)
		}
		sshArgs = append(sshArgs, dialogClientHost)

		sshCmd = exec.Command("ssh", sshArgs...)
		sshCmd.Stdout = os.Stdout
		sshCmd.Stderr = os.Stderr

		if err := sshCmd.Start(); err != nil {
			return fmt.Errorf("failed to start SSH tunnel: %w", err)
		}
		fmt.Printf("SSH tunnel established to %s (remote port %d)\n", dialogClientHost, dialogClientPort)

		// Handle cleanup
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			fmt.Println("\nShutting down...")
			if sshCmd.Process != nil {
				sshCmd.Process.Kill()
			}
			listener.Close()
			os.Exit(0)
		}()
	}

	fmt.Println("Ready for dialog requests...")

	// Accept connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			if strings.Contains(err.Error(), "use of closed") {
				break
			}
			fmt.Fprintf(os.Stderr, "Accept error: %v\n", err)
			continue
		}
		go handleDialogConnection(conn)
	}

	if sshCmd != nil {
		sshCmd.Wait()
	}
	return nil
}

func handleDialogConnection(conn net.Conn) {
	defer conn.Close()
	fmt.Printf("Connection from %s\n", conn.RemoteAddr())

	reader := bufio.NewReader(conn)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "Read error: %v\n", err)
			}
			return
		}

		var req DialogRequest
		if err := json.Unmarshal(line, &req); err != nil {
			fmt.Fprintf(os.Stderr, "Invalid JSON: %v\n", err)
			sendDialogError(conn, "", fmt.Sprintf("invalid JSON: %v", err))
			continue
		}

		fmt.Printf("Dialog request: %s (%s)\n", req.ID, req.Type)

		// Show dialog and get response
		var resp DialogResponse
		if dialogClientCLI {
			resp = showDialogCLI(req)
		} else {
			resp = showDialogOSA(req)
		}

		// Send response
		respJSON, _ := json.Marshal(resp)
		conn.Write(append(respJSON, '\n'))
	}
}

func sendDialogError(conn net.Conn, id, errMsg string) {
	resp := DialogResponse{ID: id, Error: errMsg}
	respJSON, _ := json.Marshal(resp)
	conn.Write(append(respJSON, '\n'))
}

func showDialogCLI(req DialogRequest) DialogResponse {
	resp := DialogResponse{ID: req.ID}

	fmt.Println()
	fmt.Println("════════════════════════════════════════")
	fmt.Printf("  %s\n", req.Title)
	fmt.Println("════════════════════════════════════════")
	fmt.Printf("\n%s\n\n", req.Prompt)

	switch req.Type {
	case "entry":
		fmt.Print("Enter text (or 'cancel'): ")
		input, err := dialogStdinReader.ReadString('\n')
		if err != nil {
			resp.Error = fmt.Sprintf("read error: %v", err)
			return resp
		}
		input = strings.TrimSpace(input)
		if strings.ToLower(input) == "cancel" {
			resp.Cancelled = true
		} else {
			resp.Text = input
		}

	case "choice":
		for i, opt := range req.Options {
			defaultMark := ""
			if opt.ID == req.Default {
				defaultMark = " (default)"
			}
			fmt.Printf("  [%d] %s - %s%s\n", i+1, opt.ID, opt.Label, defaultMark)
		}
		fmt.Println("  [0] Cancel")
		fmt.Print("\nSelect option (number or ID): ")

		input, err := dialogStdinReader.ReadString('\n')
		if err != nil {
			resp.Error = fmt.Sprintf("read error: %v", err)
			return resp
		}
		input = strings.TrimSpace(input)

		if input == "0" || strings.ToLower(input) == "cancel" {
			resp.Cancelled = true
			return resp
		}

		// Try as number first
		var num int
		if _, err := fmt.Sscanf(input, "%d", &num); err == nil && num >= 1 && num <= len(req.Options) {
			resp.Selected = req.Options[num-1].ID
			return resp
		}

		// Try as option ID
		for _, opt := range req.Options {
			if strings.EqualFold(opt.ID, input) {
				resp.Selected = opt.ID
				return resp
			}
		}

		// Use default if empty input
		if input == "" && req.Default != "" {
			resp.Selected = req.Default
			return resp
		}

		resp.Error = fmt.Sprintf("invalid selection: %s", input)

	case "confirm":
		fmt.Print("Confirm? [y]es / [n]o / [c]ancel: ")
		input, err := dialogStdinReader.ReadString('\n')
		if err != nil {
			resp.Error = fmt.Sprintf("read error: %v", err)
			return resp
		}
		input = strings.ToLower(strings.TrimSpace(input))

		switch input {
		case "y", "yes":
			resp.Selected = "Yes"
		case "n", "no":
			resp.Selected = "No"
		case "c", "cancel", "":
			resp.Cancelled = true
		default:
			resp.Error = fmt.Sprintf("invalid input: %s", input)
		}

	default:
		resp.Error = fmt.Sprintf("unknown dialog type: %s", req.Type)
	}

	return resp
}

func showDialogOSA(req DialogRequest) DialogResponse {
	resp := DialogResponse{ID: req.ID}

	var script string
	switch req.Type {
	case "entry":
		script = buildEntryScript(req)
	case "choice":
		script = buildChoiceScript(req)
	case "confirm":
		script = buildConfirmScript(req)
	default:
		resp.Error = fmt.Sprintf("unknown dialog type: %s", req.Type)
		return resp
	}

	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.Output()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			resp.Cancelled = true
			return resp
		}
		resp.Error = fmt.Sprintf("osascript error: %v", err)
		return resp
	}

	result := strings.TrimSpace(string(output))

	switch req.Type {
	case "entry":
		resp.Text = result
	case "choice":
		for _, opt := range req.Options {
			if opt.Label == result {
				resp.Selected = opt.ID
				break
			}
		}
		if resp.Selected == "" {
			resp.Selected = result
		}
	case "confirm":
		resp.Selected = result
	}

	return resp
}

func buildEntryScript(req DialogRequest) string {
	return fmt.Sprintf(`set theResponse to display dialog "%s" with title "%s" default answer "%s" buttons {"Cancel", "OK"} default button "OK"
text returned of theResponse`,
		escapeAS(req.Prompt), escapeAS(req.Title), escapeAS(req.Default))
}

func buildChoiceScript(req DialogRequest) string {
	if len(req.Options) > 3 {
		var items []string
		for _, opt := range req.Options {
			items = append(items, fmt.Sprintf(`"%s"`, escapeAS(opt.Label)))
		}
		return fmt.Sprintf(`choose from list {%s} with title "%s" with prompt "%s"
item 1 of result`,
			strings.Join(items, ", "), escapeAS(req.Title), escapeAS(req.Prompt))
	}

	var buttons []string
	for i, opt := range req.Options {
		if i >= 3 {
			break
		}
		buttons = append(buttons, escapeAS(opt.Label))
	}
	buttonList := `{"` + strings.Join(buttons, `", "`) + `"}`
	return fmt.Sprintf(`set theResponse to display dialog "%s" with title "%s" buttons %s default button %d
button returned of theResponse`,
		escapeAS(req.Prompt), escapeAS(req.Title), buttonList, len(buttons))
}

func buildConfirmScript(req DialogRequest) string {
	return fmt.Sprintf(`set theResponse to display dialog "%s" with title "%s" buttons {"Cancel", "No", "Yes"} default button "Yes"
button returned of theResponse`,
		escapeAS(req.Prompt), escapeAS(req.Title))
}

func escapeAS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// =============================================================================
// Dialog Server Implementation
// =============================================================================

// DecisionPayload matches notification.DecisionPayload
type DecisionPayload struct {
	Type      string                 `json:"type"`
	ID        string                 `json:"id"`
	Prompt    string                 `json:"prompt"`
	Options   []DecisionPayloadOption `json:"options"`
	Default   string                 `json:"default"`
	TimeoutAt *time.Time             `json:"timeout_at"`
}

type DecisionPayloadOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// DialogGateway handles webhook notifications and dialog interactions
type DialogGateway struct {
	dialogAddr string
	bdPath     string
	verbose    bool
	conn       net.Conn
	reader     *bufio.Reader
	mu         sync.Mutex
	connected  bool
}

func runDialogServer(cmd *cobra.Command, args []string) error {
	fmt.Printf("Starting dialog server on :%d\n", dialogServerPort)
	fmt.Printf("Dialog client address: %s\n", dialogServerDialogAddr)
	fmt.Printf("bd path: %s\n", dialogServerBdPath)

	gateway := &DialogGateway{
		dialogAddr: dialogServerDialogAddr,
		bdPath:     dialogServerBdPath,
		verbose:    dialogServerVerbose,
	}

	// Try initial connection
	if err := gateway.connect(); err != nil {
		fmt.Printf("Warning: Could not connect to dialog client: %v\n", err)
		fmt.Println("Server will retry on each request")
	} else {
		fmt.Println("Connected to dialog client")
	}

	// Setup HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", gateway.handleHealth)
	mux.HandleFunc("/api/decisions/webhook", gateway.handleWebhook)
	mux.HandleFunc("/api/dialogs", gateway.handleDialog)
	mux.HandleFunc("/", gateway.handleIndex)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", dialogServerPort),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 15 * time.Minute,
	}

	// Handle shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		fmt.Println("Shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
		gateway.close()
	}()

	fmt.Printf("Ready - listening on http://localhost:%d\n", dialogServerPort)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

func (g *DialogGateway) connect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.connected {
		return nil
	}

	conn, err := net.DialTimeout("tcp", g.dialogAddr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to dialog client at %s: %w", g.dialogAddr, err)
	}

	g.conn = conn
	g.reader = bufio.NewReader(conn)
	g.connected = true
	return nil
}

func (g *DialogGateway) close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.connected = false
	if g.conn != nil {
		return g.conn.Close()
	}
	return nil
}

func (g *DialogGateway) send(req *DialogRequest) (*DialogResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.connected || g.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	g.conn.SetDeadline(time.Now().Add(10 * time.Minute))
	defer g.conn.SetDeadline(time.Time{})

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if _, err := g.conn.Write(append(reqJSON, '\n')); err != nil {
		g.connected = false
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	line, err := g.reader.ReadBytes('\n')
	if err != nil {
		g.connected = false
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp DialogResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &resp, nil
}

func (g *DialogGateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	bdAvailable := false
	if _, err := exec.LookPath(g.bdPath); err == nil {
		bdAvailable = true
	}

	health := map[string]interface{}{
		"status":        "ok",
		"dialog_client": g.connected,
		"bd_available":  bdAvailable,
		"bd_path":       g.bdPath,
	}
	if !g.connected || !bdAvailable {
		health["status"] = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func (g *DialogGateway) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	start := time.Now()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		sendGatewayJSONError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var payload DecisionPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		sendGatewayJSONError(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if g.verbose {
		fmt.Printf("Received webhook for decision %s: %s\n", payload.ID, payload.Prompt)
	}

	if err := g.connect(); err != nil {
		sendGatewayJSONError(w, fmt.Sprintf("Dialog client not available: %v", err), http.StatusServiceUnavailable)
		return
	}

	// Convert to dialog request
	dialogReq := &DialogRequest{
		ID:     payload.ID,
		Type:   "choice",
		Title:  "Decision Required",
		Prompt: payload.Prompt,
	}
	for _, opt := range payload.Options {
		dialogReq.Options = append(dialogReq.Options, DialogOption{ID: opt.ID, Label: opt.Label})
	}
	if payload.Default != "" {
		dialogReq.Default = payload.Default
	}
	if payload.TimeoutAt != nil {
		remaining := time.Until(*payload.TimeoutAt)
		if remaining > 0 {
			dialogReq.Prompt += fmt.Sprintf("\n\n(Timeout in %s, default: %s)",
				remaining.Round(time.Minute), payload.Default)
		}
	}

	if g.verbose {
		fmt.Printf("Showing dialog for %s\n", payload.ID)
	}

	resp, err := g.send(dialogReq)
	if err != nil {
		sendGatewayJSONError(w, fmt.Sprintf("Dialog error: %v", err), http.StatusInternalServerError)
		return
	}

	if g.verbose {
		fmt.Printf("Dialog response for %s: cancelled=%v selected=%q\n", payload.ID, resp.Cancelled, resp.Selected)
	}

	result := map[string]interface{}{
		"decision_id":   payload.ID,
		"cancelled":     resp.Cancelled,
		"selected":      resp.Selected,
		"response_time": time.Since(start).String(),
	}

	if resp.Cancelled {
		result["success"] = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
		return
	}

	if resp.Error != "" {
		sendGatewayJSONError(w, fmt.Sprintf("Dialog error: %s", resp.Error), http.StatusInternalServerError)
		return
	}

	// Record response via bd CLI
	if err := g.recordResponse(payload.ID, resp.Selected, resp.Text); err != nil {
		result["success"] = false
		result["error"] = err.Error()
	} else {
		result["success"] = true
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (g *DialogGateway) handleDialog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		sendGatewayJSONError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var req DialogRequest
	if err := json.Unmarshal(body, &req); err != nil {
		sendGatewayJSONError(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if err := g.connect(); err != nil {
		sendGatewayJSONError(w, fmt.Sprintf("Dialog client not available: %v", err), http.StatusServiceUnavailable)
		return
	}

	resp, err := g.send(&req)
	if err != nil {
		sendGatewayJSONError(w, fmt.Sprintf("Dialog error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (g *DialogGateway) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<!DOCTYPE html>
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
}

func (g *DialogGateway) recordResponse(decisionID, selected, text string) error {
	args := []string{"decision", "respond", decisionID}
	if selected != "" {
		args = append(args, fmt.Sprintf("--select=%s", selected))
	}
	if text != "" {
		args = append(args, fmt.Sprintf("--text=%s", text))
	}

	if g.verbose {
		fmt.Printf("Executing: %s %s\n", g.bdPath, strings.Join(args, " "))
	}

	cmd := exec.Command(g.bdPath, args...)
	cmd.Env = os.Environ()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bd command failed: %v\nOutput: %s", err, string(output))
	}

	if g.verbose {
		fmt.Printf("bd response: %s\n", string(output))
	}

	return nil
}

func sendGatewayJSONError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
