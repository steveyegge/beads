// Package dolt implements the storage interface using Dolt (versioned MySQL-compatible database).
//
// Dolt provides native version control for SQL data with cell-level merge, history queries,
// and federation via Dolt remotes. This backend eliminates the need for JSONL sync layers
// by making the database itself version-controlled.
//
// Key differences from SQLite backend:
//   - Uses github.com/dolthub/driver for embedded Dolt access
//   - Supports version control operations (commit, push, pull, branch, merge)
//   - History queries via AS OF and dolt_history_* tables
//   - Cell-level merge instead of line-level JSONL merge
//
// Connection modes:
//   - Embedded: No server required, database/sql interface via dolthub/driver
//   - Server: Connect to running dolt sql-server for multi-writer scenarios
package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	// Import Dolt embedded driver
	_ "github.com/dolthub/driver"
	// Import MySQL driver for server mode (bd-f4f78a)
	_ "github.com/go-sql-driver/mysql"

	"github.com/steveyegge/beads/internal/storage"
)

// DoltStore implements the Storage interface using Dolt
type DoltStore struct {
	db          *sql.DB
	dbPath      string       // Path to Dolt database directory
	closed      atomic.Bool  // Tracks whether Close() has been called
	connStr     string       // Connection string for reconnection (embedded mode)
	serverDSN   string       // MySQL DSN for reconnection (server mode, bd-f4f78a)
	mu          sync.RWMutex // Protects concurrent access
	readOnly    bool         // True if opened in read-only mode
	serverMode  bool         // True if connected to sql-server (bd-f4f78a)

	// Version control config
	committerName  string
	committerEmail string
	remote         string // Default remote for push/pull
	branch         string // Current branch

	// Idle connection management (bd-d705ea)
	// Releases LOCK file after idle period to allow external dolt CLI access
	// Note: idle management is disabled in server mode (server handles connections)
	database     string        // Database name for reconnection
	lastActivity time.Time     // Last operation timestamp
	idleTimeout  time.Duration // How long to wait before releasing lock (0 = disabled)
	stopIdle     chan struct{} // Signal to stop idle monitor
	idleRunning  atomic.Bool   // Whether idle monitor is running
}

// Config holds Dolt database configuration
type Config struct {
	Path           string        // Path to Dolt database directory
	CommitterName  string        // Git-style committer name
	CommitterEmail string        // Git-style committer email
	Remote         string        // Default remote name (e.g., "origin")
	Database       string        // Database name within Dolt (default: "beads")
	ReadOnly       bool          // Open in read-only mode (skip schema init)
	LockRetries    int           // Number of retries on lock contention (default: 30)
	LockRetryDelay time.Duration // Initial retry delay (default: 100ms, doubles each retry)
	IdleTimeout    time.Duration // Release lock after this idle period (0 = disabled, default: 30s for daemon)

	// Server mode configuration (bd-f4f78a)
	// When ServerMode is true, connect to a running dolt sql-server instead of embedded mode.
	// This allows multiple concurrent clients without lock contention.
	ServerMode bool   // Use sql-server instead of embedded mode
	ServerHost string // Server host (default: "127.0.0.1")
	ServerPort int    // Server port (default: 3306)
	ServerUser string // MySQL user (default: "root")
	ServerPass string // MySQL password (default: "")
}

// DefaultIdleTimeout is the default idle timeout for releasing locks
const DefaultIdleTimeout = 30 * time.Second

