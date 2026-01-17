package jira

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client provides methods to interact with the Jira REST API.
type Client struct {
	BaseURL    string
	Project    string
	Username   string // For Cloud: email; for Server: username
	APIToken   string // For Cloud: API token; for Server: password or PAT
	IsCloud    bool   // Whether this is Jira Cloud (vs Server/DC)
	HTTPClient *http.Client
}

// NewClient creates a new Jira client.
func NewClient(baseURL, project, username, apiToken string) *Client {
	baseURL = strings.TrimSuffix(baseURL, "/")
	isCloud := strings.Contains(baseURL, "atlassian.net")

	return &Client{
		BaseURL:  baseURL,
		Project:  project,
		Username: username,
		APIToken: apiToken,
		IsCloud:  isCloud,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// doRequest performs an HTTP request with authentication.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	reqURL := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authentication
	if c.IsCloud && c.Username != "" {
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
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "bd-jira-sync/1.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// FetchIssues retrieves issues from Jira using JQL.
func (c *Client) FetchIssues(ctx context.Context, state string, since *time.Time) ([]Issue, error) {
	var allIssues []Issue
	startAt := 0

	// Build JQL query
	jql := fmt.Sprintf("project = %s", c.Project)
	if since != nil {
		jql += fmt.Sprintf(" AND updated >= \"%s\"", since.Format("2006-01-02 15:04"))
	}
	switch state {
	case "open":
		jql += " AND status != Done AND status != Closed"
	case "closed":
		jql += " AND (status = Done OR status = Closed)"
	}
	jql += " ORDER BY key ASC"

	for {
		// Use API v3 for Jira Cloud (v2 deprecated)
		path := fmt.Sprintf("/rest/api/3/search/jql?jql=%s&startAt=%d&maxResults=%d&fields=*all",
			url.QueryEscape(jql), startAt, MaxPageSize)

		respBody, err := c.doRequest(ctx, "GET", path, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to search issues: %w", err)
		}

		var searchResp SearchResponse
		if err := json.Unmarshal(respBody, &searchResp); err != nil {
			return nil, fmt.Errorf("failed to parse search response: %w", err)
		}

		allIssues = append(allIssues, searchResp.Issues...)

		if startAt+len(searchResp.Issues) >= searchResp.Total {
			break
		}
		startAt += len(searchResp.Issues)
	}

	return allIssues, nil
}

// FetchIssue retrieves a single issue by key.
func (c *Client) FetchIssue(ctx context.Context, key string) (*Issue, error) {
	path := fmt.Sprintf("/rest/api/3/issue/%s", key)

	respBody, err := c.doRequest(ctx, "GET", path, nil)
	if err != nil {
		// Check if 404
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}

	var issue Issue
	if err := json.Unmarshal(respBody, &issue); err != nil {
		return nil, fmt.Errorf("failed to parse issue: %w", err)
	}

	return &issue, nil
}

// CreateIssue creates a new issue in Jira.
func (c *Client) CreateIssue(ctx context.Context, summary, description string, issueType, priority string, labels []string) (*Issue, error) {
	req := CreateIssueRequest{
		Fields: CreateFields{
			Project: ProjectRef{Key: c.Project},
			Summary: summary,
			IssueType: TypeRef{Name: issueType},
		},
	}

	// Set description as ADF for Jira Cloud
	if description != "" {
		if c.IsCloud {
			req.Fields.Description = TextToADF(description)
		} else {
			req.Fields.Description = description
		}
	}

	if priority != "" {
		req.Fields.Priority = &TypeRef{Name: priority}
	}

	if len(labels) > 0 {
		req.Fields.Labels = labels
	}

	respBody, err := c.doRequest(ctx, "POST", "/rest/api/3/issue", req)
	if err != nil {
		return nil, fmt.Errorf("failed to create issue: %w", err)
	}

	var createResp CreateIssueResponse
	if err := json.Unmarshal(respBody, &createResp); err != nil {
		return nil, fmt.Errorf("failed to parse create response: %w", err)
	}

	// Fetch the full issue to get all fields
	return c.FetchIssue(ctx, createResp.Key)
}

// UpdateIssue updates an existing issue.
func (c *Client) UpdateIssue(ctx context.Context, key string, fields map[string]interface{}) error {
	// Convert description to ADF if needed
	if desc, ok := fields["description"].(string); ok && c.IsCloud {
		fields["description"] = TextToADF(desc)
	}

	req := UpdateIssueRequest{Fields: fields}
	path := fmt.Sprintf("/rest/api/3/issue/%s", key)

	_, err := c.doRequest(ctx, "PUT", path, req)
	if err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	return nil
}

// BuildIssueURL returns the web URL for an issue.
func (c *Client) BuildIssueURL(key string) string {
	return fmt.Sprintf("%s/browse/%s", c.BaseURL, key)
}

// TextToADF converts plain text to an ADF document.
func TextToADF(text string) *ADFDocument {
	if text == "" {
		return nil
	}

	// Split into paragraphs
	paragraphs := strings.Split(text, "\n\n")
	var content []ADFNode

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// Handle code blocks
		if strings.HasPrefix(para, "```") {
			lines := strings.Split(para, "\n")
			lang := strings.TrimPrefix(lines[0], "```")
			code := strings.Join(lines[1:], "\n")
			code = strings.TrimSuffix(code, "```")

			content = append(content, ADFNode{
				Type: "codeBlock",
				Attrs: map[string]interface{}{
					"language": lang,
				},
				Content: []ADFNode{{Type: "text", Text: code}},
			})
			continue
		}

		// Handle headings
		if strings.HasPrefix(para, "# ") {
			level := 1
			text := strings.TrimPrefix(para, "# ")
			content = append(content, ADFNode{
				Type:  "heading",
				Attrs: map[string]interface{}{"level": level},
				Content: []ADFNode{{Type: "text", Text: text}},
			})
			continue
		}
		if strings.HasPrefix(para, "## ") {
			level := 2
			text := strings.TrimPrefix(para, "## ")
			content = append(content, ADFNode{
				Type:  "heading",
				Attrs: map[string]interface{}{"level": level},
				Content: []ADFNode{{Type: "text", Text: text}},
			})
			continue
		}

		// Regular paragraph
		content = append(content, ADFNode{
			Type:    "paragraph",
			Content: []ADFNode{{Type: "text", Text: para}},
		})
	}

	if len(content) == 0 {
		return nil
	}

	return &ADFDocument{
		Version: 1,
		Type:    "doc",
		Content: content,
	}
}

// ADFToText converts an ADF document to plain text.
func ADFToText(doc interface{}) string {
	if doc == nil {
		return ""
	}

	// If it's already a string, return it
	if s, ok := doc.(string); ok {
		return s
	}

	// If it's a map (JSON object), process it recursively
	m, ok := doc.(map[string]interface{})
	if !ok {
		return ""
	}

	nodeType, _ := m["type"].(string)
	text, _ := m["text"].(string)
	content, _ := m["content"].([]interface{})

	// Process children
	var childText strings.Builder
	for _, child := range content {
		childText.WriteString(ADFToText(child))
	}

	switch nodeType {
	case "text":
		return text
	case "doc":
		return strings.TrimSpace(childText.String())
	case "paragraph":
		return childText.String() + "\n\n"
	case "heading":
		level := 1
		if attrs, ok := m["attrs"].(map[string]interface{}); ok {
			if l, ok := attrs["level"].(float64); ok {
				level = int(l)
			}
		}
		return strings.Repeat("#", level) + " " + childText.String() + "\n\n"
	case "bulletList", "orderedList":
		return childText.String()
	case "listItem":
		return "- " + strings.TrimSpace(childText.String()) + "\n"
	case "codeBlock":
		lang := ""
		if attrs, ok := m["attrs"].(map[string]interface{}); ok {
			if l, ok := attrs["language"].(string); ok {
				lang = l
			}
		}
		return "```" + lang + "\n" + childText.String() + "```\n\n"
	case "hardBreak":
		return "\n"
	case "rule":
		return "---\n\n"
	default:
		return childText.String()
	}
}
