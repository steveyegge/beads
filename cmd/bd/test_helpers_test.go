//go:build cgo

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/testutil"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

// testDoltServerPort is the port of the shared test Dolt server (0 = not running).
var testDoltServerPort int

// uniqueTestDBName generates a unique database name for test isolation.
func uniqueTestDBName(t *testing.T) string {
	t.Helper()
	h := sha256.Sum256([]byte(t.Name() + fmt.Sprintf("%d", time.Now().UnixNano())))
	return "testdb_" + hex.EncodeToString(h[:6])
}

// testIDCounter ensures unique IDs across all test runs
var testIDCounter atomic.Uint64

// doltNewMutex serializes dolt.New() calls in tests. The Dolt embedded engine's
// InitStatusVariables() has an internal race condition when called concurrently
// from multiple goroutines (writes to a shared global map without synchronization).
// Serializing store creation prevents this race while allowing tests to run their
// assertions in parallel after the store is created.
var doltNewMutex sync.Mutex

// stdioMutex serializes tests that redirect os.Stdout or os.Stderr.
// These process-global file descriptors cannot be safely redirected from
// concurrent goroutines.
//
// IMPORTANT: Any test that calls cobra's Help(), Execute(), or Print*()
// MUST NOT be parallel (no t.Parallel()), OR must serialize those calls
// under stdioMutex. Setting cmd.SetOut() is NOT sufficient because cobra's
// OutOrStdout() eagerly evaluates os.Stdout as the default argument even
// when outWriter is set — the Go race detector catches this read.
//
// TestCobraParallelPolicyGuard in stdio_race_guard_test.go enforces this.
var stdioMutex sync.Mutex

var (
	testCLIHelperOnce sync.Once
	testCLIHelperPath string
	testCLIHelperErr  error
)

// generateUniqueTestID creates a globally unique test ID using prefix, test name, and atomic counter.
// This prevents ID collisions when multiple tests manipulate global state.
func generateUniqueTestID(t *testing.T, prefix string, index int) string {
	t.Helper()
	counter := testIDCounter.Add(1)
	// include test name, counter, and index for uniqueness
	data := []byte(t.Name() + prefix + string(rune(counter)) + string(rune(index)))
	hash := sha256.Sum256(data)
	return prefix + "-" + hex.EncodeToString(hash[:])[:8]
}

const windowsOS = "windows"

// initConfigForTest initializes viper config for a test and ensures cleanup.
// main.go's init() calls config.Initialize() which picks up the real .beads/config.yaml.
// TestMain resets viper, but any test calling config.Initialize() re-loads the real config.
// This helper ensures viper is reset after the test completes, preventing state pollution
// (e.g., sync.mode=dolt-native leaking into JSONL export tests).
func initConfigForTest(t *testing.T) {
	t.Helper()
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
	t.Cleanup(config.ResetForTesting)
}

// ensureTestMode is a no-op; BEADS_TEST_MODE is set once in TestMain.
// Previously each test set/unset the env var, which raced under t.Parallel().
func ensureTestMode(t *testing.T) {
	t.Helper()
	// BEADS_TEST_MODE is set in TestMain and stays set for the entire test run.
}

// ensureCleanGlobalState resets global state that may have been modified by other tests.
// Call this at the start of tests that manipulate globals directly.
func ensureCleanGlobalState(t *testing.T) {
	t.Helper()
	// Reset CommandContext so accessor functions fall back to globals
	resetCommandContext()
}

// savedGlobals holds a snapshot of package-level globals for safe restoration.
// Used by saveAndRestoreGlobals to ensure test isolation.
type savedGlobals struct {
	dbPath      string
	store       *dolt.DoltStore
	storeActive bool
}

// saveAndRestoreGlobals snapshots all commonly-mutated package-level globals
// and registers a t.Cleanup() to restore them when the test completes.
// This replaces the fragile manual save/defer pattern:
//
//	oldDBPath := dbPath
//	defer func() { dbPath = oldDBPath }()
//
// With the safer:
//
//	saveAndRestoreGlobals(t)
//
// Benefits:
//   - All globals saved atomically (can't forget one)
//   - t.Cleanup runs even on panic (no risk of missed defer registration)
//   - Single call replaces multiple save/defer pairs
func saveAndRestoreGlobals(t *testing.T) *savedGlobals {
	t.Helper()
	saved := &savedGlobals{
		dbPath:      dbPath,
		store:       store,
		storeActive: storeActive,
	}
	t.Cleanup(func() {
		dbPath = saved.dbPath
		store = saved.store
		storeMutex.Lock()
		storeActive = saved.storeActive
		storeMutex.Unlock()
	})
	return saved
}

