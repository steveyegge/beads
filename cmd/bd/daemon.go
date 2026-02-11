package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/cmd/bd/doctor"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/daemon"
	"github.com/steveyegge/beads/internal/eventbus"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/syncbranch"
)

var daemonCmd = &cobra.Command{
	Use:     "daemon",
	GroupID: "sync",
	Short:   "Manage the bd daemon",
	Long: `Manage the bd daemon that serves as the RPC backend for all bd operations.

The daemon is the central server that bd CLI commands communicate with.
In production (K8s), the daemon runs as a pod and clients connect via
BD_DAEMON_HOST. For local development, it runs as a background process
and clients connect via Unix socket.

The daemon will:
- Serve RPC requests from bd CLI clients (Unix socket, TCP, or HTTP)
- Poll for changes at configurable intervals (default: 5 seconds)
- Export pending database changes to JSONL
- Auto-commit changes if --auto-commit flag set
- Auto-push commits if --auto-push flag set
- Pull remote changes periodically
- Auto-import when remote changes detected

Common operations:
  bd daemon start                Start the daemon (background)
  bd daemon start --foreground   Start in foreground (for systemd/K8s)
  bd daemon stop                 Stop current workspace daemon
  bd daemon status               Show daemon status
  bd daemon status --all         Show all daemons with health check
  bd daemon logs                 View daemon logs
  bd daemon restart              Restart daemon
  bd daemon killall              Stop all running daemons

Run 'bd daemon --help' to see all subcommands.`,
	Run: func(cmd *cobra.Command, args []string) {
		start, _ := cmd.Flags().GetBool("start")
		stop, _ := cmd.Flags().GetBool("stop")
		stopAll, _ := cmd.Flags().GetBool("stop-all")
		status, _ := cmd.Flags().GetBool("status")
		health, _ := cmd.Flags().GetBool("health")
		metrics, _ := cmd.Flags().GetBool("metrics")
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

		// If no operation flags provided, show help
		if !start && !stop && !stopAll && !status && !health && !metrics {
			_ = cmd.Help()
			return
		}

		// Show deprecation warnings for flag-based actions (skip in JSON mode for agent ergonomics)
		if !jsonOutput {
			if start {
				fmt.Fprintf(os.Stderr, "Warning: --start is deprecated, use 'bd daemon start' instead\n")
			}
			if stop {
				fmt.Fprintf(os.Stderr, "Warning: --stop is deprecated, use 'bd daemon stop' instead\n")
			}
			if stopAll {
				fmt.Fprintf(os.Stderr, "Warning: --stop-all is deprecated, use 'bd daemon killall' instead\n")
			}
			if status {
				fmt.Fprintf(os.Stderr, "Warning: --status is deprecated, use 'bd daemon status' instead\n")
			}
			if health {
				fmt.Fprintf(os.Stderr, "Warning: --health is deprecated, use 'bd daemon status --all' instead\n")
			}
		}

		// If auto-commit/auto-push flags weren't explicitly provided, read from config
		// GH#871: Read from config.yaml first (team-shared), then fall back to SQLite (legacy)
		// (skip if --stop, --status, --health, --metrics)
		if start && !stop && !status && !health && !metrics {
			// Load auto-commit/push/pull defaults from env vars, config, or sync-branch
			autoCommit, autoPush, autoPull = loadDaemonAutoSettings(cmd, autoCommit, autoPush, autoPull)
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

		if status {
			showDaemonStatus(pidFile)
			return
		}

		if health {
			showDaemonHealth()
			return
		}

		if metrics {
			showDaemonMetrics()
			return
		}

		if stop {
			stopDaemon(pidFile)
			return
		}

		if stopAll {
			stopAllDaemons()
			return
		}

		// If we get here and --start wasn't provided, something is wrong
		// (should have been caught by help check above)
		if !start {
			fmt.Fprintf(os.Stderr, "Error: --start flag is required to start the daemon\n")
			fmt.Fprintf(os.Stderr, "Run 'bd daemon --help' to see available options\n")
			os.Exit(1)
		}

		// Check if BD_DAEMON_HOST is set - refuse to start local daemon when configured for remote
		if remoteHost := os.Getenv("BD_DAEMON_HOST"); remoteHost != "" {
			fmt.Fprintf(os.Stderr, "Error: BD_DAEMON_HOST is set (%s)\n", remoteHost)
			fmt.Fprintf(os.Stderr, "Cannot start a local daemon when configured for remote daemon.\n")
			fmt.Fprintf(os.Stderr, "Hint: Use 'bd daemon status' to check the remote daemon, or unset BD_DAEMON_HOST to use a local daemon.\n")
			os.Exit(1)
		}

		// Guard: refuse to start daemon with Dolt backend (unless --federation)
		// This matches guardDaemonStartForDolt which guards the 'bd daemon start' subcommand.
		if !federation {
			if err := guardDaemonStartForDolt(cmd, args); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}

		// Skip daemon-running check if we're the forked child (BD_DAEMON_FOREGROUND=1)
		// because the check happens in the parent process before forking
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
						// Continue with daemon startup
					}
				} else {
					// Can't check version - assume incompatible
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
		// When sync-branch is configured, check that branch's upstream instead of current HEAD.
		// This fixes compatibility with jj/jujutsu which always operates in detached HEAD mode.
		if autoPush {
			hasUpstream := false
			if syncBranch := syncbranch.GetFromYAML(); syncBranch != "" {
				// sync-branch configured: check that branch's upstream
				hasUpstream = gitBranchHasUpstream(syncBranch)
			} else {
				// No sync-branch: check current HEAD's upstream (original behavior)
				hasUpstream = gitHasUpstream()
			}
			if !hasUpstream {
				fmt.Fprintf(os.Stderr, "Error: no upstream configured (required for --auto-push)\n")
				fmt.Fprintf(os.Stderr, "Hint: git push -u origin <branch-name>\n")
				os.Exit(1)
			}
		}

		// Warn if starting daemon in a git worktree
		// Ensure dbPath is set for warning
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
		} else {
			fmt.Printf("Starting bd daemon (interval: %v, auto-commit: %v, auto-push: %v, auto-pull: %v)\n",
				interval, autoCommit, autoPush, autoPull)
		}
		if logFile != "" {
			fmt.Printf("Logging to: %s\n", logFile)
		}

		federationPort, _ := cmd.Flags().GetInt("federation-port")
		remotesapiPort, _ := cmd.Flags().GetInt("remotesapi-port")
		tcpAddr, _ := cmd.Flags().GetString("tcp-addr")
		if tcpAddr == "" {
			tcpAddr = os.Getenv("BD_DAEMON_TCP_ADDR")
		}
		tlsCert, _ := cmd.Flags().GetString("tls-cert")
		if tlsCert == "" {
			tlsCert = os.Getenv("BD_DAEMON_TLS_CERT")
		}
		tlsKey, _ := cmd.Flags().GetString("tls-key")
		if tlsKey == "" {
			tlsKey = os.Getenv("BD_DAEMON_TLS_KEY")
		}
		tcpToken, _ := cmd.Flags().GetString("tcp-token")
		if tcpToken == "" {
			tcpToken = os.Getenv("BD_DAEMON_TOKEN")
		}
		httpAddr, _ := cmd.Flags().GetString("http-addr")
		if httpAddr == "" {
			httpAddr = os.Getenv("BD_DAEMON_HTTP_ADDR")
		}
		startDaemon(interval, autoCommit, autoPush, autoPull, localMode, foreground, logFile, pidFile, logLevel, logJSON, federation, federationPort, remotesapiPort, tcpAddr, tlsCert, tlsKey, tcpToken, httpAddr)
	},
}

