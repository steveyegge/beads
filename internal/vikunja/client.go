// internal/vikunja/client.go
package vikunja

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	DefaultTimeout = 30 * time.Second
	MaxRetries     = 3
	RetryDelay     = time.Second
	DefaultPerPage = 50
)

// Client provides methods to interact with the Vikunja REST API.
type Client struct {
	BaseURL    string
	Token      string
	ProjectID  int64
	ViewID     int64
	HTTPClient *http.Client
}

// NewClient creates a new Vikunja client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// WithProjectID returns a new client configured for a specific project.
func (c *Client) WithProjectID(projectID int64) *Client {
	return &Client{
		BaseURL:    c.BaseURL,
		Token:      c.Token,
		ProjectID:  projectID,
		ViewID:     c.ViewID,
		HTTPClient: c.HTTPClient,
	}
}

// WithViewID returns a new client configured for a specific view.
func (c *Client) WithViewID(viewID int64) *Client {
	return &Client{
		BaseURL:    c.BaseURL,
		Token:      c.Token,
		ProjectID:  c.ProjectID,
		ViewID:     viewID,
		HTTPClient: c.HTTPClient,
	}
}

// request sends an HTTP request to the Vikunja API.
func (c *Client) request(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		var bodyReader io.Reader
		if body != nil {
			jsonBody, err := json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal request body: %w", err)
			}
			bodyReader = bytes.NewReader(jsonBody)
		}

		req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed (attempt %d/%d): %w", attempt+1, MaxRetries+1, err)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response (attempt %d/%d): %w", attempt+1, MaxRetries+1, err)
			continue
		}

		// Rate limiting with exponential backoff
		if resp.StatusCode == http.StatusTooManyRequests {
			delay := RetryDelay * time.Duration(1<<attempt)
			lastErr = fmt.Errorf("rate limited (attempt %d/%d)", attempt+1, MaxRetries+1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				continue
			}
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("API error: %s (status %d)", string(respBody), resp.StatusCode)
		}

		return respBody, nil
	}

	return nil, fmt.Errorf("max retries (%d) exceeded: %w", MaxRetries+1, lastErr)
}

// requestWithPagination fetches all pages of a paginated endpoint.
func (c *Client) requestWithPagination(ctx context.Context, path string, params url.Values) ([]json.RawMessage, error) {
	var allResults []json.RawMessage
	page := 1

	for {
		params.Set("page", strconv.Itoa(page))
		params.Set("per_page", strconv.Itoa(DefaultPerPage))

		fullPath := path
		if len(params) > 0 {
			fullPath = path + "?" + params.Encode()
		}

		resp, err := c.request(ctx, "GET", fullPath, nil)
		if err != nil {
			return nil, err
		}

		var pageResults []json.RawMessage
		if err := json.Unmarshal(resp, &pageResults); err != nil {
			return nil, fmt.Errorf("failed to parse page results: %w", err)
		}

		allResults = append(allResults, pageResults...)

		// If we got fewer than per_page results, we've reached the end
		if len(pageResults) < DefaultPerPage {
			break
		}

		page++
	}

	return allResults, nil
}

