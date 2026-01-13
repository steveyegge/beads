// Package shortcut provides client and data types for the Shortcut REST API.
//
// This package handles all interactions with Shortcut's issue tracking system,
// including fetching, creating, and updating stories. It provides bidirectional
// mapping between Shortcut's data model and Beads' internal types.
package shortcut

import (
	"net/http"
	"time"
)

// API configuration constants.
const (
	// DefaultAPIEndpoint is the Shortcut REST API endpoint.
	DefaultAPIEndpoint = "https://api.app.shortcut.com/api/v3"

	// DefaultTimeout is the default HTTP request timeout.
	DefaultTimeout = 30 * time.Second

	// MaxRetries is the maximum number of retries for rate-limited requests.
	MaxRetries = 5

	// RetryDelay is the base delay between retries (exponential backoff).
	RetryDelay = time.Second

	// MaxPageSize is the maximum number of stories to fetch per page.
	MaxPageSize = 25
)

// Client provides methods to interact with the Shortcut REST API.
type Client struct {
	APIToken   string
	TeamID     string // Team UUID (for story creation), converted to mention name for searches
	Endpoint   string // REST API endpoint URL (defaults to DefaultAPIEndpoint)
	HTTPClient *http.Client
}

// Story represents a story from the Shortcut API.
type Story struct {
	ID              int64       `json:"id"`
	Name            string      `json:"name"`
	Description     string      `json:"description"`
	AppURL          string      `json:"app_url"`
	StoryType       string      `json:"story_type"` // "feature", "bug", "chore"
	WorkflowStateID int64       `json:"workflow_state_id"`
	Priority        string      `json:"priority"` // "none", "low", "medium", "high", "urgent" (can also be missing)
	Estimate        *int        `json:"estimate"`
	OwnerIDs        []string    `json:"owner_ids"`
	Labels          []Label     `json:"labels"`
	StoryLinks      []StoryLink `json:"story_links"`
	ExternalLinks   []string    `json:"external_links"`
	Blocked         bool        `json:"blocked"`
	Blocker         bool        `json:"blocker"`
	EpicID          *int64      `json:"epic_id"`
	GroupID         *string     `json:"group_id"` // Team ID
	CreatedAt       string      `json:"created_at"`
	UpdatedAt       string      `json:"updated_at"`
	CompletedAt     *string     `json:"completed_at"`
	Archived        bool        `json:"archived"`
}

// StorySlim is a slimmed down version of Story returned by search.
type StorySlim struct {
	ID              int64    `json:"id"`
	Name            string   `json:"name"`
	AppURL          string   `json:"app_url"`
	StoryType       string   `json:"story_type"`
	WorkflowStateID int64    `json:"workflow_state_id"`
	OwnerIDs        []string `json:"owner_ids"`
	Blocked         bool     `json:"blocked"`
	Blocker         bool     `json:"blocker"`
	EpicID          *int64   `json:"epic_id"`
	GroupID         *string  `json:"group_id"`
	UpdatedAt       string   `json:"updated_at"`
	Archived        bool     `json:"archived"`
}

// StoryLink represents a relationship between stories in Shortcut.
type StoryLink struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`       // "blocks", "blocked by", "duplicates", "relates to"
	SubjectID int64  `json:"subject_id"` // The story this link is on
	ObjectID  int64  `json:"object_id"`  // The related story
	Verb      string `json:"verb"`       // "blocks", "is blocked by", "duplicates", "relates to"
}

// Label represents a label in Shortcut.
type Label struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Archived    bool   `json:"archived"`
}

// Member represents a user/member in Shortcut.
type Member struct {
	ID          string  `json:"id"` // UUID
	MentionName string  `json:"mention_name"`
	Name        string  `json:"name"`
	Email       *string `json:"email"`
}

// Team represents a team (group) in Shortcut.
type Team struct {
	ID          string `json:"id"` // UUID
	Name        string `json:"name"`
	MentionName string `json:"mention_name"`
	Description string `json:"description"`
	Archived    bool   `json:"archived"`
}

// Workflow represents a workflow in Shortcut.
type Workflow struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	TeamID      *int64          `json:"team_id,omitempty"`
	States      []WorkflowState `json:"states"`
}

// WorkflowState represents a state within a workflow.
type WorkflowState struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"` // "backlog", "unstarted", "started", "done"
	Position    int    `json:"position"`
}

// SearchResults represents the response from story search.
type SearchResults struct {
	Data          []StorySlim `json:"data"`
	NextPageToken *string     `json:"next"`
	Total         int         `json:"total"`
}

// CreateStoryParams represents parameters for creating a story.
type CreateStoryParams struct {
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	StoryType       string   `json:"story_type,omitempty"`
	WorkflowStateID int64    `json:"workflow_state_id,omitempty"`
	OwnerIDs        []string `json:"owner_ids,omitempty"`
	Labels          []Label  `json:"labels,omitempty"`
	GroupID         string   `json:"group_id,omitempty"` // Team ID
}