func init() {
	// Register subcommands (preferred interface)
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	// Note: stop, restart, logs, killall, list, health subcommands are registered in daemons.go

	// Legacy flags (deprecated - use subcommands instead)
	daemonCmd.Flags().Bool("start", false, "Start the daemon (deprecated: use 'bd daemon start')")
	daemonCmd.Flags().Duration("interval", 5*time.Second, "Sync check interval")
	daemonCmd.Flags().Bool("auto-commit", false, "Automatically commit changes")
	daemonCmd.Flags().Bool("auto-push", false, "Automatically push commits")
	daemonCmd.Flags().Bool("auto-pull", false, "Automatically pull from remote (default: true when sync.branch configured)")
	daemonCmd.Flags().Bool("local", false, "Run in local-only mode (no git required, no sync)")
	daemonCmd.Flags().Bool("stop", false, "Stop running daemon (deprecated: use 'bd daemon stop')")
	daemonCmd.Flags().Bool("stop-all", false, "Stop all running bd daemons (deprecated: use 'bd daemon killall')")
	daemonCmd.Flags().Bool("status", false, "Show daemon status (deprecated: use 'bd daemon status')")
	daemonCmd.Flags().Bool("health", false, "Check daemon health (deprecated: use 'bd daemon status --all')")
	daemonCmd.Flags().Bool("metrics", false, "Show detailed daemon metrics")
	daemonCmd.Flags().String("log", "", "Log file path (default: .beads/daemon.log)")
	daemonCmd.Flags().Bool("foreground", false, "Run in foreground (don't daemonize)")
	daemonCmd.Flags().String("log-level", "info", "Log level (debug, info, warn, error)")
	daemonCmd.Flags().Bool("log-json", false, "Output logs in JSON format (structured logging)")
	daemonCmd.Flags().Bool("federation", false, "Enable federation mode (runs dolt sql-server with remotesapi)")
	daemonCmd.Flags().Int("federation-port", 3306, "MySQL port for federation mode dolt sql-server")
	daemonCmd.Flags().Int("remotesapi-port", 8080, "remotesapi port for peer-to-peer sync in federation mode")
	daemonCmd.Flags().String("tcp-addr", "", "TCP address to listen on (e.g., :9876 or 0.0.0.0:9876)")
	daemonCmd.Flags().String("tls-cert", "", "TLS certificate file for TCP connections")
	daemonCmd.Flags().String("tls-key", "", "TLS key file for TCP connections")
	daemonCmd.Flags().String("tcp-token", "", "Token for TCP connection authentication (or use BD_DAEMON_TOKEN)")
	daemonCmd.Flags().String("http-addr", "", "HTTP address for Connect-RPC style API (e.g., :9080)")
	daemonCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output JSON format")
	rootCmd.AddCommand(daemonCmd)
}