// New creates a new Dolt storage backend
func New(ctx context.Context, cfg *Config) (*DoltStore, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("database path is required")
	}

	// Default values
	if cfg.Database == "" {
		cfg.Database = "beads"
	}
	if cfg.CommitterName == "" {
		cfg.CommitterName = os.Getenv("GIT_AUTHOR_NAME")
		if cfg.CommitterName == "" {
			cfg.CommitterName = "beads"
		}
	}
	if cfg.CommitterEmail == "" {
		cfg.CommitterEmail = os.Getenv("GIT_AUTHOR_EMAIL")
		if cfg.CommitterEmail == "" {
			cfg.CommitterEmail = "beads@local"
		}
	}
	if cfg.Remote == "" {
		cfg.Remote = "origin"
	}
	// Lock retry defaults
	if cfg.LockRetries == 0 {
		cfg.LockRetries = 30 // ~6 seconds with exponential backoff
	}
	if cfg.LockRetryDelay == 0 {
		cfg.LockRetryDelay = 100 * time.Millisecond
	}

	// Server mode defaults (bd-f4f78a)
	if cfg.ServerMode {
		if cfg.ServerHost == "" {
			cfg.ServerHost = "127.0.0.1"
		}
		if cfg.ServerPort == 0 {
			cfg.ServerPort = 3306
		}
		if cfg.ServerUser == "" {
			cfg.ServerUser = "root"
		}
		// ServerPass can be empty (Dolt default)
	}

	// Read-only mode: skip all write operations (directory creation, lock cleanup, CREATE DATABASE)
	// This path is used by read-only commands (list, show, ready, etc.) to avoid acquiring write locks
	if cfg.ReadOnly {
		if cfg.ServerMode {
			return newServerMode(ctx, cfg)
		}
		return newReadOnly(ctx, cfg)
	}

	// Server mode: connect to running dolt sql-server (bd-f4f78a)
	// This allows multiple concurrent clients without lock contention
	if cfg.ServerMode {
		return newServerMode(ctx, cfg)
	}

	// Ensure directory exists
	if err := os.MkdirAll(cfg.Path, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Clean up stale LOCK file if present (bd-d7b931)
	// The Dolt embedded driver creates a LOCK file in .dolt/noms/ that may persist
	// after crashes or unexpected termination. This causes "database is read only" errors.
	if err := cleanupStaleDoltLock(cfg.Path, cfg.Database); err != nil {
		// Log but don't fail - the lock may be legitimately held
		fmt.Fprintf(os.Stderr, "Warning: could not check/clean Dolt lock: %v\n", err)
	}

	// Build connection string (used throughout and stored in DoltStore)
	// Connect without specifying a database first (required for CREATE DATABASE)
	// IMPORTANT: We use a single connection and switch databases using USE.
	// The Dolt embedded driver shares internal state between connections to the same path.
	// If we create two separate *sql.DB connections and close the first one, it closes
	// the shared Dolt session, causing "sql: database is closed" errors on the second
	// connection. (bd-z6d.1)
	connStr := fmt.Sprintf(
		"file://%s?commitname=%s&commitemail=%s",
		cfg.Path, cfg.CommitterName, cfg.CommitterEmail)

	// Retry logic for transient Dolt errors (lock contention, format version issues)
	// bd-g9fg (lock contention), bd-3q6.4 (format version)
	var db *sql.DB
	var lastErr error
	retryDelay := cfg.LockRetryDelay

	for attempt := 0; attempt <= cfg.LockRetries; attempt++ {
		if attempt > 0 {
			// Log transient error for debugging
			fmt.Fprintf(os.Stderr, "Dolt transient error detected (attempt %d/%d), retrying in %v...\n",
				attempt, cfg.LockRetries, retryDelay)
			time.Sleep(retryDelay)
			// Exponential backoff
			retryDelay *= 2
		}

		db, lastErr = sql.Open("dolt", connStr)
		if lastErr != nil {
			if isTransientDoltError(lastErr) {
				continue // Retry
			}
			return nil, fmt.Errorf("failed to open Dolt database: %w", lastErr)
		}

		// Create the database if it doesn't exist
		_, lastErr = db.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", cfg.Database))
		if lastErr != nil {
			if isTransientDoltError(lastErr) {
				_ = db.Close()
				continue // Retry
			}
			_ = db.Close() // nolint:gosec // G104: error ignored on early return
			return nil, fmt.Errorf("failed to create database: %w", lastErr)
		}

		// Switch to the target database using USE
		_, lastErr = db.ExecContext(ctx, fmt.Sprintf("USE %s", cfg.Database))
		if lastErr != nil {
			if isTransientDoltError(lastErr) {
				_ = db.Close()
				continue // Retry
			}
			_ = db.Close() // nolint:gosec // G104: error ignored on early return
			return nil, fmt.Errorf("failed to switch to database %s: %w", cfg.Database, lastErr)
		}

		// Configure connection pool
		// Dolt embedded mode is single-writer like SQLite
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(0)

		// Test connection
		lastErr = db.PingContext(ctx)
		if lastErr != nil {
			if isTransientDoltError(lastErr) {
				_ = db.Close()
				continue // Retry
			}
			_ = db.Close()
			return nil, fmt.Errorf("failed to ping Dolt database: %w", lastErr)
		}

		// Success! Break out of retry loop
		break
	}

	// Check if all retries exhausted
	if lastErr != nil {
		return nil, fmt.Errorf("failed to connect to Dolt database after %d retries: %w", cfg.LockRetries, lastErr)
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	store := &DoltStore{
		db:             db,
		dbPath:         absPath,
		connStr:        connStr,
		committerName:  cfg.CommitterName,
		committerEmail: cfg.CommitterEmail,
		remote:         cfg.Remote,
		branch:         "main",
		readOnly:       cfg.ReadOnly,
		database:       cfg.Database,
		lastActivity:   time.Now(),
		idleTimeout:    cfg.IdleTimeout,
		stopIdle:       make(chan struct{}),
	}

	// Initialize schema (write mode)
	if err := store.initSchema(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Start idle monitor if timeout is configured (bd-d705ea)
	// This releases the LOCK file after idle period to allow external dolt CLI access
	if cfg.IdleTimeout > 0 {
		store.startIdleMonitor()
	}

	return store, nil
}

// newReadOnly opens an existing Dolt database in read-only mode.
// This avoids acquiring write locks by:
// - Not creating directories
// - Not cleaning up lock files
// - Not executing CREATE DATABASE
// - Connecting directly to an existing database
//
// Returns an error if the database doesn't exist.
// Includes retry logic for transient errors (bd-3q6.4).
func newReadOnly(ctx context.Context, cfg *Config) (*DoltStore, error) {
	// Verify the database exists - check for the .dolt directory
	doltDir := filepath.Join(cfg.Path, cfg.Database, ".dolt")
	if _, err := os.Stat(doltDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("Dolt database does not exist: %s (run a write command first to initialize)", cfg.Path)
	}

	// Build connection string - connect directly to the existing database
	// The Dolt driver doesn't have a native read-only mode, but by not running
	// CREATE DATABASE or schema initialization, we avoid write lock acquisition
	connStr := fmt.Sprintf(
		"file://%s?commitname=%s&commitemail=%s",
		cfg.Path, cfg.CommitterName, cfg.CommitterEmail)

	// Retry logic for transient Dolt errors (bd-3q6.4)
	// Read-only operations can also encounter transient errors during concurrent access
	retries := cfg.LockRetries
	if retries == 0 {
		retries = 5 // Fewer retries for read-only since we're not competing for writes
	}
	retryDelay := cfg.LockRetryDelay
	if retryDelay == 0 {
		retryDelay = 100 * time.Millisecond
	}

	var db *sql.DB
	var lastErr error

	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			fmt.Fprintf(os.Stderr, "Dolt read-only transient error (attempt %d/%d), retrying in %v...\n",
				attempt, retries, retryDelay)
			time.Sleep(retryDelay)
			retryDelay *= 2
		}

		db, lastErr = sql.Open("dolt", connStr)
		if lastErr != nil {
			if isTransientDoltError(lastErr) {
				continue // Retry
			}
			return nil, fmt.Errorf("failed to open Dolt database read-only: %w", lastErr)
		}

		// Switch to the target database using USE (read operation)
		_, lastErr = db.ExecContext(ctx, fmt.Sprintf("USE %s", cfg.Database))
		if lastErr != nil {
			_ = db.Close()
			if isTransientDoltError(lastErr) {
				continue // Retry
			}
			return nil, fmt.Errorf("failed to switch to database %s: %w", cfg.Database, lastErr)
		}

		// Configure connection pool - read-only doesn't need large pool
		db.SetMaxOpenConns(2)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(0)

		// Test connection
		lastErr = db.PingContext(ctx)
		if lastErr != nil {
			_ = db.Close()
			if isTransientDoltError(lastErr) {
				continue // Retry
			}
			return nil, fmt.Errorf("failed to ping Dolt database: %w", lastErr)
		}

		// Success!
		break
	}

	// Check if all retries exhausted
	if lastErr != nil {
		return nil, fmt.Errorf("failed to connect to Dolt database (read-only) after %d retries: %w", retries, lastErr)
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(cfg.Path)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	return &DoltStore{
		db:             db,
		dbPath:         absPath,
		connStr:        connStr,
		committerName:  cfg.CommitterName,
		committerEmail: cfg.CommitterEmail,
		remote:         cfg.Remote,
		branch:         "main",
		readOnly:       true,
	}, nil
}

// newServerMode connects to a running dolt sql-server via MySQL protocol (bd-f4f78a).
// This mode allows multiple concurrent clients without lock contention because:
// - The server manages all locking internally
// - Clients connect via standard MySQL protocol
// - Connection pooling is handled by the server
//
// If the server is not running, this function will attempt to start it automatically.
func newServerMode(ctx context.Context, cfg *Config) (*DoltStore, error) {
	// Auto-start server if not running (bd-649383)
	if !IsServerRunning(cfg.ServerHost, cfg.ServerPort) {
		fmt.Fprintf(os.Stderr, "Dolt sql-server not running, starting automatically...\n")
		serverCfg := ServerConfigFromStoreConfig(cfg)
		if err := EnsureServerRunning(ctx, serverCfg); err != nil {
			return nil, fmt.Errorf("failed to auto-start dolt sql-server: %w", err)
		}
	}

	// Build MySQL DSN: user:password@tcp(host:port)/database
	// See: https://github.com/go-sql-driver/mysql#dsn-data-source-name
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&multiStatements=true",
		cfg.ServerUser, cfg.ServerPass, cfg.ServerHost, cfg.ServerPort, cfg.Database)

	// Store DSN without password for logging/display
	connStrDisplay := fmt.Sprintf("%s:***@tcp(%s:%d)/%s",
		cfg.ServerUser, cfg.ServerHost, cfg.ServerPort, cfg.Database)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open MySQL connection to dolt sql-server: %w", err)
	}

	// Configure connection pool - server mode can handle multiple connections
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection with retry
	var lastErr error
	for attempt := 0; attempt <= cfg.LockRetries; attempt++ {
		if attempt > 0 {
			fmt.Fprintf(os.Stderr, "Dolt sql-server connection failed (attempt %d/%d), retrying...\n",
				attempt, cfg.LockRetries)
			time.Sleep(cfg.LockRetryDelay * time.Duration(1<<uint(attempt-1))) // exponential backoff
		}

		lastErr = db.PingContext(ctx)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to connect to dolt sql-server at %s:%d after %d retries: %w",
			cfg.ServerHost, cfg.ServerPort, cfg.LockRetries, lastErr)
	}

	// Initialize schema if not read-only
	// In server mode, we still need to ensure schema exists
	if !cfg.ReadOnly {
		store := &DoltStore{
			db:             db,
			dbPath:         cfg.Path,
			connStr:        connStrDisplay,
			serverDSN:      dsn,
			committerName:  cfg.CommitterName,
			committerEmail: cfg.CommitterEmail,
			remote:         cfg.Remote,
			branch:         "main",
			readOnly:       false,
			serverMode:     true,
			database:       cfg.Database,
		}
		if err := store.initSchema(ctx); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to initialize schema via sql-server: %w", err)
		}
		return store, nil
	}

	// Convert to absolute path if provided
	absPath := cfg.Path
	if cfg.Path != "" {
		var err error
		absPath, err = filepath.Abs(cfg.Path)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to get absolute path: %w", err)
		}
	}

	return &DoltStore{
		db:             db,
		dbPath:         absPath,
		connStr:        connStrDisplay,
		serverDSN:      dsn,
		committerName:  cfg.CommitterName,
		committerEmail: cfg.CommitterEmail,
		remote:         cfg.Remote,
		branch:         "main",
		readOnly:       cfg.ReadOnly,
		serverMode:     true,
		database:       cfg.Database,
	}, nil
}

