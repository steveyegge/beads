package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/rpc"
)

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the background daemon",
	Long: `Start the background daemon that serves as the central RPC server for bd operations.

Most bd commands communicate with the daemon via RPC (Unix socket or TCP).
Remote clients connect using BD_DAEMON_HOST and BD_DAEMON_TOKEN.

The daemon will:
- Serve RPC requests from bd CLI clients
- Poll for changes at configurable intervals (default: 5 seconds)
- Export pending database changes to JSONL
- Auto-commit changes if --auto-commit flag set
- Auto-push commits if --auto-push flag set
- Pull remote changes periodically
- Auto-import when remote changes detected

Federation mode (--federation):
- Starts dolt sql-server for multi-writer support
- Exposes remotesapi on port 8080 for peer-to-peer push/pull
- Enables real-time sync between Gas Towns

Examples:
  bd daemon start                    # Start with defaults
  bd daemon start --auto-commit      # Enable auto-commit
  bd daemon start --auto-push        # Enable auto-push (implies --auto-commit)
  bd daemon start --foreground       # Run in foreground (for systemd/supervisord)
  bd daemon start --federation       # Enable federation mode (dolt sql-server)
  bd daemon start --tcp-addr :9876   # Listen on TCP for remote clients
  bd daemon start --http-addr :9080  # Enable Connect-RPC HTTP API`,
	PreRunE: guardDaemonStartForDolt,
	Run: func(cmd *cobra.Command, args []string) {
		// Check if BD_DAEMON_HOST is set - refuse to start local daemon when configured for remote
		if remoteHost := os.Getenv("BD_DAEMON_HOST"); remoteHost != "" {
			fmt.Fprintf(os.Stderr, "Error: BD_DAEMON_HOST is set (%s)\n", remoteHost)
			fmt.Fprintf(os.Stderr, "Cannot start a local daemon when configured for remote daemon.\n")
			fmt.Fprintf(os.Stderr, "Hint: Use 'bd daemon status' to check the remote daemon, or unset BD_DAEMON_HOST to use a local daemon.\n")
			os.Exit(1)
		}

		interval, _ := cmd.Flags().GetDuration("interval")
		autoCommit, _ := cmd.Flags().GetBool("auto-commit")
		autoPush, _ := cmd.Flags().GetBool("auto-push")
		autoPull, _ := cmd.Flags().GetBool("auto-pull")
		localMode, _ := cmd.Flags().GetBool("local")
		logFile, _ := cmd.Flags().GetString("log")
		foreground, _ := cmd.Flags().GetBool("foreground")
		logLevel, _ := cmd.Flags().GetString("log-level")
		logJSON, _ := cmd.Flags().GetBool("log-json")
		federation, _ := cmd.Flags().GetBool("federation")
		federationPort, _ := cmd.Flags().GetInt("federation-port")
		remotesapiPort, _ := cmd.Flags().GetInt("remotesapi-port")
		tcpAddr, _ := cmd.Flags().GetString("tcp-addr")
		tlsCert, _ := cmd.Flags().GetString("tls-cert")
		tlsKey, _ := cmd.Flags().GetString("tls-key")

		// Also check environment variables for TCP/TLS config
		if tcpAddr == "" {
			tcpAddr = os.Getenv("BD_DAEMON_TCP_ADDR")
		}
		if tlsCert == "" {
			tlsCert = os.Getenv("BD_DAEMON_TLS_CERT")
		}
		if tlsKey == "" {
			tlsKey = os.Getenv("BD_DAEMON_TLS_KEY")
		}

		// Validate TLS config: both cert and key must be provided together
		if (tlsCert != "" && tlsKey == "") || (tlsCert == "" && tlsKey != "") {
			fmt.Fprintf(os.Stderr, "Error: --tls-cert and --tls-key must both be provided\n")
			os.Exit(1)
		}

		// TLS requires TCP address
		if tlsCert != "" && tcpAddr == "" {
			fmt.Fprintf(os.Stderr, "Error: --tcp-addr is required when using TLS\n")
			os.Exit(1)
		}

		// Get TCP token for authentication
		tcpToken, _ := cmd.Flags().GetString("tcp-token")
		if tcpToken == "" {
			tcpToken = os.Getenv("BD_DAEMON_TOKEN")
		}

		// Get HTTP address for Connect-RPC style API
		httpAddr, _ := cmd.Flags().GetString("http-addr")
		if httpAddr == "" {
			httpAddr = os.Getenv("BD_DAEMON_HTTP_ADDR")
		}

		// NOTE: Only load daemon auto-settings from the database in foreground mode.
		//
		// In background mode, `bd daemon start` spawns a child process to run the
		// daemon loop. Opening the database here in the parent process can briefly
		// hold Dolt's LOCK file long enough for the child to time out and fall back
		// to read-only mode (100ms lock timeout), which can break startup.
		//
		// In background mode, auto-settings are loaded in the actual daemon process
		// (the BD_DAEMON_FOREGROUND=1 child spawned by startDaemon).
		if foreground {
			autoCommit, autoPush, autoPull = loadDaemonAutoSettings(cmd, autoCommit, autoPush, autoPull)
		} else {
			// In background mode, load YAML/env settings for the startup message
			// (full settings including database are loaded in the child process)
			if yamlCommit, yamlPush, yamlPull, hasSettings := loadYAMLDaemonSettings(); hasSettings {
				autoCommit, autoPush, autoPull = yamlCommit, yamlPush, yamlPull
			}
		}

		if interval <= 0 {
			fmt.Fprintf(os.Stderr, "Error: interval must be positive (got %v)\n", interval)
			os.Exit(1)
		}

		pidFile, err := getPIDFilePath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Skip daemon-running check if we're the forked child (BD_DAEMON_FOREGROUND=1)
		if os.Getenv("BD_DAEMON_FOREGROUND") != "1" {
			// Check if daemon is already running
			if isRunning, pid := isDaemonRunning(pidFile); isRunning {
				// Check if running daemon has compatible version
				socketPath := getSocketPathForPID(pidFile)
				if client, err := rpc.TryConnectWithTimeout(socketPath, 1*time.Second); err == nil && client != nil {
					health, healthErr := client.Health()
					_ = client.Close()

					// If we can check version and it's compatible, exit successfully (idempotent)
					if healthErr == nil && health.Compatible {
						fmt.Printf("Daemon already running (PID %d, version %s)\n", pid, health.Version)
						os.Exit(0)
					}

					// Version mismatch - auto-stop old daemon
					if healthErr == nil && !health.Compatible {
						fmt.Fprintf(os.Stderr, "Warning: daemon version mismatch (daemon: %s, client: %s)\n", health.Version, Version)
						fmt.Fprintf(os.Stderr, "Stopping old daemon and starting new one...\n")
						stopDaemon(pidFile)
					}
				} else {
					fmt.Fprintf(os.Stderr, "Error: daemon already running (PID %d)\n", pid)
					fmt.Fprintf(os.Stderr, "Use 'bd daemon stop' to stop it first\n")
					os.Exit(1)
				}
			}
		}

		// Validate --local mode constraints
		if localMode {
			if autoCommit {
				fmt.Fprintf(os.Stderr, "Error: --auto-commit cannot be used with --local mode\n")
				fmt.Fprintf(os.Stderr, "Hint: --local mode runs without git, so commits are not possible\n")
				os.Exit(1)
			}
			if autoPush {
				fmt.Fprintf(os.Stderr, "Error: --auto-push cannot be used with --local mode\n")
				fmt.Fprintf(os.Stderr, "Hint: --local mode runs without git, so pushes are not possible\n")
				os.Exit(1)
			}
		}

		// Validate we're in a git repo (skip in local mode)
		if !localMode && !isGitRepo() {
			fmt.Fprintf(os.Stderr, "Error: not in a git repository\n")
			fmt.Fprintf(os.Stderr, "Hint: run 'git init' to initialize a repository, or use --local for local-only mode\n")
			os.Exit(1)
		}

		// Check for upstream if auto-push enabled
		if autoPush && !gitHasUpstream() {
			fmt.Fprintf(os.Stderr, "Error: no upstream configured (required for --auto-push)\n")
			fmt.Fprintf(os.Stderr, "Hint: git push -u origin <branch-name>\n")
			os.Exit(1)
		}

		// Warn if starting daemon in a git worktree
		if dbPath == "" {
			if foundDB := beads.FindDatabasePath(); foundDB != "" {
				dbPath = foundDB
			}
		}
		if dbPath != "" {
			warnWorktreeDaemon(dbPath)
		}

		// Start daemon
		if localMode {
			fmt.Printf("Starting bd daemon in LOCAL mode (interval: %v, no git sync)\n", interval)
		} else if federation {
			fmt.Printf("Starting bd daemon in FEDERATION mode (interval: %v, dolt sql-server with remotesapi)\n", interval)
		} else {
			fmt.Printf("Starting bd daemon (interval: %v, auto-commit: %v, auto-push: %v, auto-pull: %v)\n",
				interval, autoCommit, autoPush, autoPull)
		}
		if logFile != "" {
			fmt.Printf("Logging to: %s\n", logFile)
		}
		if tcpAddr != "" {
			if tlsCert != "" {
				fmt.Printf("TCP address: %s (TLS enabled)\n", tcpAddr)
			} else {
				fmt.Printf("TCP address: %s\n", tcpAddr)
			}
			if tcpToken != "" {
				fmt.Printf("TCP authentication: token required\n")
			}
		}
		if httpAddr != "" {
			fmt.Printf("HTTP address: %s (Connect-RPC style API)\n", httpAddr)
		}

		startDaemon(interval, autoCommit, autoPush, autoPull, localMode, foreground, logFile, pidFile, logLevel, logJSON, federation, federationPort, remotesapiPort, tcpAddr, tlsCert, tlsKey, tcpToken, httpAddr)
	},
}