// computeDaemonParentPID determines which parent PID the daemon should track.
// When BD_DAEMON_FOREGROUND=1 (used by startDaemon for background CLI launches),
// we return 0 to disable parent tracking, since the short-lived launcher
// process is expected to exit immediately after spawning the daemon.
// In all other cases we track the current OS parent PID.
func computeDaemonParentPID() int {
	if os.Getenv("BD_DAEMON_FOREGROUND") == "1" {
		// 0 means "not tracked" in checkParentProcessAlive
		return 0
	}
	return os.Getppid()
}
func runDaemonLoop(interval time.Duration, autoCommit, autoPush, autoPull, localMode bool, logPath, pidFile, logLevel string, logJSON, federation bool, federationPort, remotesapiPort int, tcpAddr, tlsCert, tlsKey, tcpToken, httpAddr string) {
	level := parseLogLevel(logLevel)
	logF, log := setupDaemonLogger(logPath, logJSON, level)
	defer func() { _ = logF.Close() }()

	// Set up signal-aware context for graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Top-level panic recovery to ensure clean shutdown and diagnostics
	defer func() {
		if r := recover(); r != nil {
			log.Error("daemon crashed", "panic", r)

			// Capture stack trace
			stackBuf := make([]byte, 4096)
			stackSize := runtime.Stack(stackBuf, false)
			stackTrace := string(stackBuf[:stackSize])
			log.Error("stack trace", "trace", stackTrace)

			var beadsDir string
			if dbPath != "" {
				beadsDir = filepath.Dir(dbPath)
			} else if foundDB := beads.FindDatabasePath(); foundDB != "" {
				beadsDir = filepath.Dir(foundDB)
			}

			if beadsDir != "" {
				crashReport := fmt.Sprintf("Daemon crashed at %s\n\nPanic: %v\n\nStack trace:\n%s\n",
					time.Now().Format(time.RFC3339), r, stackTrace)
				log.Error("crash report", "report", crashReport)
			}

			// Clean up PID file
			_ = os.Remove(pidFile)

			log.Info("daemon terminated after panic")
		}
	}()

	// Determine database path first (needed for lock file metadata)
	daemonDBPath := dbPath
	if daemonDBPath == "" {
		if foundDB := beads.FindDatabasePath(); foundDB != "" {
			daemonDBPath = foundDB
		} else if os.Getenv("BEADS_DOLT_SERVER_MODE") == "1" {
			// Server mode: database lives on remote Dolt sql-server, not locally.
			// Create a minimal .beads/dolt directory as a placeholder for path resolution.
			beadsDirEnv := os.Getenv("BEADS_DIR")
			if beadsDirEnv == "" {
				log.Error("BEADS_DOLT_SERVER_MODE=1 requires BEADS_DIR to be set")
				return
			}
			doltDir := filepath.Join(beadsDirEnv, "dolt")
			if err := os.MkdirAll(doltDir, 0755); err != nil {
				log.Error("failed to create server-mode dolt directory", "path", doltDir, "error", err)
				return
			}
			daemonDBPath = doltDir
			log.Info("server mode: using placeholder database path", "path", doltDir)
		} else {
			log.Error("no beads database found")
			log.Info("hint: run 'bd init' to create a database or set BEADS_DB environment variable")
			return // Use return instead of os.Exit to allow defers to run
		}
	}

	lock, err := setupDaemonLock(pidFile, daemonDBPath, log)
	if err != nil {
		return // Use return instead of os.Exit to allow defers to run
	}
	defer func() { _ = lock.Close() }()
	defer func() { _ = os.Remove(pidFile) }()

	if localMode {
		log.log("Daemon started in LOCAL mode (interval: %v, no git sync)", interval)
	} else {
		log.log("Daemon started (interval: %v, auto-commit: %v, auto-push: %v)", interval, autoCommit, autoPush)
	}

	// Get the proper .beads directory with config files.
	// For Dolt server mode, the database may be in a separate location (e.g., ~/.beads-dolt)
	// while the config (metadata.json) is in the .beads directory. Using FindBeadsDir()
	// ensures we load config from the right place.
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		// Fallback: derive from database path (works for SQLite in .beads/)
		beadsDir = filepath.Dir(daemonDBPath)
	}

	// dbDir is the parent of the database file/directory - used for database-relative
	// paths like Dolt server logs. Distinct from beadsDir which has config files.
	dbDir := filepath.Dir(daemonDBPath)

	// Server mode bootstrap: generate metadata.json from env vars if missing.
	// In K8s with env-var-only config, no metadata.json exists yet. Generate
	// one so the config loading chain (IsDoltServerMode, GetCapabilities, etc.)
	// works correctly.
	if os.Getenv("BEADS_DOLT_SERVER_MODE") == "1" {
		if existingCfg, _ := configfile.Load(beadsDir); existingCfg == nil {
			serverCfg := &configfile.Config{
				Backend:  configfile.BackendDolt,
				Database: "dolt",
				DoltMode: configfile.DoltModeServer,
			}
			if h := os.Getenv("BEADS_DOLT_SERVER_HOST"); h != "" {
				serverCfg.DoltServerHost = h
			}
			if p := os.Getenv("BEADS_DOLT_SERVER_PORT"); p != "" {
				if port, err := strconv.Atoi(p); err == nil {
					serverCfg.DoltServerPort = port
				}
			}
			if u := os.Getenv("BEADS_DOLT_SERVER_USER"); u != "" {
				serverCfg.DoltServerUser = u
			}
			if d := os.Getenv("BEADS_DOLT_SERVER_DATABASE"); d != "" {
				serverCfg.DoltDatabase = d
			}
			if err := serverCfg.Save(beadsDir); err != nil {
				log.Warn("failed to generate server-mode metadata.json", "error", err)
			} else {
				log.Info("generated metadata.json from env vars", "mode", "server")
			}
		}
	}

	backend := factory.GetBackendFromConfig(beadsDir)
	if backend == "" {
		backend = configfile.BackendSQLite
	}

	// Daemon is not supported with single-process backends (e.g., embedded Dolt)
	// Note: Dolt server mode supports multi-process, so check capabilities not backend type
	cfg, cfgErr := configfile.Load(beadsDir)
	if cfgErr == nil && cfg != nil && cfg.GetCapabilities().SingleProcessOnly {
		errMsg := fmt.Sprintf(`DAEMON NOT SUPPORTED WITH %s BACKEND

The bd daemon is designed for multi-process backends only.
With single-process backends, run commands in direct mode.

The daemon will now exit.`, strings.ToUpper(backend))
		log.Error(errMsg)

		// Write error to file so user can see it without checking logs
		errFile := filepath.Join(beadsDir, "daemon-error")
		// nolint:gosec // G306: Error file needs to be readable for debugging
		if err := os.WriteFile(errFile, []byte(errMsg), 0644); err != nil {
			log.Warn("could not write daemon-error file", "error", err)
		}
		return
	}

	// Reset backoff on daemon start (fresh start, but preserve NeedsManualSync hint)
	if !localMode {
		ResetBackoffOnDaemonStart(beadsDir)
	}

	// Check for multiple .db files (ambiguity error) - SQLite only.
	// Dolt is directory-backed so this check is irrelevant and can be misleading.
	if backend == configfile.BackendSQLite {
		matches, err := filepath.Glob(filepath.Join(beadsDir, "*.db"))
		if err == nil && len(matches) > 1 {
			// Filter out backup files (*.backup-*.db, *.backup.db)
			var validDBs []string
			for _, match := range matches {
				baseName := filepath.Base(match)
				// Skip if it's a backup file (contains ".backup" in name)
				if !strings.Contains(baseName, ".backup") && baseName != "vc.db" {
					validDBs = append(validDBs, match)
				}
			}
			if len(validDBs) > 1 {
				errMsg := fmt.Sprintf("Error: Multiple database files found in %s:\n", beadsDir)
				for _, db := range validDBs {
					errMsg += fmt.Sprintf("  - %s\n", filepath.Base(db))
				}
				errMsg += fmt.Sprintf("\nBeads requires a single canonical database: %s\n", beads.CanonicalDatabaseName)
				errMsg += "Run 'bd init' to migrate legacy databases or manually remove old databases\n"
				errMsg += "Or run 'bd doctor' for more diagnostics"

				log.log(errMsg)

				// Write error to file so user can see it without checking logs
				errFile := filepath.Join(beadsDir, "daemon-error")
				// nolint:gosec // G306: Error file needs to be readable for debugging
				if err := os.WriteFile(errFile, []byte(errMsg), 0644); err != nil {
					log.Warn("could not write daemon-error file", "error", err)
				}

				return // Use return instead of os.Exit to allow defers to run
			}
		}
	}

	// Validate using canonical name (SQLite only).
	// Dolt uses a directory-backed store (typically .beads/dolt), so the "beads.db"
	// basename invariant does not apply.
	if backend == configfile.BackendSQLite {
		dbBaseName := filepath.Base(daemonDBPath)
		if dbBaseName != beads.CanonicalDatabaseName {
			log.Error("non-canonical database name", "name", dbBaseName, "expected", beads.CanonicalDatabaseName)
			log.Info("run 'bd init' to migrate to canonical name")
			return // Use return instead of os.Exit to allow defers to run
		}
	}

	log.Info("using database", "path", daemonDBPath)

	// Check for config mismatch between metadata.json and config.yaml
	// This helps catch cases where these two config sources have diverged,
	// which can cause confusing behavior (e.g., daemon opening wrong database).
	if yamlDBPath := config.GetString("db"); yamlDBPath != "" {
		if mismatches := configfile.CheckConfigMismatch(beadsDir, yamlDBPath); len(mismatches) > 0 {
			for _, m := range mismatches {
				log.Warn("config mismatch detected",
					"field", m.Field,
					"metadata.json", m.MetadataValue,
					"config.yaml", m.YAMLValue)
			}
			log.Warn("metadata.json takes precedence; update it to match config.yaml if needed")
		}
	}

	// Clear any previous daemon-error file on successful startup
	errFile := filepath.Join(beadsDir, "daemon-error")
	if err := os.Remove(errFile); err != nil && !os.IsNotExist(err) {
		log.Warn("could not remove daemon-error file", "error", err)
	}

	// Start dolt sql-server if federation mode is enabled and backend is dolt
	var doltServer *DoltServerHandle
	factoryOpts := factory.Options{}
	if federation && backend != configfile.BackendDolt {
		log.Warn("federation mode requires dolt backend, ignoring --federation flag")
		federation = false
	}
	if federation && backend == configfile.BackendDolt {
		if !DoltServerAvailable() {
			log.Error("federation mode requires CGO; use pre-built binaries from GitHub releases")
			return
		}
		log.Info("starting dolt sql-server for federation mode")

		doltPath := filepath.Join(dbDir, "dolt")
		serverLogFile := filepath.Join(dbDir, "dolt-server.log")

		// Use provided ports or defaults
		sqlPort := federationPort
		if sqlPort == 0 {
			sqlPort = DoltDefaultSQLPort
		}
		remotePort := remotesapiPort
		if remotePort == 0 {
			remotePort = DoltDefaultRemotesAPIPort
		}

		var err error
		doltServer, err = StartDoltServer(ctx, doltPath, serverLogFile, sqlPort, remotePort)
		if err != nil {
			log.Error("failed to start dolt sql-server", "error", err)
			return
		}
		defer func() {
			log.Info("stopping dolt sql-server")
			if err := doltServer.Stop(); err != nil {
				log.Warn("error stopping dolt sql-server", "error", err)
			}
		}()

		log.Info("dolt sql-server started",
			"sql_port", doltServer.SQLPort(),
			"remotesapi_port", doltServer.RemotesAPIPort())

		// Configure factory to use server mode
		factoryOpts.ServerMode = true
		factoryOpts.ServerHost = doltServer.Host()
		factoryOpts.ServerPort = doltServer.SQLPort()
	}

	var store storage.Storage
	if os.Getenv("BEADS_DOLT_SERVER_MODE") == "1" {
		// In server mode, wait for the Dolt server to become reachable.
		// This replaces the init container's nc -z retry loop and allows the
		// daemon to handle startup ordering without a separate init container.
		store, err = waitForStore(ctx, beadsDir, factoryOpts, log)
	} else {
		store, err = factory.NewFromConfigWithOptions(ctx, beadsDir, factoryOpts)
	}
	if err != nil {
		log.Error("cannot open database", "error", err)
		return // Use return instead of os.Exit to allow defers to run
	}
	defer func() { _ = store.Close() }()

	// Enable freshness checking for SQLite backend to detect external database file modifications
	// (e.g., when git merge replaces the database file)
	// Dolt doesn't need this since it handles versioning natively.
	if sqliteStore, ok := store.(*sqlite.SQLiteStorage); ok {
		sqliteStore.EnableFreshnessChecking()
		log.Info("database opened", "path", store.Path(), "backend", "sqlite", "freshness_checking", true)
	} else if federation {
		log.Info("database opened", "path", store.Path(), "backend", "dolt", "mode", "federation/server")
	} else if os.Getenv("BEADS_DOLT_SERVER_MODE") == "1" {
		log.Info("database opened", "path", store.Path(), "backend", "dolt", "mode", "server")
	} else {
		log.Info("database opened", "path", store.Path(), "backend", "dolt", "mode", "embedded")
	}

	// Checkout specified branch if BEADS_DOLT_BRANCH is set (Dolt only)
	// Default is "main" if not specified. This enables isolated environments
	// that share the same base data but write to their own branch.
	if branch := os.Getenv("BEADS_DOLT_BRANCH"); branch != "" && branch != "main" {
		if vs, ok := storage.AsVersioned(store); ok {
			// Check if branch exists
			branches, err := vs.ListBranches(ctx)
			if err != nil {
				log.Warn("failed to list branches", "error", err)
			} else {
				branchExists := false
				for _, b := range branches {
					if b == branch {
						branchExists = true
						break
					}
				}
				if !branchExists {
					log.Info("creating branch from main", "branch", branch)
					if err := vs.Branch(ctx, branch); err != nil {
						log.Error("failed to create branch", "branch", branch, "error", err)
						return
					}
				}
				log.Info("checking out branch", "branch", branch)
				if err := vs.Checkout(ctx, branch); err != nil {
					log.Error("failed to checkout branch", "branch", branch, "error", err)
					return
				}
				log.Info("branch checkout complete", "branch", branch)
			}
		}
	}

	// Auto-upgrade .beads/.gitignore if outdated
	gitignoreCheck := doctor.CheckGitignore()
	if gitignoreCheck.Status == "warning" || gitignoreCheck.Status == "error" {
		log.Info("upgrading .beads/.gitignore")
		if err := doctor.FixGitignore(); err != nil {
			log.Warn("failed to upgrade .gitignore", "error", err)
		} else {
			log.Info("successfully upgraded .beads/.gitignore")
		}
	}

	// Hydrate from multi-repo if configured (SQLite only)
	if sqliteStore, ok := store.(*sqlite.SQLiteStorage); ok {
		if results, err := sqliteStore.HydrateFromMultiRepo(ctx); err != nil {
			log.Error("multi-repo hydration failed", "error", err)
			return // Use return instead of os.Exit to allow defers to run
		} else if results != nil {
			log.Info("multi-repo hydration complete")
			for repo, count := range results {
				log.Info("hydrated issues", "repo", repo, "count", count)
			}
		}
	}

	// Validate database fingerprint (skip in local mode - no git available)
	if localMode {
		log.Info("skipping fingerprint validation (local mode)")
	} else if err := validateDatabaseFingerprint(ctx, store, &log); err != nil {
		if os.Getenv("BEADS_IGNORE_REPO_MISMATCH") != "1" {
			log.Error("repository fingerprint validation failed", "error", err)
			// Write error to daemon-error file so user sees it instead of just "daemon took too long"
			errFile := filepath.Join(beadsDir, "daemon-error")
			// nolint:gosec // G306: Error file needs to be readable for debugging
			if writeErr := os.WriteFile(errFile, []byte(err.Error()), 0644); writeErr != nil {
				log.Warn("could not write daemon-error file", "error", writeErr)
			}
			return // Use return instead of os.Exit to allow defers to run
		}
		log.Warn("repository mismatch ignored (BEADS_IGNORE_REPO_MISMATCH=1)")
	}

	// GH#1258: Warn at startup if sync-branch == current-branch (misconfiguration)
	// This is a one-time warning - per-operation skipping is handled by shouldSkipDueToSameBranch()
	// Skip check in local mode (no sync-branch is used)
	if !localMode {
		warnIfSyncBranchMisconfigured(ctx, store, log)
	}

	// Validate schema version matches daemon version
	versionCtx := context.Background()
	dbVersion, err := store.GetMetadata(versionCtx, "bd_version")
	if err != nil && err.Error() != "metadata key not found: bd_version" {
		log.Error("failed to read database version", "error", err)
		return // Use return instead of os.Exit to allow defers to run
	}

	if dbVersion != "" && dbVersion != Version {
		log.Warn("database schema version mismatch", "db_version", dbVersion, "daemon_version", Version)
		log.Info("auto-upgrading database to daemon version")

		// Auto-upgrade database to daemon version
		// The daemon operates on its own database, so it should always use its own version
		if err := store.SetMetadata(versionCtx, "bd_version", Version); err != nil {
			log.Error("failed to update database version", "error", err)

			// Allow override via environment variable for emergencies
			if os.Getenv("BEADS_IGNORE_VERSION_MISMATCH") != "1" {
				return // Use return instead of os.Exit to allow defers to run
			}
			log.Warn("proceeding despite version update failure (BEADS_IGNORE_VERSION_MISMATCH=1)")
		} else {
			log.Info("database version updated", "version", Version)
		}
	} else if dbVersion == "" {
		// Old database without version metadata - set it now
		log.Warn("database missing version metadata", "setting_to", Version)
		if err := store.SetMetadata(versionCtx, "bd_version", Version); err != nil {
			log.Error("failed to set database version", "error", err)
			return // Use return instead of os.Exit to allow defers to run
		}
	}

	// Hydrate deploy.* config from database into environment variables.
	// Priority: env vars already set > deploy.* config values > defaults.
	// This allows the database config table to drive daemon behavior without
	// duplicating settings in Helm values.yaml and env vars.
	hydrateDeployConfig(ctx, store, log)

	// Get workspace path (.beads directory) - beadsDir already defined above
	// Get actual workspace root (parent of .beads)
	workspacePath := filepath.Dir(beadsDir)
	// Use short socket path to avoid Unix socket path length limits (macOS: 104 chars)
	// Check BD_SOCKET env var first for custom socket path (e.g., test isolation,
	// or filesystems that don't support sockets in .beads directory)
	socketPath := os.Getenv("BD_SOCKET")
	if socketPath == "" {
		socketPath = rpc.ShortSocketPath(workspacePath)
	}
	socketPath, err = rpc.EnsureSocketDir(socketPath)
	if err != nil {
		log.Error("failed to create socket directory", "error", err)
		return
	}
	serverCtx, serverCancel := context.WithCancel(ctx)
	defer serverCancel()

	server, serverErrChan, err := startRPCServer(serverCtx, socketPath, store, workspacePath, daemonDBPath, tcpAddr, tlsCert, tlsKey, tcpToken, httpAddr, log)
	if err != nil {
		return
	}

	// Log TCP address if configured
	if tcpAddr != "" {
		if tlsCert != "" {
			log.Info("TCP listener enabled with TLS", "addr", tcpAddr, "cert", tlsCert)
		} else {
			log.Info("TCP listener enabled", "addr", tcpAddr)
		}
		if tcpToken != "" {
			log.Info("TCP token authentication enabled")
		}
	}

	// Log HTTP address if configured
	if httpAddr != "" {
		log.Info("HTTP listener enabled (Connect-RPC style API)", "addr", httpAddr)
	}

	// NATS startup: three modes
	// 1. BD_NATS_URL set → connect as client to standalone NATS
	// 2. BD_NATS_DISABLED=true → no NATS at all
	// 3. Default → start embedded NATS server
	var natsServer *daemon.NATSServer
	var externalNATS *daemon.ExternalNATSConn
	var jsCtx nats.JetStreamContext
	externalNATSURL := os.Getenv("BD_NATS_URL")

	if externalNATSURL != "" {
		// Mode 1: Connect to standalone NATS server
		token := os.Getenv("BD_NATS_TOKEN")
		if token == "" {
			token = os.Getenv("BD_DAEMON_TOKEN")
		}
		var err error
		externalNATS, err = daemon.ConnectExternalNATS(externalNATSURL, token)
		if err != nil {
			log.Error("failed to connect to external NATS", "url", externalNATSURL, "error", err)
			log.Warn("continuing without NATS - JetStream event persistence disabled")
		} else {
			defer externalNATS.Close()
			log.Info("connected to standalone NATS server", "url", externalNATSURL)

			jsCtx, err = externalNATS.Conn().JetStream()
			if err != nil {
				log.Error("failed to get JetStream context from external NATS", "error", err)
				jsCtx = nil
			} else {
				if err := eventbus.EnsureStreams(jsCtx); err != nil {
					log.Error("failed to create JetStream streams", "error", err)
					jsCtx = nil
				} else {
					log.Info("JetStream streams initialized on standalone NATS")
				}
			}
		}
	} else if os.Getenv("BD_NATS_DISABLED") != "true" {
		// Mode 3: Start embedded NATS server (default)
		natsCfg := daemon.NATSConfigFromEnv(filepath.Join(beadsDir, ".runtime"))
		var err error
		natsServer, err = daemon.StartNATSServer(natsCfg)
		if err != nil {
			log.Error("failed to start embedded NATS server", "error", err)
			log.Warn("continuing without NATS - JetStream event persistence disabled")
		} else {
			defer func() {
				natsServer.RemoveConnectionInfo()
				natsServer.Shutdown()
			}()
			log.Info("embedded NATS server started", "port", natsServer.Port())

			jsCtx, err = natsServer.Conn().JetStream()
			if err != nil {
				log.Error("failed to get JetStream context", "error", err)
				jsCtx = nil
			} else {
				if err := eventbus.EnsureStreams(jsCtx); err != nil {
					log.Error("failed to create JetStream streams", "error", err)
					jsCtx = nil
				} else {
					log.Info("JetStream streams initialized")
				}
			}

			// Write connection info for sidecar discovery (e.g., Coop)
			if err := natsServer.WriteConnectionInfo(natsCfg.Token); err != nil {
				log.Warn("failed to write NATS connection info", "error", err)
			} else {
				log.Info("NATS connection info written for sidecar discovery")
			}

			health := natsServer.Health()
			log.Info("NATS health", "status", health.Status, "jetstream", health.JetStream, "streams", health.Streams)
		}
	} else {
		// Mode 2: NATS explicitly disabled
		log.Info("NATS disabled via BD_NATS_DISABLED=true")
	}

	// Choose event loop based on BEADS_DAEMON_MODE (need to determine early for SetConfig)
	daemonMode := os.Getenv("BEADS_DAEMON_MODE")
	if daemonMode == "" {
		daemonMode = "events" // Default to event-driven mode (production-ready as of v0.21.0)
	}

	// Set daemon configuration for status reporting
	server.SetConfig(autoCommit, autoPush, autoPull, localMode, interval.String(), daemonMode)

	// Create event bus and register built-in handlers (bd-66fp)
	bus := eventbus.New()
	for _, h := range eventbus.DefaultHandlers() {
		bus.Register(h)
	}

	// Connect JetStream to bus for event persistence
	if jsCtx != nil {
		bus.SetJetStream(jsCtx)
		log.Info("JetStream connected to event bus - events will be persisted")
	}

	// Wire bus reference into StopLoopDetector for JetStream publishing (bd-5r1cw).
	for _, h := range bus.Handlers() {
		if sld, ok := h.(*eventbus.StopLoopDetector); ok {
			sld.SetBus(bus)
			break
		}
	}

	// Load persisted external handlers from config table (bd-4q86.1)
	if store != nil {
		allCfg, cfgErr := store.GetAllConfig(context.Background())
		if cfgErr == nil {
			n := bus.LoadPersistedHandlers(allCfg)
			if n > 0 {
				log.Info("loaded persisted bus handlers", "count", n)
			}
		} else {
			log.Warn("failed to load persisted bus handlers", "error", cfgErr)
		}
	}

	server.SetBus(bus)

	// Wire NATS health into RPC status reporting
	if natsServer != nil {
		server.SetNATSHealthFn(func() rpc.NATSHealthInfo {
			h := natsServer.Health()
			return rpc.NATSHealthInfo{
				Enabled:     true,
				Status:      h.Status,
				Port:        h.Port,
				Connections: h.Connections,
				JetStream:   h.JetStream,
				Streams:     h.Streams,
			}
		})
	} else if externalNATS != nil {
		server.SetNATSHealthFn(func() rpc.NATSHealthInfo {
			nc := externalNATS.Conn()
			status := "connected"
			if !nc.IsConnected() {
				status = "disconnected"
			}
			return rpc.NATSHealthInfo{
				Enabled:   true,
				Status:    status,
				JetStream: jsCtx != nil,
			}
		})
	}

	log.Info("event bus initialized", "handlers", len(bus.Handlers()), "jetstream", jsCtx != nil)

	// Register daemon in global registry
	registry, err := daemon.NewRegistry()
	if err != nil {
		log.Warn("failed to create registry", "error", err)
	} else {
		entry := daemon.RegistryEntry{
			WorkspacePath: workspacePath,
			SocketPath:    socketPath,
			DatabasePath:  daemonDBPath,
			PID:           os.Getpid(),
			Version:       Version,
			StartedAt:     time.Now(),
		}
		if err := registry.Register(entry); err != nil {
			log.Warn("failed to register daemon", "error", err)
		} else {
			log.Info("registered in global registry")
		}
		// Ensure we unregister on exit
		defer func() {
			if err := registry.Unregister(workspacePath, os.Getpid()); err != nil {
				log.Warn("failed to unregister daemon", "error", err)
			}
		}()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Check for dolt-native mode (hq-c005e8)
	// Dolt-native mode uses lightweight sync without JSONL export/import
	syncMode := GetSyncMode(ctx, store)
	isDoltNative := syncMode == SyncModeDoltNative

	// Skip dirty tracking in dolt-native mode to eliminate write amplification (bd-8csx)
	if isDoltNative {
		if s, ok := store.(interface{ SetSkipDirtyTracking(bool) }); ok {
			s.SetSkipDirtyTracking(true)
			log.Info("dirty tracking disabled (dolt-native mode)")
		}
	}

	// Create sync function based on mode
	var doSync func()
	if isDoltNative {
		doSync = createDoltNativeSyncFunc(ctx, store, autoCommit, autoPush, autoPull, log)
		log.Info("using dolt-native sync mode (no JSONL)")
	} else if localMode {
		doSync = createLocalSyncFunc(ctx, store, log)
	} else {
		doSync = createSyncFunc(ctx, store, autoCommit, autoPush, log)
	}
	doSync()

	// Get parent PID for monitoring (exit if parent dies)
	parentPID := computeDaemonParentPID()
	log.Info("monitoring parent process", "pid", parentPID)

	// daemonMode already determined above for SetConfig
	switch daemonMode {
	case "events":
		log.Info("using event-driven mode")

		// Event-driven mode uses separate export-only and import-only functions
		var doExport, doAutoImport func()

		if isDoltNative {
			// Dolt-native: lightweight commit/push without JSONL
			doExport = createDoltNativeExportFunc(ctx, store, autoCommit, autoPush, log)
			doAutoImport = createDoltNativePullFunc(ctx, store, log)
			// Use empty jsonlPath since we don't need file watching
			runEventDrivenLoop(ctx, cancel, server, serverErrChan, store, "", doExport, doAutoImport, autoPull, parentPID, log)
		} else {
			jsonlPath := findJSONLPath()
			if jsonlPath == "" {
				log.Error("JSONL path not found, cannot use event-driven mode")
				log.Info("falling back to polling mode")
				runEventLoop(ctx, cancel, ticker, doSync, server, serverErrChan, parentPID, log)
			} else {
				if localMode {
					doExport = createLocalExportFunc(ctx, store, log)
					doAutoImport = createLocalAutoImportFunc(ctx, store, log)
				} else {
					doExport = createExportFunc(ctx, store, autoCommit, autoPush, log)
					doAutoImport = createAutoImportFunc(ctx, store, log)
				}
				runEventDrivenLoop(ctx, cancel, server, serverErrChan, store, jsonlPath, doExport, doAutoImport, autoPull, parentPID, log)
			}
		}
	case "poll":
		log.Info("using polling mode", "interval", interval)
		runEventLoop(ctx, cancel, ticker, doSync, server, serverErrChan, parentPID, log)
	default:
		log.Warn("unknown BEADS_DAEMON_MODE, defaulting to poll", "mode", daemonMode, "valid", "poll, events")
		runEventLoop(ctx, cancel, ticker, doSync, server, serverErrChan, parentPID, log)
	}
}

