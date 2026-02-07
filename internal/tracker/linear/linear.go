// Package linear provides a Linear integration plugin for the tracker framework.
// It wraps the existing internal/linear package to implement the IssueTracker interface.
package linear

import (
	"context"
	"time"

	"github.com/steveyegge/beads/internal/linear"
	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

func init() {
	// Register the Linear tracker plugin
	tracker.Register("linear", func() tracker.IssueTracker {
		return &LinearTracker{}
	})
}

// LinearTracker implements the tracker.IssueTracker interface for Linear.
type LinearTracker struct {
	client *linear.Client
	config *tracker.Config
	mapper *LinearMapper
}

// Name returns the tracker identifier.
func (t *LinearTracker) Name() string {
	return "linear"
}

// DisplayName returns the human-readable tracker name.
func (t *LinearTracker) DisplayName() string {
	return "Linear"
}

// ConfigPrefix returns the config key prefix.
func (t *LinearTracker) ConfigPrefix() string {
	return "linear"
}

// Init initializes the tracker with configuration.
func (t *LinearTracker) Init(ctx context.Context, cfg *tracker.Config) error {
	t.config = cfg

	// Get required configuration
	apiKey, err := cfg.GetRequired("api_key")
	if err != nil {
		return err
	}

	teamID, err := cfg.GetRequired("team_id")
	if err != nil {
		return err
	}

	// Create the Linear client
	t.client = linear.NewClient(apiKey, teamID)

	// Apply optional configuration
	if endpoint, _ := cfg.Get("api_endpoint"); endpoint != "" {
		t.client = t.client.WithEndpoint(endpoint)
	}
	if projectID, _ := cfg.Get("project_id"); projectID != "" {
		t.client = t.client.WithProjectID(projectID)
	}

	// Initialize mapper with config
	t.mapper = NewLinearMapper()
	t.mapper.LoadConfig(cfg)

	return nil
}

// Validate checks that the tracker is properly configured.
func (t *LinearTracker) Validate() error {
	if t.client == nil {
		return &tracker.ErrNotInitialized{Tracker: "linear"}
	}
	return nil
}

// Close releases any resources.
func (t *LinearTracker) Close() error {
	return nil
}

// FetchIssues retrieves issues from Linear.
func (t *LinearTracker) FetchIssues(ctx context.Context, opts tracker.FetchOptions) ([]tracker.TrackerIssue, error) {
	var linearIssues []linear.Issue
	var err error

	if opts.Since != nil {
		linearIssues, err = t.client.FetchIssuesSince(ctx, opts.State, *opts.Since)
	} else {
		linearIssues, err = t.client.FetchIssues(ctx, opts.State)
	}

	if err != nil {
		return nil, err
	}

	result := make([]tracker.TrackerIssue, len(linearIssues))
	for i, li := range linearIssues {
		result[i] = t.toTrackerIssue(&li)
	}
	return result, nil
}

// FetchIssue retrieves a single issue by identifier.
func (t *LinearTracker) FetchIssue(ctx context.Context, identifier string) (*tracker.TrackerIssue, error) {
	li, err := t.client.FetchIssueByIdentifier(ctx, identifier)
	if err != nil {
		return nil, err
	}
	if li == nil {
		return nil, nil
	}
	ti := t.toTrackerIssue(li)
	return &ti, nil
}

// CreateIssue creates a new issue in Linear.
func (t *LinearTracker) CreateIssue(ctx context.Context, issue *types.Issue) (*tracker.TrackerIssue, error) {
	// Build state cache for status mapping
	stateCache, err := linear.BuildStateCache(ctx, t.client)
	if err != nil {
		return nil, err
	}

	// Map fields to Linear
	priority := t.mapper.PriorityToTracker(issue.Priority).(int)
	stateID := stateCache.FindStateForBeadsStatus(issue.Status)

	// Build description with extra fields
	description := linear.BuildLinearDescription(issue)

	// Create the issue
	li, err := t.client.CreateIssue(ctx, issue.Title, description, priority, stateID, nil)
	if err != nil {
		return nil, err
	}

	ti := t.toTrackerIssue(li)
	return &ti, nil
}

// UpdateIssue updates an existing issue in Linear.
func (t *LinearTracker) UpdateIssue(ctx context.Context, externalID string, issue *types.Issue) (*tracker.TrackerIssue, error) {
	// Build state cache for status mapping
	stateCache, err := linear.BuildStateCache(ctx, t.client)
	if err != nil {
		return nil, err
	}

	// Build update payload
	description := linear.BuildLinearDescription(issue)
	priority := t.mapper.PriorityToTracker(issue.Priority).(int)
	stateID := stateCache.FindStateForBeadsStatus(issue.Status)

	updates := map[string]interface{}{
		"title":       issue.Title,
		"description": description,
	}
	if priority > 0 {
		updates["priority"] = priority
	}
	if stateID != "" {
		updates["stateId"] = stateID
	}

	li, err := t.client.UpdateIssue(ctx, externalID, updates)
	if err != nil {
		return nil, err
	}

	ti := t.toTrackerIssue(li)
	return &ti, nil
}

// FieldMapper returns the Linear field mapper.
func (t *LinearTracker) FieldMapper() tracker.FieldMapper {
	return t.mapper
}

// IsExternalRef checks if a URL is a Linear issue URL.
func (t *LinearTracker) IsExternalRef(ref string) bool {
	return linear.IsLinearExternalRef(ref)
}

// ExtractIdentifier extracts the issue identifier from a Linear URL.
func (t *LinearTracker) ExtractIdentifier(ref string) string {
	return linear.ExtractLinearIdentifier(ref)
}

// BuildExternalRef builds a Linear URL from an issue.
func (t *LinearTracker) BuildExternalRef(issue *tracker.TrackerIssue) string {
	return issue.URL
}

// CanonicalizeRef normalizes a Linear URL.
func (t *LinearTracker) CanonicalizeRef(ref string) string {
	canonical, ok := linear.CanonicalizeLinearExternalRef(ref)
	if ok {
		return canonical
	}
	return ref
}

// toTrackerIssue converts a Linear issue to a TrackerIssue.
func (t *LinearTracker) toTrackerIssue(li *linear.Issue) tracker.TrackerIssue {
	ti := tracker.TrackerIssue{
		ID:          li.ID,
		Identifier:  li.Identifier,
		URL:         li.URL,
		Title:       li.Title,
		Description: li.Description,
		Priority:    li.Priority,
		State:       li.State,
		Raw:         li,
	}

	// Parse timestamps
	if createdAt, err := time.Parse(time.RFC3339, li.CreatedAt); err == nil {
		ti.CreatedAt = createdAt
	}
	if updatedAt, err := time.Parse(time.RFC3339, li.UpdatedAt); err == nil {
		ti.UpdatedAt = updatedAt
	}
	if li.CompletedAt != "" {
		if completedAt, err := time.Parse(time.RFC3339, li.CompletedAt); err == nil {
			ti.CompletedAt = &completedAt
		}
	}

	// Extract labels
	if li.Labels != nil {
		for _, label := range li.Labels.Nodes {
			ti.Labels = append(ti.Labels, label.Name)
		}
	}

	// Extract assignee
	if li.Assignee != nil {
		if li.Assignee.Email != "" {
			ti.Assignee = li.Assignee.Email
			ti.AssigneeEmail = li.Assignee.Email
		} else {
			ti.Assignee = li.Assignee.Name
		}
		ti.AssigneeID = li.Assignee.ID
	}

	// Extract parent
	if li.Parent != nil {
		ti.ParentID = li.Parent.Identifier
		ti.ParentInternalID = li.Parent.ID
	}

	return ti
}

// Client returns the underlying Linear client for advanced operations.
func (t *LinearTracker) Client() *linear.Client {
	return t.client
}