// writeTestMetadata writes metadata.json in the .beads directory (parent of dbPath)
// so that NewFromConfig can find the correct database name and server settings when
// routing reopens a store by path.
func writeTestMetadata(t *testing.T, dbPath string, database string) {
	t.Helper()
	beadsDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create beads dir: %v", err)
	}
	cfg := &configfile.Config{
		Database:       "dolt",
		Backend:        configfile.BackendDolt,
		DoltMode:       configfile.DoltModeServer,
		DoltDatabase:   database,
		DoltServerHost: "127.0.0.1",
		DoltServerPort: testDoltServerPort,
	}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("Failed to write test metadata.json: %v", err)
	}
}

// newTestStore creates a dolt store with issue_prefix configured (bd-166).
// Uses shared database with branch-per-test isolation (bd-xmf) to avoid
// the overhead of CREATE/DROP DATABASE per test.
// Falls back to per-test databases if the shared DB is not available.
func newTestStore(t *testing.T, dbPath string) *dolt.DoltStore {
	t.Helper()
	return newTestStoreWithPrefix(t, dbPath, "test")
}

// newTestStoreIsolatedDB creates a dolt store with its own dedicated database.
// Use this instead of newTestStoreWithPrefix when the test needs a truly separate
// database (e.g., routing tests that create multiple stores with different paths
// and expect routing to reopen them by path via metadata.json).
func newTestStoreIsolatedDB(t *testing.T, dbPath string, prefix string) *dolt.DoltStore {
	t.Helper()

	ensureTestMode(t)

	if testDoltServerPort == 0 {
		t.Skip("Dolt test server not available, skipping")
	}
	if testutil.DoltContainerCrashed() {
		t.Skipf("Dolt test server crashed: %v", testutil.DoltContainerCrashError())
	}

	ctx := context.Background()

	cfg := &dolt.Config{
		Path:            dbPath,
		ServerHost:      "127.0.0.1",
		ServerPort:      testDoltServerPort,
		Database:        uniqueTestDBName(t),
		CreateIfMissing: true,
	}
	writeTestMetadata(t, dbPath, cfg.Database)

	doltNewMutex.Lock()
	s, err := dolt.New(ctx, cfg)
	doltNewMutex.Unlock()
	if err != nil {
		t.Fatalf("Failed to create dolt store: %v", err)
	}

	if err := s.SetConfig(ctx, "issue_prefix", prefix); err != nil {
		s.Close()
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}
	if err := s.SetConfig(ctx, "types.custom", "molecule,gate,convoy,merge-request,slot,agent,role,rig,event,message"); err != nil {
		s.Close()
		t.Fatalf("Failed to set types.custom: %v", err)
	}

	t.Cleanup(func() {
		s.Close()
		if cfg.Database != "" {
			dropTestDatabase(cfg.Database, testDoltServerPort)
		}
	})
	return s
}