// isLockError detects if an error is related to lock contention or readonly mode
func isLockError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "database is read only") ||
		strings.Contains(errStr, "database is locked") ||
		strings.Contains(errStr, "lock") && strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "lock") && strings.Contains(errStr, "contention")
}

// isTransientDoltError detects if an error is transient and should be retried.
// This includes lock errors and format version errors which can occur during
// concurrent access when the manifest is being updated. (bd-3q6.4)
func isTransientDoltError(err error) bool {
	if err == nil {
		return false
	}
	// Check lock errors first
	if isLockError(err) {
		return true
	}
	// Check for format version errors - these can occur transiently during
	// concurrent manifest updates (e.g., during push/pull operations)
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "invalid format version") ||
		strings.Contains(errStr, "failed to load database") ||
		strings.Contains(errStr, "manifest") && strings.Contains(errStr, "invalid")
}

// cleanupStaleDoltLock removes stale LOCK files from the Dolt noms directory.
// The embedded Dolt driver creates a LOCK file that persists after crashes,
// causing subsequent opens to fail with "database is read only" errors. (bd-d7b931)
func cleanupStaleDoltLock(dbPath string, database string) error {
	// The LOCK file is in the noms directory under .dolt
	// For a database at /path/to/dolt with database name "beads",
	// the lock is at /path/to/dolt/beads/.dolt/noms/LOCK
	lockPath := filepath.Join(dbPath, database, ".dolt", "noms", "LOCK")

	info, err := os.Stat(lockPath)
	if os.IsNotExist(err) {
		// No lock file, nothing to do
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat lock file: %w", err)
	}

	// Check if lock file is empty (Dolt creates empty LOCK files)
	// An empty LOCK file is likely stale - the driver should have released it
	if info.Size() == 0 {
		// Check how old the lock is - if it's been more than a few seconds,
		// it's likely stale from a crashed process
		age := time.Since(info.ModTime())
		if age > 5*time.Second {
			fmt.Fprintf(os.Stderr, "Removing stale Dolt LOCK file (age: %v)\n", age.Round(time.Second))
			if err := os.Remove(lockPath); err != nil {
				return fmt.Errorf("remove stale lock: %w", err)
			}
			return nil
		}
		// Lock is recent, might be held by another process
		return nil
	}

	// Non-empty lock file - might contain PID info, try to check if process is alive
	// For now, just log and don't touch it
	return nil
}


