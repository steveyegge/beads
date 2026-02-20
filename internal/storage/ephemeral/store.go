// Package ephemeral provides a SQLite-backed store for ephemeral beads (wisps, molecules).
//
// Ephemeral beads are transient artifacts (patrol wisps, heartbeats, molecule steps)
// that accumulate rapidly but have no long-term value. Storing them in Dolt pollutes
// the permanent commit history. This store keeps them in a separate SQLite database
// that can be freely nuked without affecting the main ledger.
package ephemeral

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/steveyegge/beads/internal/types"
)

// Store is a SQLite-backed store for ephemeral beads.
type Store struct {
	db     *sql.DB
	dbPath string
	mu     sync.RWMutex
	prefix string // issue prefix (e.g., "bd")
}

// New creates a new ephemeral store at the given path.
func New(dbPath string, prefix string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create ephemeral db dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_journal=WAL&_busy_timeout=5000&_foreign_keys=1", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open ephemeral db: %w", err)
	}

	// Set connection pool limits appropriate for SQLite
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Verify connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping ephemeral db: %w", err)
	}

	s := &Store{
		db:     db,
		dbPath: dbPath,
		prefix: prefix,
	}

	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init ephemeral schema: %w", err)
	}

	return s, nil
}

// initSchema creates all tables if they don't exist.
func (s *Store) initSchema() error {
	// Execute schema in a transaction
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Split and execute each statement
	stmts := strings.Split(schema, ";")
	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("exec schema statement: %w\nSQL: %s", err, stmt)
		}
	}

	// Seed prefix config
	if s.prefix != "" {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO config (key, value) VALUES ('issue_prefix', ?)`, s.prefix); err != nil {
			return fmt.Errorf("seed prefix config: %w", err)
		}
	}

	return tx.Commit()
}

// Close closes the database connection.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		err := s.db.Close()
		s.db = nil
		return err
	}
	return nil
}

// DB returns the underlying *sql.DB for advanced use.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Path returns the database file path.
func (s *Store) Path() string {
	return s.dbPath
}

// Count returns the number of issues in the ephemeral store.
func (s *Store) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM issues").Scan(&count)
	return count, err
}

// Nuke deletes all data from the ephemeral store.
// This is a fast operation that doesn't leave history behind.
func (s *Store) Nuke(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	tables := []string{"events", "comments", "labels", "dependencies", "issues", "config"}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			return fmt.Errorf("nuke table %s: %w", t, err)
		}
	}

	// Re-seed prefix
	if s.prefix != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO config (key, value) VALUES ('issue_prefix', ?)`, s.prefix); err != nil {
			return fmt.Errorf("re-seed prefix: %w", err)
		}
	}

	return tx.Commit()
}

// IsEphemeralID returns true if the ID belongs to an ephemeral issue (contains "-wisp-").
func IsEphemeralID(id string) bool {
	return strings.Contains(id, "-wisp-")
}

// formatTime formats a time.Time as a SQLite-compatible string.
func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// parseTime parses a SQLite timestamp string into time.Time.
func parseTime(s string) time.Time {
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// parseNullTime parses a nullable time string.
func parseNullTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t := parseTime(s.String)
	if t.IsZero() {
		return nil
	}
	return &t
}

