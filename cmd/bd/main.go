package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"runtime/trace"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/molecules"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/utils"
)

var (
	dbPath       string
	actor        string
	store        storage.Storage
	jsonOutput   bool
	daemonStatus DaemonStatus // Tracks daemon connection state for current command

	// Daemon mode
	daemonClient *rpc.Client // RPC client when daemon is running

	// Signal-aware context for graceful cancellation
	rootCtx    context.Context
	rootCancel context.CancelFunc

	// Auto-flush state
	autoFlushEnabled  = true // Can be disabled with --no-auto-flush
	flushMutex        sync.Mutex
	storeMutex        sync.Mutex // Protects store access from background goroutine
	storeActive       = false    // Tracks if store is available
	flushFailureCount = 0        // Consecutive flush failures
	lastFlushError    error      // Last flush error for debugging

	// Auto-flush manager (event-driven, fixes race condition)
	flushManager *FlushManager

	// Hook runner for extensibility
	hookRunner *hooks.Runner

	// skipFinalFlush is set by sync command when sync.branch mode completes successfully.
	// This prevents PersistentPostRun from re-exporting and dirtying the working directory.
	skipFinalFlush = false

	// Auto-import state
	autoImportEnabled = true // Can be disabled with --no-auto-import

	// Version upgrade tracking
	versionUpgradeDetected = false // Set to true if bd version changed since last run
	previousVersion        = ""    // The last bd version user had (empty = first run or unknown)
	upgradeAcknowledged    = false // Set to true after showing upgrade notification once per session
)
var (
	noAutoFlush     bool
	noAutoImport    bool
	allowStale      bool          // Use --allow-stale: skip staleness check (emergency escape hatch)
	readonlyMode    bool          // Read-only mode: block write operations (for worker sandboxes)
	storeIsReadOnly bool          // Track if store was opened read-only (for staleness checks)
	lockTimeout     time.Duration // SQLite busy_timeout (default 30s, 0 = fail immediately)
	profileEnabled  bool
	profileFile     *os.File
	traceFile       *os.File
	verboseFlag     bool // Enable verbose/debug output
	quietFlag       bool // Suppress non-essential output

	// Dolt auto-commit policy (flag/config). Values: off | on
	doltAutoCommit string

	// commandDidWrite is set when a command performs a write that should trigger
	// auto-flush. Used to decide whether to auto-commit Dolt after the command completes.
	// Thread-safe via atomic.Bool to avoid data races in concurrent flush operations.
	commandDidWrite atomic.Bool

	// commandDidExplicitDoltCommit is set when a command already created a Dolt commit
	// explicitly (e.g., bd sync in dolt-native mode, hook flows, bd vc commit).
	// This prevents a redundant auto-commit attempt in PersistentPostRun.
	commandDidExplicitDoltCommit bool

	// commandDidWriteTipMetadata is set when a command records a tip as "shown" by writing
	// metadata (tip_*_last_shown). This will be used to create a separate Dolt commit for
	// tip writes, even when the main command is read-only.
	commandDidWriteTipMetadata bool

	// commandTipIDsShown tracks which tip IDs were shown in this command (deduped).
	// This is used for tip-commit message formatting.
	commandTipIDsShown map[string]struct{}
)

// readOnlyCommands lists commands that only read from the database.
// These commands open SQLite in read-only mode to avoid modifying the
// database file (which breaks file watchers). See GH#804.
var readOnlyCommands = map[string]bool{
	"list":       true,
	"ready":      true,
	"show":       true,
	"stats":      true,
	"blocked":    true,
	"count":      true,
	"search":     true,
	"graph":      true,
	"duplicates": true,
	"comments":   true, // list comments (not add)
	// NOTE: "export" is NOT read-only - it writes to clear dirty issues and update jsonl_file_hash
}

// readOnlySubcommands lists subcommand paths (parent/child) that are read-only.
// This handles cases where the leaf command name alone is ambiguous.
// Format: "parent child" (space-separated).
var readOnlySubcommands = map[string]bool{
	"decision check":       true, // bd decision check: only reads decision status
	"merge-slot check":     true, // bd merge-slot check: only reads slot availability
	"gate session-check":   true, // bd gate session-check: only reads marker files
	"gate session-list":    true, // bd gate session-list: only reads registry
	"gate status":          true, // bd gate status: only reads marker files
	// NOTE: "gate check" is NOT read-only - it closes resolved gates
}

