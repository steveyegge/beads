package azuredevops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

func init() {
	// Register the Azure DevOps tracker plugin
	tracker.Register("azuredevops", func() tracker.IssueTracker {
		return &AzureDevOpsTracker{}
	})
}

// AzureDevOpsTracker implements the tracker.IssueTracker interface for Azure DevOps.
type AzureDevOpsTracker struct {
	client *Client
	config *tracker.Config
	mapper *AzureDevOpsMapper
}

// Name returns the tracker identifier.
func (t *AzureDevOpsTracker) Name() string {
	return "azuredevops"
}

// DisplayName returns the human-readable tracker name.
func (t *AzureDevOpsTracker) DisplayName() string {
	return "Azure DevOps"
}

// ConfigPrefix returns the config key prefix.
func (t *AzureDevOpsTracker) ConfigPrefix() string {
	return "azuredevops"
}

// Init initializes the tracker with configuration.
func (t *AzureDevOpsTracker) Init(ctx context.Context, cfg *tracker.Config) error {
	t.config = cfg

	// Get required configuration
	organization, err := cfg.GetRequired("organization")
	if err != nil {
		return err
	}

	project, err := cfg.GetRequired("project")
	if err != nil {
		return err
	}

	pat, err := cfg.GetRequired("pat")
	if err != nil {
		return err
	}

	// Create the Azure DevOps client
	t.client = NewClient(organization, project, pat)

	// Initialize mapper with config
	t.mapper = NewAzureDevOpsMapper()
	t.mapper.LoadConfig(cfg)

	return nil
}

// Validate checks that the tracker is properly configured.
func (t *AzureDevOpsTracker) Validate() error {
	if t.client == nil {
		return &tracker.ErrNotInitialized{Tracker: "azuredevops"}
	}
	return nil
}

// Close releases any resources.
func (t *AzureDevOpsTracker) Close() error {
	return nil
}

// FetchIssues retrieves work items from Azure DevOps.
func (t *AzureDevOpsTracker) FetchIssues(ctx context.Context, opts tracker.FetchOptions) ([]tracker.TrackerIssue, error) {
	workItems, err := t.client.FetchWorkItems(ctx, opts.State, opts.Since)
	if err != nil {
		return nil, err
	}

	result := make([]tracker.TrackerIssue, len(workItems))
	for i, wi := range workItems {
		result[i] = t.toTrackerIssue(&wi)
	}
	return result, nil
}

// FetchIssue retrieves a single work item by ID.
func (t *AzureDevOpsTracker) FetchIssue(ctx context.Context, identifier string) (*tracker.TrackerIssue, error) {
	var id int
	if _, err := fmt.Sscanf(identifier, "%d", &id); err != nil {
		return nil, fmt.Errorf("invalid work item ID: %s", identifier)
	}

	wi, err := t.client.FetchWorkItem(ctx, id)
	if err != nil {
		return nil, err
	}
	if wi == nil {
		return nil, nil
	}
	ti := t.toTrackerIssue(wi)
	return &ti, nil
}

// CreateIssue creates a new work item in Azure DevOps.
func (t *AzureDevOpsTracker) CreateIssue(ctx context.Context, issue *types.Issue) (*tracker.TrackerIssue, error) {
	// Map fields to Azure DevOps
	workItemType := t.mapper.TypeToTracker(issue.IssueType).(string)
	priority := t.mapper.PriorityToTracker(issue.Priority).(int)

	wi, err := t.client.CreateWorkItem(ctx, workItemType, issue.Title, issue.Description, priority, issue.Labels)
	if err != nil {
		return nil, err
	}

	ti := t.toTrackerIssue(wi)
	return &ti, nil
}

// UpdateIssue updates an existing work item in Azure DevOps.
func (t *AzureDevOpsTracker) UpdateIssue(ctx context.Context, externalID string, issue *types.Issue) (*tracker.TrackerIssue, error) {
	var id int
	if _, err := fmt.Sscanf(externalID, "%d", &id); err != nil {
		return nil, fmt.Errorf("invalid work item ID: %s", externalID)
	}

	// Build patch operations
	ops := []PatchOperation{
		{Op: "replace", Path: "/fields/System.Title", Value: issue.Title},
	}

	if issue.Description != "" {
		ops = append(ops, PatchOperation{
			Op: "replace", Path: "/fields/System.Description", Value: issue.Description,
		})
	}

	priority := t.mapper.PriorityToTracker(issue.Priority).(int)
	if priority > 0 {
		ops = append(ops, PatchOperation{
			Op: "replace", Path: "/fields/Microsoft.VSTS.Common.Priority", Value: priority,
		})
	}

	if len(issue.Labels) > 0 {
		ops = append(ops, PatchOperation{
			Op: "replace", Path: "/fields/System.Tags", Value: strings.Join(issue.Labels, "; "),
		})
	}

	wi, err := t.client.UpdateWorkItem(ctx, id, ops)
	if err != nil {
		return nil, err
	}

	ti := t.toTrackerIssue(wi)
	return &ti, nil
}

