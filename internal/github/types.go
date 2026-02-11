// Package github provides client and data types for the GitHub REST API.
//
// This package handles all interactions with GitHub's issue tracking system,
// including fetching, creating, and updating issues. It provides bidirectional
// mapping between GitHub's data model and Beads' internal types.
package github

import (
	"net/http"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// API configuration constants.
const (
	// DefaultAPIEndpoint is the GitHub REST API base URL.
	DefaultAPIEndpoint = "https://api.github.com"

	// DefaultTimeout is the default HTTP request timeout.
	DefaultTimeout = 30 * time.Second

	// MaxRetries is the maximum number of retries for rate-limited requests.
	MaxRetries = 3

	// RetryDelay is the base delay between retries (exponential backoff).
	RetryDelay = time.Second

	// MaxPageSize is the maximum number of issues to fetch per page.
	MaxPageSize = 100

	// MaxPages is the maximum number of pages to fetch before stopping.
	// This prevents infinite loops from malformed Link headers.
	MaxPages = 1000
)

// Client provides methods to interact with the GitHub REST API.
type Client struct {
	Token      string       // GitHub personal access token
	Owner      string       // Repository owner (user or org)
	Repo       string       // Repository name
	BaseURL    string       // API base URL (default: https://api.github.com)
	HTTPClient *http.Client // Optional custom HTTP client
}

// Issue represents an issue from the GitHub API.
type Issue struct {
	ID          int        `json:"id"`                       // Global unique ID
	Number      int        `json:"number"`                   // Repository-scoped issue number
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	State       string     `json:"state"`                    // "open" or "closed"
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
	Labels      []Label    `json:"labels"`
	Assignee    *User      `json:"assignee,omitempty"`
	Assignees   []User     `json:"assignees,omitempty"`
	User        *User      `json:"user,omitempty"`           // Author
	Milestone   *Milestone `json:"milestone,omitempty"`
	HTMLURL     string     `json:"html_url"`
	PullRequest *PullRef   `json:"pull_request,omitempty"`   // Non-nil if this is a PR
}

// PullRef indicates an issue is actually a pull request.
// The GitHub Issues API returns PRs alongside issues; this field
// distinguishes them.
type PullRef struct {
	URL string `json:"url,omitempty"`
}

// User represents a GitHub user.
type User struct {
	ID        int    `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
	HTMLURL   string `json:"html_url,omitempty"`
}

// Label represents a GitHub label.
type Label struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description,omitempty"`
}

// Milestone represents a GitHub milestone.
type Milestone struct {
	ID          int        `json:"id"`
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	State       string     `json:"state"`                  // "open" or "closed"
	DueOn       *time.Time `json:"due_on,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
	HTMLURL     string     `json:"html_url,omitempty"`
}

// Repository represents a GitHub repository (for listing repos).
type Repository struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Description   string `json:"description,omitempty"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch,omitempty"`
	Private       bool   `json:"private"`
	Owner         *User  `json:"owner,omitempty"`
}

// SyncStats tracks statistics for a GitHub sync operation.
type SyncStats struct {
	Pulled    int `json:"pulled"`
	Pushed    int `json:"pushed"`
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Skipped   int `json:"skipped"`
	Errors    int `json:"errors"`
	Conflicts int `json:"conflicts"`
}

// SyncResult represents the result of a GitHub sync operation.
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
	Incremental bool     // Whether this was an incremental sync
	SyncedSince string   // Timestamp we synced since (if incremental)
	Warnings    []string // Non-fatal warnings encountered during pull
}

// PushStats tracks push operation statistics.
type PushStats struct {
	Created int
	Updated int
	Skipped int
	Errors  int
}

// Conflict represents a conflict between local and GitHub versions.
// A conflict occurs when both the local and GitHub versions have been modified
// since the last sync.
type Conflict struct {
	IssueID           string    // Beads issue ID
	LocalUpdated      time.Time // When the local version was last modified
	GitHubUpdated     time.Time // When the GitHub version was last modified
	GitHubExternalRef string    // URL to the GitHub issue
	GitHubNumber      int       // GitHub issue number (repository-scoped)
	GitHubID          int       // GitHub's global issue ID
}

// IssueConversion holds the result of converting a GitHub issue to Beads.
// It includes the issue and any dependencies that should be created.
type IssueConversion struct {
	Issue        *types.Issue
	Dependencies []DependencyInfo
}

// DependencyInfo represents a dependency to be created after issue import.
// Stored separately since we need all issues imported before linking dependencies.
type DependencyInfo struct {
	FromGitHubNumber int    // GitHub number of the dependent issue
	ToGitHubNumber   int    // GitHub number of the dependency target
	Type             string // Beads dependency type (blocks, related, parent-child)
}

// PriorityMapping maps priority label values to beads priority (0-4).
// This is the single source of truth for priority mappings.
// Exported so DefaultMappingConfig in mapping.go can use it.
var PriorityMapping = map[string]int{
	"critical": 0, // P0
	"high":     1, // P1
	"medium":   2, // P2
	"low":      3, // P3
	"none":     4, // P4
}

// StatusMapping maps status label values to beads status strings.
// This is the single source of truth for status mappings.
// Exported so DefaultMappingConfig in mapping.go can use it.
var StatusMapping = map[string]string{
	"open":        "open",
	"in_progress": "in_progress",
	"blocked":     "blocked",
	"deferred":    "deferred",
	"closed":      "closed",
}

// TypeMapping maps type label values to beads issue type strings.
// This is the single source of truth for type mappings.
// Exported so DefaultMappingConfig in mapping.go can use it.
var TypeMapping = map[string]string{
	"bug":         "bug",
	"feature":     "feature",
	"task":        "task",
	"epic":        "epic",
	"chore":       "chore",
	"enhancement": "feature",
}

// validStates for GitHub issues.
var validStates = map[string]bool{
	"open":   true,
	"closed": true,
}

// IsValidState checks if a GitHub state string is valid.
func IsValidState(state string) bool {
	return validStates[state]
}

// ParseLabelName extracts prefix and value from a label like "priority:high" or "priority/high".
// GitHub doesn't have scoped labels like GitLab (::), so we support both ":" and "/" separators.
func ParseLabelName(label string) (prefix, value string) {
	// Try colon separator first (priority:high)
	if parts := strings.SplitN(label, ":", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	// Try slash separator (priority/high)
	if parts := strings.SplitN(label, "/", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", label
}

// GetPriorityFromLabel returns the beads priority for a priority label value.
// Returns -1 if the value is not recognized.
func GetPriorityFromLabel(value string) int {
	if p, ok := PriorityMapping[strings.ToLower(value)]; ok {
		return p
	}
	return -1
}

// GetStatusFromLabel returns the beads status for a status label value.
// Returns empty string if the value is not recognized.
func GetStatusFromLabel(value string) string {
	return StatusMapping[strings.ToLower(value)]
}

// GetTypeFromLabel returns the beads issue type for a type label value.
// Returns empty string if the value is not recognized.
func GetTypeFromLabel(value string) string {
	return TypeMapping[strings.ToLower(value)]
}

// LabelNames extracts label name strings from a slice of Label structs.
func LabelNames(labels []Label) []string {
	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
	}
	return names
}
