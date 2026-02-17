package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/storage/dolt"
)

var (
	dbPath     string
	actor      string
	store      *dolt.DoltStore
	jsonOutput bool

	// Signal-aware context for graceful cancellation
	rootCtx    context.Context
	rootCancel context.CancelFunc

	// Hook runner for extensibility
	hookRunner *hooks.Runner

	// Store concurrency protection
	storeMutex  sync.Mutex // Protects store access from background goroutine
	storeActive = false    // Tracks if store is available

	// No-db mode
	noDb bool // Use --no-db mode: load from JSONL, write back after each command

	// Version upgrade tracking
	versionUpgradeDetected = false // Set to true if bd version changed since last run
	previousVersion        = ""    // The last bd version user had (empty = first run or unknown)
	upgradeAcknowledged    = false // Set to true after showing upgrade notification once per session
)
var (
	sandboxMode     bool
	allowStale      bool               // Use --allow-stale: skip staleness check (emergency escape hatch)
	readonlyMode    bool               // Read-only mode: block write operations (for worker sandboxes)
	storeIsReadOnly bool               // Track if store was opened read-only (for staleness checks)
	lockTimeout     = 30 * time.Second // Dolt open timeout (fixed default)
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
// These commands open the store in read-only mode. See GH#804.
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
	"current":    true, // bd sync mode current
	// NOTE: "export" is NOT read-only - it writes to clear dirty issues and update jsonl_file_hash
}

// isReadOnlyCommand returns true if the command only reads from the database.
// This is used to open SQLite in read-only mode, preventing file modifications
// that would trigger file watchers. See GH#804.
func isReadOnlyCommand(cmdName string) bool {
	return readOnlyCommands[cmdName]
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

	return "unknown"
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
	rootCmd.PersistentFlags().BoolVar(&sandboxMode, "sandbox", false, "Sandbox mode: disables auto-sync")
	rootCmd.PersistentFlags().BoolVar(&allowStale, "allow-stale", false, "Allow operations on potentially stale data (skip staleness check)")
	rootCmd.PersistentFlags().BoolVar(&readonlyMode, "readonly", false, "Read-only mode: block write operations (for worker sandboxes)")
	rootCmd.PersistentFlags().StringVar(&doltAutoCommit, "dolt-auto-commit", "", "Dolt backend: auto-commit after write commands (off|on). Default: on for embedded, off for server mode. Override via config key dolt.auto-commit")
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
		_ = cmd.Help() // Help() always returns nil for cobra commands
	},
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// --- Phase 1: Universal setup (runs for every command) ---
		initCommandContext()
		resetWriteTracking()
		setupSignalContext()
		applyVerbosityFlags()
		validateEnvVars()
		applyViperOverrides(cmd)
		syncFlagBoundGlobals()
		validateDoltAutoCommitFlag()

		// --- Phase 2: Early exit for commands that don't need a database ---
		if isNoDbCommand(cmd) {
			return
		}

		// --- Phase 3: Pre-database setup ---
		defer signalOrchestratorActivity()
		ensureForkProtection()
		setupProfiling(cmd)
		detectSandbox(cmd)

		// --- Phase 4: No-db / JSONL-only mode ---
		if initNoDbIfEnabled() {
			return
		}

		// --- Phase 5: Database discovery and path resolution ---
		if discoverDatabasePath(cmd, args) {
			return // no-db mode was auto-activated from JSONL-only config
		}

		// --- Phase 6: Actor, version tracking, store, and extensions ---
		setupActor()
		trackVersionChanges(cmd)
		openStore(cmd)
		initHookRunner()
		warnMultipleDbs()
		loadMoleculeTemplates(cmd)

		// NOTE: No syncCommandContext() needed -- prerun functions now use
		// set* accessors that write to both cmdCtx and the global.
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if isNoDb() {
			return
		}

		// Dolt auto-commit: after a successful write command (and after final flush),
		// create a Dolt commit so changes don't remain only in the working set.
		if commandDidWrite.Load() && !commandDidExplicitDoltCommit {
			if err := maybeAutoCommit(getRootContext(), doltAutoCommitParams{Command: cmd.Name()}); err != nil {
				fmt.Fprintf(os.Stderr, "Error: dolt auto-commit failed: %v\n", err)
				os.Exit(1)
			}
		}

		// Tip metadata auto-commit: if a tip was shown, create a separate Dolt commit for the
		// tip_*_last_shown metadata updates. This may happen even for otherwise read-only commands.
		if commandDidWriteTipMetadata && len(commandTipIDsShown) > 0 {
			// Only applies when dolt auto-commit is enabled and backend is versioned (Dolt).
			if mode, err := getDoltAutoCommitMode(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: dolt tip auto-commit failed: %v\n", err)
				os.Exit(1)
			} else if mode == doltAutoCommitOn {
				// Apply tip metadata writes now (deferred in recordTipShown for Dolt).
				ctx := getRootContext()
				s := getStore()
				for tipID := range commandTipIDsShown {
					key := fmt.Sprintf("tip_%s_last_shown", tipID)
					value := time.Now().Format(time.RFC3339)
					if err := s.SetMetadata(ctx, key, value); err != nil {
						fmt.Fprintf(os.Stderr, "Error: dolt tip auto-commit failed: %v\n", err)
						os.Exit(1)
					}
				}

				ids := make([]string, 0, len(commandTipIDsShown))
				for tipID := range commandTipIDsShown {
					ids = append(ids, tipID)
				}
				msg := formatDoltAutoCommitMessage("tip", getActor(), ids)
				if err := maybeAutoCommit(ctx, doltAutoCommitParams{Command: "tip", MessageOverride: msg}); err != nil {
					fmt.Fprintf(os.Stderr, "Error: dolt tip auto-commit failed: %v\n", err)
					os.Exit(1)
				}
			}
		}

		// Signal that store is closing (prevents background flush from accessing closed store)
		lockStore()
		setStoreActive(false)
		unlockStore()

		if s := getStore(); s != nil {
			_ = s.Close()
		}

		if pf := getProfileFile(); pf != nil {
			pprof.StopCPUProfile()
			_ = pf.Close()
		}
		if tf := getTraceFile(); tf != nil {
			trace.Stop()
			_ = tf.Close()
		}

		// Cancel the signal context to clean up resources
		if rootCancel != nil {
			rootCancel()
		}
	},
}

// blockedEnvVars lists environment variables that must not be set because they
// could silently override the storage backend via viper's AutomaticEnv, causing
// data fragmentation between sqlite and dolt (bd-hevyw).
var blockedEnvVars = []string{"BD_BACKEND", "BD_DATABASE_BACKEND"}

// checkBlockedEnvVars returns an error if any blocked env vars are set.
func checkBlockedEnvVars() error {
	for _, name := range blockedEnvVars {
		if os.Getenv(name) != "" {
			return fmt.Errorf("%s env var is not supported and has been removed to prevent data fragmentation.\n"+
				"The storage backend is set in .beads/metadata.json. To change it, use: bd migrate dolt", name)
		}
	}
	return nil
}

func main() {
	// BD_NAME overrides the binary name in help text (e.g. BD_NAME=ops makes
	// "ops --help" show "ops" instead of "bd"). Useful for multi-instance
	// setups where wrapper scripts set BEADS_DIR for routing.
	if name := os.Getenv("BD_NAME"); name != "" {
		rootCmd.Use = name
	}

	// Register --all flag on Cobra's auto-generated help command.
	// Must be called after init() so all subcommands are registered and
	// Cobra has created its default help command.
	rootCmd.InitDefaultHelpCmd()
	registerHelpAllFlag()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