// initSchema creates all tables if they don't exist
func (s *DoltStore) initSchema(ctx context.Context) error {
	// Execute schema creation - split into individual statements
	// because MySQL/Dolt doesn't support multiple statements in one Exec
	for _, stmt := range splitStatements(schema) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		// Skip pure comment-only statements, but execute statements that start with comments
		if isOnlyComments(stmt) {
			continue
		}
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to create schema: %w\nStatement: %s", err, truncateForError(stmt))
		}
	}

	// Migration: Remove FK constraint on depends_on_id to allow external references
	// (external:<project>:<capability>). See bd-3q6.6-1 and SQLite migration
	// 025_remove_depends_on_fk.go. This is idempotent - errors are ignored if
	// the constraint doesn't exist (new databases) or was already dropped.
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE dependencies DROP FOREIGN KEY fk_dep_depends_on")

	// Insert default config values
	for _, stmt := range splitStatements(defaultConfig) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if isOnlyComments(stmt) {
			continue
		}
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to insert default config: %w", err)
		}
	}

	// Create views
	if _, err := s.db.ExecContext(ctx, readyIssuesView); err != nil {
		return fmt.Errorf("failed to create ready_issues view: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, blockedIssuesView); err != nil {
		return fmt.Errorf("failed to create blocked_issues view: %w", err)
	}

	return nil
}

