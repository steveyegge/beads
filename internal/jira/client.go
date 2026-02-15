package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client provides HTTP access to a Jira instance.
type Client struct {
	URL        string
	Username   string
	APIToken   string
	HTTPClient *http.Client
}

// NewClient creates a new Jira client.
func NewClient(url, username, apiToken string) *Client {
	return &Client{
		URL:      strings.TrimSuffix(url, "/"),
		Username: username,
		APIToken: apiToken,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FetchIssueTimestamp fetches the updated timestamp for a single Jira issue.
func (c *Client) FetchIssueTimestamp(ctx context.Context, jiraKey string) (time.Time, error) {
	var zero time.Time

	if c.URL == "" {
		return zero, fmt.Errorf("jira URL not configured")
	}
	if c.APIToken == "" {
		return zero, fmt.Errorf("jira API token not configured")
	}

	// Build API URL - use v3 for Jira Cloud (v2 is deprecated)
	// Only fetch the 'updated' field to minimize response size
	apiURL := fmt.Sprintf("%s/rest/api/3/issue/%s?fields=updated", c.URL, jiraKey)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return zero, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication header
	isCloud := strings.Contains(c.URL, "atlassian.net")
	if isCloud && c.Username != "" {
		// Jira Cloud: Basic auth with email:api_token
		auth := base64.StdEncoding.EncodeToString([]byte(c.Username + ":" + c.APIToken))
		req.Header.Set("Authorization", "Basic "+auth)
	} else if c.Username != "" {
		// Jira Server with username: Basic auth
		auth := base64.StdEncoding.EncodeToString([]byte(c.Username + ":" + c.APIToken))
		req.Header.Set("Authorization", "Basic "+auth)
	} else {
		// Jira Server without username: Bearer token (PAT)
		req.Header.Set("Authorization", "Bearer "+c.APIToken)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "bd-jira-sync/1.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return zero, fmt.Errorf("failed to fetch issue %s: %w", jiraKey, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return zero, fmt.Errorf("jira API returned %d for issue %s: %s", resp.StatusCode, jiraKey, string(body))
	}

	var result struct {
		Fields struct {
			Updated string `json:"updated"`
		} `json:"fields"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return zero, fmt.Errorf("failed to parse Jira response: %w", err)
	}

	updated, err := ParseTimestamp(result.Fields.Updated)
	if err != nil {
		return zero, fmt.Errorf("failed to parse Jira timestamp: %w", err)
	}

	return updated, nil
}
