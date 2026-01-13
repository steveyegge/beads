package shortcut

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// NewClient creates a new Shortcut client with the given API token and team ID.
func NewClient(apiToken, teamID string) *Client {
	return &Client{
		APIToken: apiToken,
		TeamID:   teamID,
		Endpoint: DefaultAPIEndpoint,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// WithEndpoint returns a new client configured to use the specified endpoint.
// This is useful for testing with mock servers.
func (c *Client) WithEndpoint(endpoint string) *Client {
	return &Client{
		APIToken:   c.APIToken,
		TeamID:     c.TeamID,
		Endpoint:   endpoint,
		HTTPClient: c.HTTPClient,
	}
}

// WithHTTPClient returns a new client configured to use the specified HTTP client.
// This is useful for testing or customizing timeouts and transport settings.
func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	return &Client{
		APIToken:   c.APIToken,
		TeamID:     c.TeamID,
		Endpoint:   c.Endpoint,
		HTTPClient: httpClient,
	}
}

// doRequest executes an HTTP request to the Shortcut API with rate limiting.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	var lastErr error
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		reqURL := c.Endpoint + path
		httpReq, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Shortcut-Token", c.APIToken)

		resp, err := c.HTTPClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("request failed (attempt %d/%d): %w", attempt+1, MaxRetries+1, err)
			time.Sleep(RetryDelay * time.Duration(1<<attempt))
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response (attempt %d/%d): %w", attempt+1, MaxRetries+1, err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			delay := RetryDelay * time.Duration(1<<attempt) // Exponential backoff

			// Check for Retry-After header
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
				if seconds, err := strconv.Atoi(retryAfter); err == nil {
					delay = time.Duration(seconds) * time.Second
				}
			}

			lastErr = fmt.Errorf("rate limited (attempt %d/%d), retrying after %v", attempt+1, MaxRetries+1, delay)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				// Reset body reader for retry
				if body != nil {
					bodyBytes, _ := json.Marshal(body)
					bodyReader = bytes.NewReader(bodyBytes)
				}
				continue
			}
		}

		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("not found: %s", path)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("API error: %s (status %d)", string(respBody), resp.StatusCode)
		}

		return respBody, nil
	}

	return nil, fmt.Errorf("max retries (%d) exceeded: %w", MaxRetries+1, lastErr)
}

// GetStory retrieves a single story by ID.
func (c *Client) GetStory(ctx context.Context, storyID int64) (*Story, error) {
	path := fmt.Sprintf("/stories/%d", storyID)
	respBody, err := c.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var story Story
	if err := json.Unmarshal(respBody, &story); err != nil {
		return nil, fmt.Errorf("failed to parse story: %w", err)
	}

	return &story, nil
}

// SearchStories searches for stories matching the given criteria.
func (c *Client) SearchStories(ctx context.Context, query string, pageToken *string) (*SearchResults, error) {
	searchParams := map[string]interface{}{
		"query":     query,
		"page_size": MaxPageSize,
	}
	if pageToken != nil {
		searchParams["next"] = *pageToken
	}

	respBody, err := c.doRequest(ctx, "POST", "/search/stories", searchParams)
	if err != nil {
		return nil, err
	}

	var results SearchResults
	if err := json.Unmarshal(respBody, &results); err != nil {
		return nil, fmt.Errorf("failed to parse search results: %w", err)
	}

	return &results, nil
}

// FetchStories retrieves stories from Shortcut for the configured team.
// state can be: "open" (unstarted/started), "closed" (done), or "all".
func (c *Client) FetchStories(ctx context.Context, state string) ([]Story, error) {
	var allStories []Story
	var pageToken *string

	// Build search query
	var queryParts []string

	// Filter by team if configured (use mention name for search queries)
	if c.TeamID != "" {
		mentionName := c.GetTeamMentionName(ctx, c.TeamID)
		queryParts = append(queryParts, fmt.Sprintf("team:%s", mentionName))
	}

	// Filter by state
	switch state {
	case "open":
		queryParts = append(queryParts, "is:unstarted OR is:started")
	case "closed":
		queryParts = append(queryParts, "is:done")
	}

	// Don't include archived by default
	queryParts = append(queryParts, "is:unarchived")

	query := strings.Join(queryParts, " ")

	for {
		results, err := c.SearchStories(ctx, query, pageToken)
		if err != nil {
			return nil, fmt.Errorf("failed to search stories: %w", err)
		}

		// Fetch full story details for each result
		for _, slim := range results.Data {
			story, err := c.GetStory(ctx, slim.ID)
			if err != nil {
				// Log warning but continue
				continue
			}
			allStories = append(allStories, *story)
		}

		if results.NextPageToken == nil {
			break
		}
		pageToken = results.NextPageToken
	}

	return allStories, nil
}