// splitStatements splits a SQL script into individual statements
func splitStatements(script string) []string {
	var statements []string
	var current strings.Builder
	inString := false
	stringChar := byte(0)

	for i := 0; i < len(script); i++ {
		c := script[i]

		if inString {
			current.WriteByte(c)
			if c == stringChar && (i == 0 || script[i-1] != '\\') {
				inString = false
			}
			continue
		}

		if c == '\'' || c == '"' || c == '`' {
			inString = true
			stringChar = c
			current.WriteByte(c)
			continue
		}

		if c == ';' {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
			continue
		}

		current.WriteByte(c)
	}

	// Handle last statement without semicolon
	stmt := strings.TrimSpace(current.String())
	if stmt != "" {
		statements = append(statements, stmt)
	}

	return statements
}

// truncateForError truncates a string for use in error messages
func truncateForError(s string) string {
	if len(s) > 100 {
		return s[:100] + "..."
	}
	return s
}

// isOnlyComments returns true if the statement contains only SQL comments
func isOnlyComments(stmt string) bool {
	lines := strings.Split(stmt, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		// Found a non-comment, non-empty line
		return false
	}
	return true
}

// Close closes the database connection
func (s *DoltStore) Close() error {
	s.closed.Store(true)
	// Stop idle monitor if running
	if s.idleRunning.CompareAndSwap(true, false) {
		close(s.stopIdle)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Path returns the database directory path
func (s *DoltStore) Path() string {
	return s.dbPath
}

// IsClosed returns true if Close() has been called
func (s *DoltStore) IsClosed() bool {
	return s.closed.Load()
}

// IsServerMode returns true if connected to dolt sql-server (bd-f4f78a)
func (s *DoltStore) IsServerMode() bool {
	return s.serverMode
}

// startIdleMonitor starts a goroutine that releases the lock after idle timeout (bd-d705ea)
// Note: This is disabled in server mode since the server manages connections (bd-f4f78a)
func (s *DoltStore) startIdleMonitor() {
	if s.idleTimeout <= 0 || s.idleRunning.Load() || s.serverMode {
		return
	}
	s.idleRunning.Store(true)
	go func() {
		ticker := time.NewTicker(s.idleTimeout / 2) // Check at half the timeout interval
		defer ticker.Stop()
		for {
			select {
			case <-s.stopIdle:
				return
			case <-ticker.C:
				s.checkAndReleaseLock()
			}
		}
	}()
}

// checkAndReleaseLock releases the lock if idle timeout has passed (bd-d705ea)
func (s *DoltStore) checkAndReleaseLock() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil || s.closed.Load() {
		return
	}

	if time.Since(s.lastActivity) > s.idleTimeout {
		// Release the lock by closing the connection
		fmt.Fprintf(os.Stderr, "Dolt idle timeout: releasing lock (idle for %v)\n", time.Since(s.lastActivity).Round(time.Second))
		_ = s.db.Close()
		s.db = nil
	}
}

// ensureConnected reopens the database connection if it was closed due to idle timeout (bd-d705ea)
// In server mode (bd-f4f78a), reconnection uses the MySQL driver instead of embedded Dolt.
func (s *DoltStore) ensureConnected(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	if s.db != nil {
		// Already connected
		s.lastActivity = time.Now()
		return nil
	}

	// Reopen the connection
	fmt.Fprintf(os.Stderr, "Dolt: reopening connection after idle release\n")

	var db *sql.DB
	var err error

	if s.serverMode {
		// Server mode: reconnect via MySQL driver (bd-f4f78a)
		// Use serverDSN which contains the full connection string with credentials
		// Note: In practice, server mode connections rarely close due to idle since
		// we don't run the idle monitor in server mode
		db, err = sql.Open("mysql", s.serverDSN)
		if err != nil {
			return fmt.Errorf("failed to reopen MySQL connection: %w", err)
		}
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(5 * time.Minute)
	} else {
		// Embedded mode: reconnect via Dolt driver
		// Clean up stale lock file first
		if err := cleanupStaleDoltLock(s.dbPath, s.database); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not check/clean Dolt lock: %v\n", err)
		}

		db, err = sql.Open("dolt", s.connStr)
		if err != nil {
			return fmt.Errorf("failed to reopen Dolt database: %w", err)
		}

		// Switch to the target database
		if _, err := db.ExecContext(ctx, fmt.Sprintf("USE %s", s.database)); err != nil {
			_ = db.Close()
			return fmt.Errorf("failed to switch to database %s: %w", s.database, err)
		}

		// Configure connection pool for embedded mode
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(0)
	}

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("failed to ping Dolt database: %w", err)
	}

	s.db = db
	s.lastActivity = time.Now()
	return nil
}