// scanIssue scans a full issue from a row scanner.
// The caller must ensure the query selected issueSelectColumns in order.
func scanIssue(s interface{ Scan(dest ...any) error }) (*types.Issue, error) {
	var issue types.Issue
	var createdAtStr, updatedAtStr sql.NullString
	var closedAtStr, compactedAtStr, lastActivityStr, dueAtStr, deferUntilStr sql.NullString
	var estimatedMinutes, originalSize, timeoutNs sql.NullInt64
	var assignee, externalRef, specID, compactedAtCommit, owner sql.NullString
	var contentHash, sourceRepo, closeReason sql.NullString
	var workType, sourceSystem sql.NullString
	var sender, wispType, molType, eventKind, actor, target, payload sql.NullString
	var awaitType, awaitID, waiters sql.NullString
	var hookBead, roleBead, agentState, roleType, rig sql.NullString
	var ephemeral, pinned, isTemplate, crystallizes sql.NullInt64
	var qualityScore sql.NullFloat64
	var metadata sql.NullString
	var closedBySession sql.NullString

	if err := s.Scan(
		&issue.ID, &contentHash, &issue.Title, &issue.Description, &issue.Design,
		&issue.AcceptanceCriteria, &issue.Notes, &issue.Status,
		&issue.Priority, &issue.IssueType, &assignee, &estimatedMinutes,
		&createdAtStr, &issue.CreatedBy, &owner, &updatedAtStr, &closedAtStr, &closedBySession, &externalRef, &specID,
		&issue.CompactionLevel, &compactedAtStr, &compactedAtCommit, &originalSize, &sourceRepo, &closeReason,
		&sender, &ephemeral, &wispType, &pinned, &isTemplate, &crystallizes,
		&awaitType, &awaitID, &timeoutNs, &waiters,
		&hookBead, &roleBead, &agentState, &lastActivityStr, &roleType, &rig, &molType,
		&eventKind, &actor, &target, &payload,
		&dueAtStr, &deferUntilStr,
		&qualityScore, &workType, &sourceSystem, &metadata,
	); err != nil {
		return nil, err
	}

	if createdAtStr.Valid {
		issue.CreatedAt = parseTime(createdAtStr.String)
	}
	if updatedAtStr.Valid {
		issue.UpdatedAt = parseTime(updatedAtStr.String)
	}
	if contentHash.Valid {
		issue.ContentHash = contentHash.String
	}
	issue.ClosedAt = parseNullTime(closedAtStr)
	if closedBySession.Valid {
		issue.ClosedBySession = closedBySession.String
	}
	if estimatedMinutes.Valid {
		mins := int(estimatedMinutes.Int64)
		issue.EstimatedMinutes = &mins
	}
	if assignee.Valid {
		issue.Assignee = assignee.String
	}
	if owner.Valid {
		issue.Owner = owner.String
	}
	if externalRef.Valid {
		issue.ExternalRef = &externalRef.String
	}
	if specID.Valid {
		issue.SpecID = specID.String
	}
	issue.CompactedAt = parseNullTime(compactedAtStr)
	if compactedAtCommit.Valid {
		issue.CompactedAtCommit = &compactedAtCommit.String
	}
	if originalSize.Valid {
		issue.OriginalSize = int(originalSize.Int64)
	}
	if sourceRepo.Valid {
		issue.SourceRepo = sourceRepo.String
	}
	if closeReason.Valid {
		issue.CloseReason = closeReason.String
	}
	if sender.Valid {
		issue.Sender = sender.String
	}
	if ephemeral.Valid && ephemeral.Int64 != 0 {
		issue.Ephemeral = true
	}
	if wispType.Valid {
		issue.WispType = types.WispType(wispType.String)
	}
	if pinned.Valid && pinned.Int64 != 0 {
		issue.Pinned = true
	}
	if isTemplate.Valid && isTemplate.Int64 != 0 {
		issue.IsTemplate = true
	}
	if crystallizes.Valid && crystallizes.Int64 != 0 {
		issue.Crystallizes = true
	}
	if awaitType.Valid {
		issue.AwaitType = awaitType.String
	}
	if awaitID.Valid {
		issue.AwaitID = awaitID.String
	}
	if timeoutNs.Valid {
		issue.Timeout = time.Duration(timeoutNs.Int64)
	}
	if waiters.Valid && waiters.String != "" {
		// Simple comma-separated parsing
		issue.Waiters = strings.Split(waiters.String, ",")
	}
	if hookBead.Valid {
		issue.HookBead = hookBead.String
	}
	if roleBead.Valid {
		issue.RoleBead = roleBead.String
	}
	if agentState.Valid {
		issue.AgentState = types.AgentState(agentState.String)
	}
	issue.LastActivity = parseNullTime(lastActivityStr)
	if roleType.Valid {
		issue.RoleType = roleType.String
	}
	if rig.Valid {
		issue.Rig = rig.String
	}
	if molType.Valid {
		issue.MolType = types.MolType(molType.String)
	}
	if eventKind.Valid {
		issue.EventKind = eventKind.String
	}
	if actor.Valid {
		issue.Actor = actor.String
	}
	if target.Valid {
		issue.Target = target.String
	}
	if payload.Valid {
		issue.Payload = payload.String
	}
	issue.DueAt = parseNullTime(dueAtStr)
	issue.DeferUntil = parseNullTime(deferUntilStr)
	if qualityScore.Valid {
		qs := float32(qualityScore.Float64)
		issue.QualityScore = &qs
	}
	if workType.Valid {
		issue.WorkType = types.WorkType(workType.String)
	}
	if sourceSystem.Valid {
		issue.SourceSystem = sourceSystem.String
	}
	if metadata.Valid && metadata.String != "" && metadata.String != "{}" {
		issue.Metadata = []byte(metadata.String)
	}

	return &issue, nil
}

// issueSelectColumns is the column list matching the scanIssue order.
const issueSelectColumns = `id, content_hash, title, description, design, acceptance_criteria, notes,
    status, priority, issue_type, assignee, estimated_minutes,
    created_at, created_by, owner, updated_at, closed_at, closed_by_session, external_ref, spec_id,
    compaction_level, compacted_at, compacted_at_commit, original_size, source_repo, close_reason,
    sender, ephemeral, wisp_type, pinned, is_template, crystallizes,
    await_type, await_id, timeout_ns, waiters,
    hook_bead, role_bead, agent_state, last_activity, role_type, rig, mol_type,
    event_kind, actor, target, payload,
    due_at, defer_until,
    quality_score, work_type, source_system, metadata`
