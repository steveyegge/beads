// Package github provides client and data types for the GitHub REST API.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

// NewClient creates a new GitHub client.
func NewClient(token, owner, repo string) *Client {
	return &Client{
		Token:   token,
		Owner:   owner,
		Repo:    repo,
		BaseURL: DefaultAPIEndpoint,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// WithHTTPClient returns a new client with a custom HTTP client.
func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	return &Client{
		Token:      c.Token,
		Owner:      c.Owner,
		Repo:       c.Repo,
		BaseURL:    c.BaseURL,
		HTTPClient: httpClient,
	}
}

// WithBaseURL returns a new client with a custom base URL (for testing or GitHub Enterprise).
func (c *Client) WithBaseURL(baseURL string) *Client {
	return &Client{
		Token:      c.Token,
		Owner:      c.Owner,
		Repo:       c.Repo,
		BaseURL:    baseURL,
		HTTPClient: c.HTTPClient,
	}
}

// repoPath returns the "owner/repo" path segment.
func (c *Client) repoPath() string {
	return c.Owner + "/" + c.Repo
}

// buildURL constructs a full API URL.
func (c *Client) buildURL(path string, params map[string]string) string {
	u := c.BaseURL + path

	if len(params) > 0 {
		values := url.Values{}
		for k, v := range params {
			values.Set(k, v)
		}
		u += "?" + values.Encode()
	}

	return u
}

// doRequest performs an HTTP request with authentication and retry logic.
func (c *Client) doRequest(ctx context.Context, method, urlStr string, body interface{}) ([]byte, http.Header, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	var lastErr error
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, urlStr, reqBody)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed (attempt %d/%d): %w", attempt+1, MaxRetries+1, err)
			continue
		}

		const maxResponseSize = 50 * 1024 * 1024
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response (attempt %d/%d): %w", attempt+1, MaxRetries+1, err)
			continue
		}

		// Handle rate limiting (GitHub uses 403 with X-RateLimit-Remaining: 0, or 429)
		if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0") {
			delay := RetryDelay * time.Duration(1<<attempt)
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
				if seconds, err := strconv.Atoi(retryAfter); err == nil {
					delay = time.Duration(seconds) * time.Second
				}
			}
			lastErr = fmt.Errorf("rate limited (attempt %d/%d)", attempt+1, MaxRetries+1)
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(delay):
				if body != nil {
					jsonBody, err := json.Marshal(body)
					if err != nil {
						lastErr = fmt.Errorf("retry marshal failed: %w", err)
						continue
					}
					reqBody = bytes.NewReader(jsonBody)
				}
				continue
			}
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, nil, fmt.Errorf("API error: %s (status %d)", string(respBody), resp.StatusCode)
		}

		return respBody, resp.Header, nil
	}

	return nil, nil, fmt.Errorf("max retries (%d) exceeded: %w", MaxRetries+1, lastErr)
}

// linkNextPattern matches the "next" relation in GitHub Link headers.
var linkNextPattern = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// hasNextPage checks the Link header for a next page URL and returns it.
func hasNextPage(headers http.Header) (string, bool) {
	link := headers.Get("Link")
	if link == "" {
		return "", false
	}
	matches := linkNextPattern.FindStringSubmatch(link)
	if len(matches) < 2 {
		return "", false
	}
	return matches[1], true
}

// FetchIssues retrieves issues from GitHub with optional state filtering.
// state can be: "open", "closed", or "all".
// This filters out pull requests (GitHub returns PRs in the issues endpoint).
func (c *Client) FetchIssues(ctx context.Context, state string) ([]Issue, error) {
	var allIssues []Issue
	page := 1

	for {
		select {
		case <-ctx.Done():
			return allIssues, ctx.Err()
		default:
		}

		params := map[string]string{
			"per_page": strconv.Itoa(MaxPageSize),
			"page":     strconv.Itoa(page),
		}
		if state != "" && state != "all" {
			params["state"] = state
		} else {
			params["state"] = "all"
		}

		urlStr := c.buildURL("/repos/"+c.repoPath()+"/issues", params)
		respBody, headers, err := c.doRequest(ctx, http.MethodGet, urlStr, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch issues: %w", err)
		}

		var issues []Issue
		if err := json.Unmarshal(respBody, &issues); err != nil {
			return nil, fmt.Errorf("failed to parse issues response: %w", err)
		}

		for i := range issues {
			if issues[i].PullRequest == nil {
				allIssues = append(allIssues, issues[i])
			}
		}

		if _, ok := hasNextPage(headers); !ok {
			break
		}
		page++

		if page > MaxPages {
			return nil, fmt.Errorf("pagination limit exceeded: stopped after %d pages", MaxPages)
		}
	}

	return allIssues, nil
}