// isReadOnlyCommand returns true if the command only reads from the database.
// This is used to open SQLite/Dolt in read-only mode, preventing file modifications
// that would trigger file watchers. See GH#804.
// Checks both leaf command names and parent/child subcommand paths.
func isReadOnlyCommand(cmd *cobra.Command) bool {
	// First check leaf command name
	if readOnlyCommands[cmd.Name()] {
		return true
	}
	// Then check parent/child path for subcommands
	if cmd.Parent() != nil && cmd.Parent().Name() != "bd" {
		path := cmd.Parent().Name() + " " + cmd.Name()
		if readOnlySubcommands[path] {
			return true
		}
	}
	return false
}

// getActorWithGit returns the actor for audit trails with git config fallback.
// Priority: --actor flag > BD_ACTOR env > BEADS_ACTOR env > git config user.name > $USER > "unknown"
// This provides a sensible default for developers: their git identity is used unless
// explicitly overridden
func getActorWithGit() string {
	// If actor is already set (from --actor flag), use it
	if actor != "" {
		return actor
	}

	// Check BD_ACTOR env var (primary env override)
	if bdActor := os.Getenv("BD_ACTOR"); bdActor != "" {
		return bdActor
	}

	// Check BEADS_ACTOR env var (alias for MCP/integration compatibility)
	if beadsActor := os.Getenv("BEADS_ACTOR"); beadsActor != "" {
		return beadsActor
	}

	// Check GT_ROLE env var (Gas Town agent identity, e.g. "gastown/polecats/furiosa")
	if gtRole := os.Getenv("GT_ROLE"); gtRole != "" {
		return gtRole
	}

	// Try git config user.name - the natural default for a git-native tool
	if out, err := exec.Command("git", "config", "user.name").Output(); err == nil {
		if gitUser := strings.TrimSpace(string(out)); gitUser != "" {
			return gitUser
		}
	}

	// Fall back to system username
	if user := os.Getenv("USER"); user != "" {
		return user
	}

	return ""
}

// getOwner returns the human owner for CV attribution.
// Priority: GIT_AUTHOR_EMAIL env > git config user.email > "" (empty)
// This is the foundation for HOP CV (curriculum vitae) chains per Decision 008.
// Unlike actor (which tracks who executed), owner tracks the human responsible.
func getOwner() string {
	// Check GIT_AUTHOR_EMAIL first - this is set during git commit operations
	if authorEmail := os.Getenv("GIT_AUTHOR_EMAIL"); authorEmail != "" {
		return authorEmail
	}

	// Fall back to git config user.email - the natural default
	if out, err := exec.Command("git", "config", "user.email").Output(); err == nil {
		if gitEmail := strings.TrimSpace(string(out)); gitEmail != "" {
			return gitEmail
		}
	}

	// Return empty if no email found (owner is optional)
	return ""
}

