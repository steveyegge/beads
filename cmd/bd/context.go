package main

import (
	"context"
	"os"
	"time"

	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/storage/dolt"
)

// CommandContext holds all runtime state for command execution.
// This consolidates the previously scattered global variables for:
// - Better testability (can inject mock contexts)
// - Clearer state ownership (all state in one place)
// - Reduced global count (20+ globals -> 1 context)
// - Thread safety (mutexes grouped with the data they protect)
type CommandContext struct {
	// Configuration (derived from flags and config)
	DBPath       string
	Actor        string
	JSONOutput   bool
	SandboxMode  bool
	AllowStale   bool
	NoDb         bool
	ReadonlyMode bool
	LockTimeout  time.Duration
	Verbose      bool
	Quiet        bool

	// Runtime state
	Store      *dolt.DoltStore
	RootCtx    context.Context
	RootCancel context.CancelFunc
	HookRunner *hooks.Runner

	// Version tracking
	VersionUpgradeDetected bool
	PreviousVersion        string
	UpgradeAcknowledged    bool

	// Profiling
	ProfileFile *os.File
	TraceFile   *os.File
}

// cmdCtx is the global CommandContext instance.
// Commands access state through this single point instead of scattered globals.
var cmdCtx *CommandContext

// initCommandContext creates and initializes a new CommandContext.
// Called from PersistentPreRun to set up runtime state.
func initCommandContext() {
	cmdCtx = &CommandContext{}
}

// GetCommandContext returns the current CommandContext.
// Returns nil if called before initialization (e.g., during init() or help).
func GetCommandContext() *CommandContext {
	return cmdCtx
}

// resetCommandContext clears the CommandContext for testing.
// Only call this in tests, never in production code.
func resetCommandContext() {
	cmdCtx = nil
}

// getStore returns the current storage backend.
func getStore() *dolt.DoltStore {
	if cmdCtx == nil {
		return store
	}
	return cmdCtx.Store
}

// setStore updates the storage backend in both CommandContext and the global.
func setStore(s *dolt.DoltStore) {
	if cmdCtx != nil {
		cmdCtx.Store = s
	}
	store = s
}

// getActor returns the current actor name for audit trail.
func getActor() string {
	if cmdCtx == nil {
		return actor
	}
	return cmdCtx.Actor
}

// setActor updates the actor name in both CommandContext and the global.
func setActor(a string) {
	if cmdCtx != nil {
		cmdCtx.Actor = a
	}
	actor = a
}

// isJSONOutput returns true if JSON output mode is enabled.
func isJSONOutput() bool {
	if cmdCtx == nil {
		return jsonOutput
	}
	return cmdCtx.JSONOutput
}

// setJSONOutput updates the JSON output flag in both CommandContext and the global.
func setJSONOutput(j bool) {
	if cmdCtx != nil {
		cmdCtx.JSONOutput = j
	}
	jsonOutput = j
}

// getDBPath returns the database path.
func getDBPath() string {
	if cmdCtx == nil {
		return dbPath
	}
	return cmdCtx.DBPath
}

// setDBPath updates the database path in both CommandContext and the global.
func setDBPath(p string) {
	if cmdCtx != nil {
		cmdCtx.DBPath = p
	}
	dbPath = p
}