// newTestStoreWithPrefix creates a dolt store with custom issue_prefix configured.
// Uses shared database with branch-per-test isolation (bd-xmf) when available,
// falling back to per-test databases otherwise.
func newTestStoreWithPrefix(t *testing.T, dbPath string, prefix string) *dolt.DoltStore {
	t.Helper()

	ensureTestMode(t)

	if testDoltServerPort == 0 {
		t.Skip("Dolt test server not available, skipping")
	}
	if testutil.DoltContainerCrashed() {
		t.Skipf("Dolt test server crashed: %v", testutil.DoltContainerCrashError())
	}

	ctx := context.Background()

	// Fast path: use shared DB with branch-per-test isolation (bd-xmf)
	if testSharedDB != "" {
		return newTestStoreSharedBranch(t, dbPath, prefix)
	}

	// Fallback: per-test database (original slow path)
	cfg := &dolt.Config{
		Path:            dbPath,
		ServerHost:      "127.0.0.1",
		ServerPort:      testDoltServerPort,
		Database:        uniqueTestDBName(t),
		CreateIfMissing: true,
	}
	writeTestMetadata(t, dbPath, cfg.Database)

	doltNewMutex.Lock()
	s, err := dolt.New(ctx, cfg)
	doltNewMutex.Unlock()
	if err != nil {
		t.Fatalf("Failed to create dolt store: %v", err)
	}

	if err := s.SetConfig(ctx, "issue_prefix", prefix); err != nil {
		s.Close()
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}
	if err := s.SetConfig(ctx, "types.custom", "molecule,gate,convoy,merge-request,slot,agent,role,rig,event,message"); err != nil {
		s.Close()
		t.Fatalf("Failed to set types.custom: %v", err)
	}

	t.Cleanup(func() {
		s.Close()
		if cfg.Database != "" {
			dropTestDatabase(cfg.Database, testDoltServerPort)
		}
	})
	return s
}

// newTestStoreSharedBranch creates a store using the shared database with
// branch-per-test isolation. Each test gets its own Dolt branch, avoiding
// the expensive CREATE DATABASE + schema init + DROP DATABASE + PURGE cycle.
func newTestStoreSharedBranch(t *testing.T, dbPath string, prefix string) *dolt.DoltStore {
	t.Helper()
	ctx := context.Background()

	// Write metadata.json pointing to the shared database
	writeTestMetadata(t, dbPath, testSharedDB)

	// Open store against the shared database with MaxOpenConns=1
	// (required for DOLT_CHECKOUT session affinity)
	doltNewMutex.Lock()
	s, err := dolt.New(ctx, &dolt.Config{
		Path:         dbPath,
		ServerHost:   "127.0.0.1",
		ServerPort:   testDoltServerPort,
		Database:     testSharedDB,
		MaxOpenConns: 1,
	})
	doltNewMutex.Unlock()
	if err != nil {
		t.Fatalf("Failed to create dolt store (shared): %v", err)
	}

	// Create isolated branch for this test
	_, branchCleanup := testutil.StartTestBranch(t, s.DB(), testSharedDB)

	// Create ignored tables on this branch
	if err := dolt.CreateIgnoredTables(s.DB()); err != nil {
		branchCleanup()
		s.Close()
		t.Fatalf("CreateIgnoredTables: %v", err)
	}

	// Set prefix for this test (overrides the shared schema's default)
	if err := s.SetConfig(ctx, "issue_prefix", prefix); err != nil {
		branchCleanup()
		s.Close()
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}

	t.Cleanup(func() {
		branchCleanup()
		s.Close()
	})
	return s
}

