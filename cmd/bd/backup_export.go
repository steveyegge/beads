package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/debug"
)

// dbQuerier abstracts query execution so callers can use a retry-wrapped
// DoltStore.QueryContext instead of a raw *sql.DB.  Both *sql.DB and
// *dolt.DoltStore satisfy this interface.
type dbQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// backupState tracks watermarks for incremental backup.
type backupState struct {
	LastDoltCommit string    `json:"last_dolt_commit"`
	LastEventID    int64     `json:"last_event_id"`
	Timestamp      time.Time `json:"timestamp"`
	Counts         struct {
		Issues       int `json:"issues"`
		Events       int `json:"events"`
		Comments     int `json:"comments"`
		Dependencies int `json:"dependencies"`
		Labels       int `json:"labels"`
		Config       int `json:"config"`
	} `json:"counts"`
}

// backupDir returns the backup directory path, creating it if needed.
// When backup.git-repo is set to a valid git repo, returns a backup/ subdirectory
// inside that repo. Otherwise falls back to .beads/backup/.
func backupDir() (string, error) {
	gitRepo := config.GetString("backup.git-repo")
	if gitRepo != "" {
		if strings.HasPrefix(gitRepo, "~/") {
			home, _ := os.UserHomeDir()
			gitRepo = filepath.Join(home, gitRepo[2:])
		}
		if _, err := os.Stat(filepath.Join(gitRepo, ".git")); err != nil {
			debug.Logf("backup: git-repo %s is not a git repo, falling back to .beads/backup\n", gitRepo)
		} else {
			dir := filepath.Join(gitRepo, "backup")
			if err := os.MkdirAll(dir, 0700); err != nil {
				return "", fmt.Errorf("failed to create backup dir in git-repo: %w", err)
			}
			return dir, nil
		}
	}
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		beadsDir = ".beads"
	}
	dir := filepath.Join(beadsDir, "backup")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}
	return dir, nil
}

// loadBackupState reads the backup state file, returning a zero state if missing.
func loadBackupState(dir string) (*backupState, error) {
	path := filepath.Join(dir, "backup_state.json")
	data, err := os.ReadFile(path) //nolint:gosec // path is constructed internally
	if os.IsNotExist(err) {
		return &backupState{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read backup state: %w", err)
	}
	var state backupState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse backup state: %w", err)
	}
	return &state, nil
}

// saveBackupState writes the backup state file atomically.
func saveBackupState(dir string, state *backupState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal backup state: %w", err)
	}
	return atomicWriteFile(filepath.Join(dir, "backup_state.json"), data)
}

// atomicWriteFile writes data to a temp file and renames it into place (crash-safe).
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".backup-tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	return nil
}

// runBackupExport exports all tables to JSONL files in .beads/backup/.
// Returns the updated state. Events are exported incrementally using the high-water mark.
func runBackupExport(ctx context.Context, force bool) (*backupState, error) {
	dir, err := backupDir()
	if err != nil {
		return nil, err
	}

	state, err := loadBackupState(dir)
	if err != nil {
		return nil, err
	}

	// Change detection: skip if nothing changed (unless forced)
	if !force {
		currentCommit, err := store.GetCurrentCommit(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get current commit: %w", err)
		}
		if currentCommit == state.LastDoltCommit && state.LastDoltCommit != "" {
			debug.Logf("backup: no changes since last backup (commit %s)\n", truncateHash(currentCommit))
			return state, nil
		}
	}

	hasWisps := tableExistsCheck(ctx, store, "wisps")

	// Export each table. Use SELECT * so we capture all columns (schema has 50+
	// fields and grows over time). The dynamic column scanner handles this automatically.
	issuesQuery := "SELECT * FROM issues ORDER BY id"
	if hasWisps {
		issuesQuery = "SELECT * FROM issues UNION ALL SELECT * FROM wisps ORDER BY id"
	}
	n, err := exportTable(ctx, store, dir, "issues.jsonl", issuesQuery)
	if err != nil {
		return nil, fmt.Errorf("backup issues: %w", err)
	}
	state.Counts.Issues = n

	// Events: incremental append
	n, err = exportEventsIncremental(ctx, store, dir, state, hasWisps)
	if err != nil {
		return nil, fmt.Errorf("backup events: %w", err)
	}
	state.Counts.Events += n

	commentsQuery := "SELECT id, issue_id, author, text, created_at FROM comments ORDER BY id"
	if hasWisps {
		commentsQuery = "SELECT id, issue_id, author, text, created_at FROM comments " +
			"UNION ALL " +
			"SELECT id, issue_id, author, text, created_at FROM wisp_comments " +
			"ORDER BY id"
	}
	n, err = exportTable(ctx, store, dir, "comments.jsonl", commentsQuery)
	if err != nil {
		return nil, fmt.Errorf("backup comments: %w", err)
	}
	state.Counts.Comments = n

	depsQuery := "SELECT issue_id, depends_on_id, type, created_at, created_by FROM dependencies ORDER BY issue_id, depends_on_id"
	if hasWisps {
		depsQuery = "SELECT issue_id, depends_on_id, type, created_at, created_by FROM dependencies " +
			"UNION ALL " +
			"SELECT issue_id, depends_on_id, type, created_at, created_by FROM wisp_dependencies " +
			"ORDER BY issue_id, depends_on_id"
	}
	n, err = exportTable(ctx, store, dir, "dependencies.jsonl", depsQuery)
	if err != nil {
		return nil, fmt.Errorf("backup dependencies: %w", err)
	}
	state.Counts.Dependencies = n

	labelsQuery := "SELECT issue_id, label FROM labels ORDER BY issue_id, label"
	if hasWisps {
		labelsQuery = "SELECT issue_id, label FROM labels " +
			"UNION ALL " +
			"SELECT issue_id, label FROM wisp_labels " +
			"ORDER BY issue_id, label"
	}
	n, err = exportTable(ctx, store, dir, "labels.jsonl", labelsQuery)
	if err != nil {
		return nil, fmt.Errorf("backup labels: %w", err)
	}
	state.Counts.Labels = n

	n, err = exportTable(ctx, store, dir, "config.jsonl",
		"SELECT `key`, value FROM config ORDER BY `key`")
	if err != nil {
		return nil, fmt.Errorf("backup config: %w", err)
	}
	state.Counts.Config = n

	// Update watermarks
	currentCommit, err := store.GetCurrentCommit(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current commit for state: %w", err)
	}
	state.LastDoltCommit = currentCommit
	state.Timestamp = time.Now().UTC()

	if err := saveBackupState(dir, state); err != nil {
		return nil, err
	}

	return state, nil
}