// FetchTasks retrieves tasks from the configured project and view.
// state can be "all", "open", or "closed".
func (c *Client) FetchTasks(ctx context.Context, state string) ([]Task, error) {
	if c.ProjectID == 0 {
		return nil, fmt.Errorf("project ID not configured")
	}
	if c.ViewID == 0 {
		return nil, fmt.Errorf("view ID not configured")
	}

	path := fmt.Sprintf("/projects/%d/views/%d/tasks", c.ProjectID, c.ViewID)
	params := url.Values{}

	// Apply state filter
	switch state {
	case "open":
		params.Set("filter", "done = false")
	case "closed":
		params.Set("filter", "done = true")
	// "all" - no filter
	}

	rawResults, err := c.requestWithPagination(ctx, path, params)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tasks: %w", err)
	}

	tasks := make([]Task, 0, len(rawResults))
	for _, raw := range rawResults {
		var task Task
		if err := json.Unmarshal(raw, &task); err != nil {
			return nil, fmt.Errorf("failed to parse task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// FetchTasksSince retrieves tasks updated since the given time.
func (c *Client) FetchTasksSince(ctx context.Context, state string, since time.Time) ([]Task, error) {
	if c.ProjectID == 0 {
		return nil, fmt.Errorf("project ID not configured")
	}
	if c.ViewID == 0 {
		return nil, fmt.Errorf("view ID not configured")
	}

	path := fmt.Sprintf("/projects/%d/views/%d/tasks", c.ProjectID, c.ViewID)
	params := url.Values{}

	// Build filter for incremental sync
	filter := fmt.Sprintf("updated > %q", since.Format(time.RFC3339))
	switch state {
	case "open":
		filter += " && done = false"
	case "closed":
		filter += " && done = true"
	}
	params.Set("filter", filter)

	rawResults, err := c.requestWithPagination(ctx, path, params)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tasks: %w", err)
	}

	tasks := make([]Task, 0, len(rawResults))
	for _, raw := range rawResults {
		var task Task
		if err := json.Unmarshal(raw, &task); err != nil {
			return nil, fmt.Errorf("failed to parse task: %w", err)
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// FetchTask retrieves a single task by ID.
func (c *Client) FetchTask(ctx context.Context, taskID int64) (*Task, error) {
	path := fmt.Sprintf("/tasks/%d", taskID)

	resp, err := c.request(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch task %d: %w", taskID, err)
	}

	var task Task
	if err := json.Unmarshal(resp, &task); err != nil {
		return nil, fmt.Errorf("failed to parse task: %w", err)
	}

	return &task, nil
}

// CreateTask creates a new task in the configured project.
func (c *Client) CreateTask(ctx context.Context, task *Task) (*Task, error) {
	if c.ProjectID == 0 {
		return nil, fmt.Errorf("project ID not configured")
	}

	path := fmt.Sprintf("/projects/%d/tasks", c.ProjectID)

	resp, err := c.request(ctx, "PUT", path, task)
	if err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	var created Task
	if err := json.Unmarshal(resp, &created); err != nil {
		return nil, fmt.Errorf("failed to parse created task: %w", err)
	}

	return &created, nil
}

// UpdateTask updates an existing task.
func (c *Client) UpdateTask(ctx context.Context, taskID int64, updates map[string]interface{}) (*Task, error) {
	path := fmt.Sprintf("/tasks/%d", taskID)

	resp, err := c.request(ctx, "POST", path, updates)
	if err != nil {
		return nil, fmt.Errorf("failed to update task %d: %w", taskID, err)
	}

	var updated Task
	if err := json.Unmarshal(resp, &updated); err != nil {
		return nil, fmt.Errorf("failed to parse updated task: %w", err)
	}

	return &updated, nil
}

// FetchProjects retrieves all projects accessible to the user.
func (c *Client) FetchProjects(ctx context.Context) ([]Project, error) {
	rawResults, err := c.requestWithPagination(ctx, "/projects", url.Values{})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch projects: %w", err)
	}

	projects := make([]Project, 0, len(rawResults))
	for _, raw := range rawResults {
		var project Project
		if err := json.Unmarshal(raw, &project); err != nil {
			return nil, fmt.Errorf("failed to parse project: %w", err)
		}
		projects = append(projects, project)
	}

	return projects, nil
}

// FetchProject retrieves a single project with its views.
func (c *Client) FetchProject(ctx context.Context, projectID int64) (*Project, error) {
	path := fmt.Sprintf("/projects/%d", projectID)

	resp, err := c.request(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch project %d: %w", projectID, err)
	}

	var project Project
	if err := json.Unmarshal(resp, &project); err != nil {
		return nil, fmt.Errorf("failed to parse project: %w", err)
	}

	return &project, nil
}

// CreateRelation creates a relation between two tasks.
func (c *Client) CreateRelation(ctx context.Context, taskID, otherTaskID int64, relationKind string) error {
	path := fmt.Sprintf("/tasks/%d/relations", taskID)

	relation := TaskRelation{
		OtherTaskID:  otherTaskID,
		RelationKind: relationKind,
	}

	_, err := c.request(ctx, "PUT", path, relation)
	if err != nil {
		return fmt.Errorf("failed to create relation: %w", err)
	}

	return nil
}

// DeleteRelation removes a relation between two tasks.
func (c *Client) DeleteRelation(ctx context.Context, taskID int64, relationKind string, otherTaskID int64) error {
	path := fmt.Sprintf("/tasks/%d/relations/%s/%d", taskID, relationKind, otherTaskID)

	_, err := c.request(ctx, "DELETE", path, nil)
	if err != nil {
		return fmt.Errorf("failed to delete relation: %w", err)
	}

	return nil
}