func init() {
	// Initialize viper configuration
	if err := config.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize config: %v\n", err)
	}

	// Add command groups for organized help output
	rootCmd.AddGroup(
		&cobra.Group{ID: GroupMaintenance, Title: "Maintenance:"},
		&cobra.Group{ID: GroupIntegrations, Title: "Integrations & Advanced:"},
	)

	// Register persistent flags
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", "", "Database path (default: auto-discover .beads/*.db)")
	rootCmd.PersistentFlags().StringVar(&actor, "actor", "", "Actor name for audit trail (default: $BD_ACTOR, git user.name, $USER)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&noAutoFlush, "no-auto-flush", false, "Disable automatic JSONL sync after CRUD operations")
	rootCmd.PersistentFlags().BoolVar(&noAutoImport, "no-auto-import", false, "Disable automatic JSONL import when newer than DB")
	rootCmd.PersistentFlags().BoolVar(&allowStale, "allow-stale", false, "Allow operations on potentially stale data (skip staleness check)")
	rootCmd.PersistentFlags().BoolVar(&readonlyMode, "readonly", false, "Read-only mode: block write operations (for worker sandboxes)")
	rootCmd.PersistentFlags().StringVar(&doltAutoCommit, "dolt-auto-commit", "", "Dolt backend: auto-commit after write commands (off|on). Default from config key dolt.auto-commit")
	rootCmd.PersistentFlags().DurationVar(&lockTimeout, "lock-timeout", 30*time.Second, "SQLite busy timeout (0 = fail immediately if locked)")
	rootCmd.PersistentFlags().BoolVar(&profileEnabled, "profile", false, "Generate CPU profile for performance analysis")
	rootCmd.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "Enable verbose/debug output")
	rootCmd.PersistentFlags().BoolVarP(&quietFlag, "quiet", "q", false, "Suppress non-essential output (errors only)")

	// Add --version flag to root command (same behavior as version subcommand)
	rootCmd.Flags().BoolP("version", "V", false, "Print version information")

	// Command groups for organized help output (Tufte-inspired)
	rootCmd.AddGroup(&cobra.Group{ID: "issues", Title: "Working With Issues:"})
	rootCmd.AddGroup(&cobra.Group{ID: "views", Title: "Views & Reports:"})
	rootCmd.AddGroup(&cobra.Group{ID: "deps", Title: "Dependencies & Structure:"})
	rootCmd.AddGroup(&cobra.Group{ID: "sync", Title: "Sync & Data:"})
	rootCmd.AddGroup(&cobra.Group{ID: "setup", Title: "Setup & Configuration:"})
	// NOTE: Many maintenance commands (clean, cleanup, compact, validate, repair-deps)
	// should eventually be consolidated into 'bd doctor' and 'bd doctor --fix' to simplify
	// the user experience. The doctor command can detect issues and offer fixes interactively.
	rootCmd.AddGroup(&cobra.Group{ID: "maint", Title: "Maintenance:"})
	rootCmd.AddGroup(&cobra.Group{ID: "advanced", Title: "Integrations & Advanced:"})

	// Custom help function with semantic coloring (Tufte-inspired)
	// Note: Usage output (shown on errors) is not styled to avoid recursion issues
	rootCmd.SetHelpFunc(colorizedHelpFunc)

	// Initialize route querier for daemon-based route resolution
	// This enables LoadRoutes() to query route beads before falling back to routes.jsonl
	initRouteQuerier()
}

var rootCmd = &cobra.Command{
	Use:   "bd",
	Short: "bd - Dependency-aware issue tracker",
	Long:  `Issues chained together like beads. A lightweight issue tracker with first-class dependency support.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Handle --version flag on root command
		if v, _ := cmd.Flags().GetBool("version"); v {
			fmt.Printf("bd version %s (%s)\n", Version, Build)
			return
		}
		// No subcommand - show help
		_ = cmd.Help()
	},
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize CommandContext to hold runtime state (replaces scattered globals)
		initCommandContext()

		// Reset per-command write tracking (used by Dolt auto-commit).
		commandDidWrite.Store(false)
		commandDidExplicitDoltCommit = false
		commandDidWriteTipMetadata = false
		commandTipIDsShown = make(map[string]struct{})

		// Set up signal-aware context for graceful cancellation
		rootCtx, rootCancel = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

		// Apply verbosity flags early (before any output)
		debug.SetVerbose(verboseFlag)
		debug.SetQuiet(quietFlag)

		// Apply viper configuration if flags weren't explicitly set
		// Priority: flags > viper (config file + env vars) > defaults
		// Do this BEFORE early-return so init/version/help respect config

		// Track flag overrides for notification (only in verbose mode)
		flagOverrides := make(map[string]struct {
			Value  interface{}
			WasSet bool
		})

		// If flag wasn't explicitly set, use viper value
		if !cmd.Flags().Changed("json") {
			jsonOutput = config.GetBool("json")
		} else {
			flagOverrides["json"] = struct {
				Value  interface{}
				WasSet bool
			}{jsonOutput, true}
		}

		if !cmd.Flags().Changed("no-auto-flush") {
			noAutoFlush = config.GetBool("no-auto-flush")
		} else {
			flagOverrides["no-auto-flush"] = struct {
				Value  interface{}
				WasSet bool
			}{noAutoFlush, true}
		}
		if !cmd.Flags().Changed("no-auto-import") {
			noAutoImport = config.GetBool("no-auto-import")
		} else {
			flagOverrides["no-auto-import"] = struct {
				Value  interface{}
				WasSet bool
			}{noAutoImport, true}
		}
		if !cmd.Flags().Changed("readonly") {
			readonlyMode = config.GetBool("readonly")
		} else {
			flagOverrides["readonly"] = struct {
				Value  interface{}
				WasSet bool
			}{readonlyMode, true}
		}
		if !cmd.Flags().Changed("lock-timeout") {
			lockTimeout = config.GetDuration("lock-timeout")
		} else {
			flagOverrides["lock-timeout"] = struct {
				Value  interface{}
				WasSet bool
			}{lockTimeout, true}
		}
		if !cmd.Flags().Changed("db") && dbPath == "" {
			dbPath = config.GetString("db")
		} else if cmd.Flags().Changed("db") {
			flagOverrides["db"] = struct {
				Value  interface{}
				WasSet bool
			}{dbPath, true}
		}
		if !cmd.Flags().Changed("actor") && actor == "" {
			actor = config.GetString("actor")
		} else if cmd.Flags().Changed("actor") {
			flagOverrides["actor"] = struct {
				Value  interface{}
				WasSet bool
			}{actor, true}
		}
		if !cmd.Flags().Changed("dolt-auto-commit") && strings.TrimSpace(doltAutoCommit) == "" {
			doltAutoCommit = config.GetString("dolt.auto-commit")
		} else if cmd.Flags().Changed("dolt-auto-commit") {
			flagOverrides["dolt-auto-commit"] = struct {
				Value  interface{}
				WasSet bool
			}{doltAutoCommit, true}
		}

		// Check for and log configuration overrides (only in verbose mode)
		if verboseFlag {
			overrides := config.CheckOverrides(flagOverrides)
			for _, override := range overrides {
				config.LogOverride(override)
			}
		}

		// Validate Dolt auto-commit mode early so all commands fail fast on invalid config.
		if _, err := getDoltAutoCommitMode(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// GH#1093: Check noDbCommands BEFORE expensive operations (ensureForkProtection,
		// signalOrchestratorActivity) to avoid spawning git subprocesses for simple commands
		// like "bd version" that don't need database access.
		noDbCommands := []string{
			cmdDaemon,
			"__complete",       // Cobra's internal completion command (shell completions work without db)
			"__completeNoDesc", // Cobra's completion without descriptions (used by fish)
			"bash",
			"completion",
			"doctor",
			"fish",
			"help",
			"hooks",
			"human",
			"init",
			"merge",
			"onboard",
			"powershell",
			"prime",
			"quickstart",
			"repair",
			"resolve-conflicts",
			"setup",
			"slack",
			"version",
			"zsh",
		}
		// Check both the command name and parent command name for subcommands
		cmdName := cmd.Name()
		if cmd.Parent() != nil {
			parentName := cmd.Parent().Name()
			if slices.Contains(noDbCommands, parentName) {
				return
			}
		}
		// Special case: "skill prime" needs actor initialization even though
		// top-level "prime" is in noDbCommands. Check full command path.
		isSkillPrime := cmdName == "prime" && cmd.Parent() != nil && cmd.Parent().Name() == "skill"
		if slices.Contains(noDbCommands, cmdName) && !isSkillPrime {
			return
		}

		// Skip for root command with no subcommand (just shows help)
		if cmd.Parent() == nil && cmdName == "bd" {
			return
		}

		// Also skip for --version flag on root command (cmdName would be "bd")
		if v, _ := cmd.Flags().GetBool("version"); v {
			return
		}

		// Signal orchestrator daemon about bd activity (best-effort, for exponential backoff)
		// GH#1093: Moved after noDbCommands check to avoid git subprocesses for simple commands
		defer signalOrchestratorActivity()

		// Protect forks from accidentally committing upstream issue database
		ensureForkProtection()

		// Emit deprecation warnings for local-mode flags when BD_DAEMON_HOST is set.
		// These flags are only meaningful for local/direct storage and are ignored or
		// blocked by the remote daemon. (bd-dx85)
		if remoteHost := rpc.GetDaemonHost(); remoteHost != "" {
			localFlags := map[string]string{
				"db":             "--db is ignored with remote daemon (BD_DAEMON_HOST is set)",
				"lock-timeout":   "--lock-timeout is a SQLite setting, ignored with remote daemon",
				"no-auto-flush":  "--no-auto-flush is a JSONL setting, ignored with remote daemon",
				"no-auto-import": "--no-auto-import is a JSONL setting, ignored with remote daemon",
			}
			for flag, msg := range localFlags {
				if cmd.Flags().Changed(flag) {
					fmt.Fprintf(os.Stderr, "Warning: %s\n", msg)
				}
			}
		}

		// Track if direct mode is forced for this command (profile, edit, doctor, restore)
		forceDirectMode := false

		// Performance profiling setup
		// When --profile is enabled, force direct mode to capture actual database operations
		// rather than just RPC serialization/network overhead. This gives accurate profiles
		// of the storage layer, query performance, and business logic.
		if profileEnabled {
			forceDirectMode = true
			timestamp := time.Now().Format("20060102-150405")
			if f, _ := os.Create(fmt.Sprintf("bd-profile-%s-%s.prof", cmd.Name(), timestamp)); f != nil {
				profileFile = f
				_ = pprof.StartCPUProfile(f)
			}
			if f, _ := os.Create(fmt.Sprintf("bd-trace-%s-%s.out", cmd.Name(), timestamp)); f != nil {
				traceFile = f
				_ = trace.Start(f)
			}
		}

		// Force direct mode for human-only interactive commands
		// edit: can take minutes in $EDITOR, local daemon connection may time out (GH #227)
		// Exception: when BD_DAEMON_HOST is set (remote daemon), we must use daemon RPC
		// since direct database access is blocked. The edit command's RPC calls (Show + Update)
		// are short-lived; only the local $EDITOR session is long-running. (bd-bdbt)
		if cmd.Name() == "edit" && rpc.GetDaemonHost() == "" {
			forceDirectMode = true
		}

		// Set auto-flush based on flag (invert no-auto-flush)
		autoFlushEnabled = !noAutoFlush

		// Set auto-import based on flag (invert no-auto-import)
		autoImportEnabled = !noAutoImport

		// Initialize database path
		if dbPath == "" {
			// Use public API to find database (same logic as extensions)
			if foundDB := beads.FindDatabasePath(); foundDB != "" {
				dbPath = foundDB
			} else {
				// No database found
				beadsDir := beads.FindBeadsDir()

				// Allow some commands to run without a database
				// - import: auto-initializes database if missing
				// - setup: creates editor integration files (no DB needed)
				// - config set/get for yaml-only keys: writes to config.yaml, not SQLite (GH#536)
				isYamlOnlyConfigOp := false
				if (cmd.Name() == "set" || cmd.Name() == "get") && cmd.Parent() != nil && cmd.Parent().Name() == "config" {
					if len(args) > 0 && config.IsYamlOnlyKey(args[0]) {
						isYamlOnlyConfigOp = true
					}
				}

				// Allow read-only commands to auto-bootstrap from JSONL (GH#b09)
				// This enables `bd show` after cold-start when DB is missing
				canAutoBootstrap := false
				if isReadOnlyCommand(cmd) && beadsDir != "" {
					jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
					if _, err := os.Stat(jsonlPath); err == nil {
						canAutoBootstrap = true
						debug.Logf("cold-start bootstrap: JSONL exists, allowing auto-create for %s", cmd.Name())
					}
				}

				if cmd.Name() != "import" && cmd.Name() != "setup" && !isYamlOnlyConfigOp && !canAutoBootstrap {
					// No database found - provide context-aware error message
					fmt.Fprintf(os.Stderr, "Error: no beads database found\n")
					fmt.Fprintf(os.Stderr, "Hint: run 'bd connect' to connect to a remote daemon (BD_DAEMON_HOST)\n")
					fmt.Fprintf(os.Stderr, "      or run 'bd init' to create a local workspace\n")
					fmt.Fprintf(os.Stderr, "      or set BEADS_DIR to point to your .beads directory\n")
					os.Exit(1)
				}
				// For import/setup commands, set default database path
				// Invariant: dbPath must always be absolute for filepath.Rel() compatibility
				// in daemon sync-branch code path. Use CanonicalizePath for OS-agnostic
				// handling (symlinks, case normalization on macOS).
				//
				// IMPORTANT: Use FindBeadsDir() to get the correct .beads directory,
				// which follows redirect files. Without this, a redirected .beads
				// would create a local database instead of using the redirect target.
				// (GH#bd-0qel)
				targetBeadsDir := beads.FindBeadsDir()
				if targetBeadsDir == "" {
					targetBeadsDir = ".beads"
				}
				dbPath = utils.CanonicalizePath(filepath.Join(targetBeadsDir, beads.CanonicalDatabaseName))
			}
		}

		// Set actor for audit trail
		actor = getActorWithGit()

		// Track bd version changes
		// Best-effort tracking - failures are silent
		trackBdVersion()

		// Initialize daemon status
		socketPath := getSocketPath()
		daemonStatus = DaemonStatus{
			Mode:       "direct",
			Connected:  false,
			Degraded:   true,
			SocketPath: socketPath,
		}

		// Doctor should always run in direct mode. It's specifically used to diagnose and
		// repair daemon/DB issues, so attempting to connect to (or auto-start) a daemon
		// can add noise and timeouts.
		if cmd.Name() == "doctor" {
			forceDirectMode = true
		}

		// Restore should always run in direct mode. It performs git checkouts to read
		// historical issue data, which could conflict with daemon operations.
		if cmd.Name() == "restore" {
			forceDirectMode = true
		}

		// Try to connect to daemon first (unless direct mode is forced or worktree safety check fails)
		if forceDirectMode {
			// When BD_DAEMON_HOST is set, direct mode is not allowed for most commands
			// since the remote daemon should handle all operations. Only doctor is exempt
			// because it specifically diagnoses daemon connectivity issues. (bd-lkks)
			if remoteHost := rpc.GetDaemonHost(); remoteHost != "" && cmd.Name() != "doctor" {
				fmt.Fprintf(os.Stderr, "Error: this command requested direct database access, but BD_DAEMON_HOST is set (%s)\n", remoteHost)
				fmt.Fprintf(os.Stderr, "Direct mode (--profile, restore) is not available with a remote daemon.\n")
				fmt.Fprintf(os.Stderr, "Hint: unset BD_DAEMON_HOST to use local mode, or run the command without --profile\n")
				os.Exit(1)
			}
			debug.Logf("direct mode forced for this command")
		} else if shouldDisableDaemonForWorktree() {
			// In a git worktree without sync-branch configured - daemon is unsafe
			// because all worktrees share the same .beads directory and the daemon
			// would commit to whatever branch its working directory has checked out.
			daemonStatus.Detail = "git worktree without sync-branch"
			debug.Logf("git worktree detected without sync-branch, using direct mode for safety")
		} else {
			// Attempt daemon connection (auto-selects TCP via BD_DAEMON_HOST or local Unix socket)
			client, err := rpc.TryConnectAuto(socketPath)
			if err == nil && client != nil {
				// Set expected database path for validation (skip for remote TCP connections
				// where local path doesn't match remote daemon's database)
				if dbPath != "" && rpc.GetDaemonHost() == "" {
					absDBPath, _ := filepath.Abs(dbPath)
					client.SetDatabasePath(absDBPath)
				}

				// Perform health check
				health, healthErr := client.Health()
				if healthErr == nil && health.Status == statusHealthy {
					// Check version compatibility
					if !health.Compatible {
						debug.Logf("daemon version mismatch (daemon: %s, client: %s), restarting daemon",
							health.Version, Version)
						_ = client.Close()

						// Kill old daemon and restart with new version
						if restartDaemonForVersionMismatch() {
							// Retry connection after restart
							client, err = rpc.TryConnectAuto(socketPath)
							if err == nil && client != nil {
								if dbPath != "" && rpc.GetDaemonHost() == "" {
									absDBPath, _ := filepath.Abs(dbPath)
									client.SetDatabasePath(absDBPath)
								}
								health, healthErr = client.Health()
								if healthErr == nil && health.Status == statusHealthy {
									client.SetActor(actor)
									daemonClient = client
									daemonStatus.Mode = cmdDaemon
									daemonStatus.Connected = true
									daemonStatus.Degraded = false
									daemonStatus.Health = health.Status
									debug.Logf("connected to restarted daemon (version: %s)", health.Version)
									warnWorktreeDaemon(dbPath)
									return
								}
							}
						}
						// If restart failed, fall through to direct mode
						daemonStatus.Detail = fmt.Sprintf("version mismatch (daemon: %s, client: %s) and restart failed",
							health.Version, Version)
					} else {
						// Daemon is healthy and compatible - use it
						client.SetActor(actor)
						daemonClient = client
						daemonStatus.Mode = cmdDaemon
						daemonStatus.Connected = true
						daemonStatus.Degraded = false
						daemonStatus.Health = health.Status
						debug.Logf("connected to daemon at %s (health: %s)", socketPath, health.Status)
						// Warn if using daemon with git worktrees
						warnWorktreeDaemon(dbPath)
						// Initialize hook runner (hooks run on client side, not daemon)
						if dbPath != "" {
							beadsDir := filepath.Dir(dbPath)
							hooksDir := filepath.Join(beadsDir, "hooks")
							debug.Logf("initializing hook runner: dbPath=%s, hooksDir=%s", dbPath, hooksDir)
							hookRunner = hooks.NewRunner(hooksDir)
						} else {
							debug.Logf("hook runner not initialized: dbPath is empty")
						}
						return // Skip direct storage initialization
					}
				} else {
					// Health check failed or daemon unhealthy
					_ = client.Close()
					if healthErr != nil {
						daemonStatus.Detail = healthErr.Error()
						debug.Logf("daemon health check failed: %v", healthErr)
					} else {
						daemonStatus.Health = health.Status
						daemonStatus.Detail = health.Error
						debug.Logf("daemon unhealthy (status=%s): %s", health.Status, health.Error)
					}
				}
			} else {
				// Connection failed
				if err != nil {
					daemonStatus.Detail = err.Error()
					debug.Logf("daemon connect failed at %s: %v", socketPath, err)
				}
			}

			// If BD_DAEMON_HOST is set, fail hard instead of falling back to local mode.
			// The user explicitly requested a remote daemon - silent fallback would be confusing.
			if remoteHost := rpc.GetDaemonHost(); remoteHost != "" {
				fmt.Fprintf(os.Stderr, "Error: failed to connect to remote daemon at %s\n", remoteHost)
				if daemonStatus.Detail != "" {
					fmt.Fprintf(os.Stderr, "Detail: %s\n", daemonStatus.Detail)
				}
				fmt.Fprintf(os.Stderr, "Hint: check that the daemon is running and BD_DAEMON_TOKEN is correct\n")
				os.Exit(1)
			}

			// Daemon not available - log the reason and continue to direct storage fallback
			debug.Logf("daemon not available (detail: %s)", daemonStatus.Detail)
		}

		// SOFT DISABLE: Direct mode is disabled for Dolt server backend (hq-463d49)
		// Direct mode creates a new connection pool per bd invocation, causing
		// massive connection churn and CPU overhead. Require daemon mode instead.
		// Exclude daemon commands - they need direct mode to start/manage the daemon.
		// Also exclude --rig flag usage - it opens a different rig's database directly (hq-5e851d).
		isDaemonCommand := cmd.Name() == "daemon" || cmd.Name() == "daemons" ||
			(cmd.Parent() != nil && (cmd.Parent().Name() == "daemon" || cmd.Parent().Name() == "daemons"))
		rigFlag, _ := cmd.Flags().GetString("rig")
		isCrossRig := rigFlag != ""
		// Only enforce daemon requirement if direct mode wasn't explicitly forced
		if daemonClient == nil && !forceDirectMode && !isDaemonCommand && !isCrossRig {
			// Check if Dolt server mode is enabled
			checkBeadsDir := beads.FindBeadsDir()
			if checkBeadsDir == "" && dbPath != "" {
				checkBeadsDir = filepath.Dir(dbPath)
			}
			if checkBeadsDir != "" {
				if cfg, cfgErr := configfile.Load(checkBeadsDir); cfgErr == nil && cfg != nil && cfg.IsDoltServerMode() {
					fmt.Fprintf(os.Stderr, "Error: daemon connection required for Dolt server mode\n")
					fmt.Fprintf(os.Stderr, "Direct mode is disabled to prevent connection churn (see hq-463d49)\n")
					fmt.Fprintf(os.Stderr, "\nDaemon status: not connected\n")
					if daemonStatus.Detail != "" {
						fmt.Fprintf(os.Stderr, "Detail: %s\n", daemonStatus.Detail)
					}
					fmt.Fprintf(os.Stderr, "\nTo fix: start daemon with 'bd daemon start'\n")
					os.Exit(1)
				}
			}
		}

		// Check if this is a read-only command (GH#804)
		// Read-only commands open SQLite in read-only mode to avoid modifying
		// the database file (which breaks file watchers).
		useReadOnly := isReadOnlyCommand(cmd)

		// Fall back to direct storage access
		var err error
		var needsBootstrap bool // Track if DB needs initial import (GH#b09)

		// Find the beads directory - prefer FindBeadsDir() which respects BEADS_DIR env
		// and follows redirects. Fall back to deriving from dbPath for explicit --db usage.
		beadsDir := beads.FindBeadsDir()
		if beadsDir == "" {
			beadsDir = filepath.Dir(dbPath)
		}

		// Auto-migrate database on version bump
		// Skip for read-only commands - they can't write anyway
		// Do this AFTER daemon check but BEFORE opening database for main operation
		// This ensures: 1) no daemon has DB open, 2) we don't open DB twice
		if !useReadOnly {
			autoMigrateOnVersionBump(beadsDir)
		}

		// Create storage with appropriate options using NewFromConfig
		// This ensures server mode settings from metadata.json are respected
		opts := factory.Options{
			ReadOnly:    useReadOnly,
			LockTimeout: lockTimeout,
		}

		// Use NewFromConfigWithOptions which handles:
		// - Backend detection from config
		// - Dolt server mode with GetDoltDatabase() for proper database name (7e3b828f)
		// - All server connection settings from metadata.json
		store, err = factory.NewFromConfigWithOptions(rootCtx, beadsDir, opts)
		if err != nil && useReadOnly {
			// If read-only fails (e.g., DB doesn't exist), fall back to read-write
			// This handles the case where user runs "bd list" before "bd init"
			debug.Logf("read-only open failed, falling back to read-write: %v", err)
			opts.ReadOnly = false
			store, err = factory.NewFromConfigWithOptions(rootCtx, beadsDir, opts)
			needsBootstrap = true // New DB needs auto-import (GH#b09)
		}

		// Track final read-only state for staleness checks (GH#1089)
		// opts.ReadOnly may have changed if read-only open failed and fell back
		storeIsReadOnly = opts.ReadOnly

		if err != nil {
			// Check for fresh clone scenario
			if handleFreshCloneError(err, beadsDir) {
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Error: failed to open database: %v\n", err)
			os.Exit(1)
		}

		// Skip dirty tracking in dolt-native mode to eliminate write amplification (bd-8csx)
		if !ShouldExportJSONL(rootCtx, store) {
			if s, ok := store.(interface{ SetSkipDirtyTracking(bool) }); ok {
				s.SetSkipDirtyTracking(true)
			}
		}

		// Mark store as active for flush goroutine safety
		storeMutex.Lock()
		storeActive = true
		storeMutex.Unlock()

		// Initialize flush manager (fixes race condition in auto-flush)
		// Skip for read-only commands - they don't write anything (GH#804)
		// For in-process test scenarios where commands run multiple times,
		// we create a new manager each time. Shutdown() is idempotent so
		// PostRun can safely shutdown whichever manager is active.
		if !useReadOnly {
			flushManager = NewFlushManager(autoFlushEnabled, getDebounceDuration())
		}

		// Initialize hook runner
		// dbPath is .beads/something.db, so workspace root is parent of .beads
		if dbPath != "" {
			beadsDir := filepath.Dir(dbPath)
			hookRunner = hooks.NewRunner(filepath.Join(beadsDir, "hooks"))
		}

		// Warn if multiple databases detected in directory hierarchy
		warnMultipleDatabases(dbPath)

		// Auto-import if JSONL is newer than DB (e.g., after git pull)
		// Skip for import command itself to avoid recursion
		// Skip for delete command to prevent resurrection of deleted issues
		// Skip if sync --dry-run to avoid modifying DB in dry-run mode
		// Skip for read-only commands - they can't write anyway (GH#804)
		// Exception: allow auto-import for read-only commands that fell back to
		// read-write mode due to missing DB (needsBootstrap) - fixes GH#b09
		if cmd.Name() != "import" && cmd.Name() != "delete" && autoImportEnabled && (!useReadOnly || needsBootstrap) {
			// Check if this is sync command with --dry-run flag
			if cmd.Name() == "sync" {
				if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
					// Skip auto-import in dry-run mode
					debug.Logf("auto-import skipped for sync --dry-run")
				} else {
					autoImportIfNewer()
				}
			} else {
				autoImportIfNewer()
			}
		}

		// Load molecule templates from hierarchical catalog locations
		// Templates are loaded after auto-import to ensure the database is up-to-date.
		// Skip for import command to avoid conflicts during import operations.
		if cmd.Name() != "import" && store != nil {
			beadsDir := filepath.Dir(dbPath)
			loader := molecules.NewLoader(store)
			if result, err := loader.LoadAll(rootCtx, beadsDir); err != nil {
				debug.Logf("warning: failed to load molecules: %v", err)
			} else if result.Loaded > 0 {
				debug.Logf("loaded %d molecules from %v", result.Loaded, result.Sources)
			}
		}

		// Tips (including sync conflict proactive checks) are shown via maybeShowTip()
		// after successful command execution, not in PreRun

		// Sync all state to CommandContext for unified access
		syncCommandContext()
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		// Close daemon client
		_ = daemonClient.Close()
		if profileFile != nil {
			pprof.StopCPUProfile()
			_ = profileFile.Close()
		}
		if traceFile != nil {
			trace.Stop()
			_ = traceFile.Close()
		}

		// Cancel the signal context to clean up resources
		if rootCancel != nil {
			rootCancel()
		}
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