// dropTestDatabase drops a test database from the shared server (best-effort cleanup).
func dropTestDatabase(dbName string, port int) {
	dsn := fmt.Sprintf("root@tcp(127.0.0.1:%d)/?parseTime=true&timeout=5s", port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	//nolint:gosec // G201: dbName is generated by uniqueTestDBName (testdb_ + random hex)
	_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
	// Purge dropped databases from Dolt's trash directory to reclaim disk space
	_, _ = db.ExecContext(ctx, "CALL dolt_purge_dropped_databases()")
}

// openExistingTestDB reopens an existing Dolt store for verification in tests.
// It tries NewFromConfig first (reads metadata.json for correct database name),
// then falls back to direct open for BEADS_DB or other non-standard paths.
func openExistingTestDB(t *testing.T, dbPath string) (*dolt.DoltStore, error) {
	t.Helper()
	// Serialize dolt.New() to avoid race in Dolt's InitStatusVariables (bd-cqjoi)
	doltNewMutex.Lock()
	defer doltNewMutex.Unlock()
	ctx := context.Background()
	// Try NewFromConfig which reads metadata.json for correct database name
	beadsDir := filepath.Dir(dbPath)
	if store, err := dolt.NewFromConfig(ctx, beadsDir); err == nil {
		return store, nil
	}
	// Fallback: open directly with test server config
	cfg := &dolt.Config{Path: dbPath}
	if testDoltServerPort != 0 {
		cfg.ServerHost = "127.0.0.1"
		cfg.ServerPort = testDoltServerPort
	}
	return dolt.New(ctx, cfg)
}

func newCLIIntegrationRepo(t *testing.T) string {
	t.Helper()
	return newGitRepo(t)
}

func cliIntegrationEnv(extra ...string) []string {
	return cliIntegrationEnvWithNoDaemon("1", extra...)
}

func cliIntegrationEnvWithNoDaemon(noDaemon string, extra ...string) []string {
	env := filteredEnvForCLI(
		"BEADS_DB",
		"BEADS_DIR",
		"BEADS_NO_DAEMON",
		"BEADS_TEST_MODE",
		"BEADS_DOLT_SERVER_PORT",
		"BEADS_DOLT_SERVER_HOST",
		"BEADS_DOLT_SERVER_MODE",
		"BEADS_DOLT_PASSWORD",
		"BD_SOCKET",
	)
	env = append(env,
		"BEADS_TEST_MODE=1",
		"BEADS_NO_DAEMON="+noDaemon,
	)
	env = append(env, extra...)
	return env
}

func filteredEnvForCLI(keys ...string) []string {
	strip := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		strip[key+"="] = struct{}{}
	}

	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		trim := false
		for prefix := range strip {
			if strings.HasPrefix(entry, prefix) {
				trim = true
				break
			}
		}
		if !trim {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func ensureTestBDForCLI(t *testing.T) string {
	t.Helper()

	testCLIHelperOnce.Do(func() {
		if existingBD := strings.TrimSpace(os.Getenv("BD_TEST_BINARY")); existingBD != "" {
			if _, err := os.Stat(existingBD); err != nil {
				testCLIHelperErr = fmt.Errorf("BD_TEST_BINARY %q not usable: %w", existingBD, err)
				return
			}
			testCLIHelperPath = existingBD
			return
		}

		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			testCLIHelperErr = fmt.Errorf("failed to locate test_helpers_test.go for repo root discovery")
			return
		}
		repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))

		bdBinary := "bd"
		if runtime.GOOS == windowsOS {
			bdBinary = "bd.exe"
		}

		tmpDir, err := os.MkdirTemp("", "bd-cli-test-*")
		if err != nil {
			testCLIHelperErr = fmt.Errorf("failed to create temp dir for bd build: %w", err)
			return
		}
		testCLIHelperPath = filepath.Join(tmpDir, bdBinary)
		cmd := exec.Command("go", "build", "-buildvcs=false", "-o", testCLIHelperPath, "./cmd/bd")
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			testCLIHelperErr = fmt.Errorf("failed to build test bd binary: %v\n%s", err, out)
		}
	})

	if testCLIHelperErr != nil {
		t.Fatalf("ensureTestBDForCLI: %v", testCLIHelperErr)
	}
	return testCLIHelperPath
}

func withUniqueInitDatabaseArgForCLI(t *testing.T, args []string) []string {
	t.Helper()
	if len(args) == 0 || args[0] != "init" {
		return args
	}
	for i, arg := range args {
		if arg == "--database" || arg == "-d" {
			return args
		}
		if strings.HasPrefix(arg, "--database=") {
			return args
		}
		if strings.HasPrefix(arg, "-d") && len(arg) > 2 && i > 0 {
			return args
		}
	}
	return append(append([]string{}, args...), "--database", uniqueTestDBName(t))
}

func runBDExecAllowErrorWithEnvForCLI(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()

	cmd := exec.Command(ensureTestBDForCLI(t), withUniqueInitDatabaseArgForCLI(t, args)...)
	cmd.Dir = dir
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String() + stderr.String(), err
}

