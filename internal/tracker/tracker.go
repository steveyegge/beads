package tracker

import (
	"context"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TrackerIssue represents an issue from an external tracker in its native format.
// Each tracker plugin converts between this generic format and its specific API types.
type TrackerIssue struct {
	// Core identification
	ID         string // External tracker's internal ID (e.g., UUID)
	Identifier string // Human-readable identifier (e.g., "TEAM-123", "PROJ-456")
	URL        string // Web URL to the issue

	// Content
	Title       string
	Description string

	// Status and workflow
	Priority int         // Priority value (tracker-specific)
	State    interface{} // Tracker-specific state object
	Labels   []string    // Labels/tags

	// Assignment
	Assignee     string // Assignee name or email
	AssigneeID   string // Assignee's tracker-specific ID
	AssigneeEmail string // Assignee email if available

	// Timestamps
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time

	// Relationships
	ParentID        string // Parent issue identifier (for subtasks/children)
	ParentInternalID string // Parent issue internal ID

	// Raw data for tracker-specific processing
	Raw interface{} // Original API response for tracker-specific access
}

// FetchOptions specifies options for fetching issues from an external tracker.
type FetchOptions struct {
	// State filter: "open", "closed", or "all" (default)
	State string

	// Incremental sync: only fetch issues updated since this time
	Since *time.Time

	// Maximum number of issues to fetch (0 = no limit)
	Limit int
}

// IssueTracker defines the interface that all tracker plugins must implement.
// This interface provides a consistent API for interacting with different external
// issue trackers like Linear, Jira, Azure DevOps, etc.
type IssueTracker interface {
	// Identity returns information about this tracker.
	Name() string        // Lowercase identifier: "linear", "jira", "azuredevops"
	DisplayName() string // Human-readable name: "Linear", "Jira", "Azure DevOps"
	ConfigPrefix() string // Config key prefix: "linear", "jira", "azuredevops"

	// Lifecycle manages the tracker connection.
	Init(ctx context.Context, cfg *Config) error
	Validate() error
	Close() error

	// FetchIssues retrieves issues from the external tracker.
	// Use opts.Since for incremental sync, opts.State for filtering.
	FetchIssues(ctx context.Context, opts FetchOptions) ([]TrackerIssue, error)

	// FetchIssue retrieves a single issue by its identifier.
	// Returns nil, nil if the issue doesn't exist.
	FetchIssue(ctx context.Context, identifier string) (*TrackerIssue, error)

	// CreateIssue creates a new issue in the external tracker.
	// Returns the created issue with its external ID and URL populated.
	CreateIssue(ctx context.Context, issue *types.Issue) (*TrackerIssue, error)

	// UpdateIssue updates an existing issue in the external tracker.
	// The externalID is the tracker's internal ID (not the human-readable identifier).
	UpdateIssue(ctx context.Context, externalID string, issue *types.Issue) (*TrackerIssue, error)

	// FieldMapper returns the field mapper for this tracker.
	// The mapper handles bidirectional conversion of priorities, statuses, types, etc.
	FieldMapper() FieldMapper

	// External reference handling
	IsExternalRef(ref string) bool        // Check if an external_ref belongs to this tracker
	ExtractIdentifier(ref string) string  // Extract identifier from external_ref URL
	BuildExternalRef(issue *TrackerIssue) string // Build external_ref URL from issue
	CanonicalizeRef(ref string) string    // Normalize external_ref to canonical form
}