// FetchIssuesSince retrieves issues updated since the given time.
func (c *Client) FetchIssuesSince(ctx context.Context, state string, since time.Time) ([]Issue, error) {
	var allIssues []Issue
	page := 1
	sinceStr := since.UTC().Format(time.RFC3339)

	for {
		select {
		case <-ctx.Done():
			return allIssues, ctx.Err()
		default:
		}

		params := map[string]string{
			"per_page": strconv.Itoa(MaxPageSize),
			"page":     strconv.Itoa(page),
			"since":    sinceStr,
		}
		if state != "" && state != "all" {
			params["state"] = state
		} else {
			params["state"] = "all"
		}

		urlStr := c.buildURL("/repos/"+c.repoPath()+"/issues", params)
		respBody, headers, err := c.doRequest(ctx, http.MethodGet, urlStr, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch issues since %s: %w", sinceStr, err)
		}

		var issues []Issue
		if err := json.Unmarshal(respBody, &issues); err != nil {
			return nil, fmt.Errorf("failed to parse issues response: %w", err)
		}

		for i := range issues {
			if issues[i].PullRequest == nil {
				allIssues = append(allIssues, issues[i])
			}
		}

		if _, ok := hasNextPage(headers); !ok {
			break
		}
		page++

		if page > MaxPages {
			return nil, fmt.Errorf("pagination limit exceeded: stopped after %d pages", MaxPages)
		}
	}

	return allIssues, nil
}

// CreateIssue creates a new issue in GitHub.
func (c *Client) CreateIssue(ctx context.Context, title, body string, labels []string) (*Issue, error) {
	reqBody := map[string]interface{}{
		"title": title,
		"body":  body,
	}
	if len(labels) > 0 {
		reqBody["labels"] = labels
	}

	urlStr := c.buildURL("/repos/"+c.repoPath()+"/issues", nil)
	respBody, _, err := c.doRequest(ctx, http.MethodPost, urlStr, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create issue: %w", err)
	}

	var issue Issue
	if err := json.Unmarshal(respBody, &issue); err != nil {
		return nil, fmt.Errorf("failed to parse create response: %w", err)
	}

	return &issue, nil
}

// UpdateIssue updates an existing issue in GitHub.
// GitHub uses PATCH for issue updates.
func (c *Client) UpdateIssue(ctx context.Context, number int, updates map[string]interface{}) (*Issue, error) {
	urlStr := c.buildURL("/repos/"+c.repoPath()+"/issues/"+strconv.Itoa(number), nil)
	respBody, _, err := c.doRequest(ctx, http.MethodPatch, urlStr, updates)
	if err != nil {
		return nil, fmt.Errorf("failed to update issue: %w", err)
	}

	var issue Issue
	if err := json.Unmarshal(respBody, &issue); err != nil {
		return nil, fmt.Errorf("failed to parse update response: %w", err)
	}

	return &issue, nil
}

// FetchIssueByNumber retrieves a single issue by its number.
func (c *Client) FetchIssueByNumber(ctx context.Context, number int) (*Issue, error) {
	urlStr := c.buildURL("/repos/"+c.repoPath()+"/issues/"+strconv.Itoa(number), nil)
	respBody, _, err := c.doRequest(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch issue #%d: %w", number, err)
	}

	var issue Issue
	if err := json.Unmarshal(respBody, &issue); err != nil {
		return nil, fmt.Errorf("failed to parse issue response: %w", err)
	}

	return &issue, nil
}

// ListRepositories retrieves repositories accessible to the authenticated user.
func (c *Client) ListRepositories(ctx context.Context) ([]Repository, error) {
	params := map[string]string{
		"per_page": "100",
		"sort":     "updated",
	}
	urlStr := c.buildURL("/user/repos", params)
	respBody, _, err := c.doRequest(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list repositories: %w", err)
	}

	var repos []Repository
	if err := json.Unmarshal(respBody, &repos); err != nil {
		return nil, fmt.Errorf("failed to parse repositories response: %w", err)
	}

	return repos, nil
}