// FetchStoriesSince retrieves stories updated since the given timestamp.
func (c *Client) FetchStoriesSince(ctx context.Context, state string, since time.Time) ([]Story, error) {
	var allStories []Story
	var pageToken *string

	// Build search query with updated filter
	var queryParts []string

	// Filter by team if configured (use mention name for search queries)
	if c.TeamID != "" {
		mentionName := c.GetTeamMentionName(ctx, c.TeamID)
		queryParts = append(queryParts, fmt.Sprintf("team:%s", mentionName))
	}

	switch state {
	case "open":
		queryParts = append(queryParts, "is:unstarted OR is:started")
	case "closed":
		queryParts = append(queryParts, "is:done")
	}

	queryParts = append(queryParts, "is:unarchived")
	queryParts = append(queryParts, fmt.Sprintf("updated:%s..*", since.Format("2006-01-02")))

	query := strings.Join(queryParts, " ")

	for {
		results, err := c.SearchStories(ctx, query, pageToken)
		if err != nil {
			return nil, fmt.Errorf("failed to search stories: %w", err)
		}

		for _, slim := range results.Data {
			story, err := c.GetStory(ctx, slim.ID)
			if err != nil {
				continue
			}
			allStories = append(allStories, *story)
		}

		if results.NextPageToken == nil {
			break
		}
		pageToken = results.NextPageToken
	}

	return allStories, nil
}

// CreateStory creates a new story in Shortcut.
func (c *Client) CreateStory(ctx context.Context, params *CreateStoryParams) (*Story, error) {
	// Set team if not specified and client has a team configured
	if params.GroupID == "" && c.TeamID != "" {
		params.GroupID = c.TeamID
	}

	respBody, err := c.doRequest(ctx, "POST", "/stories", params)
	if err != nil {
		return nil, err
	}

	var story Story
	if err := json.Unmarshal(respBody, &story); err != nil {
		return nil, fmt.Errorf("failed to parse created story: %w", err)
	}

	return &story, nil
}

// UpdateStory updates an existing story in Shortcut.
func (c *Client) UpdateStory(ctx context.Context, storyID int64, params *UpdateStoryParams) (*Story, error) {
	path := fmt.Sprintf("/stories/%d", storyID)
	respBody, err := c.doRequest(ctx, "PUT", path, params)
	if err != nil {
		return nil, err
	}

	var story Story
	if err := json.Unmarshal(respBody, &story); err != nil {
		return nil, fmt.Errorf("failed to parse updated story: %w", err)
	}

	return &story, nil
}

// GetWorkflows retrieves all workflows.
func (c *Client) GetWorkflows(ctx context.Context) ([]Workflow, error) {
	respBody, err := c.doRequest(ctx, "GET", "/workflows", nil)
	if err != nil {
		return nil, err
	}

	var workflows []Workflow
	if err := json.Unmarshal(respBody, &workflows); err != nil {
		return nil, fmt.Errorf("failed to parse workflows: %w", err)
	}

	return workflows, nil
}

// GetTeams retrieves all teams (groups) in the workspace.
func (c *Client) GetTeams(ctx context.Context) ([]Team, error) {
	respBody, err := c.doRequest(ctx, "GET", "/groups", nil)
	if err != nil {
		return nil, err
	}

	var teams []Team
	if err := json.Unmarshal(respBody, &teams); err != nil {
		return nil, fmt.Errorf("failed to parse teams: %w", err)
	}

	return teams, nil
}

