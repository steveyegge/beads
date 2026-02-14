// Package linear provides a tracker.IssueTracker adapter for Linear.
//
// It wraps the existing internal/linear package (client + mapping) to conform
// to the plugin framework interfaces, enabling the shared SyncEngine to handle
// Linear synchronization without duplicating logic.
package linear

import (
	"context"
	"fmt"
	"time"

	linearlib "github.com/steveyegge/beads/internal/linear"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

func init() {
	tracker.Register("linear", func() tracker.IssueTracker {
		return &Adapter{}
	})
}

// Adapter implements tracker.IssueTracker for Linear.
type Adapter struct {
	client    *linearlib.Client
	config    *linearlib.MappingConfig
	store     storage.Storage
	teamID    string
	projectID string
}

func (a *Adapter) Name() string         { return "linear" }
func (a *Adapter) DisplayName() string   { return "Linear" }
func (a *Adapter) ConfigPrefix() string  { return "linear" }

func (a *Adapter) Init(ctx context.Context, store storage.Storage) error {
	a.store = store

	apiKey, err := a.getConfig(ctx, "linear.api_key", "LINEAR_API_KEY")
	if err != nil || apiKey == "" {
		return fmt.Errorf("Linear API key not configured (set linear.api_key or LINEAR_API_KEY)")
	}

	teamID, err := a.getConfig(ctx, "linear.team_id", "LINEAR_TEAM_ID")
	if err != nil || teamID == "" {
		return fmt.Errorf("Linear team ID not configured (set linear.team_id or LINEAR_TEAM_ID)")
	}
	a.teamID = teamID

	client := linearlib.NewClient(apiKey, teamID)

	if endpoint, _ := store.GetConfig(ctx, "linear.api_endpoint"); endpoint != "" {
		client = client.WithEndpoint(endpoint)
	}
	if projectID, _ := store.GetConfig(ctx, "linear.project_id"); projectID != "" {
		client = client.WithProjectID(projectID)
		a.projectID = projectID
	}

	a.client = client
	a.config = linearlib.LoadMappingConfig(&configLoaderAdapter{ctx: ctx, store: store})
	return nil
}

func (a *Adapter) Validate() error {
	if a.client == nil {
		return fmt.Errorf("Linear adapter not initialized")
	}
	return nil
}

func (a *Adapter) Close() error { return nil }

func (a *Adapter) FetchIssues(ctx context.Context, opts tracker.FetchOptions) ([]tracker.TrackerIssue, error) {
	var issues []linearlib.Issue
	var err error

	state := opts.State
	if state == "" {
		state = "all"
	}

	if opts.Since != nil {
		issues, err = a.client.FetchIssuesSince(ctx, state, *opts.Since)
	} else {
		issues, err = a.client.FetchIssues(ctx, state)
	}
	if err != nil {
		return nil, err
	}

	result := make([]tracker.TrackerIssue, 0, len(issues))
	for _, li := range issues {
		result = append(result, linearToTrackerIssue(&li))
	}
	return result, nil
}

func (a *Adapter) FetchIssue(ctx context.Context, identifier string) (*tracker.TrackerIssue, error) {
	li, err := a.client.FetchIssueByIdentifier(ctx, identifier)
	if err != nil {
		return nil, err
	}
	if li == nil {
		return nil, nil
	}
	ti := linearToTrackerIssue(li)
	return &ti, nil
}

func (a *Adapter) CreateIssue(ctx context.Context, issue *types.Issue) (*tracker.TrackerIssue, error) {
	priority := linearlib.PriorityToLinear(issue.Priority, a.config)

	// Find the appropriate state ID for the issue's status
	stateID, err := a.findStateID(ctx, issue.Status)
	if err != nil {
		return nil, fmt.Errorf("finding state for status %s: %w", issue.Status, err)
	}

	created, err := a.client.CreateIssue(ctx, issue.Title, issue.Description, priority, stateID, nil)
	if err != nil {
		return nil, err
	}

	ti := linearToTrackerIssue(created)
	return &ti, nil
}

func (a *Adapter) UpdateIssue(ctx context.Context, externalID string, issue *types.Issue) (*tracker.TrackerIssue, error) {
	mapper := a.FieldMapper()
	updates := mapper.IssueToTracker(issue)

	updated, err := a.client.UpdateIssue(ctx, externalID, updates)
	if err != nil {
		return nil, err
	}

	ti := linearToTrackerIssue(updated)
	return &ti, nil
}

func (a *Adapter) FieldMapper() tracker.FieldMapper {
	return &fieldMapper{config: a.config}
}

func (a *Adapter) IsExternalRef(ref string) bool {
	return linearlib.IsLinearExternalRef(ref)
}

func (a *Adapter) ExtractIdentifier(ref string) string {
	return linearlib.ExtractLinearIdentifier(ref)
}

func (a *Adapter) BuildExternalRef(issue *tracker.TrackerIssue) string {
	if issue.URL != "" {
		if canonical, ok := linearlib.CanonicalizeLinearExternalRef(issue.URL); ok {
			return canonical
		}
		return issue.URL
	}
	return fmt.Sprintf("https://linear.app/issue/%s", issue.Identifier)
}

// findStateID looks up the Linear workflow state ID for a beads status.
func (a *Adapter) findStateID(ctx context.Context, status types.Status) (string, error) {
	targetType := linearlib.StatusToLinearStateType(status)

	states, err := a.client.GetTeamStates(ctx)
	if err != nil {
		return "", err
	}

	for _, s := range states {
		if s.Type == targetType {
			return s.ID, nil
		}
	}

	// Fallback: return first state
	if len(states) > 0 {
		return states[0].ID, nil
	}
	return "", fmt.Errorf("no workflow states found")
}

// getConfig reads a config value from storage, falling back to env var.
func (a *Adapter) getConfig(ctx context.Context, key, envVar string) (string, error) {
	val, err := a.store.GetConfig(ctx, key)
	if err == nil && val != "" {
		return val, nil
	}
	// Fall back to environment variable
	if envVar != "" {
		if envVal := envLookup(envVar); envVal != "" {
			return envVal, nil
		}
	}
	return "", nil
}

// linearToTrackerIssue converts a linear.Issue to a tracker.TrackerIssue.
func linearToTrackerIssue(li *linearlib.Issue) tracker.TrackerIssue {
	ti := tracker.TrackerIssue{
		ID:         li.ID,
		Identifier: li.Identifier,
		URL:        li.URL,
		Title:      li.Title,
		Description: li.Description,
		Priority:   li.Priority,
		Labels:     make([]string, 0),
		Raw:        li,
	}

	if li.State != nil {
		ti.State = li.State
	}

	if li.Labels != nil {
		for _, l := range li.Labels.Nodes {
			ti.Labels = append(ti.Labels, l.Name)
		}
	}

	if li.Assignee != nil {
		ti.Assignee = li.Assignee.Name
		ti.AssigneeEmail = li.Assignee.Email
		ti.AssigneeID = li.Assignee.ID
	}

	if li.Parent != nil {
		ti.ParentID = li.Parent.Identifier
		ti.ParentInternalID = li.Parent.ID
	}

	if t, err := time.Parse(time.RFC3339, li.CreatedAt); err == nil {
		ti.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, li.UpdatedAt); err == nil {
		ti.UpdatedAt = t
	}
	if li.CompletedAt != "" {
		if t, err := time.Parse(time.RFC3339, li.CompletedAt); err == nil {
			ti.CompletedAt = &t
		}
	}

	return ti
}

// configLoaderAdapter wraps storage.Storage to implement linear.ConfigLoader.
type configLoaderAdapter struct {
	ctx   context.Context
	store storage.Storage
}

func (c *configLoaderAdapter) GetAllConfig() (map[string]string, error) {
	return c.store.GetAllConfig(c.ctx)
}
