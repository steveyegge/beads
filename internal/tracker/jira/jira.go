package jira

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

func init() {
	// Register the Jira tracker plugin
	tracker.Register("jira", func() tracker.IssueTracker {
		return &JiraTracker{}
	})
}

// JiraTracker implements the tracker.IssueTracker interface for Jira.
type JiraTracker struct {
	client *Client
	config *tracker.Config
	mapper *JiraMapper
}

// Name returns the tracker identifier.
func (t *JiraTracker) Name() string {
	return "jira"
}

// DisplayName returns the human-readable tracker name.
func (t *JiraTracker) DisplayName() string {
	return "Jira"
}

// ConfigPrefix returns the config key prefix.
func (t *JiraTracker) ConfigPrefix() string {
	return "jira"
}

// Init initializes the tracker with configuration.
func (t *JiraTracker) Init(ctx context.Context, cfg *tracker.Config) error {
	t.config = cfg

	// Get required configuration
	baseURL, err := cfg.GetRequired("url")
	if err != nil {
		return err
	}

	project, err := cfg.GetRequired("project")
	if err != nil {
		return err
	}

	apiToken, err := cfg.GetRequired("api_token")
	if err != nil {
		return err
	}

	// Username is optional for server with PAT
	username, _ := cfg.Get("username")

	// Create the Jira client
	t.client = NewClient(baseURL, project, username, apiToken)

	// Initialize mapper with config
	t.mapper = NewJiraMapper()
	t.mapper.LoadConfig(cfg)

	return nil
}

// Validate checks that the tracker is properly configured.
func (t *JiraTracker) Validate() error {
	if t.client == nil {
		return &tracker.ErrNotInitialized{Tracker: "jira"}
	}
	return nil
}

// Close releases any resources.
func (t *JiraTracker) Close() error {
	return nil
}

// FetchIssues retrieves issues from Jira.
func (t *JiraTracker) FetchIssues(ctx context.Context, opts tracker.FetchOptions) ([]tracker.TrackerIssue, error) {
	issues, err := t.client.FetchIssues(ctx, opts.State, opts.Since)
	if err != nil {
		return nil, err
	}

	result := make([]tracker.TrackerIssue, len(issues))
	for i, ji := range issues {
		result[i] = t.toTrackerIssue(&ji)
	}
	return result, nil
}

// FetchIssue retrieves a single issue by key.
func (t *JiraTracker) FetchIssue(ctx context.Context, identifier string) (*tracker.TrackerIssue, error) {
	ji, err := t.client.FetchIssue(ctx, identifier)
	if err != nil {
		return nil, err
	}
	if ji == nil {
		return nil, nil
	}
	ti := t.toTrackerIssue(ji)
	return &ti, nil
}

// CreateIssue creates a new issue in Jira.
func (t *JiraTracker) CreateIssue(ctx context.Context, issue *types.Issue) (*tracker.TrackerIssue, error) {
	// Map fields to Jira
	issueType := t.mapper.TypeToTracker(issue.IssueType).(string)
	priority := t.mapper.PriorityToTracker(issue.Priority).(string)

	ji, err := t.client.CreateIssue(ctx, issue.Title, issue.Description, issueType, priority, issue.Labels)
	if err != nil {
		return nil, err
	}

	ti := t.toTrackerIssue(ji)
	return &ti, nil
}

// UpdateIssue updates an existing issue in Jira.
func (t *JiraTracker) UpdateIssue(ctx context.Context, externalID string, issue *types.Issue) (*tracker.TrackerIssue, error) {
	// Build update fields
	fields := map[string]interface{}{
		"summary":     issue.Title,
		"description": issue.Description,
	}

	// Add priority if set
	priority := t.mapper.PriorityToTracker(issue.Priority).(string)
	if priority != "" {
		fields["priority"] = map[string]string{"name": priority}
	}

	// Add labels
	if len(issue.Labels) > 0 {
		fields["labels"] = issue.Labels
	}

	err := t.client.UpdateIssue(ctx, externalID, fields)
	if err != nil {
		return nil, err
	}

	// Fetch the updated issue
	return t.FetchIssue(ctx, externalID)
}

