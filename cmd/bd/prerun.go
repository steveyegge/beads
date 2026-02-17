package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"runtime/trace"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/molecules"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/utils"
)

// --------------------------------------------------------------------------
// Bootstrap pipeline steps for PersistentPreRun
//
// Each function represents a single concern in the initialization sequence.
// The PersistentPreRun in main.go calls these in order, making the boot
// sequence self-documenting. No behavior changes — purely structural.
// --------------------------------------------------------------------------

// syncFlagBoundGlobals pushes cobra flag-bound global values into cmdCtx.
// Cobra's PersistentFlags().BoolVar() writes directly to package globals.
// This function copies those values into the CommandContext so accessor
// functions return the correct values. Must be called after applyViperOverrides
// which may further modify the flag-bound globals.
func syncFlagBoundGlobals() {
	if cmdCtx == nil {
		return
	}
	cmdCtx.DBPath = dbPath
	cmdCtx.Actor = actor
	cmdCtx.JSONOutput = jsonOutput
	cmdCtx.SandboxMode = sandboxMode
	cmdCtx.AllowStale = allowStale
	cmdCtx.NoDb = noDb
	cmdCtx.ReadonlyMode = readonlyMode
	cmdCtx.LockTimeout = lockTimeout
	cmdCtx.Verbose = verboseFlag
	cmdCtx.Quiet = quietFlag
}

// resetWriteTracking resets per-command write tracking flags used by Dolt
// auto-commit to decide whether a commit is needed after the command.
func resetWriteTracking() {
	commandDidWrite.Store(false)
	commandDidExplicitDoltCommit = false
	commandDidWriteTipMetadata = false
	commandTipIDsShown = make(map[string]struct{})
}

// setupSignalContext creates a context that cancels on SIGINT/SIGTERM for
// graceful shutdown of long-running operations.
func setupSignalContext() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	setRootContext(ctx, cancel)
}

// applyVerbosityFlags propagates --verbose and --quiet flags to the debug
// package so all subsequent log output respects the user's preference.
func applyVerbosityFlags() {
	setVerbose(verboseFlag)
	setQuiet(quietFlag)
	debug.SetVerbose(verboseFlag)
	debug.SetQuiet(quietFlag)
}