// tableExistsCheck returns true if the named table exists in the database.
func tableExistsCheck(ctx context.Context, q dbQuerier, table string) bool {
	rows, err := q.QueryContext(ctx, "SELECT TABLE_NAME FROM information_schema.tables WHERE TABLE_NAME = ?", table)
	if err != nil {
		return false
	}
	defer rows.Close()
	return rows.Next()
}

// truncateHash returns the first 8 characters of a hash, or the full string if shorter.
func truncateHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// exportTable runs a query and writes each row as a JSON object to a JSONL file.
// Returns the number of rows exported.
func exportTable(ctx context.Context, q dbQuerier, dir, filename, query string) (int, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("failed to get columns: %w", err)
	}

	var lines []byte
	count := 0

	for rows.Next() {
		// Scan into interface{} values
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return 0, fmt.Errorf("scan failed: %w", err)
		}

		// Build a map for JSON serialization
		row := make(map[string]interface{}, len(cols))
		for i, col := range cols {
			row[col] = normalizeValue(values[i])
		}

		data, err := json.Marshal(row)
		if err != nil {
			return 0, fmt.Errorf("marshal failed: %w", err)
		}
		lines = append(lines, data...)
		lines = append(lines, '\n')
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("row iteration failed: %w", err)
	}

	return count, atomicWriteFile(filepath.Join(dir, filename), lines)
}

// exportEventsIncremental appends new events since the last high-water mark.
// On first export (lastEventID=0), dumps all events as a full snapshot.
func exportEventsIncremental(ctx context.Context, q dbQuerier, dir string, state *backupState, hasWisps bool) (int, error) {
	query := "SELECT id, issue_id, event_type, actor, old_value, new_value, comment, created_at " +
		"FROM events WHERE id > ? ORDER BY id ASC"
	args := []interface{}{state.LastEventID}

	if hasWisps {
		query = "SELECT id, issue_id, event_type, actor, old_value, new_value, comment, created_at " +
			"FROM events WHERE id > ? " +
			"UNION ALL " +
			"SELECT id, issue_id, event_type, actor, old_value, new_value, comment, created_at " +
			"FROM wisp_events WHERE id > ? " +
			"ORDER BY id ASC"
		args = []interface{}{state.LastEventID, state.LastEventID}
	}

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("failed to get columns: %w", err)
	}

	var newLines []byte
	count := 0
	var maxID int64

	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return 0, fmt.Errorf("scan failed: %w", err)
		}

		row := make(map[string]interface{}, len(cols))
		for i, col := range cols {
			row[col] = normalizeValue(values[i])
		}

		// Track high-water mark
		if id, ok := row["id"].(int64); ok && id > maxID {
			maxID = id
		}

		data, err := json.Marshal(row)
		if err != nil {
			return 0, fmt.Errorf("marshal failed: %w", err)
		}
		newLines = append(newLines, data...)
		newLines = append(newLines, '\n')
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("row iteration failed: %w", err)
	}

	if count == 0 {
		return 0, nil
	}

	// Append to existing events file (or create new)
	eventsPath := filepath.Join(dir, "events.jsonl")
	if state.LastEventID == 0 {
		// First export: full snapshot via atomic write
		if err := atomicWriteFile(eventsPath, newLines); err != nil {
			return 0, err
		}
	} else {
		// Incremental: append to existing file
		f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600) //nolint:gosec // path is constructed internally
		if err != nil {
			return 0, fmt.Errorf("failed to open events file: %w", err)
		}
		if _, err := f.Write(newLines); err != nil {
			_ = f.Close()
			return 0, fmt.Errorf("failed to append events: %w", err)
		}
		if err := f.Close(); err != nil {
			return 0, fmt.Errorf("failed to close events file: %w", err)
		}
	}

	state.LastEventID = maxID
	return count, nil
}

// normalizeValue converts database driver types to JSON-friendly values.
func normalizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case []byte:
		return string(val)
	case time.Time:
		if val.IsZero() {
			return nil
		}
		return val.Format(time.RFC3339)
	case nil:
		return nil
	default:
		return val
	}
}