// FieldMapper returns the Jira field mapper.
func (t *JiraTracker) FieldMapper() tracker.FieldMapper {
	return t.mapper
}

// IsExternalRef checks if a URL is a Jira issue URL.
func (t *JiraTracker) IsExternalRef(ref string) bool {
	return strings.Contains(ref, "/browse/")
}

// ExtractIdentifier extracts the issue key from a Jira URL.
func (t *JiraTracker) ExtractIdentifier(ref string) string {
	// URL format: https://company.atlassian.net/browse/PROJ-123
	idx := strings.LastIndex(ref, "/browse/")
	if idx == -1 {
		return ""
	}
	return ref[idx+len("/browse/"):]
}

// BuildExternalRef builds a Jira URL from an issue.
func (t *JiraTracker) BuildExternalRef(issue *tracker.TrackerIssue) string {
	return t.client.BuildIssueURL(issue.Identifier)
}

// CanonicalizeRef normalizes a Jira URL.
func (t *JiraTracker) CanonicalizeRef(ref string) string {
	// Jira URLs are already canonical
	return ref
}

// toTrackerIssue converts a Jira issue to a TrackerIssue.
func (t *JiraTracker) toTrackerIssue(ji *Issue) tracker.TrackerIssue {
	ti := tracker.TrackerIssue{
		ID:          ji.ID,
		Identifier:  ji.Key,
		URL:         t.client.BuildIssueURL(ji.Key),
		Title:       ji.Fields.Summary,
		Description: ADFToText(ji.Fields.Description),
		State:       ji.Fields.Status,
		Raw:         ji,
	}

	// Set priority value
	if ji.Fields.Priority != nil {
		ti.Priority = t.mapper.PriorityToBeads(ji.Fields.Priority)
	}

	// Parse timestamps
	if createdAt, err := parseJiraTimestamp(ji.Fields.Created); err == nil {
		ti.CreatedAt = createdAt
	}
	if updatedAt, err := parseJiraTimestamp(ji.Fields.Updated); err == nil {
		ti.UpdatedAt = updatedAt
	}
	if ji.Fields.Resolved != "" {
		if resolvedAt, err := parseJiraTimestamp(ji.Fields.Resolved); err == nil {
			ti.CompletedAt = &resolvedAt
		}
	}

	// Extract labels
	ti.Labels = ji.Fields.Labels

	// Extract assignee
	if ji.Fields.Assignee != nil {
		if ji.Fields.Assignee.EmailAddress != "" {
			ti.Assignee = ji.Fields.Assignee.EmailAddress
			ti.AssigneeEmail = ji.Fields.Assignee.EmailAddress
		} else {
			ti.Assignee = ji.Fields.Assignee.DisplayName
		}
		ti.AssigneeID = ji.Fields.Assignee.AccountID
	}

	// Extract parent
	if ji.Fields.Parent != nil {
		ti.ParentID = ji.Fields.Parent.Key
		ti.ParentInternalID = ji.Fields.Parent.ID
	}

	return ti
}

// parseJiraTimestamp parses Jira's timestamp format.
// Jira uses ISO 8601: 2024-01-15T10:30:00.000+0000 or 2024-01-15T10:30:00.000Z
func parseJiraTimestamp(ts string) (time.Time, error) {
	if ts == "" {
		return time.Time{}, nil
	}

	// Try common formats
	formats := []string{
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
		time.RFC3339Nano,
	}

	for _, format := range formats {
		if t, err := time.Parse(format, ts); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse timestamp: %s", ts)
}

// Client returns the underlying Jira client for advanced operations.
func (t *JiraTracker) Client() *Client {
	return t.client
}