// loadDaemonAutoSettings loads daemon sync mode settings.
//
// # Two Sync Modes
//
// Read/Write Mode (full sync):
//
//	daemon.auto-sync: true  (or BEADS_AUTO_SYNC=true)
//
// Enables auto-commit, auto-push, AND auto-pull. Full bidirectional sync
// with team. Eliminates need for manual `bd sync`. This is the default
// when sync-branch is configured.
//
// Read-Only Mode:
//
//	daemon.auto-pull: true  (or BEADS_AUTO_PULL=true)
//
// Only enables auto-pull (receive updates from team). Does NOT auto-publish
// your changes. Useful for experimental work or manual review before sharing.
//
// # Precedence
//
// 1. auto-sync=true → Read/Write mode (all three ON, no exceptions)
// 2. auto-sync=false → Write-side OFF, auto-pull can still be enabled
// 3. auto-sync not set → Legacy compat mode:
//   - If either BEADS_AUTO_COMMIT/daemon.auto_commit or BEADS_AUTO_PUSH/daemon.auto_push
//     is enabled, treat as auto-sync=true (full read/write)
//   - Otherwise check auto-pull for read-only mode
//
// 4. Fallback: all default to true when sync-branch configured
//
// loadYAMLDaemonSettings loads daemon auto-settings from YAML config and env vars only (no database).
// This is safe to call from the parent process since it doesn't require database access.
// Returns (autoCommit, autoPush, autoPull, hasSettings) where hasSettings indicates
// if any settings were found (env var or YAML).
func loadYAMLDaemonSettings() (autoCommit, autoPush, autoPull, hasSettings bool) {
	// Check unified auto-sync first (env var > YAML)
	if envVal := os.Getenv("BEADS_AUTO_SYNC"); envVal == "true" || envVal == "1" {
		return true, true, true, true
	}
	if yamlAutoSync := config.GetString("daemon.auto-sync"); yamlAutoSync == "true" {
		return true, true, true, true
	}

	// Check individual settings (env var > YAML for each)
	yamlAutoCommit := config.GetString("daemon.auto-commit")
	yamlAutoPush := config.GetString("daemon.auto-push")
	yamlAutoPull := config.GetString("daemon.auto-pull")
	envAutoCommit := os.Getenv("BEADS_AUTO_COMMIT")
	envAutoPush := os.Getenv("BEADS_AUTO_PUSH")
	envAutoPull := os.Getenv("BEADS_AUTO_PULL")

	hasSettings = yamlAutoCommit != "" || yamlAutoPush != "" || yamlAutoPull != "" ||
		envAutoCommit != "" || envAutoPush != "" || envAutoPull != ""

	if !hasSettings {
		return false, false, false, false
	}

	// For each: env var > YAML
	if envAutoCommit != "" {
		autoCommit = envAutoCommit == "true" || envAutoCommit == "1"
	} else if yamlAutoCommit != "" {
		autoCommit = yamlAutoCommit == "true"
	}

	if envAutoPush != "" {
		autoPush = envAutoPush == "true" || envAutoPush == "1"
	} else if yamlAutoPush != "" {
		autoPush = yamlAutoPush == "true"
	}

	if envAutoPull != "" {
		autoPull = envAutoPull == "true" || envAutoPull == "1"
	} else if yamlAutoPull != "" {
		autoPull = yamlAutoPull == "true"
	}

	return autoCommit, autoPush, autoPull, true
}

