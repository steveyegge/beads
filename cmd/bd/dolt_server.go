package main

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/ui"
)

var doltStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a local Dolt SQL server",
	Long: `Start a local Dolt SQL server for database operations.

The server runs in the background and stores its PID in .beads/dolt/sql-server.pid.
Logs are written to .beads/dolt/sql-server.log.

This is idempotent — if a server is already running on the configured port, this
command does nothing.`,
	Run: func(cmd *cobra.Command, args []string) {
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			fmt.Fprintf(os.Stderr, "Error: not in a beads repository (no .beads directory found)\n")
			fmt.Fprintf(os.Stderr, "Run 'bd init' first.\n")
			os.Exit(1)
		}

		cfg, err := configfile.Load(beadsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		if cfg == nil {
			cfg = configfile.DefaultConfig()
		}

		host := cfg.GetDoltServerHost()
		port := cfg.GetDoltServerPort()
		dataDir := filepath.Join(beadsDir, "dolt")

		if err := startLocalDoltServer(dataDir, host, port, false); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

var doltStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the local Dolt SQL server",
	Long: `Stop the local Dolt SQL server that was started by 'bd dolt start' or 'bd init'.

Reads the PID from .beads/dolt/sql-server.pid and terminates the server process.`,
	Run: func(cmd *cobra.Command, args []string) {
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			fmt.Fprintf(os.Stderr, "Error: not in a beads repository (no .beads directory found)\n")
			os.Exit(1)
		}

		pidPath := filepath.Join(beadsDir, "dolt", "sql-server.pid")
		if err := stopLocalDoltServer(pidPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  %s Dolt server stopped\n", ui.RenderPass("✓"))
	},
}

func init() {
	doltCmd.AddCommand(doltStartCmd)
	doltCmd.AddCommand(doltStopCmd)
}

// isLocalHost returns true if the host refers to the local machine.
func isLocalHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1" || host == ""
}