func init() {
	daemonStartCmd.Flags().Duration("interval", 5*time.Second, "Sync check interval")
	daemonStartCmd.Flags().Bool("auto-commit", false, "Automatically commit changes")
	daemonStartCmd.Flags().Bool("auto-push", false, "Automatically push commits")
	daemonStartCmd.Flags().Bool("auto-pull", false, "Automatically pull from remote")
	daemonStartCmd.Flags().Bool("local", false, "Run in local-only mode (no git required, no sync)")
	daemonStartCmd.Flags().String("log", "", "Log file path (default: .beads/daemon.log)")
	daemonStartCmd.Flags().Bool("foreground", false, "Run in foreground (don't daemonize)")
	daemonStartCmd.Flags().String("log-level", "info", "Log level (debug, info, warn, error)")
	daemonStartCmd.Flags().Bool("log-json", false, "Output logs in JSON format")
	daemonStartCmd.Flags().Bool("federation", false, "Enable federation mode (runs dolt sql-server)")
	daemonStartCmd.Flags().Int("federation-port", 3306, "MySQL port for federation mode dolt sql-server")
	daemonStartCmd.Flags().Int("remotesapi-port", 8080, "remotesapi port for peer-to-peer sync in federation mode")
	daemonStartCmd.Flags().String("tcp-addr", "", "TCP address to listen on (e.g., :9876 or 0.0.0.0:9876)")
	daemonStartCmd.Flags().String("tls-cert", "", "TLS certificate file for TCP connections")
	daemonStartCmd.Flags().String("tls-key", "", "TLS key file for TCP connections")
	daemonStartCmd.Flags().String("tcp-token", "", "Token for TCP connection authentication (or use BD_DAEMON_TOKEN)")
	daemonStartCmd.Flags().String("http-addr", "", "HTTP address for Connect-RPC style API (e.g., :9080)")
}