// Note: The individual auto-commit/auto-push settings are deprecated.
// Use auto-sync for read/write mode, auto-pull for read-only mode.
func loadDaemonAutoSettings(cmd *cobra.Command, autoCommit, autoPush, autoPull bool) (bool, bool, bool) {
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return autoCommit, autoPush, autoPull
	}

	ctx := context.Background()
	store, err := factory.NewFromConfig(ctx, beadsDir)
	if err != nil {
		return autoCommit, autoPush, autoPull
	}
	defer func() { _ = store.Close() }()

	// Check if sync-branch is configured (used for defaults)
	syncBranch, _ := store.GetConfig(ctx, "sync.branch")
	hasSyncBranch := syncBranch != ""

	// Check unified auto-sync setting first (controls auto-commit + auto-push)
	// Priority: env var > YAML config > database config
	unifiedAutoSync := ""
	if envVal := os.Getenv("BEADS_AUTO_SYNC"); envVal != "" {
		unifiedAutoSync = envVal
	} else if configVal := config.GetString("daemon.auto-sync"); configVal != "" {
		unifiedAutoSync = configVal
	} else if configVal, _ := store.GetConfig(ctx, "daemon.auto-sync"); configVal != "" {
		unifiedAutoSync = configVal
	}

	// Handle unified auto-sync setting
	if unifiedAutoSync != "" {
		enabled := unifiedAutoSync == "true" || unifiedAutoSync == "1"
		if enabled {
			// auto-sync=true: MASTER CONTROL, forces all three ON
			// Individual CLI flags are ignored - you said "full sync"
			autoCommit = true
			autoPush = true
			autoPull = true
			return autoCommit, autoPush, autoPull
		}
		// auto-sync=false: Write-side (commit/push) locked OFF
		// Only auto-pull can be individually enabled (for read-only mode)
		autoCommit = false
		autoPush = false
		// Auto-pull can still be enabled via CLI flag or individual config
		// Priority: CLI flag > env var > YAML config > database config
		if cmd.Flags().Changed("auto-pull") {
			// Use the CLI flag value (already in autoPull)
		} else if envVal := os.Getenv("BEADS_AUTO_PULL"); envVal != "" {
			autoPull = envVal == "true" || envVal == "1"
		} else if configVal := config.GetString("daemon.auto-pull"); configVal != "" {
			autoPull = configVal == "true"
		} else if configVal, _ := store.GetConfig(ctx, "daemon.auto-pull"); configVal != "" {
			autoPull = configVal == "true"
		} else if configVal, _ := store.GetConfig(ctx, "daemon.auto_pull"); configVal != "" {
			autoPull = configVal == "true"
		} else if hasSyncBranch {
			// Default auto-pull to true when sync-branch configured
			autoPull = true
		} else {
			autoPull = false
		}
		return autoCommit, autoPush, autoPull
	}

	// Check YAML config for individual daemon settings (allows fine-grained control)
	// Priority for each setting: CLI flag > env var > YAML config > database config
	yamlAutoCommit := config.GetString("daemon.auto-commit")
	yamlAutoPush := config.GetString("daemon.auto-push")
	yamlAutoPull := config.GetString("daemon.auto-pull")

	// Check individual env vars (take precedence over YAML)
	envAutoCommit := os.Getenv("BEADS_AUTO_COMMIT")
	envAutoPush := os.Getenv("BEADS_AUTO_PUSH")
	envAutoPull := os.Getenv("BEADS_AUTO_PULL")

	// If any YAML individual settings OR individual env vars are set, use fine-grained control
	// This allows users to set just auto-commit without forcing auto-push/auto-pull
	hasIndividualSettings := yamlAutoCommit != "" || yamlAutoPush != "" || yamlAutoPull != "" ||
		envAutoCommit != "" || envAutoPush != "" || envAutoPull != ""

	if hasIndividualSettings {
		// For each setting: CLI flag > env var > YAML config
		if !cmd.Flags().Changed("auto-commit") {
			if envAutoCommit != "" {
				autoCommit = envAutoCommit == "true" || envAutoCommit == "1"
			} else if yamlAutoCommit != "" {
				autoCommit = yamlAutoCommit == "true"
			}
		}
		if !cmd.Flags().Changed("auto-push") {
			if envAutoPush != "" {
				autoPush = envAutoPush == "true" || envAutoPush == "1"
			} else if yamlAutoPush != "" {
				autoPush = yamlAutoPush == "true"
			}
		}
		if !cmd.Flags().Changed("auto-pull") {
			if envAutoPull != "" {
				autoPull = envAutoPull == "true" || envAutoPull == "1"
			} else if yamlAutoPull != "" {
				autoPull = yamlAutoPull == "true"
			}
		}
		return autoCommit, autoPush, autoPull
	}

	// No YAML individual settings - check legacy env vars and database config
	// Legacy behavior: if either auto-commit or auto-push is enabled, enable full auto-sync
	legacyCommit := false
	legacyPush := false

	// Check legacy auto-commit (env var or database config)
	if envVal := os.Getenv("BEADS_AUTO_COMMIT"); envVal != "" {
		legacyCommit = envVal == "true" || envVal == "1"
	} else if configVal, _ := store.GetConfig(ctx, "daemon.auto_commit"); configVal != "" {
		legacyCommit = configVal == "true"
	}

	// Check legacy auto-push (env var or database config)
	if envVal := os.Getenv("BEADS_AUTO_PUSH"); envVal != "" {
		legacyPush = envVal == "true" || envVal == "1"
	} else if configVal, _ := store.GetConfig(ctx, "daemon.auto_push"); configVal != "" {
		legacyPush = configVal == "true"
	}

	// If either legacy write-side option is enabled, enable full auto-sync
	// (backward compat: user wanted writes, so give them full sync)
	if legacyCommit || legacyPush {
		autoCommit = true
		autoPush = true
		autoPull = true
		return autoCommit, autoPush, autoPull
	}

	// Neither legacy write option enabled - check auto-pull for read-only mode
	// Priority: CLI flag > env var > database config
	if !cmd.Flags().Changed("auto-pull") {
		if envVal := os.Getenv("BEADS_AUTO_PULL"); envVal != "" {
			autoPull = envVal == "true" || envVal == "1"
		} else if configVal, _ := store.GetConfig(ctx, "daemon.auto-pull"); configVal != "" {
			autoPull = configVal == "true"
		} else if configVal, _ := store.GetConfig(ctx, "daemon.auto_pull"); configVal != "" {
			autoPull = configVal == "true"
		} else if hasSyncBranch {
			// Default auto-pull to true when sync-branch configured
			autoPull = true
		}
	}

	// Fallback: if sync-branch configured and no explicit settings, default to full sync
	if hasSyncBranch && !cmd.Flags().Changed("auto-commit") && !cmd.Flags().Changed("auto-push") {
		autoCommit = true
		autoPush = true
		autoPull = true
	}

	return autoCommit, autoPush, autoPull
}