// isDoltServerReachable checks if a Dolt server is accepting MySQL connections
// on the given host:port.
func isDoltServerReachable(host string, port int) bool {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// waitForDoltServer polls until the Dolt server accepts a MySQL connection.
func waitForDoltServer(host string, port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	dsn := fmt.Sprintf("root@tcp(%s:%d)/?timeout=1s", host, port)
	for time.Now().Before(deadline) {
		db, err := sql.Open("mysql", dsn)
		if err == nil {
			if err := db.Ping(); err == nil {
				_ = db.Close()
				return true
			}
			_ = db.Close()
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// ensureDoltDataDir initializes a Dolt data directory if not already initialized.
// Uses DOLT_ROOT_PATH to avoid polluting user's global Dolt config.
func ensureDoltDataDir(dataDir string) error {
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Already initialized?
	doltDir := filepath.Join(dataDir, ".dolt")
	if info, err := os.Stat(doltDir); err == nil && info.IsDir() {
		return nil
	}

	// Derive identity from git config, falling back to defaults
	name := gitConfigValue("user.name")
	if name == "" {
		name = "beads"
	}
	email := gitConfigValue("user.email")
	if email == "" {
		email = "beads@local"
	}

	// Use DOLT_ROOT_PATH to isolate dolt config
	doltEnv := append(os.Environ(), "DOLT_ROOT_PATH="+dataDir)

	// Configure dolt identity (required by dolt init)
	for _, args := range [][]string{
		{"dolt", "config", "--global", "--add", "user.name", name},
		{"dolt", "config", "--global", "--add", "user.email", email},
	} {
		cfgCmd := exec.Command(args[0], args[1:]...) // #nosec G204 -- fixed dolt commands
		cfgCmd.Env = doltEnv
		if out, err := cfgCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("dolt config failed (%s): %v\n%s", args[3], err, out)
		}
	}

	initCmd := exec.Command("dolt", "init")
	initCmd.Dir = dataDir
	initCmd.Env = doltEnv
	if out, err := initCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("dolt init failed: %v\n%s", err, out)
	}

	return nil
}

// gitConfigValue reads a git config value, returning "" if not available.
func gitConfigValue(key string) string {
	cmd := exec.Command("git", "config", "--get", key)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// startLocalDoltServer starts a Dolt SQL server in the background.
// It is idempotent — if a server is already reachable, it does nothing.
func startLocalDoltServer(dataDir, host string, port int, quiet bool) error {
	// Already running?
	if isDoltServerReachable(host, port) {
		if !quiet {
			fmt.Printf("  %s Dolt server already running on %s:%d\n", ui.RenderPass("✓"), host, port)
		}
		return nil
	}

	// Check for dolt binary
	doltPath, err := exec.LookPath("dolt")
	if err != nil {
		return fmt.Errorf("dolt binary not found in PATH\n\n" +
			"Beads requires Dolt for database operations. Install it:\n" +
			"  https://docs.dolthub.com/introduction/installation\n\n" +
			"On Windows:  winget install DoltHub.Dolt\n" +
			"On macOS:    brew install dolt\n" +
			"On Linux:    curl -L https://github.com/dolthub/dolt/releases/latest/download/install.sh | bash")
	}

	// Handle stale PID file
	pidPath := filepath.Join(dataDir, "sql-server.pid")
	if _, err := os.Stat(pidPath); err == nil {
		// PID file exists but server not reachable — stale
		_ = os.Remove(pidPath)
	}

	// Initialize data directory
	if err := ensureDoltDataDir(dataDir); err != nil {
		return fmt.Errorf("failed to initialize Dolt data directory: %w", err)
	}

	// Open log file
	logPath := filepath.Join(dataDir, "sql-server.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", logPath, err)
	}

	// Start server
	// Use short flags (-H, -P) for cross-version compatibility.
	// Skip --user (removed in newer dolt versions; root@localhost created by default).
	serverCmd := exec.Command(doltPath, "sql-server",
		"-H", host,
		"-P", strconv.Itoa(port),
		"--no-auto-commit",
	)
	serverCmd.Dir = dataDir
	serverCmd.Env = append(os.Environ(), "DOLT_ROOT_PATH="+dataDir)
	serverCmd.Stdout = logFile
	serverCmd.Stderr = logFile

	// Platform-specific: detach the server process so it survives after bd exits
	configureBackgroundProcess(serverCmd)

	if err := serverCmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("failed to start dolt sql-server: %w\nCheck logs at: %s", err, logPath)
	}

	// Close the log file handle — the child process has inherited it
	_ = logFile.Close()

	// Write PID file
	pid := serverCmd.Process.Pid
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0600); err != nil {
		// Non-fatal: server is running, just can't track PID
		if !quiet {
			fmt.Fprintf(os.Stderr, "Warning: failed to write PID file: %v\n", err)
		}
	}

	// Release the process handle so it isn't waited on
	_ = serverCmd.Process.Release()

	// Wait for readiness
	if !waitForDoltServer(host, port, 15*time.Second) {
		return fmt.Errorf("dolt sql-server started but not responding on %s:%d after 15s\nCheck logs at: %s", host, port, logPath)
	}

	if !quiet {
		fmt.Printf("  %s Dolt server started on %s:%d (pid %d)\n", ui.RenderPass("✓"), host, port, pid)
	}
	return nil
}

// stopLocalDoltServer stops a Dolt server using its PID file.
func stopLocalDoltServer(pidPath string) error {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no PID file found at %s — server may not be running", pidPath)
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		_ = os.Remove(pidPath)
		return fmt.Errorf("invalid PID in %s: %w", pidPath, err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		_ = os.Remove(pidPath)
		return fmt.Errorf("process %d not found: %w", pid, err)
	}

	if err := process.Kill(); err != nil {
		// Process may already be dead
		_ = os.Remove(pidPath)
		return nil
	}

	_ = os.Remove(pidPath)
	return nil
}

// ensureDoltServer checks if a local Dolt server is running and starts one if needed.
// Used by bd init to auto-start the server for a frictionless experience.
// Only auto-starts for local hosts; for remote hosts, returns an error with guidance.
func ensureDoltServer(dataDir, host string, port int, quiet bool) error {
	if isDoltServerReachable(host, port) {
		return nil
	}

	if !isLocalHost(host) {
		return fmt.Errorf("Dolt server unreachable at %s:%d\n\n"+
			"The configured server host is not local, so bd cannot auto-start it.\n"+
			"Ensure the remote Dolt server is running and accessible.", host, port)
	}

	if !quiet {
		fmt.Printf("  Starting Dolt server...\n")
	}
	return startLocalDoltServer(dataDir, host, port, quiet)
}