// trackActivity updates the last activity timestamp (bd-d705ea)
func (s *DoltStore) trackActivity() {
	s.mu.Lock()
	s.lastActivity = time.Now()
	s.mu.Unlock()
}

// getDB returns the database connection, ensuring it's connected (bd-d705ea)
// This method handles reconnection after idle timeout transparently.
// All database operations should use this method instead of accessing s.db directly.
func (s *DoltStore) getDB(ctx context.Context) (*sql.DB, error) {
	// Fast path: check if already connected with read lock
	s.mu.RLock()
	if s.db != nil {
		db := s.db
		s.mu.RUnlock()
		s.trackActivity()
		return db, nil
	}
	s.mu.RUnlock()

	// Slow path: need to reconnect
	if err := s.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Return the db (ensureConnected already set lastActivity)
	s.mu.RLock()
	db := s.db
	s.mu.RUnlock()
	return db, nil
}

// UnderlyingDB returns the underlying *sql.DB connection
func (s *DoltStore) UnderlyingDB() *sql.DB {
	return s.db
}

// UnderlyingConn returns a connection from the pool
func (s *DoltStore) UnderlyingConn(ctx context.Context) (*sql.Conn, error) {
	return s.db.Conn(ctx)
}

// =============================================================================
// Version Control Operations (Dolt-specific extensions)
// =============================================================================