// UpdateStoryParams represents parameters for updating a story.
type UpdateStoryParams struct {
	Name            *string  `json:"name,omitempty"`
	Description     *string  `json:"description,omitempty"`
	StoryType       *string  `json:"story_type,omitempty"`
	WorkflowStateID *int64   `json:"workflow_state_id,omitempty"`
	OwnerIDs        []string `json:"owner_ids,omitempty"`
	Archived        *bool    `json:"archived,omitempty"`
}

// SyncStats tracks statistics for a Shortcut sync operation.
type SyncStats struct {
	Pulled    int `json:"pulled"`
	Pushed    int `json:"pushed"`
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Skipped   int `json:"skipped"`
	Errors    int `json:"errors"`
	Conflicts int `json:"conflicts"`
}

// SyncResult represents the result of a Shortcut sync operation.
type SyncResult struct {
	Success  bool      `json:"success"`
	Stats    SyncStats `json:"stats"`
	LastSync string    `json:"last_sync,omitempty"`
	Error    string    `json:"error,omitempty"`
	Warnings []string  `json:"warnings,omitempty"`
}

// PullStats tracks pull operation statistics.
type PullStats struct {
	Created     int
	Updated     int
	Skipped     int
	Incremental bool   // Whether this was an incremental sync
	SyncedSince string // Timestamp we synced since (if incremental)
}

// PushStats tracks push operation statistics.
type PushStats struct {
	Created int
	Updated int
	Skipped int
	Errors  int
}

// Conflict represents a conflict between local and Shortcut versions.
type Conflict struct {
	IssueID             string    // Beads issue ID
	LocalUpdated        time.Time // When the local version was last modified
	ShortcutUpdated     time.Time // When the Shortcut version was last modified
	ShortcutExternalRef string    // URL to the Shortcut story
	ShortcutStoryID     int64     // Shortcut story ID
}

// StoryConversion holds the result of converting a Shortcut story to Beads.
type StoryConversion struct {
	Issue        interface{} // *types.Issue - avoiding circular import
	Dependencies []DependencyInfo
}

// DependencyInfo represents a dependency to be created after story import.
type DependencyInfo struct {
	FromStoryID int64  // Shortcut story ID of the dependent story
	ToStoryID   int64  // Shortcut story ID of the dependency target
	Type        string // Beads dependency type (blocks, related, duplicates, parent-child)
}

// StateCache caches workflow states to avoid repeated API calls.
type StateCache struct {
	Workflows          []Workflow
	StatesByID         map[int64]WorkflowState
	WorkflowByStateID  map[int64]int64 // Maps state ID to workflow ID
	OpenStateID        int64           // First "unstarted" state
	DoneStateID        int64           // First "done" state
}

// beadsStatusToStateType maps a beads status to a Shortcut state type.
func beadsStatusToStateType(status string) string {
	switch status {
	case "open", "blocked", "deferred":
		return "unstarted"
	case "in_progress", "hooked", "pinned":
		return "started"
	case "closed":
		return "done"
	default:
		return "unstarted"
	}
}

// FindStateForBeadsStatus finds a Shortcut workflow state ID for a given beads status.
// This returns the first matching state across all workflows.
func (sc *StateCache) FindStateForBeadsStatus(status string) int64 {
	targetType := beadsStatusToStateType(status)

	// Find a matching state
	for _, state := range sc.StatesByID {
		if state.Type == targetType {
			return state.ID
		}
	}

	// Fall back to open state (which could be backlog or unstarted)
	return sc.OpenStateID
}

// FindStateForBeadsStatusInWorkflow finds a Shortcut workflow state ID for a given
// beads status, staying within the same workflow as the provided current state.
// This is used when updating stories to ensure the new state is compatible with the team.
func (sc *StateCache) FindStateForBeadsStatusInWorkflow(status string, currentStateID int64) int64 {
	targetType := beadsStatusToStateType(status)

	// Find the workflow that contains the current state
	workflowID, ok := sc.WorkflowByStateID[currentStateID]
	if !ok {
		// Fall back to global search if we can't find the workflow
		return sc.FindStateForBeadsStatus(status)
	}

	// Find the workflow and search within its states
	for _, wf := range sc.Workflows {
		if wf.ID != workflowID {
			continue
		}

		// First pass: find exact type match
		for _, state := range wf.States {
			if state.Type == targetType {
				return state.ID
			}
		}

		// Second pass: fall back to backlog if looking for unstarted
		if targetType == "unstarted" {
			for _, state := range wf.States {
				if state.Type == "backlog" {
					return state.ID
				}
			}
		}
	}

	// Fall back to global search
	return sc.FindStateForBeadsStatus(status)
}