// FieldMapper returns the Azure DevOps field mapper.
func (t *AzureDevOpsTracker) FieldMapper() tracker.FieldMapper {
	return t.mapper
}

// IsExternalRef checks if a URL is an Azure DevOps work item URL.
func (t *AzureDevOpsTracker) IsExternalRef(ref string) bool {
	return strings.Contains(ref, "/_workitems/edit/") || strings.Contains(ref, "dev.azure.com")
}

// ExtractIdentifier extracts the work item ID from a URL.
func (t *AzureDevOpsTracker) ExtractIdentifier(ref string) string {
	// URL format: https://dev.azure.com/org/project/_workitems/edit/123
	id, ok := ParseWorkItemID(ref)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d", id)
}

// BuildExternalRef builds an Azure DevOps URL from a work item.
func (t *AzureDevOpsTracker) BuildExternalRef(issue *tracker.TrackerIssue) string {
	var id int
	if _, err := fmt.Sscanf(issue.Identifier, "%d", &id); err != nil {
		return ""
	}
	return t.client.BuildWorkItemURL(id)
}

// CanonicalizeRef normalizes an Azure DevOps URL.
func (t *AzureDevOpsTracker) CanonicalizeRef(ref string) string {
	// Azure DevOps URLs are already canonical
	return ref
}

// toTrackerIssue converts an Azure DevOps work item to a TrackerIssue.
func (t *AzureDevOpsTracker) toTrackerIssue(wi *WorkItem) tracker.TrackerIssue {
	ti := tracker.TrackerIssue{
		ID:          fmt.Sprintf("%d", wi.ID),
		Identifier:  fmt.Sprintf("%d", wi.ID),
		URL:         t.client.BuildWorkItemURL(wi.ID),
		Title:       wi.Fields.Title,
		Description: wi.Fields.Description,
		Priority:    wi.Fields.Priority,
		State:       wi.Fields.State,
		Raw:         wi,
	}

	// Parse timestamps
	if createdAt, err := parseAzureDevOpsTimestamp(wi.Fields.CreatedDate); err == nil {
		ti.CreatedAt = createdAt
	}
	if changedAt, err := parseAzureDevOpsTimestamp(wi.Fields.ChangedDate); err == nil {
		ti.UpdatedAt = changedAt
	}
	if wi.Fields.ClosedDate != "" {
		if closedAt, err := parseAzureDevOpsTimestamp(wi.Fields.ClosedDate); err == nil {
			ti.CompletedAt = &closedAt
		}
	} else if wi.Fields.ResolvedDate != "" {
		if resolvedAt, err := parseAzureDevOpsTimestamp(wi.Fields.ResolvedDate); err == nil {
			ti.CompletedAt = &resolvedAt
		}
	}

	// Parse tags
	if wi.Fields.Tags != "" {
		tags := strings.Split(wi.Fields.Tags, ";")
		for _, tag := range tags {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				ti.Labels = append(ti.Labels, tag)
			}
		}
	}

	// Extract assignee
	if wi.Fields.AssignedTo != nil {
		ti.Assignee = wi.Fields.AssignedTo.DisplayName
		ti.AssigneeID = wi.Fields.AssignedTo.ID
		if wi.Fields.AssignedTo.UniqueName != "" {
			ti.AssigneeEmail = wi.Fields.AssignedTo.UniqueName
		}
	}

	// Extract parent
	if wi.Fields.Parent > 0 {
		ti.ParentID = fmt.Sprintf("%d", wi.Fields.Parent)
	}

	return ti
}

// parseAzureDevOpsTimestamp parses Azure DevOps timestamp format.
func parseAzureDevOpsTimestamp(ts string) (time.Time, error) {
	if ts == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}

	// Azure DevOps uses ISO 8601 format
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.0000000Z",
		"2006-01-02T15:04:05Z",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, ts); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unrecognized timestamp format: %s", ts)
}

// Client returns the underlying Azure DevOps client for advanced operations.
func (t *AzureDevOpsTracker) Client() *Client {
	return t.client
}