// Commit creates a Dolt commit with the given message
func (s *DoltStore) Commit(ctx context.Context, message string) error {
	db, err := s.getDB(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	_, err = db.ExecContext(ctx, "CALL DOLT_COMMIT('-Am', ?)", message)
	if err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}
	return nil
}

// Push pushes commits to the remote
func (s *DoltStore) Push(ctx context.Context) error {
	db, err := s.getDB(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	_, err = db.ExecContext(ctx, "CALL DOLT_PUSH(?, ?)", s.remote, s.branch)
	if err != nil {
		return fmt.Errorf("failed to push to %s/%s: %w", s.remote, s.branch, err)
	}
	return nil
}

// Pull pulls changes from the remote
func (s *DoltStore) Pull(ctx context.Context) error {
	db, err := s.getDB(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	_, err = db.ExecContext(ctx, "CALL DOLT_PULL(?)", s.remote)
	if err != nil {
		return fmt.Errorf("failed to pull from %s: %w", s.remote, err)
	}
	return nil
}

// Branch creates a new branch
func (s *DoltStore) Branch(ctx context.Context, name string) error {
	db, err := s.getDB(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	_, err = db.ExecContext(ctx, "CALL DOLT_BRANCH(?)", name)
	if err != nil {
		return fmt.Errorf("failed to create branch %s: %w", name, err)
	}
	return nil
}

// Checkout switches to the specified branch
func (s *DoltStore) Checkout(ctx context.Context, branch string) error {
	db, err := s.getDB(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	_, err = db.ExecContext(ctx, "CALL DOLT_CHECKOUT(?)", branch)
	if err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branch, err)
	}
	s.branch = branch
	return nil
}

// Merge merges the specified branch into the current branch.
// Returns any merge conflicts if present. Implements storage.VersionedStorage.
func (s *DoltStore) Merge(ctx context.Context, branch string) ([]storage.Conflict, error) {
	db, err := s.getDB(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	_, err = db.ExecContext(ctx, "CALL DOLT_MERGE(?)", branch)
	if err != nil {
		// Check if the error is due to conflicts
		conflicts, conflictErr := s.GetConflicts(ctx)
		if conflictErr == nil && len(conflicts) > 0 {
			return conflicts, nil
		}
		return nil, fmt.Errorf("failed to merge branch %s: %w", branch, err)
	}
	return nil, nil
}

// CurrentBranch returns the current branch name
func (s *DoltStore) CurrentBranch(ctx context.Context) (string, error) {
	db, err := s.getDB(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get database connection: %w", err)
	}

	var branch string
	err = db.QueryRowContext(ctx, "SELECT active_branch()").Scan(&branch)
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return branch, nil
}

// DeleteBranch deletes a branch (used to clean up import branches)
func (s *DoltStore) DeleteBranch(ctx context.Context, branch string) error {
	db, err := s.getDB(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	_, err = db.ExecContext(ctx, "CALL DOLT_BRANCH('-D', ?)", branch)
	if err != nil {
		return fmt.Errorf("failed to delete branch %s: %w", branch, err)
	}
	return nil
}

// Log returns recent commit history
func (s *DoltStore) Log(ctx context.Context, limit int) ([]CommitInfo, error) {
	db, err := s.getDB(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT commit_hash, committer, email, date, message
		FROM dolt_log
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get log: %w", err)
	}
	defer rows.Close()

	var commits []CommitInfo
	for rows.Next() {
		var c CommitInfo
		if err := rows.Scan(&c.Hash, &c.Author, &c.Email, &c.Date, &c.Message); err != nil {
			return nil, fmt.Errorf("failed to scan commit: %w", err)
		}
		commits = append(commits, c)
	}
	return commits, rows.Err()
}

// CommitInfo represents a Dolt commit
type CommitInfo struct {
	Hash    string
	Author  string
	Email   string
	Date    time.Time
	Message string
}

// HistoryEntry represents a row from dolt_history_* table
type HistoryEntry struct {
	CommitHash string
	Committer  string
	CommitDate time.Time
	// Issue data at that commit
	IssueData map[string]interface{}
}

// AddRemote adds a Dolt remote
func (s *DoltStore) AddRemote(ctx context.Context, name, url string) error {
	db, err := s.getDB(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	_, err = db.ExecContext(ctx, "CALL DOLT_REMOTE('add', ?, ?)", name, url)
	if err != nil {
		return fmt.Errorf("failed to add remote %s: %w", name, err)
	}
	return nil
}

// Status returns the current Dolt status (staged/unstaged changes)
func (s *DoltStore) Status(ctx context.Context) (*DoltStatus, error) {
	db, err := s.getDB(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}

	rows, err := db.QueryContext(ctx, "SELECT table_name, staged, status FROM dolt_status")
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}
	defer rows.Close()

	status := &DoltStatus{
		Staged:   make([]StatusEntry, 0),
		Unstaged: make([]StatusEntry, 0),
	}

	for rows.Next() {
		var tableName string
		var staged bool
		var statusStr string
		if err := rows.Scan(&tableName, &staged, &statusStr); err != nil {
			return nil, fmt.Errorf("failed to scan status: %w", err)
		}
		entry := StatusEntry{Table: tableName, Status: statusStr}
		if staged {
			status.Staged = append(status.Staged, entry)
		} else {
			status.Unstaged = append(status.Unstaged, entry)
		}
	}
	return status, rows.Err()
}

// DoltStatus represents the current repository status
type DoltStatus struct {
	Staged   []StatusEntry
	Unstaged []StatusEntry
}

// StatusEntry represents a changed table
type StatusEntry struct {
	Table  string
	Status string // "new", "modified", "deleted"
}
