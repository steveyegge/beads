package jira

import (
	"context"
	"fmt"
	"os"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// ErrNotImplemented is returned by Jira tracker methods that are not yet supported.
// Jira pull currently uses a Python script (jira2jsonl.py), not the Go API client.
var ErrNotImplemented = fmt.Errorf("not implemented: Jira sync uses Python script (jira2jsonl.py)")

func init() {
	tracker.Register("jira", func() tracker.IssueTracker {
		return &Tracker{}
	})
}

// Tracker implements tracker.IssueTracker for Jira.
type Tracker struct {
	client  *Client
	store   storage.Storage
	jiraURL string
}

func (t *Tracker) Name() string         { return "jira" }
func (t *Tracker) DisplayName() string  { return "Jira" }
func (t *Tracker) ConfigPrefix() string { return "jira" }

func (t *Tracker) Init(ctx context.Context, store storage.Storage) error {
	t.store = store

	jiraURL, err := t.getConfig(ctx, "jira.url", "JIRA_URL")
	if err != nil || jiraURL == "" {
		return fmt.Errorf("Jira URL not configured (set jira.url or JIRA_URL)")
	}
	t.jiraURL = jiraURL

	username, _ := t.getConfig(ctx, "jira.username", "JIRA_USERNAME")
	apiToken, err := t.getConfig(ctx, "jira.api_token", "JIRA_API_TOKEN")
	if err != nil || apiToken == "" {
		return fmt.Errorf("Jira API token not configured (set jira.api_token or JIRA_API_TOKEN)")
	}

	t.client = NewClient(jiraURL, username, apiToken)
	return nil
}

func (t *Tracker) Validate() error {
	if t.client == nil {
		return fmt.Errorf("Jira tracker not initialized")
	}
	return nil
}

func (t *Tracker) Close() error { return nil }

func (t *Tracker) FetchIssues(_ context.Context, _ tracker.FetchOptions) ([]tracker.TrackerIssue, error) {
	return nil, ErrNotImplemented
}

func (t *Tracker) FetchIssue(_ context.Context, _ string) (*tracker.TrackerIssue, error) {
	return nil, ErrNotImplemented
}

func (t *Tracker) CreateIssue(_ context.Context, _ *types.Issue) (*tracker.TrackerIssue, error) {
	return nil, ErrNotImplemented
}

func (t *Tracker) UpdateIssue(_ context.Context, _ string, _ *types.Issue) (*tracker.TrackerIssue, error) {
	return nil, ErrNotImplemented
}

func (t *Tracker) FieldMapper() tracker.FieldMapper {
	return &jiraFieldMapper{}
}

func (t *Tracker) IsExternalRef(ref string) bool {
	return IsJiraExternalRef(ref, t.jiraURL)
}

func (t *Tracker) ExtractIdentifier(ref string) string {
	return ExtractJiraKey(ref)
}

func (t *Tracker) BuildExternalRef(issue *tracker.TrackerIssue) string {
	return fmt.Sprintf("%s/browse/%s", t.jiraURL, issue.Identifier)
}

// getConfig reads a config value from storage, falling back to env var.
func (t *Tracker) getConfig(ctx context.Context, key, envVar string) (string, error) {
	val, err := t.store.GetConfig(ctx, key)
	if err == nil && val != "" {
		return val, nil
	}
	if envVar != "" {
		if envVal := os.Getenv(envVar); envVal != "" {
			return envVal, nil
		}
	}
	return "", nil
}
