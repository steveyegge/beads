// internal/vikunja/types.go
package vikunja

import "time"

// Task represents a Vikunja task.
type Task struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Done        bool      `json:"done"`
	DoneAt      time.Time `json:"done_at,omitempty"`
	DueDate     time.Time `json:"due_date,omitempty"`
	StartDate   time.Time `json:"start_date,omitempty"`
	EndDate     time.Time `json:"end_date,omitempty"`
	Priority    int       `json:"priority"` // 0-4, same as beads
	ProjectID   int64     `json:"project_id"`
	Identifier  string    `json:"identifier"` // e.g., "PROJ-123"
	Index       int64     `json:"index"`
	PercentDone float64   `json:"percent_done"`
	HexColor    string    `json:"hex_color,omitempty"`
	IsFavorite  bool      `json:"is_favorite"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`

	// Nested objects
	CreatedBy    *User             `json:"created_by,omitempty"`
	Assignees    []User            `json:"assignees,omitempty"`
	Labels       []Label           `json:"labels,omitempty"`
	RelatedTasks map[string][]Task `json:"related_tasks,omitempty"` // relation_kind -> tasks
	Comments     []TaskComment     `json:"comments,omitempty"`
	Attachments  []TaskAttachment  `json:"attachments,omitempty"`
}

// User represents a Vikunja user.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
	Name     string `json:"name,omitempty"`
}

// Label represents a Vikunja label.
type Label struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	HexColor    string    `json:"hex_color,omitempty"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
}

// Project represents a Vikunja project.
type Project struct {
	ID          int64         `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Identifier  string        `json:"identifier"` // Short identifier for task IDs
	HexColor    string        `json:"hex_color,omitempty"`
	IsArchived  bool          `json:"is_archived"`
	IsFavorite  bool          `json:"is_favorite"`
	Owner       *User         `json:"owner,omitempty"`
	Views       []ProjectView `json:"views,omitempty"`
	Created     time.Time     `json:"created"`
	Updated     time.Time     `json:"updated"`
}

// ProjectView represents a view within a project (list, kanban, etc.).
type ProjectView struct {
	ID              int64  `json:"id"`
	Title           string `json:"title"`
	ProjectID       int64  `json:"project_id"`
	ViewKind        string `json:"view_kind"` // "list", "gantt", "table", "kanban"
	DefaultBucketID int64  `json:"default_bucket_id,omitempty"`
	DoneBucketID    int64  `json:"done_bucket_id,omitempty"`
}

// TaskRelation represents a relation between two tasks.
type TaskRelation struct {
	TaskID       int64  `json:"task_id"`
	OtherTaskID  int64  `json:"other_task_id"`
	RelationKind string `json:"relation_kind"`
	CreatedBy    *User  `json:"created_by,omitempty"`
}

// TaskComment represents a comment on a task.
type TaskComment struct {
	ID      int64     `json:"id"`
	Comment string    `json:"comment"`
	Author  *User     `json:"author,omitempty"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

// TaskAttachment represents a file attachment.
type TaskAttachment struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	CreatedBy *User     `json:"created_by,omitempty"`
	Created   time.Time `json:"created"`
}

// APIToken represents a Vikunja API token.
type APIToken struct {
	ID          int64               `json:"id"`
	Title       string              `json:"title"`
	Token       string              `json:"token,omitempty"` // Only visible after creation
	Permissions map[string][]string `json:"permissions,omitempty"`
	ExpiresAt   time.Time           `json:"expires_at,omitempty"`
	Created     time.Time           `json:"created"`
}

// RelationKind constants matching Vikunja's enum.
const (
	RelationUnknown     = "unknown"
	RelationSubtask     = "subtask"
	RelationParenttask  = "parenttask"
	RelationRelated     = "related"
	RelationDuplicateOf = "duplicateof"
	RelationDuplicates  = "duplicates"
	RelationBlocking    = "blocking"
	RelationBlocked     = "blocked"
	RelationPrecedes    = "precedes"
	RelationFollows     = "follows"
	RelationCopiedFrom  = "copiedfrom"
	RelationCopiedTo    = "copiedto"
)

// PullStats tracks statistics for pull operations.
type PullStats struct {
	Created     int
	Updated     int
	Skipped     int
	Incremental bool
	SyncedSince string
}

// PushStats tracks statistics for push operations.
type PushStats struct {
	Created int
	Updated int
	Skipped int
	Errors  int
}

// SyncResult represents the overall sync operation result.
type SyncResult struct {
	Success  bool      `json:"success"`
	Stats    SyncStats `json:"stats"`
	LastSync string    `json:"last_sync,omitempty"`
	Error    string    `json:"error,omitempty"`
	Warnings []string  `json:"warnings,omitempty"`
}

// SyncStats aggregates pull and push statistics.
type SyncStats struct {
	Pulled    int `json:"pulled"`
	Pushed    int `json:"pushed"`
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Skipped   int `json:"skipped"`
	Errors    int `json:"errors"`
	Conflicts int `json:"conflicts"`
}

// Conflict represents a sync conflict between local and remote.
type Conflict struct {
	IssueID            string
	LocalUpdated       time.Time
	VikunjaUpdated     time.Time
	VikunjaExternalRef string
	VikunjaTaskID      int64
}

// IssueConversion holds converted issue and extracted dependencies.
type IssueConversion struct {
	Issue        interface{} // *types.Issue - avoiding circular import
	Dependencies []DependencyInfo
}

// DependencyInfo stores relation info for later processing.
type DependencyInfo struct {
	FromVikunjaID int64
	ToVikunjaID   int64
	Type          string // Beads dependency type
}
