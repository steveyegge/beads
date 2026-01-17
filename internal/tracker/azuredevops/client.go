package azuredevops

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

// Client provides methods to interact with the Azure DevOps REST API.
type Client struct {
	Organization string // Organization name or URL
	Project      string
	PAT          string // Personal Access Token
	BaseURL      string // Full base URL (derived from Organization)
	HTTPClient   *http.Client
}

// NewClient creates a new Azure DevOps client.
func NewClient(organization, project, pat string) *Client {
	// Handle both organization name and full URL
	baseURL := organization
	if !strings.HasPrefix(organization, "http") {
		baseURL = fmt.Sprintf("https://dev.azure.com/%s", organization)
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &Client{
		Organization: organization,
		Project:      project,
		PAT:          pat,
		BaseURL:      baseURL,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// doRequest performs an HTTP request with authentication.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, contentType string) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	// Add API version to path
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	reqURL := c.BaseURL + path + separator + "api-version=" + APIVersion

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Azure DevOps uses Basic auth with empty username and PAT as password
	auth := base64.StdEncoding.EncodeToString([]byte(":" + c.PAT))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	} else if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

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

// FetchWorkItems retrieves work items using WIQL query.
func (c *Client) FetchWorkItems(ctx context.Context, state string, since *time.Time) ([]WorkItem, error) {
	// Build WIQL query
	wiql := fmt.Sprintf("SELECT [System.Id] FROM WorkItems WHERE [System.TeamProject] = '%s'", c.Project)
	if since != nil {
		wiql += fmt.Sprintf(" AND [System.ChangedDate] >= '%s'", since.Format("2006-01-02T15:04:05Z"))
	}
	switch state {
	case "open":
		wiql += " AND [System.State] <> 'Closed' AND [System.State] <> 'Done' AND [System.State] <> 'Removed'"
	case "closed":
		wiql += " AND ([System.State] = 'Closed' OR [System.State] = 'Done')"
	}
	wiql += " ORDER BY [System.Id] ASC"

	// Execute WIQL query to get work item IDs
	queryReq := WIQLQueryRequest{Query: wiql}
	path := fmt.Sprintf("/%s/_apis/wit/wiql", c.Project)

	respBody, err := c.doRequest(ctx, "POST", path, queryReq, "application/json")
	if err != nil {
		return nil, fmt.Errorf("WIQL query failed: %w", err)
	}

	var queryResp WIQLQueryResponse
	if err := json.Unmarshal(respBody, &queryResp); err != nil {
		return nil, fmt.Errorf("failed to parse WIQL response: %w", err)
	}

	if len(queryResp.WorkItems) == 0 {
		return []WorkItem{}, nil
	}

	// Fetch work items in batches
	var allWorkItems []WorkItem
	ids := make([]int, len(queryResp.WorkItems))
	for i, ref := range queryResp.WorkItems {
		ids[i] = ref.ID
	}

	// Batch fetch (max 200 at a time)
	for i := 0; i < len(ids); i += MaxPageSize {
		end := i + MaxPageSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]

		// Build comma-separated ID list
		idStrings := make([]string, len(batch))
		for j, id := range batch {
			idStrings[j] = fmt.Sprintf("%d", id)
		}

		path := fmt.Sprintf("/%s/_apis/wit/workitems?ids=%s&$expand=all",
			c.Project, strings.Join(idStrings, ","))

		respBody, err := c.doRequest(ctx, "GET", path, nil, "")
		if err != nil {
			return nil, fmt.Errorf("failed to fetch work items batch: %w", err)
		}

		var batchResp WorkItemBatchResponse
		if err := json.Unmarshal(respBody, &batchResp); err != nil {
			return nil, fmt.Errorf("failed to parse work items response: %w", err)
		}

		allWorkItems = append(allWorkItems, batchResp.Value...)
	}

	return allWorkItems, nil
}

// FetchWorkItem retrieves a single work item by ID.
func (c *Client) FetchWorkItem(ctx context.Context, id int) (*WorkItem, error) {
	path := fmt.Sprintf("/%s/_apis/wit/workitems/%d?$expand=all", c.Project, id)

	respBody, err := c.doRequest(ctx, "GET", path, nil, "")
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}

	var workItem WorkItem
	if err := json.Unmarshal(respBody, &workItem); err != nil {
		return nil, fmt.Errorf("failed to parse work item: %w", err)
	}

	return &workItem, nil
}

// CreateWorkItem creates a new work item.
func (c *Client) CreateWorkItem(ctx context.Context, workItemType, title, description string, priority int, tags []string) (*WorkItem, error) {
	ops := []PatchOperation{
		{Op: "add", Path: "/fields/System.Title", Value: title},
	}

	if description != "" {
		ops = append(ops, PatchOperation{
			Op: "add", Path: "/fields/System.Description", Value: description,
		})
	}

	if priority > 0 {
		ops = append(ops, PatchOperation{
			Op: "add", Path: "/fields/Microsoft.VSTS.Common.Priority", Value: priority,
		})
	}

	if len(tags) > 0 {
		ops = append(ops, PatchOperation{
			Op: "add", Path: "/fields/System.Tags", Value: strings.Join(tags, "; "),
		})
	}

	// Work item type must be URL encoded
	path := fmt.Sprintf("/%s/_apis/wit/workitems/$%s", c.Project, url.PathEscape(workItemType))

	respBody, err := c.doRequest(ctx, "POST", path, ops, "application/json-patch+json")
	if err != nil {
		return nil, fmt.Errorf("failed to create work item: %w", err)
	}

	var workItem WorkItem
	if err := json.Unmarshal(respBody, &workItem); err != nil {
		return nil, fmt.Errorf("failed to parse create response: %w", err)
	}

	return &workItem, nil
}

// UpdateWorkItem updates an existing work item.
func (c *Client) UpdateWorkItem(ctx context.Context, id int, ops []PatchOperation) (*WorkItem, error) {
	path := fmt.Sprintf("/%s/_apis/wit/workitems/%d", c.Project, id)

	respBody, err := c.doRequest(ctx, "PATCH", path, ops, "application/json-patch+json")
	if err != nil {
		return nil, fmt.Errorf("failed to update work item: %w", err)
	}

	var workItem WorkItem
	if err := json.Unmarshal(respBody, &workItem); err != nil {
		return nil, fmt.Errorf("failed to parse update response: %w", err)
	}

	return &workItem, nil
}

// BuildWorkItemURL returns the web URL for a work item.
func (c *Client) BuildWorkItemURL(id int) string {
	return fmt.Sprintf("%s/%s/_workitems/edit/%d", c.BaseURL, c.Project, id)
}

// ParseWorkItemID extracts the work item ID from a URL.
func ParseWorkItemID(url string) (int, bool) {
	// URL format: https://dev.azure.com/org/project/_workitems/edit/123
	idx := strings.LastIndex(url, "/")
	if idx == -1 {
		return 0, false
	}
	idStr := url[idx+1:]
	var id int
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		return 0, false
	}
	return id, true
}

// ListProjects retrieves all projects accessible in the organization.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	// Core API is at org level, not project level
	// GET https://dev.azure.com/{organization}/_apis/projects
	path := "/_apis/projects?$top=100"

	respBody, err := c.doRequest(ctx, "GET", path, nil, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}

	var resp ProjectListResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse projects response: %w", err)
	}

	return resp.Value, nil
}
