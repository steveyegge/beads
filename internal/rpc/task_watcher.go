package rpc

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/steveyegge/beads/internal/storage"
)

// TaskWatcher monitors ~/.claude/tasks/ for changes and syncs task state to beads.
// It uses MD5 hashing for change detection and extracts bead IDs from task content.
type TaskWatcher struct {
	tasksDir     string              // ~/.claude/tasks/
	pollInterval time.Duration       // How often to check for changes
	hashCache    map[string]string   // task_list_id -> MD5 hash
	store        storage.Storage     // Beads storage
	mu           sync.RWMutex        // Protects hashCache
	beadIDRegex  *regexp.Regexp      // Pattern to extract bead IDs from task content
	enabled      bool                // Whether watcher is enabled
}

// TaskFile represents the structure of a Claude Code tasks.json file
type TaskFile struct {
	Todos []TaskTodo `json:"todos"`
}

// TaskTodo represents a single task from TodoWrite
type TaskTodo struct {
	Content    string `json:"content"`
	ActiveForm string `json:"activeForm"`
	Status     string `json:"status"` // pending, in_progress, completed
}

// CCTask represents a task record in the cc_tasks table
type CCTask struct {
	ID          string     `json:"id"`
	TaskListID  string     `json:"task_list_id"`
	BeadID      *string    `json:"bead_id,omitempty"`
	Ordinal     int        `json:"ordinal"`
	Content     string     `json:"content"`
	ActiveForm  string     `json:"active_form"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// TaskListInfo represents aggregated info about a task list
type TaskListInfo struct {
	TaskListID     string    `json:"task_list_id"`
	FilePath       string    `json:"file_path"`
	LastModifiedAt time.Time `json:"last_modified_at"`
	TaskCount      int       `json:"task_count"`
	CompletedCount int       `json:"completed_count"`
	InProgressCount int      `json:"in_progress_count"`
	PendingCount   int       `json:"pending_count"`
}

// NewTaskWatcher creates a new TaskWatcher instance
func NewTaskWatcher(store storage.Storage) *TaskWatcher {
	homeDir, _ := os.UserHomeDir()
	return &TaskWatcher{
		tasksDir:     filepath.Join(homeDir, ".claude", "tasks"),
		pollInterval: 2 * time.Second,
		hashCache:    make(map[string]string),
		store:        store,
		beadIDRegex:  regexp.MustCompile(`^(bd-[a-z0-9]+):\s*(.+)$`),
		enabled:      true,
	}
}

// SetTasksDir allows overriding the tasks directory (for testing)
func (w *TaskWatcher) SetTasksDir(dir string) {
	w.tasksDir = dir
}

// SetPollInterval allows overriding the poll interval
func (w *TaskWatcher) SetPollInterval(interval time.Duration) {
	w.pollInterval = interval
}

// SetEnabled enables or disables the watcher
func (w *TaskWatcher) SetEnabled(enabled bool) {
	w.enabled = enabled
}

// Run starts the task watcher loop
func (w *TaskWatcher) Run(ctx context.Context) error {
	if !w.enabled {
		return nil
	}

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	// Initial scan
	if err := w.scanForChanges(ctx); err != nil {
		// Log but don't fail - directory might not exist yet
		fmt.Fprintf(os.Stderr, "task watcher initial scan: %v\n", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.scanForChanges(ctx); err != nil {
				// Log but continue - transient errors are expected
				fmt.Fprintf(os.Stderr, "task watcher scan error: %v\n", err)
			}
		}
	}
}

// scanForChanges scans the tasks directory for changes
func (w *TaskWatcher) scanForChanges(ctx context.Context) error {
	// List task list directories
	entries, err := os.ReadDir(w.tasksDir)
	if os.IsNotExist(err) {
		return nil // No tasks yet - not an error
	}
	if err != nil {
		return fmt.Errorf("reading tasks directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check context for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		taskListID := entry.Name()
		taskFile := filepath.Join(w.tasksDir, taskListID, "tasks.json")

		if err := w.checkAndSync(ctx, taskListID, taskFile); err != nil {
			// Log but continue with other task lists
			fmt.Fprintf(os.Stderr, "sync error for task list %s: %v\n", taskListID, err)
		}
	}

	return nil
}

// checkAndSync checks if a task file has changed and syncs if needed
func (w *TaskWatcher) checkAndSync(ctx context.Context, taskListID, taskFile string) error {
	// Read file and compute MD5
	data, err := os.ReadFile(taskFile)
	if os.IsNotExist(err) {
		return nil // File doesn't exist - not an error
	}
	if err != nil {
		return fmt.Errorf("reading task file: %w", err)
	}

	hash := fmt.Sprintf("%x", md5.Sum(data))

	// Check cache
	w.mu.RLock()
	cachedHash := w.hashCache[taskListID]
	w.mu.RUnlock()

	if hash == cachedHash {
		return nil // No changes
	}

	// Parse and sync
	if err := w.syncTasks(ctx, taskListID, taskFile, data, hash); err != nil {
		return err
	}

	// Update cache
	w.mu.Lock()
	w.hashCache[taskListID] = hash
	w.mu.Unlock()

	return nil
}

// syncTasks parses the task file and syncs to the database
func (w *TaskWatcher) syncTasks(ctx context.Context, taskListID, taskFile string, data []byte, hash string) error {
	var taskFileData TaskFile
	if err := json.Unmarshal(data, &taskFileData); err != nil {
		return fmt.Errorf("parsing task file: %w", err)
	}

	// Get underlying database
	sqlStore, ok := w.store.(interface{ UnderlyingDB() *sql.DB })
	if !ok {
		return fmt.Errorf("storage does not support UnderlyingDB")
	}
	db := sqlStore.UnderlyingDB()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Ensure task_file_state record exists
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_file_state (task_list_id, file_path, md5_hash, last_modified_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(task_list_id) DO UPDATE SET
			md5_hash = excluded.md5_hash,
			last_modified_at = CURRENT_TIMESTAMP,
			last_checked_at = CURRENT_TIMESTAMP
	`, taskListID, taskFile, hash)
	if err != nil {
		return fmt.Errorf("updating task_file_state: %w", err)
	}

	// Clear existing tasks for this task list (full replace strategy)
	_, err = tx.ExecContext(ctx, `DELETE FROM cc_tasks WHERE task_list_id = ?`, taskListID)
	if err != nil {
		return fmt.Errorf("clearing existing tasks: %w", err)
	}

	// Insert new tasks
	for i, todo := range taskFileData.Todos {
		var beadID *string
		content := todo.Content

		// Extract bead ID if present (format: "bd-xxxx: task description")
		if matches := w.beadIDRegex.FindStringSubmatch(todo.Content); len(matches) == 3 {
			beadID = &matches[1]
			content = matches[2] // Strip prefix from stored content
		}

		taskID := generateTaskID(taskListID, content, i)

		// Determine completed_at
		var completedAt interface{}
		if todo.Status == "completed" {
			completedAt = time.Now()
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO cc_tasks (id, task_list_id, bead_id, ordinal, content, active_form, status, completed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, taskID, taskListID, beadID, i, content, todo.ActiveForm, todo.Status, completedAt)
		if err != nil {
			return fmt.Errorf("inserting task: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// generateTaskID creates a stable task ID from task list, content, and ordinal
func generateTaskID(taskListID, content string, ordinal int) string {
	h := sha256.New()
	h.Write([]byte(taskListID))
	h.Write([]byte(content))
	h.Write([]byte(fmt.Sprintf("%d", ordinal)))
	return fmt.Sprintf("cct-%s", hex.EncodeToString(h.Sum(nil))[:8])
}

// GetTasksForTaskList retrieves all tasks for a given task list ID
func (w *TaskWatcher) GetTasksForTaskList(ctx context.Context, taskListID string) ([]CCTask, error) {
	sqlStore, ok := w.store.(interface{ UnderlyingDB() *sql.DB })
	if !ok {
		return nil, fmt.Errorf("storage does not support UnderlyingDB")
	}
	db := sqlStore.UnderlyingDB()

	rows, err := db.QueryContext(ctx, `
		SELECT id, task_list_id, bead_id, ordinal, content, active_form, status,
		       created_at, updated_at, completed_at
		FROM cc_tasks
		WHERE task_list_id = ?
		ORDER BY ordinal
	`, taskListID)
	if err != nil {
		return nil, fmt.Errorf("querying tasks: %w", err)
	}
	defer rows.Close()

	var tasks []CCTask
	for rows.Next() {
		var t CCTask
		var beadID sql.NullString
		var completedAt sql.NullTime
		err := rows.Scan(&t.ID, &t.TaskListID, &beadID, &t.Ordinal, &t.Content,
			&t.ActiveForm, &t.Status, &t.CreatedAt, &t.UpdatedAt, &completedAt)
		if err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		if beadID.Valid {
			t.BeadID = &beadID.String
		}
		if completedAt.Valid {
			t.CompletedAt = &completedAt.Time
		}
		tasks = append(tasks, t)
	}

	return tasks, rows.Err()
}

// GetTasksForBead retrieves all tasks linked to a given bead ID
func (w *TaskWatcher) GetTasksForBead(ctx context.Context, beadID string) ([]CCTask, error) {
	sqlStore, ok := w.store.(interface{ UnderlyingDB() *sql.DB })
	if !ok {
		return nil, fmt.Errorf("storage does not support UnderlyingDB")
	}
	db := sqlStore.UnderlyingDB()

	rows, err := db.QueryContext(ctx, `
		SELECT id, task_list_id, bead_id, ordinal, content, active_form, status,
		       created_at, updated_at, completed_at
		FROM cc_tasks
		WHERE bead_id = ?
		ORDER BY task_list_id, ordinal
	`, beadID)
	if err != nil {
		return nil, fmt.Errorf("querying tasks: %w", err)
	}
	defer rows.Close()

	var tasks []CCTask
	for rows.Next() {
		var t CCTask
		var beadIDVal sql.NullString
		var completedAt sql.NullTime
		err := rows.Scan(&t.ID, &t.TaskListID, &beadIDVal, &t.Ordinal, &t.Content,
			&t.ActiveForm, &t.Status, &t.CreatedAt, &t.UpdatedAt, &completedAt)
		if err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		if beadIDVal.Valid {
			t.BeadID = &beadIDVal.String
		}
		if completedAt.Valid {
			t.CompletedAt = &completedAt.Time
		}
		tasks = append(tasks, t)
	}

	return tasks, rows.Err()
}

// GetAllTaskLists retrieves summary info for all tracked task lists
func (w *TaskWatcher) GetAllTaskLists(ctx context.Context) ([]TaskListInfo, error) {
	sqlStore, ok := w.store.(interface{ UnderlyingDB() *sql.DB })
	if !ok {
		return nil, fmt.Errorf("storage does not support UnderlyingDB")
	}
	db := sqlStore.UnderlyingDB()

	rows, err := db.QueryContext(ctx, `
		SELECT
			tfs.task_list_id,
			tfs.file_path,
			tfs.last_modified_at,
			COUNT(ct.id) as task_count,
			SUM(CASE WHEN ct.status = 'completed' THEN 1 ELSE 0 END) as completed_count,
			SUM(CASE WHEN ct.status = 'in_progress' THEN 1 ELSE 0 END) as in_progress_count,
			SUM(CASE WHEN ct.status = 'pending' THEN 1 ELSE 0 END) as pending_count
		FROM task_file_state tfs
		LEFT JOIN cc_tasks ct ON tfs.task_list_id = ct.task_list_id
		GROUP BY tfs.task_list_id
		ORDER BY tfs.last_modified_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("querying task lists: %w", err)
	}
	defer rows.Close()

	var lists []TaskListInfo
	for rows.Next() {
		var info TaskListInfo
		var lastModified sql.NullTime
		err := rows.Scan(&info.TaskListID, &info.FilePath, &lastModified,
			&info.TaskCount, &info.CompletedCount, &info.InProgressCount, &info.PendingCount)
		if err != nil {
			return nil, fmt.Errorf("scanning task list: %w", err)
		}
		if lastModified.Valid {
			info.LastModifiedAt = lastModified.Time
		}
		lists = append(lists, info)
	}

	return lists, rows.Err()
}

// GetActiveTasks retrieves all tasks that are currently in_progress
func (w *TaskWatcher) GetActiveTasks(ctx context.Context) ([]CCTask, error) {
	sqlStore, ok := w.store.(interface{ UnderlyingDB() *sql.DB })
	if !ok {
		return nil, fmt.Errorf("storage does not support UnderlyingDB")
	}
	db := sqlStore.UnderlyingDB()

	rows, err := db.QueryContext(ctx, `
		SELECT id, task_list_id, bead_id, ordinal, content, active_form, status,
		       created_at, updated_at, completed_at
		FROM cc_tasks
		WHERE status = 'in_progress'
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("querying active tasks: %w", err)
	}
	defer rows.Close()

	var tasks []CCTask
	for rows.Next() {
		var t CCTask
		var beadID sql.NullString
		var completedAt sql.NullTime
		err := rows.Scan(&t.ID, &t.TaskListID, &beadID, &t.Ordinal, &t.Content,
			&t.ActiveForm, &t.Status, &t.CreatedAt, &t.UpdatedAt, &completedAt)
		if err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		if beadID.Valid {
			t.BeadID = &beadID.String
		}
		if completedAt.Valid {
			t.CompletedAt = &completedAt.Time
		}
		tasks = append(tasks, t)
	}

	return tasks, rows.Err()
}