// GetTeam retrieves a specific team by its UUID.
func (c *Client) GetTeam(ctx context.Context, teamID string) (*Team, error) {
	respBody, err := c.doRequest(ctx, "GET", "/groups/"+url.PathEscape(teamID), nil)
	if err != nil {
		return nil, err
	}

	var team Team
	if err := json.Unmarshal(respBody, &team); err != nil {
		return nil, fmt.Errorf("failed to parse team: %w", err)
	}

	return &team, nil
}

// GetTeamMentionName looks up a team's mention name from its UUID.
// Returns the mention name or the original teamID if lookup fails.
func (c *Client) GetTeamMentionName(ctx context.Context, teamID string) string {
	// If it doesn't look like a UUID (no hyphens), assume it's already a mention name
	if !strings.Contains(teamID, "-") {
		return teamID
	}

	team, err := c.GetTeam(ctx, teamID)
	if err != nil {
		// Fallback to using teamID as-is
		return teamID
	}

	return team.MentionName
}

// GetMembers retrieves all members in the workspace.
func (c *Client) GetMembers(ctx context.Context) ([]Member, error) {
	respBody, err := c.doRequest(ctx, "GET", "/members", nil)
	if err != nil {
		return nil, err
	}

	var members []Member
	if err := json.Unmarshal(respBody, &members); err != nil {
		return nil, fmt.Errorf("failed to parse members: %w", err)
	}

	return members, nil
}

// BuildStateCache fetches workflows and builds a state cache for quick lookups.
func BuildStateCache(ctx context.Context, client *Client) (*StateCache, error) {
	workflows, err := client.GetWorkflows(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch workflows: %w", err)
	}

	cache := &StateCache{
		Workflows:  workflows,
		StatesByID: make(map[int64]WorkflowState),
	}

	for _, wf := range workflows {
		for _, state := range wf.States {
			cache.StatesByID[state.ID] = state
			if state.Type == "unstarted" && cache.OpenStateID == 0 {
				cache.OpenStateID = state.ID
			}
			if state.Type == "done" && cache.DoneStateID == 0 {
				cache.DoneStateID = state.ID
			}
		}
	}

	return cache, nil
}

// IsShortcutExternalRef checks if an external_ref URL is a Shortcut story URL.
func IsShortcutExternalRef(externalRef string) bool {
	return strings.Contains(externalRef, "app.shortcut.com/") && strings.Contains(externalRef, "/story/")
}

// CanonicalizeShortcutExternalRef returns a stable Shortcut story URL without the slug.
// Example: https://app.shortcut.com/org/story/12345/title -> https://app.shortcut.com/org/story/12345
func CanonicalizeShortcutExternalRef(externalRef string) (canonical string, ok bool) {
	if externalRef == "" || !IsShortcutExternalRef(externalRef) {
		return "", false
	}

	parsed, err := url.Parse(externalRef)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}

	segments := strings.Split(parsed.Path, "/")
	for i, segment := range segments {
		if segment == "story" && i+1 < len(segments) && segments[i+1] != "" {
			// Include org and story/ID, exclude the title slug
			path := "/" + strings.Join(segments[1:i+2], "/")
			return fmt.Sprintf("%s://%s%s", parsed.Scheme, parsed.Host, path), true
		}
	}

	return "", false
}

// ExtractStoryID extracts the story ID from a Shortcut URL.
func ExtractStoryID(externalRef string) (int64, bool) {
	if !IsShortcutExternalRef(externalRef) {
		return 0, false
	}

	parsed, err := url.Parse(externalRef)
	if err != nil {
		return 0, false
	}

	segments := strings.Split(parsed.Path, "/")
	for i, segment := range segments {
		if segment == "story" && i+1 < len(segments) {
			idStr := segments[i+1]
			// Remove any title slug (everything after the ID)
			if slashIdx := strings.Index(idStr, "/"); slashIdx > 0 {
				idStr = idStr[:slashIdx]
			}
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				return 0, false
			}
			return id, true
		}
	}

	return 0, false
}

// BuildStoryURL constructs a Shortcut story URL from organization and story ID.
func BuildStoryURL(org string, storyID int64) string {
	return fmt.Sprintf("https://app.shortcut.com/%s/story/%d", org, storyID)
}