// validateEnvVars blocks dangerous environment variables that could silently
// override the storage backend via viper's AutomaticEnv, causing data
// fragmentation between sqlite and dolt (bd-hevyw).
func validateEnvVars() {
	if err := checkBlockedEnvVars(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// flagOverrideEntry captures whether a flag was explicitly set on the command line.
type flagOverrideEntry struct {
	Value  interface{}
	WasSet bool
}

// applyViperOverrides merges viper config values (from config file + env vars)
// into flags that weren't explicitly set on the command line.
// Priority: flags > viper (config file + env vars) > defaults.
func applyViperOverrides(cmd *cobra.Command) {
	flagOverrides := make(map[string]flagOverrideEntry)

	if !cmd.Flags().Changed("json") {
		setJSONOutput(config.GetBool("json"))
	} else {
		flagOverrides["json"] = flagOverrideEntry{jsonOutput, true}
	}
	if !cmd.Flags().Changed("readonly") {
		setReadonlyMode(config.GetBool("readonly"))
	} else {
		flagOverrides["readonly"] = flagOverrideEntry{readonlyMode, true}
	}
	if !cmd.Flags().Changed("db") && dbPath == "" {
		setDBPath(config.GetString("db"))
	} else if cmd.Flags().Changed("db") {
		flagOverrides["db"] = flagOverrideEntry{dbPath, true}
	}
	if !cmd.Flags().Changed("actor") && actor == "" {
		setActor(config.GetString("actor"))
	} else if cmd.Flags().Changed("actor") {
		flagOverrides["actor"] = flagOverrideEntry{actor, true}
	}
	if !cmd.Flags().Changed("dolt-auto-commit") && strings.TrimSpace(doltAutoCommit) == "" {
		doltAutoCommit = config.GetString("dolt.auto-commit")
	} else if cmd.Flags().Changed("dolt-auto-commit") {
		flagOverrides["dolt-auto-commit"] = flagOverrideEntry{doltAutoCommit, true}
	}

	if verboseFlag {
		// Re-pack into the shape config.CheckOverrides expects.
		packed := make(map[string]struct {
			Value  interface{}
			WasSet bool
		}, len(flagOverrides))
		for k, v := range flagOverrides {
			packed[k] = struct {
				Value  interface{}
				WasSet bool
			}{v.Value, v.WasSet}
		}
		overrides := config.CheckOverrides(packed)
		for _, override := range overrides {
			config.LogOverride(override)
		}
	}
}

// validateDoltAutoCommitFlag fails fast if the dolt-auto-commit value is
// invalid so all commands surface the misconfiguration immediately.
func validateDoltAutoCommitFlag() {
	if _, err := getDoltAutoCommitMode(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// noDbCommands lists commands that do not require database access.
// Checked before expensive operations (fork protection, orchestrator signal)
// to avoid spawning git subprocesses for simple commands like "bd version".
var noDbCommandsList = []string{
	"__complete",       // Cobra's internal completion command (shell completions work without db)
	"__completeNoDesc", // Cobra's completion without descriptions (used by fish)
	"bash",
	"completion",
	"doctor",
	"fish",
	"help",
	"hook", // manages its own store lifecycle; double-open deadlocks embedded Dolt (#1719)
	"hooks",
	"human",
	"init",
	"merge",
	"migrate", // manages its own store lifecycle; double-open deadlocks embedded Dolt (#1668)
	"onboard",
	"powershell",
	"prime",
	"quickstart",
	"resolve-conflicts",
	"setup",
	"version",
	"zsh",
}

// isNoDbCommand returns true if the command (or its parent) does not need a
// database, or if the root command is invoked without a subcommand (help), or
// if --version is set on root. Returning true means PersistentPreRun should
// return early.
func isNoDbCommand(cmd *cobra.Command) bool {
	cmdName := cmd.Name()
	if cmd.Parent() != nil {
		if slices.Contains(noDbCommandsList, cmd.Parent().Name()) {
			return true
		}
	}
	if slices.Contains(noDbCommandsList, cmdName) {
		return true
	}

	// Skip for root command with no subcommand (just shows help)
	if cmd.Parent() == nil && cmdName == cmd.Use {
		return true
	}

	// Also skip for --version flag on root command
	if v, _ := cmd.Flags().GetBool("version"); v {
		return true
	}

	return false
}

// setupProfiling starts CPU profiling and tracing when --profile is set.
func setupProfiling(cmd *cobra.Command) {
	if !profileEnabled {
		return
	}
	timestamp := time.Now().Format("20060102-150405")
	if f, _ := os.Create(fmt.Sprintf("bd-profile-%s-%s.prof", cmd.Name(), timestamp)); f != nil {
		setProfileFile(f)
		_ = pprof.StartCPUProfile(f)
	}
	if f, _ := os.Create(fmt.Sprintf("bd-trace-%s-%s.out", cmd.Name(), timestamp)); f != nil {
		setTraceFile(f)
		_ = trace.Start(f)
	}
}

// detectSandbox auto-detects sandboxed environments (e.g. containers, MCP) and
// enables sandbox mode when the --sandbox flag wasn't explicitly set.
func detectSandbox(cmd *cobra.Command) {
	if !cmd.Flags().Changed("sandbox") {
		if isSandboxed() {
			setSandboxMode(true)
			fmt.Fprintf(os.Stderr, "\u2139\ufe0f  Sandbox detected, using direct mode\n")
		}
	}
}

// initNoDbIfEnabled handles --no-db mode or auto-detected JSONL-only mode.
// Returns true if no-db mode is active and the caller should skip database
// initialization.
func initNoDbIfEnabled() bool {
	if !noDb {
		return false
	}
	if err := initializeNoDbMode(); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing --no-db mode: %v\n", err)
		os.Exit(1)
	}
	setActor(getActorWithGit())
	return true
}

// discoverDatabasePath resolves the database path via --db flag, auto-discovery,
// JSONL-only fallback, or cold-start bootstrap. Returns true if the caller
// should skip the rest of initialization (because no-db mode was activated or
// the command can run without a database).
func discoverDatabasePath(cmd *cobra.Command, args []string) bool {
	if dbPath != "" {
		return false
	}

	// Use public API to find database (same logic as extensions)
	if foundDB := beads.FindDatabasePath(); foundDB != "" {
		setDBPath(foundDB)
		return false
	}

	// No database found - check if this is JSONL-only mode
	beadsDir := beads.FindBeadsDir()
	if beadsDir != "" {
		if handleJsonlOnlyMode(beadsDir) {
			return true
		}
	}

	// Check if the command can run without a database
	if canRunWithoutDb(cmd, args, beadsDir) {
		setDefaultDbPath()
		return false
	}

	// No database found - emit context-aware error
	emitNoDatabaseError(cmd, beadsDir)
	os.Exit(1)
	return false // unreachable
}

// handleJsonlOnlyMode checks for JSONL-only configuration and activates no-db
// mode if appropriate. Returns true if no-db mode was activated.
func handleJsonlOnlyMode(beadsDir string) bool {
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	jsonlExists := false
	if _, err := os.Stat(jsonlPath); err == nil {
		jsonlExists = true
	}

	isNoDbMode := isNoDbModeConfigured(beadsDir)

	if jsonlExists && isNoDbMode {
		setNoDb(true)
		if err := initializeNoDbMode(); err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing JSONL-only mode: %v\n", err)
			os.Exit(1)
		}
		setActor(getActorWithGit())
		return true
	}
	return false
}

// canRunWithoutDb determines whether the current command can proceed without a
// database (e.g. import, setup, yaml-only config ops, read-only cold-start
// bootstrap).
func canRunWithoutDb(cmd *cobra.Command, args []string, beadsDir string) bool {
	if cmd.Name() == "import" || cmd.Name() == "setup" {
		return true
	}

	// config set/get for yaml-only keys: writes to config.yaml, not SQLite (GH#536)
	if (cmd.Name() == "set" || cmd.Name() == "get") && cmd.Parent() != nil && cmd.Parent().Name() == "config" {
		if len(args) > 0 && config.IsYamlOnlyKey(args[0]) {
			return true
		}
	}

	// Allow read-only commands to auto-bootstrap from JSONL (GH#b09)
	if isReadOnlyCommand(cmd.Name()) && beadsDir != "" {
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if _, err := os.Stat(jsonlPath); err == nil {
			configuredBackend := dolt.GetBackendFromConfig(beadsDir)
			if configuredBackend == configfile.BackendDolt {
				fmt.Fprintf(os.Stderr, "Error: Dolt backend configured but database not found\n")
				fmt.Fprintf(os.Stderr, "The .beads/metadata.json specifies backend: dolt\n")
				fmt.Fprintf(os.Stderr, "but no Dolt database was found. Check that the Dolt server is running.\n")
				fmt.Fprintf(os.Stderr, "\nHint: run 'bd doctor --fix' to diagnose and repair\n")
				os.Exit(1)
			}
			debug.Logf("cold-start bootstrap: JSONL exists, allowing auto-create for %s", cmd.Name())
			return true
		}
	}

	return false
}

// setDefaultDbPath sets the database path for commands that auto-create it
// (import, setup, cold-start bootstrap).
func setDefaultDbPath() {
	targetBeadsDir := beads.FindBeadsDir()
	if targetBeadsDir == "" {
		targetBeadsDir = ".beads"
	}
	setDBPath(utils.CanonicalizePath(filepath.Join(targetBeadsDir, beads.CanonicalDatabaseName)))
}

// emitNoDatabaseError prints context-aware error messages when no database is
// found and the command requires one.
func emitNoDatabaseError(cmd *cobra.Command, beadsDir string) {
	fmt.Fprintf(os.Stderr, "Error: no beads database found\n")

	if beadsDir != "" {
		jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
		if _, err := os.Stat(jsonlPath); err == nil {
			fmt.Fprintf(os.Stderr, "\nFound JSONL file: %s\n", jsonlPath)
			fmt.Fprintf(os.Stderr, "This looks like a fresh clone or JSONL-only project.\n\n")
			fmt.Fprintf(os.Stderr, "Options:\n")
			fmt.Fprintf(os.Stderr, "  \u2022 Run 'bd init' to create database and import issues\n")
			fmt.Fprintf(os.Stderr, "  \u2022 Use 'bd --no-db %s' for JSONL-only mode\n", cmd.Name())
			fmt.Fprintf(os.Stderr, "  \u2022 Add 'no-db: true' to .beads/config.yaml for permanent JSONL-only mode\n")
			return
		}
	}

	fmt.Fprintf(os.Stderr, "Hint: run 'bd init' to create a database in the current directory\n")
	fmt.Fprintf(os.Stderr, "      or use 'bd --no-db' to work with JSONL only (no database)\n")
	fmt.Fprintf(os.Stderr, "      or set BEADS_DIR to point to your .beads directory\n")
}

// setupActor resolves and sets the actor identity for the audit trail.
func setupActor() {
	setActor(getActorWithGit())
}

// trackVersionChanges records the current bd version and triggers auto-migration
// on version bumps.
func trackVersionChanges(cmd *cobra.Command) {
	trackBdVersion()

	useReadOnly := isReadOnlyCommand(cmd.Name())
	if !useReadOnly {
		autoMigrateOnVersionBump(filepath.Dir(dbPath))
	}
}

// openStore initializes the Dolt storage backend: builds config, bootstraps
// embedded Dolt if needed, opens the connection, and marks the store as active.
func openStore(cmd *cobra.Command) {
	useReadOnly := isReadOnlyCommand(cmd.Name())

	var err error
	beadsDir := filepath.Dir(dbPath)

	doltPath := filepath.Join(beadsDir, "dolt")
	doltCfg := &dolt.Config{
		ReadOnly: useReadOnly,
	}

	if useReadOnly {
		doltCfg.OpenTimeout = 5 * time.Second
	} else {
		doltCfg.OpenTimeout = 15 * time.Second
	}

	// Load config to get database name and server mode settings
	cfg, cfgErr := configfile.Load(beadsDir)
	if cfgErr == nil && cfg != nil {
		doltCfg.Database = cfg.GetDoltDatabase()

		if cfg.IsDoltServerMode() {
			doltCfg.ServerMode = true
			doltCfg.ServerHost = cfg.GetDoltServerHost()
			doltCfg.ServerPort = cfg.GetDoltServerPort()
			doltCfg.ServerUser = cfg.GetDoltServerUser()
			doltCfg.ServerPassword = cfg.GetDoltServerPassword()
			doltCfg.ServerTLS = cfg.GetDoltServerTLS()
		}
	}

	// Apply mode-aware default for dolt-auto-commit
	if strings.TrimSpace(doltAutoCommit) == "" {
		if doltCfg.ServerMode {
			doltAutoCommit = string(doltAutoCommitOff)
		} else {
			doltAutoCommit = string(doltAutoCommitOn)
		}
	}

	// Bootstrap embedded dolt if needed
	if !doltCfg.ServerMode {
		if bErr := bootstrapEmbeddedDolt(rootCtx, doltPath, doltCfg); bErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", bErr)
			os.Exit(1)
		}
	}

	doltCfg.Path = doltPath
	s, err := dolt.New(rootCtx, doltCfg)

	storeIsReadOnly = doltCfg.ReadOnly

	if err != nil {
		if handleFreshCloneError(err, beadsDir) {
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error: failed to open database: %v\n", err)
		os.Exit(1)
	}

	setStore(s)
	storeMutex.Lock()
	storeActive = true
	storeMutex.Unlock()
}

// initHookRunner sets up the hook runner that provides extensibility via
// user-defined scripts in the .beads/hooks directory.
func initHookRunner() {
	if dbPath != "" {
		beadsDir := filepath.Dir(dbPath)
		setHookRunner(hooks.NewRunner(filepath.Join(beadsDir, "hooks")))
	}
}

// warnMultipleDbs warns if more than one database is detected in the directory
// hierarchy, which can cause confusion about which database is being used.
func warnMultipleDbs() {
	warnMultipleDatabases(dbPath)
}

// loadMoleculeTemplates loads molecule templates from hierarchical catalog
// locations after the store is open, skipping during import to avoid conflicts.
func loadMoleculeTemplates(cmd *cobra.Command) {
	s := getStore()
	if cmd.Name() == "import" || s == nil {
		return
	}
	beadsDir := filepath.Dir(dbPath)
	loader := molecules.NewLoader(s)
	if result, err := loader.LoadAll(getRootContext(), beadsDir); err != nil {
		debug.Logf("warning: failed to load molecules: %v", err)
	} else if result.Loaded > 0 {
		debug.Logf("loaded %d molecules from %v", result.Loaded, result.Sources)
	}
}