func setupGitRepoForIntegration(t *testing.T, dir string) {
	t.Helper()
	if err := runCommandInDir(dir, "git", "init", "--initial-branch=main"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	if err := runCommandInDir(dir, "git", "config", "user.email", "test@test.com"); err != nil {
		t.Fatalf("git config user.email failed: %v", err)
	}
	if err := runCommandInDir(dir, "git", "config", "user.name", "Test User"); err != nil {
		t.Fatalf("git config user.name failed: %v", err)
	}
	if err := testutil.ForceRepoLocalHooksPath(dir); err != nil {
		t.Fatalf("ForceRepoLocalHooksPath failed: %v", err)
	}
}

func skipIfDoltBackendUnavailable(t *testing.T, out string) {
	t.Helper()
	lower := strings.ToLower(out)
	if strings.Contains(lower, "dolt") && (strings.Contains(lower, "not supported") ||
		strings.Contains(lower, "not available") ||
		strings.Contains(lower, "unknown backend")) {
		t.Skipf("dolt backend not available: %s", out)
	}
}

// runCommandInDir runs a command in the specified directory
func runCommandInDir(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.Run()
}

// runCommandInDirWithOutput runs a command in the specified directory and returns its output
func runCommandInDirWithOutput(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// captureStderr captures stderr output from fn and returns it as a string.
// Uses stdioMutex to prevent races with concurrent os.Stderr redirection.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	stdioMutex.Lock()
	defer stdioMutex.Unlock()

	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()
	_ = w.Close()
	os.Stderr = old
	<-done
	_ = r.Close()

	return buf.String()
}

type directStoreTestEnv struct {
	ctx   context.Context
	store *dolt.DoltStore
}

func newDirectStoreTestEnv(t *testing.T) *directStoreTestEnv {
	t.Helper()
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	return &directStoreTestEnv{
		ctx:   context.Background(),
		store: newTestStore(t, testDB),
	}
}

func (e *directStoreTestEnv) Context() context.Context {
	return e.ctx
}

func (e *directStoreTestEnv) Store() *dolt.DoltStore {
	return e.store
}

func (e *directStoreTestEnv) CreateIssue(t *testing.T, issue *types.Issue) {
	t.Helper()
	if err := e.store.CreateIssue(e.ctx, issue, "test-user"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}
}

func (e *directStoreTestEnv) GetIssue(t *testing.T, id string) *types.Issue {
	t.Helper()
	issue, err := e.store.GetIssue(e.ctx, id)
	if err != nil {
		t.Fatalf("GetIssue(%s) failed: %v", id, err)
	}
	return issue
}

func (e *directStoreTestEnv) UpdateIssue(t *testing.T, id string, updates map[string]interface{}) {
	t.Helper()
	if err := e.store.UpdateIssue(e.ctx, id, updates, "test-user"); err != nil {
		t.Fatalf("UpdateIssue(%s) failed: %v", id, err)
	}
}

type dualMode string

const (
	DirectMode dualMode = "direct"
	DaemonMode dualMode = "daemon"
)

type DualModeTestEnv struct {
	t     *testing.T
	mode  dualMode
	ctx   context.Context
	store *dolt.DoltStore
	repo  string
	env   []string
}

func RunDualModeTest(t *testing.T, _ string, fn func(t *testing.T, env *DualModeTestEnv)) {
	t.Helper()

	t.Run(string(DirectMode), func(t *testing.T) {
		direct := newDirectStoreTestEnv(t)
		fn(t, &DualModeTestEnv{
			t:     t,
			mode:  DirectMode,
			ctx:   direct.Context(),
			store: direct.Store(),
		})
	})

	t.Run(string(DaemonMode), func(t *testing.T) {
		repo := newCLIIntegrationRepo(t)
		socketPath := filepath.Join(repo, ".beads", "bd.sock")
		env := cliIntegrationEnvWithNoDaemon("0",
			"BEADS_AUTO_START_DAEMON=true",
			"BD_SOCKET="+socketPath,
		)
		initOut, initErr := runBDExecAllowErrorWithEnvForCLI(t, repo, env, "init", "--backend", "dolt", "--prefix", "test", "--quiet")
		if initErr != nil {
			skipIfDoltBackendUnavailable(t, initOut)
			t.Fatalf("bd init --backend dolt failed: %v\n%s", initErr, initOut)
		}
		customTypeOut, customTypeErr := runBDExecAllowErrorWithEnvForCLI(t, repo, cliIntegrationEnv(),
			"config", "set", "types.custom", "molecule,gate,convoy,merge-request,slot,agent,role,rig,event,message")
		if customTypeErr != nil {
			t.Fatalf("bd config set types.custom failed: %v\n%s", customTypeErr, customTypeOut)
		}
		t.Cleanup(func() {
			_, _ = runBDExecAllowErrorWithEnvForCLI(t, repo, env, "daemon", "stop")
		})
		fn(t, &DualModeTestEnv{
			t:    t,
			mode: DaemonMode,
			ctx:  context.Background(),
			repo: repo,
			env:  env,
		})
	})
}

func (e *DualModeTestEnv) Mode() dualMode {
	return e.mode
}

func (e *DualModeTestEnv) Context() context.Context {
	return e.ctx
}

func (e *DualModeTestEnv) Store() *dolt.DoltStore {
	return e.store
}

func (e *DualModeTestEnv) CreateIssue(issue *types.Issue) error {
	if e.mode == DirectMode {
		return e.store.CreateIssue(e.ctx, issue, "test-user")
	}

	args := []string{"create", issue.Title, "--json", "-p", fmt.Sprintf("%d", issue.Priority)}
	if issue.IssueType != "" {
		args = append(args, "--type", string(issue.IssueType))
	}
	if issue.Description != "" {
		args = append(args, "--description", issue.Description)
	}
	if issue.Assignee != "" {
		args = append(args, "--assignee", issue.Assignee)
	}
	out, err := runBDExecAllowErrorWithEnvForCLI(e.t, e.repo, e.env, args...)
	if err != nil {
		return fmt.Errorf("bd create failed: %w\n%s", err, out)
	}
	jsonStart := strings.Index(out, "{")
	if jsonStart < 0 {
		return fmt.Errorf("bd create returned no JSON: %s", out)
	}
	var created map[string]any
	if err := json.NewDecoder(strings.NewReader(out[jsonStart:])).Decode(&created); err != nil {
		return fmt.Errorf("parse create JSON: %w\n%s", err, out)
	}
	id, _ := created["id"].(string)
	if id == "" {
		return fmt.Errorf("create output missing issue ID: %s", out)
	}
	issue.ID = id
	return nil
}

func (e *DualModeTestEnv) UpdateIssue(id string, updates map[string]interface{}) error {
	if e.mode == DirectMode {
		if issueType, ok := updates["issue_type"]; ok {
			normalized := utils.NormalizeIssueType(fmt.Sprint(issueType))
			customTypes, err := e.store.GetCustomTypes(e.ctx)
			if err != nil {
				return fmt.Errorf("get custom types: %w", err)
			}
			if !types.IssueType(normalized).IsValidWithCustom(customTypes) {
				return fmt.Errorf("invalid issue type %q", normalized)
			}
			updates["issue_type"] = normalized
		}
		return e.store.UpdateIssue(e.ctx, id, updates, "test-user")
	}

	args := []string{"update", id}
	for key, value := range updates {
		switch key {
		case "title":
			args = append(args, "--title", fmt.Sprint(value))
		case "description":
			args = append(args, "--description", fmt.Sprint(value))
		case "status":
			args = append(args, "--status", fmt.Sprint(value))
		case "priority":
			args = append(args, "-p", fmt.Sprint(value))
		case "assignee":
			args = append(args, "--assignee", fmt.Sprint(value))
		case "issue_type":
			args = append(args, "--type", fmt.Sprint(value))
		default:
			return fmt.Errorf("unsupported daemon update field %q", key)
		}
	}
	out, err := runBDExecAllowErrorWithEnvForCLI(e.t, e.repo, e.env, args...)
	if err != nil {
		return fmt.Errorf("bd update failed: %w\n%s", err, out)
	}
	return nil
}

func (e *DualModeTestEnv) GetIssue(id string) (*types.Issue, error) {
	if e.mode == DirectMode {
		return e.store.GetIssue(e.ctx, id)
	}

	out, err := runBDExecAllowErrorWithEnvForCLI(e.t, e.repo, e.env, "show", id, "--json")
	if err != nil {
		return nil, fmt.Errorf("bd show failed: %w\n%s", err, out)
	}
	jsonStart := strings.Index(out, "[")
	if jsonStart < 0 {
		return nil, fmt.Errorf("bd show returned no JSON array: %s", out)
	}
	var issues []types.Issue
	if err := json.Unmarshal([]byte(out[jsonStart:]), &issues); err != nil {
		return nil, fmt.Errorf("parse show JSON: %w\n%s", err, out)
	}
	if len(issues) == 0 {
		return nil, storage.ErrNotFound
	}
	return &issues[0], nil
}
