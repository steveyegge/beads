// Package gitlab provides a tracker.IssueTracker adapter for GitLab.
//
// It wraps the existing internal/gitlab package (client + mapping) to conform
// to the plugin framework interfaces.
package gitlab

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	gitlablib "github.com/steveyegge/beads/internal/gitlab"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

func init() {
	tracker.Register("gitlab", func() tracker.IssueTracker {
		return &Adapter{}
	})
}

// issueIIDPattern matches GitLab issue URLs: .../issues/42
var issueIIDPattern = regexp.MustCompile(`/issues/(\d+)`)

// Adapter implements tracker.IssueTracker for GitLab.
type Adapter struct {
	client *gitlablib.Client
	config *gitlablib.MappingConfig
	store  storage.Storage
}

func (a *Adapter) Name() string        { return "gitlab" }
func (a *Adapter) DisplayName() string  { return "GitLab" }
func (a *Adapter) ConfigPrefix() string { return "gitlab" }

func (a *Adapter) Init(ctx context.Context, store storage.Storage) error {
	a.store = store

	token, err := a.getConfig(ctx, "gitlab.token", "GITLAB_TOKEN")
	if err != nil || token == "" {
		return fmt.Errorf("GitLab token not configured (set gitlab.token or GITLAB_TOKEN)")
	}

	baseURL, _ := a.getConfig(ctx, "gitlab.url", "GITLAB_URL")
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}

	projectID, err := a.getConfig(ctx, "gitlab.project_id", "GITLAB_PROJECT_ID")
	if err != nil || projectID == "" {
		return fmt.Errorf("GitLab project ID not configured (set gitlab.project_id or GITLAB_PROJECT_ID)")
	}

	a.client = gitlablib.NewClient(token, baseURL, projectID)
	a.config = gitlablib.DefaultMappingConfig()
	return nil
}

func (a *Adapter) Validate() error {
	if a.client == nil {
		return fmt.Errorf("GitLab adapter not initialized")
	}
	return nil
}

func (a *Adapter) Close() error { return nil }

func (a *Adapter) FetchIssues(ctx context.Context, opts tracker.FetchOptions) ([]tracker.TrackerIssue, error) {
	var issues []gitlablib.Issue
	var err error

	state := opts.State
	if state == "" {
		state = "all"
	}
	// GitLab uses "opened" not "open"
	if state == "open" {
		state = "opened"
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
	for _, gl := range issues {
		result = append(result, gitlabToTrackerIssue(&gl))
	}
	return result, nil
}

func (a *Adapter) FetchIssue(ctx context.Context, identifier string) (*tracker.TrackerIssue, error) {
	iid, err := strconv.Atoi(identifier)
	if err != nil {
		return nil, fmt.Errorf("invalid GitLab IID %q: %w", identifier, err)
	}

	gl, err := a.client.FetchIssueByIID(ctx, iid)
	if err != nil {
		return nil, err
	}
	if gl == nil {
		return nil, nil
	}

	ti := gitlabToTrackerIssue(gl)
	return &ti, nil
}

func (a *Adapter) CreateIssue(ctx context.Context, issue *types.Issue) (*tracker.TrackerIssue, error) {
	fields := gitlablib.BeadsIssueToGitLabFields(issue, a.config)
	labels, _ := fields["labels"].([]string)

	created, err := a.client.CreateIssue(ctx, issue.Title, issue.Description, labels)
	if err != nil {
		return nil, err
	}

	ti := gitlabToTrackerIssue(created)
	return &ti, nil
}

func (a *Adapter) UpdateIssue(ctx context.Context, externalID string, issue *types.Issue) (*tracker.TrackerIssue, error) {
	iid, err := strconv.Atoi(externalID)
	if err != nil {
		return nil, fmt.Errorf("invalid GitLab IID %q: %w", externalID, err)
	}

	updates := gitlablib.BeadsIssueToGitLabFields(issue, a.config)
	updated, err := a.client.UpdateIssue(ctx, iid, updates)
	if err != nil {
		return nil, err
	}

	ti := gitlabToTrackerIssue(updated)
	return &ti, nil
}

func (a *Adapter) FieldMapper() tracker.FieldMapper {
	return &fieldMapper{config: a.config}
}

func (a *Adapter) IsExternalRef(ref string) bool {
	return strings.Contains(ref, "gitlab") && issueIIDPattern.MatchString(ref)
}

func (a *Adapter) ExtractIdentifier(ref string) string {
	matches := issueIIDPattern.FindStringSubmatch(ref)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

func (a *Adapter) BuildExternalRef(issue *tracker.TrackerIssue) string {
	if issue.URL != "" {
		return issue.URL
	}
	return fmt.Sprintf("gitlab:%s", issue.Identifier)
}

// getConfig reads a config value from storage, falling back to env var.
func (a *Adapter) getConfig(ctx context.Context, key, envVar string) (string, error) {
	val, err := a.store.GetConfig(ctx, key)
	if err == nil && val != "" {
		return val, nil
	}
	if envVar != "" {
		if envVal := envLookup(envVar); envVal != "" {
			return envVal, nil
		}
	}
	return "", nil
}

// gitlabToTrackerIssue converts a gitlab.Issue to a tracker.TrackerIssue.
func gitlabToTrackerIssue(gl *gitlablib.Issue) tracker.TrackerIssue {
	ti := tracker.TrackerIssue{
		ID:         strconv.Itoa(gl.ID),
		Identifier: strconv.Itoa(gl.IID),
		URL:        gl.WebURL,
		Title:      gl.Title,
		Description: gl.Description,
		Labels:     gl.Labels,
		Raw:        gl,
	}

	if gl.State != "" {
		ti.State = gl.State
	}

	if gl.Assignee != nil {
		ti.Assignee = gl.Assignee.Username
		ti.AssigneeID = strconv.Itoa(gl.Assignee.ID)
	}

	if gl.CreatedAt != nil {
		ti.CreatedAt = *gl.CreatedAt
	}
	if gl.UpdatedAt != nil {
		ti.UpdatedAt = *gl.UpdatedAt
	}
	if gl.ClosedAt != nil {
		ti.CompletedAt = gl.ClosedAt
	}

	return ti
}
