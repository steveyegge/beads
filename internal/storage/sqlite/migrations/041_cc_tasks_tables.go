package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateCCTasksTables adds tables for Claude Code task tracking synchronization.
// This enables the daemon to monitor ~/.claude/tasks/ and sync task state to beads.
// See PRD: docs/prd/task-tracking-sync.md
func MigrateCCTasksTables(db *sql.DB) error {
	// Check if cc_tasks table already exists
	var tableExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM sqlite_master
		WHERE type = 'table' AND name = 'cc_tasks'
	`).Scan(&tableExists)
	if err != nil {
		return fmt.Errorf("failed to check cc_tasks table: %w", err)
	}

	if tableExists {
		return nil // Already migrated
	}

	// Create task_file_state table for tracking file changes via MD5 hash
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS task_file_state (
			task_list_id TEXT PRIMARY KEY,       -- Directory name under ~/.claude/tasks/ (unique task list ID)
			file_path TEXT NOT NULL,              -- Full path to tasks.json
			md5_hash TEXT NOT NULL,               -- Current file hash for change detection
			last_checked_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_modified_at TIMESTAMP            -- When file content last changed
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create task_file_state table: %w", err)
	}

	// Create cc_tasks table for storing individual tasks
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS cc_tasks (
			id TEXT PRIMARY KEY,                   -- Generated: hash(task_list_id + content + ordinal)
			task_list_id TEXT NOT NULL,            -- Directory name under ~/.claude/tasks/ (FK to task_file_state)
			bead_id TEXT,                          -- FK to issues.id (nullable - extracted from content prefix)
			ordinal INTEGER NOT NULL,              -- Position in task list
			content TEXT NOT NULL,                 -- Task description (with bead prefix stripped)
			active_form TEXT,                      -- Present continuous form
			status TEXT NOT NULL DEFAULT 'pending', -- pending, in_progress, completed
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			completed_at TIMESTAMP,                -- Set when status -> completed

			FOREIGN KEY (task_list_id) REFERENCES task_file_state(task_list_id) ON DELETE CASCADE,
			FOREIGN KEY (bead_id) REFERENCES issues(id) ON DELETE SET NULL,
			UNIQUE(task_list_id, ordinal)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create cc_tasks table: %w", err)
	}

	// Create indexes for common query patterns
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_cc_tasks_task_list ON cc_tasks(task_list_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cc_tasks_bead ON cc_tasks(bead_id)`,
		`CREATE INDEX IF NOT EXISTS idx_cc_tasks_status ON cc_tasks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_cc_tasks_task_list_status ON cc_tasks(task_list_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_task_file_state_hash ON task_file_state(md5_hash)`,
	}

	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}