// hydrateDeployConfig reads deploy.* keys from the database config table and
// sets the corresponding environment variables if they are not already set.
// This is the core of config materialization: the database drives runtime
// behavior, and env vars that are already set take precedence (e.g., from
// Helm values or K8s env overrides).
func hydrateDeployConfig(ctx context.Context, store storage.Storage, log daemonLogger) {
	allConfig, err := store.GetAllConfig(ctx)
	if err != nil {
		log.Warn("failed to read config for deploy hydration", "error", err)
		return
	}

	envMap := config.DeployKeyEnvMap()
	hydrated := 0

	for key, value := range allConfig {
		if !config.IsDeployKey(key) {
			continue
		}

		envVar, ok := envMap[key]
		if !ok || envVar == "" {
			// Deploy key with no env var mapping (e.g., deploy.ingress_host)
			log.Debug("deploy key has no env mapping, skipping", "key", key)
			continue
		}

		// Env vars already set take precedence
		if existing := os.Getenv(envVar); existing != "" {
			log.Debug("env var already set, skipping hydration", "key", key, "env", envVar)
			continue
		}

		if err := os.Setenv(envVar, value); err != nil {
			log.Warn("failed to set env var from deploy config", "key", key, "env", envVar, "error", err)
			continue
		}

		hydrated++
		log.Debug("hydrated deploy config", "key", key, "env", envVar, "value", value)
	}

	if hydrated > 0 {
		log.Info("deploy config hydrated from database", "count", hydrated)
	}
}

// waitForStore retries opening the database store with exponential backoff.
// In K8s server mode, the Dolt server may not be ready when the daemon starts
// (e.g., Dolt pod still initializing). This replaces the init container's
// nc -z retry loop with in-process retries.
func waitForStore(ctx context.Context, beadsDir string, opts factory.Options, log daemonLogger) (storage.Storage, error) {
	maxAttempts := getEnvInt("BEADS_DOLT_CONNECT_RETRIES", 30)
	retryInterval := 2 * time.Second

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		store, err := factory.NewFromConfigWithOptions(ctx, beadsDir, opts)
		if err == nil {
			if attempt > 1 {
				log.Info("database connected after retry", "attempts", attempt)
			}
			return store, nil
		}
		lastErr = err

		if attempt == maxAttempts {
			break
		}

		log.Info("waiting for database", "attempt", attempt, "max", maxAttempts, "error", err)

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context canceled while waiting for database: %w", ctx.Err())
		case <-time.After(retryInterval):
		}
	}

	return nil, fmt.Errorf("database not reachable after %d attempts: %w", maxAttempts, lastErr)
}
