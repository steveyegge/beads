// dialog-client runs on MacOS laptop, establishes SSH tunnel to EC2,
// listens for dialog requests, displays them via osascript, returns responses.
//
// Usage:
//   dialog-client -host user@ec2-host -port 9876
//
// This creates a reverse SSH tunnel so the EC2 host can connect to localhost:9876
// which forwards to this client.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

// DialogRequest is sent from EC2 to request a dialog
type DialogRequest struct {
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

// DialogResponse is sent back to EC2
type DialogResponse struct {
	ID       string `json:"id"`
	Cancelled bool   `json:"cancelled"`
	Text     string `json:"text,omitempty"`
	Selected string `json:"selected,omitempty"`
	Error    string `json:"error,omitempty"`
}

var (
	host       = flag.String("host", "", "SSH host (user@hostname)")
	port       = flag.Int("port", 9876, "Port for dialog requests")
	localOnly  = flag.Bool("local", false, "Run without SSH tunnel (for testing)")
	cliMode    = flag.Bool("cli", false, "Use CLI prompts instead of osascript (for Linux testing)")
	sshKeyPath = flag.String("key", "", "Path to SSH private key (optional)")
)

var stdinReader *bufio.Reader

func main() {
	flag.Parse()

	if *host == "" && !*localOnly && !*cliMode {
		fmt.Fprintln(os.Stderr, "Usage: dialog-client -host user@ec2-host [-port 9876]")
		fmt.Fprintln(os.Stderr, "       dialog-client -local [-port 9876]  # for local testing")
		fmt.Fprintln(os.Stderr, "       dialog-client -cli [-port 9876]    # CLI mode for Linux")
		os.Exit(1)
	}

	// In CLI mode, always run local
	if *cliMode {
		*localOnly = true
		stdinReader = bufio.NewReader(os.Stdin)
		fmt.Println("Running in CLI mode (terminal prompts)")
	}

	// Start listener first
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to listen on port %d: %v\n", *port, err)
		os.Exit(1)
	}
	defer listener.Close()
	fmt.Printf("Listening on 127.0.0.1:%d\n", *port)

	var sshCmd *exec.Cmd
	if !*localOnly {
		// Establish SSH reverse tunnel
		// -R means: connections to remotePort on EC2 forward to our local listener
		sshArgs := []string{
			"-N", // No remote command
			"-T", // No TTY
			"-o", "ExitOnForwardFailure=yes",
			"-o", "ServerAliveInterval=30",
			"-o", "ServerAliveCountMax=3",
			"-R", fmt.Sprintf("%d:127.0.0.1:%d", *port, *port),
		}
		if *sshKeyPath != "" {
			sshArgs = append(sshArgs, "-i", *sshKeyPath)
		}
		sshArgs = append(sshArgs, *host)

		sshCmd = exec.Command("ssh", sshArgs...)
		sshCmd.Stdout = os.Stdout
		sshCmd.Stderr = os.Stderr

		if err := sshCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start SSH tunnel: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("SSH tunnel established to %s (remote port %d)\n", *host, *port)

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
		go handleConnection(conn)
	}

	if sshCmd != nil {
		sshCmd.Wait()
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	fmt.Printf("Connection from %s\n", conn.RemoteAddr())

	reader := bufio.NewReader(conn)

	for {
		// Read JSON line
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
			sendError(conn, "", fmt.Sprintf("invalid JSON: %v", err))
			continue
		}

		fmt.Printf("Dialog request: %s (%s)\n", req.ID, req.Type)

		// Show dialog and get response
		resp := showDialog(req)

		// Send response
		respJSON, _ := json.Marshal(resp)
		conn.Write(append(respJSON, '\n'))
	}
}

func sendError(conn net.Conn, id, errMsg string) {
	resp := DialogResponse{ID: id, Error: errMsg}
	respJSON, _ := json.Marshal(resp)
	conn.Write(append(respJSON, '\n'))
}

func showDialog(req DialogRequest) DialogResponse {
	if *cliMode {
		return showDialogCLI(req)
	}
	return showDialogOSA(req)
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
		input, err := stdinReader.ReadString('\n')
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

		input, err := stdinReader.ReadString('\n')
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

		// Use default if empty input and default exists
		if input == "" && req.Default != "" {
			resp.Selected = req.Default
			return resp
		}

		resp.Error = fmt.Sprintf("invalid selection: %s", input)

	case "confirm":
		fmt.Print("Confirm? [y]es / [n]o / [c]ancel: ")
		input, err := stdinReader.ReadString('\n')
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

	// Run osascript
	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.Output()

	if err != nil {
		// Check if user cancelled
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
		// Map label back to ID
		for _, opt := range req.Options {
			if opt.Label == result {
				resp.Selected = opt.ID
				break
			}
		}
		if resp.Selected == "" {
			resp.Selected = result // fallback to raw value
		}
	case "confirm":
		resp.Selected = result
	}

	return resp
}

func buildEntryScript(req DialogRequest) string {
	title := escapeAppleScript(req.Title)
	prompt := escapeAppleScript(req.Prompt)
	def := escapeAppleScript(req.Default)

	return fmt.Sprintf(`
set theResponse to display dialog "%s" with title "%s" default answer "%s" buttons {"Cancel", "OK"} default button "OK"
text returned of theResponse
`, prompt, title, def)
}

func buildChoiceScript(req DialogRequest) string {
	title := escapeAppleScript(req.Title)
	prompt := escapeAppleScript(req.Prompt)

	// Build button list (max 3 buttons in AppleScript)
	var buttons []string
	for i, opt := range req.Options {
		if i >= 3 {
			break // AppleScript only supports 3 buttons
		}
		buttons = append(buttons, escapeAppleScript(opt.Label))
	}

	// If more than 3 options, use a list picker instead
	if len(req.Options) > 3 {
		var items []string
		for _, opt := range req.Options {
			items = append(items, fmt.Sprintf(`"%s"`, escapeAppleScript(opt.Label)))
		}
		return fmt.Sprintf(`
choose from list {%s} with title "%s" with prompt "%s"
item 1 of result
`, strings.Join(items, ", "), title, prompt)
	}

	buttonList := `{"` + strings.Join(buttons, `", "`) + `"}`
	return fmt.Sprintf(`
set theResponse to display dialog "%s" with title "%s" buttons %s default button %d
button returned of theResponse
`, prompt, title, buttonList, len(buttons))
}

func buildConfirmScript(req DialogRequest) string {
	title := escapeAppleScript(req.Title)
	prompt := escapeAppleScript(req.Prompt)

	return fmt.Sprintf(`
set theResponse to display dialog "%s" with title "%s" buttons {"Cancel", "No", "Yes"} default button "Yes"
button returned of theResponse
`, prompt, title)
}

func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