// getRootContext returns the signal-aware root context.
// Returns context.Background() if the root context is nil.
func getRootContext() context.Context {
	var ctx context.Context
	if cmdCtx == nil {
		ctx = rootCtx
	} else {
		ctx = cmdCtx.RootCtx
	}
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// setRootContext updates the root context and cancel function.
func setRootContext(ctx context.Context, cancel context.CancelFunc) {
	if cmdCtx != nil {
		cmdCtx.RootCtx = ctx
		cmdCtx.RootCancel = cancel
	}
	rootCtx = ctx
	rootCancel = cancel
}

// getHookRunner returns the hook runner instance.
func getHookRunner() *hooks.Runner {
	if cmdCtx == nil {
		return hookRunner
	}
	return cmdCtx.HookRunner
}

// setHookRunner updates the hook runner.
func setHookRunner(h *hooks.Runner) {
	if cmdCtx != nil {
		cmdCtx.HookRunner = h
	}
	hookRunner = h
}

// isReadonlyMode returns true if read-only mode is enabled.
func isReadonlyMode() bool {
	if cmdCtx == nil {
		return readonlyMode
	}
	return cmdCtx.ReadonlyMode
}

// setReadonlyMode updates the read-only mode flag.
func setReadonlyMode(rm bool) {
	if cmdCtx != nil {
		cmdCtx.ReadonlyMode = rm
	}
	readonlyMode = rm
}

// getLockTimeout returns the SQLite lock timeout.
func getLockTimeout() time.Duration {
	if cmdCtx == nil {
		return lockTimeout
	}
	return cmdCtx.LockTimeout
}

// lockStore acquires the store mutex for thread-safe access.
func lockStore() {
	storeMutex.Lock()
}

// unlockStore releases the store mutex.
func unlockStore() {
	storeMutex.Unlock()
}

// isStoreActive returns true if the store is currently available.
func isStoreActive() bool {
	return storeActive
}

// setStoreActive updates the store active flag.
func setStoreActive(active bool) {
	storeActive = active
}

// isVerbose returns true if verbose mode is enabled.
func isVerbose() bool {
	if cmdCtx == nil {
		return verboseFlag
	}
	return cmdCtx.Verbose
}

// setVerbose updates the verbose flag.
func setVerbose(v bool) {
	if cmdCtx != nil {
		cmdCtx.Verbose = v
	}
	verboseFlag = v
}

// isQuiet returns true if quiet mode is enabled.
func isQuiet() bool {
	if cmdCtx == nil {
		return quietFlag
	}
	return cmdCtx.Quiet
}

// setQuiet updates the quiet flag.
func setQuiet(q bool) {
	if cmdCtx != nil {
		cmdCtx.Quiet = q
	}
	quietFlag = q
}

// isNoDb returns true if no-db mode is enabled.
func isNoDb() bool {
	if cmdCtx == nil {
		return noDb
	}
	return cmdCtx.NoDb
}

// setNoDb updates the no-db flag.
func setNoDb(nd bool) {
	if cmdCtx != nil {
		cmdCtx.NoDb = nd
	}
	noDb = nd
}

// isSandboxMode returns true if sandbox mode is enabled.
func isSandboxMode() bool {
	if cmdCtx == nil {
		return sandboxMode
	}
	return cmdCtx.SandboxMode
}

// setSandboxMode updates the sandbox mode flag.
func setSandboxMode(sm bool) {
	if cmdCtx != nil {
		cmdCtx.SandboxMode = sm
	}
	sandboxMode = sm
}

// isVersionUpgradeDetected returns true if a version upgrade was detected.
func isVersionUpgradeDetected() bool {
	if cmdCtx == nil {
		return versionUpgradeDetected
	}
	return cmdCtx.VersionUpgradeDetected
}

// setVersionUpgradeDetected updates the version upgrade detected flag.
func setVersionUpgradeDetected(detected bool) {
	if cmdCtx != nil {
		cmdCtx.VersionUpgradeDetected = detected
	}
	versionUpgradeDetected = detected
}

// getPreviousVersion returns the previous bd version.
func getPreviousVersion() string {
	if cmdCtx == nil {
		return previousVersion
	}
	return cmdCtx.PreviousVersion
}

// setPreviousVersion updates the previous version.
func setPreviousVersion(v string) {
	if cmdCtx != nil {
		cmdCtx.PreviousVersion = v
	}
	previousVersion = v
}

// isUpgradeAcknowledged returns true if the upgrade notification was shown.
func isUpgradeAcknowledged() bool {
	if cmdCtx == nil {
		return upgradeAcknowledged
	}
	return cmdCtx.UpgradeAcknowledged
}

// setUpgradeAcknowledged updates the upgrade acknowledged flag.
func setUpgradeAcknowledged(ack bool) {
	if cmdCtx != nil {
		cmdCtx.UpgradeAcknowledged = ack
	}
	upgradeAcknowledged = ack
}

// getProfileFile returns the CPU profile file handle.
func getProfileFile() *os.File {
	if cmdCtx == nil {
		return profileFile
	}
	return cmdCtx.ProfileFile
}

// setProfileFile updates the CPU profile file handle.
func setProfileFile(f *os.File) {
	if cmdCtx != nil {
		cmdCtx.ProfileFile = f
	}
	profileFile = f
}

// getTraceFile returns the trace file handle.
func getTraceFile() *os.File {
	if cmdCtx == nil {
		return traceFile
	}
	return cmdCtx.TraceFile
}

// setTraceFile updates the trace file handle.
func setTraceFile(f *os.File) {
	if cmdCtx != nil {
		cmdCtx.TraceFile = f
	}
	traceFile = f
}

// isAllowStale returns true if staleness checks should be skipped.
func isAllowStale() bool {
	if cmdCtx == nil {
		return allowStale
	}
	return cmdCtx.AllowStale
}

// setAllowStale updates the allow-stale flag.
func setAllowStale(as bool) {
	if cmdCtx != nil {
		cmdCtx.AllowStale = as
	}
	allowStale = as
}
