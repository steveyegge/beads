package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
)

const (
	// linearAPIEndpoint is the Linear GraphQL API endpoint.
	linearAPIEndpoint = "https://api.linear.app/graphql"

	// linearDefaultTimeout is the default HTTP request timeout.
	linearDefaultTimeout = 30 * time.Second

	// linearMaxRetries is the maximum number of retries for rate-limited requests.
	linearMaxRetries = 3

	// linearRetryDelay is the base delay between retries (exponential backoff).
	linearRetryDelay = time.Second
)

// LinearClient provides methods to interact with the Linear GraphQL API.
type LinearClient struct {
	apiKey     string
	teamID     string
	httpClient *http.Client
}

// NewLinearClient creates a new Linear client with the given API key and team ID.
func NewLinearClient(apiKey, teamID string) *LinearClient {
	return &LinearClient{
		apiKey: apiKey,
		teamID: teamID,
		httpClient: &http.Client{
			Timeout: linearDefaultTimeout,
		},
	}
}

// GraphQLRequest represents a GraphQL request payload.
type GraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// GraphQLResponse represents a generic GraphQL response.
type GraphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []GraphQLError  `json:"errors,omitempty"`
}

// GraphQLError represents a GraphQL error.
type GraphQLError struct {
	Message    string   `json:"message"`
	Path       []string `json:"path,omitempty"`
	Extensions struct {
		Code string `json:"code,omitempty"`
	} `json:"extensions,omitempty"`
}

// LinearIssue represents an issue from the Linear API.
type LinearIssue struct {
	ID          string           `json:"id"`
	Identifier  string           `json:"identifier"` // e.g., "TEAM-123"
	Title       string           `json:"title"`
	Description string           `json:"description"`
	URL         string           `json:"url"`
	Priority    int              `json:"priority"` // 0=no priority, 1=urgent, 2=high, 3=medium, 4=low
	State       *LinearState     `json:"state"`
	Assignee    *LinearUser      `json:"assignee"`
	Labels      *LinearLabels    `json:"labels"`
	Parent      *LinearParent    `json:"parent,omitempty"`
	Relations   *LinearRelations `json:"relations,omitempty"`
	CreatedAt   string           `json:"createdAt"`
	UpdatedAt   string           `json:"updatedAt"`
	CompletedAt string           `json:"completedAt,omitempty"`
}

// LinearState represents a workflow state in Linear.
type LinearState struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // "backlog", "unstarted", "started", "completed", "canceled"
}

// LinearUser represents a user in Linear.
type LinearUser struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

// LinearLabels represents paginated labels on an issue.
type LinearLabels struct {
	Nodes []LinearLabel `json:"nodes"`
}

// LinearLabel represents a label in Linear.
type LinearLabel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// LinearParent represents a parent issue reference.
type LinearParent struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
}

// LinearRelation represents a relation between issues in Linear.
type LinearRelation struct {
	ID           string `json:"id"`
	Type         string `json:"type"` // "blocks", "blockedBy", "duplicate", "related"
	RelatedIssue struct {
		ID         string `json:"id"`
		Identifier string `json:"identifier"`
	} `json:"relatedIssue"`
}

// LinearRelations wraps the nodes array for relations.
type LinearRelations struct {
	Nodes []LinearRelation `json:"nodes"`
}

// LinearMappingConfig holds configurable mappings between Linear and Beads.
// All maps use lowercase keys for case-insensitive matching.
type LinearMappingConfig struct {
	// PriorityMap maps Linear priority (0-4) to Beads priority (0-4).
	// Key is Linear priority as string, value is Beads priority.
	PriorityMap map[string]int

	// StateMap maps Linear state types/names to Beads statuses.
	// Key is lowercase state type or name, value is Beads status string.
	StateMap map[string]string

	// LabelTypeMap maps Linear label names to Beads issue types.
	// Key is lowercase label name, value is Beads issue type.
	LabelTypeMap map[string]string

	// RelationMap maps Linear relation types to Beads dependency types.
	// Key is Linear relation type, value is Beads dependency type.
	RelationMap map[string]string
}

// defaultLinearMappingConfig returns sensible default mappings.
func defaultLinearMappingConfig() *LinearMappingConfig {
	return &LinearMappingConfig{
		// Linear priority: 0=none, 1=urgent, 2=high, 3=medium, 4=low
		// Beads priority: 0=critical, 1=high, 2=medium, 3=low, 4=backlog
		PriorityMap: map[string]int{
			"0": 4, // No priority -> Backlog
			"1": 0, // Urgent -> Critical
			"2": 1, // High -> High
			"3": 2, // Medium -> Medium
			"4": 3, // Low -> Low
		},
		// Linear state types: backlog, unstarted, started, completed, canceled
		StateMap: map[string]string{
			"backlog":   "open",
			"unstarted": "open",
			"started":   "in_progress",
			"completed": "closed",
			"canceled":  "closed",
		},
		// Label patterns for issue type inference
		LabelTypeMap: map[string]string{
			"bug":         "bug",
			"defect":      "bug",
			"feature":     "feature",
			"enhancement": "feature",
			"epic":        "epic",
			"chore":       "chore",
			"maintenance": "chore",
			"task":        "task",
		},
		// Linear relation types to Beads dependency types
		RelationMap: map[string]string{
			"blocks":    "blocks",
			"blockedBy": "blocks", // Inverse: the related issue blocks this one
			"duplicate": "duplicates",
			"related":   "related",
		},
	}
}

// loadLinearMappingConfig loads mapping configuration from beads config.
// Config keys follow the pattern: linear.<category>_map.<key> = <value>
// Examples:
//
//	linear.priority_map.0 = 4       (Linear "no priority" -> Beads backlog)
//	linear.state_map.started = in_progress
//	linear.label_type_map.bug = bug
//	linear.relation_map.blocks = blocks
func loadLinearMappingConfig(ctx context.Context) *LinearMappingConfig {
	config := defaultLinearMappingConfig()

	if store == nil {
		return config
	}

	// Load all config keys and filter for linear mappings
	allConfig, err := store.GetAllConfig(ctx)
	if err != nil {
		return config
	}

	for key, value := range allConfig {
		// Parse priority mappings: linear.priority_map.<linear_priority>
		if strings.HasPrefix(key, "linear.priority_map.") {
			linearPriority := strings.TrimPrefix(key, "linear.priority_map.")
			if beadsPriority, err := parseIntValue(value); err == nil {
				config.PriorityMap[linearPriority] = beadsPriority
			}
		}

		// Parse state mappings: linear.state_map.<state_type_or_name>
		if strings.HasPrefix(key, "linear.state_map.") {
			stateKey := strings.ToLower(strings.TrimPrefix(key, "linear.state_map."))
			config.StateMap[stateKey] = value
		}

		// Parse label-to-type mappings: linear.label_type_map.<label_name>
		if strings.HasPrefix(key, "linear.label_type_map.") {
			labelKey := strings.ToLower(strings.TrimPrefix(key, "linear.label_type_map."))
			config.LabelTypeMap[labelKey] = value
		}

		// Parse relation mappings: linear.relation_map.<relation_type>
		if strings.HasPrefix(key, "linear.relation_map.") {
			relationType := strings.TrimPrefix(key, "linear.relation_map.")
			config.RelationMap[relationType] = value
		}
	}

	return config
}

// parseIntValue safely parses an integer from a string config value.
func parseIntValue(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}

// LinearTeamStates represents workflow states for a team.
type LinearTeamStates struct {
	ID     string               `json:"id"`
	States *LinearStatesWrapper `json:"states"`
}

// LinearStatesWrapper wraps the nodes array for states.
type LinearStatesWrapper struct {
	Nodes []LinearState `json:"nodes"`
}

// LinearIssuesResponse represents the response from issues query.
type LinearIssuesResponse struct {
	Issues struct {
		Nodes    []LinearIssue `json:"nodes"`
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
	} `json:"issues"`
}

// LinearIssueCreateResponse represents the response from issueCreate mutation.
type LinearIssueCreateResponse struct {
	IssueCreate struct {
		Success bool        `json:"success"`
		Issue   LinearIssue `json:"issue"`
	} `json:"issueCreate"`
}

// LinearIssueUpdateResponse represents the response from issueUpdate mutation.
type LinearIssueUpdateResponse struct {
	IssueUpdate struct {
		Success bool        `json:"success"`
		Issue   LinearIssue `json:"issue"`
	} `json:"issueUpdate"`
}

// LinearTeamResponse represents the response from team query.
type LinearTeamResponse struct {
	Team LinearTeamStates `json:"team"`
}

// execute sends a GraphQL request to the Linear API.
// Handles rate limiting with exponential backoff.
func (c *LinearClient) execute(ctx context.Context, req *GraphQLRequest) (*GraphQLResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= linearMaxRetries; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, "POST", linearAPIEndpoint, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", c.apiKey)

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response: %w", err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			delay := linearRetryDelay * time.Duration(1<<attempt) // Exponential backoff
			lastErr = fmt.Errorf("rate limited, retrying after %v", delay)
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

		var gqlResp GraphQLResponse
		if err := json.Unmarshal(respBody, &gqlResp); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
		}

		if len(gqlResp.Errors) > 0 {
			errMsgs := make([]string, len(gqlResp.Errors))
			for i, e := range gqlResp.Errors {
				errMsgs[i] = e.Message
			}
			return nil, fmt.Errorf("GraphQL errors: %s", strings.Join(errMsgs, "; "))
		}

		return &gqlResp, nil
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// FetchIssues retrieves issues from Linear with optional filtering by state.
// state can be: "open" (unstarted/started), "closed" (completed/canceled), or "all".
func (c *LinearClient) FetchIssues(ctx context.Context, state string) ([]LinearIssue, error) {
	var allIssues []LinearIssue
	var cursor string

	query := `
		query Issues($teamId: String!, $first: Int!, $after: String, $filter: IssueFilter) {
			issues(
				first: $first
				after: $after
				filter: {
					team: { id: { eq: $teamId } }
					and: [$filter]
				}
			) {
				nodes {
					id
					identifier
					title
					description
					url
					priority
					state {
						id
						name
						type
					}
					assignee {
						id
						name
						email
						displayName
					}
					labels {
						nodes {
							id
							name
						}
					}
					parent {
						id
						identifier
					}
					relations {
						nodes {
							id
							type
							relatedIssue {
								id
								identifier
							}
						}
					}
					createdAt
					updatedAt
					completedAt
				}
				pageInfo {
					hasNextPage
					endCursor
				}
			}
		}
	`

	var filter map[string]interface{}
	switch state {
	case "open":
		filter = map[string]interface{}{
			"state": map[string]interface{}{
				"type": map[string]interface{}{
					"in": []string{"backlog", "unstarted", "started"},
				},
			},
		}
	case "closed":
		filter = map[string]interface{}{
			"state": map[string]interface{}{
				"type": map[string]interface{}{
					"in": []string{"completed", "canceled"},
				},
			},
		}
	default:
		filter = nil
	}

	for {
		variables := map[string]interface{}{
			"teamId": c.teamID,
			"first":  100, // Fetch 100 issues per page (Linear's max)
		}
		if cursor != "" {
			variables["after"] = cursor
		}
		if filter != nil {
			variables["filter"] = filter
		}

		req := &GraphQLRequest{
			Query:     query,
			Variables: variables,
		}

		resp, err := c.execute(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch issues: %w", err)
		}

		var issuesResp LinearIssuesResponse
		if err := json.Unmarshal(resp.Data, &issuesResp); err != nil {
			return nil, fmt.Errorf("failed to parse issues response: %w", err)
		}

		allIssues = append(allIssues, issuesResp.Issues.Nodes...)

		if !issuesResp.Issues.PageInfo.HasNextPage {
			break
		}
		cursor = issuesResp.Issues.PageInfo.EndCursor
	}

	return allIssues, nil
}

// FetchIssuesSince retrieves issues from Linear that have been updated since the given time.
// This enables incremental sync by only fetching issues modified after the last sync.
// The state parameter can be: "open", "closed", or "all".
func (c *LinearClient) FetchIssuesSince(ctx context.Context, state string, since time.Time) ([]LinearIssue, error) {
	var allIssues []LinearIssue
	var cursor string

	query := `
		query Issues($teamId: String!, $first: Int!, $after: String, $filter: IssueFilter) {
			issues(
				first: $first
				after: $after
				filter: {
					team: { id: { eq: $teamId } }
					and: [$filter]
				}
			) {
				nodes {
					id
					identifier
					title
					description
					url
					priority
					state {
						id
						name
						type
					}
					assignee {
						id
						name
						email
						displayName
					}
					labels {
						nodes {
							id
							name
						}
					}
					parent {
						id
						identifier
					}
					relations {
						nodes {
							id
							type
							relatedIssue {
								id
								identifier
							}
						}
					}
					createdAt
					updatedAt
					completedAt
				}
				pageInfo {
					hasNextPage
					endCursor
				}
			}
		}
	`

	// Build the filter with updatedAt constraint
	// Linear uses ISO8601 format for date comparisons
	sinceStr := since.UTC().Format(time.RFC3339)
	filter := map[string]interface{}{
		"updatedAt": map[string]interface{}{
			"gte": sinceStr,
		},
	}

	// Add state filter if specified
	switch state {
	case "open":
		filter["state"] = map[string]interface{}{
			"type": map[string]interface{}{
				"in": []string{"backlog", "unstarted", "started"},
			},
		}
	case "closed":
		filter["state"] = map[string]interface{}{
			"type": map[string]interface{}{
				"in": []string{"completed", "canceled"},
			},
		}
	}

	for {
		variables := map[string]interface{}{
			"teamId": c.teamID,
			"first":  100, // Fetch 100 issues per page (Linear's max)
			"filter": filter,
		}
		if cursor != "" {
			variables["after"] = cursor
		}

		req := &GraphQLRequest{
			Query:     query,
			Variables: variables,
		}

		resp, err := c.execute(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch issues since %s: %w", sinceStr, err)
		}

		var issuesResp LinearIssuesResponse
		if err := json.Unmarshal(resp.Data, &issuesResp); err != nil {
			return nil, fmt.Errorf("failed to parse issues response: %w", err)
		}

		allIssues = append(allIssues, issuesResp.Issues.Nodes...)

		if !issuesResp.Issues.PageInfo.HasNextPage {
			break
		}
		cursor = issuesResp.Issues.PageInfo.EndCursor
	}

	return allIssues, nil
}

// GetTeamStates fetches the workflow states for the configured team.
func (c *LinearClient) GetTeamStates(ctx context.Context) ([]LinearState, error) {
	query := `
		query TeamStates($teamId: String!) {
			team(id: $teamId) {
				id
				states {
					nodes {
						id
						name
						type
					}
				}
			}
		}
	`

	req := &GraphQLRequest{
		Query: query,
		Variables: map[string]interface{}{
			"teamId": c.teamID,
		},
	}

	resp, err := c.execute(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch team states: %w", err)
	}

	var teamResp LinearTeamResponse
	if err := json.Unmarshal(resp.Data, &teamResp); err != nil {
		return nil, fmt.Errorf("failed to parse team states response: %w", err)
	}

	if teamResp.Team.States == nil {
		return nil, fmt.Errorf("no states found for team")
	}

	return teamResp.Team.States.Nodes, nil
}

// CreateIssue creates a new issue in Linear.
func (c *LinearClient) CreateIssue(ctx context.Context, title, description string, priority int, stateID string, labelIDs []string) (*LinearIssue, error) {
	query := `
		mutation CreateIssue($input: IssueCreateInput!) {
			issueCreate(input: $input) {
				success
				issue {
					id
					identifier
					title
					description
					url
					priority
					state {
						id
						name
						type
					}
					createdAt
					updatedAt
				}
			}
		}
	`

	input := map[string]interface{}{
		"teamId":      c.teamID,
		"title":       title,
		"description": description,
	}

	if priority > 0 {
		input["priority"] = priority
	}

	if stateID != "" {
		input["stateId"] = stateID
	}

	if len(labelIDs) > 0 {
		input["labelIds"] = labelIDs
	}

	req := &GraphQLRequest{
		Query: query,
		Variables: map[string]interface{}{
			"input": input,
		},
	}

	resp, err := c.execute(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create issue: %w", err)
	}

	var createResp LinearIssueCreateResponse
	if err := json.Unmarshal(resp.Data, &createResp); err != nil {
		return nil, fmt.Errorf("failed to parse create response: %w", err)
	}

	if !createResp.IssueCreate.Success {
		return nil, fmt.Errorf("issue creation reported as unsuccessful")
	}

	return &createResp.IssueCreate.Issue, nil
}

// UpdateIssue updates an existing issue in Linear.
func (c *LinearClient) UpdateIssue(ctx context.Context, issueID string, updates map[string]interface{}) (*LinearIssue, error) {
	query := `
		mutation UpdateIssue($id: String!, $input: IssueUpdateInput!) {
			issueUpdate(id: $id, input: $input) {
				success
				issue {
					id
					identifier
					title
					description
					url
					priority
					state {
						id
						name
						type
					}
					updatedAt
				}
			}
		}
	`

	req := &GraphQLRequest{
		Query: query,
		Variables: map[string]interface{}{
			"id":    issueID,
			"input": updates,
		},
	}

	resp, err := c.execute(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to update issue: %w", err)
	}

	var updateResp LinearIssueUpdateResponse
	if err := json.Unmarshal(resp.Data, &updateResp); err != nil {
		return nil, fmt.Errorf("failed to parse update response: %w", err)
	}

	if !updateResp.IssueUpdate.Success {
		return nil, fmt.Errorf("issue update reported as unsuccessful")
	}

	return &updateResp.IssueUpdate.Issue, nil
}

// linearPriorityToBeads maps Linear priority (0-4) to Beads priority (0-4).
// Linear: 0=no priority, 1=urgent, 2=high, 3=medium, 4=low
// Beads:  0=critical, 1=high, 2=medium, 3=low, 4=backlog
// Uses configurable mapping from linear.priority_map.* config.
func linearPriorityToBeads(linearPriority int, config *LinearMappingConfig) int {
	key := fmt.Sprintf("%d", linearPriority)
	if beadsPriority, ok := config.PriorityMap[key]; ok {
		return beadsPriority
	}
	// Fallback to default mapping if not configured
	return 2 // Default to Medium
}

// beadsPriorityToLinear maps Beads priority (0-4) to Linear priority (0-4).
// Uses configurable mapping by inverting linear.priority_map.* config.
func beadsPriorityToLinear(beadsPriority int, config *LinearMappingConfig) int {
	// Build inverse map from config
	inverseMap := make(map[int]int)
	for linearKey, beadsVal := range config.PriorityMap {
		var linearVal int
		if _, err := fmt.Sscanf(linearKey, "%d", &linearVal); err == nil {
			inverseMap[beadsVal] = linearVal
		}
	}

	if linearPriority, ok := inverseMap[beadsPriority]; ok {
		return linearPriority
	}
	// Fallback to default mapping if not found
	return 3 // Default to Medium
}

// linearStateToBeadsStatus maps Linear state type to Beads status.
// Checks both state type (backlog, unstarted, etc.) and state name for custom workflows.
// Uses configurable mapping from linear.state_map.* config.
func linearStateToBeadsStatus(state *LinearState, config *LinearMappingConfig) types.Status {
	if state == nil {
		return types.StatusOpen
	}

	// First, try to match by state type (preferred)
	stateType := strings.ToLower(state.Type)
	if statusStr, ok := config.StateMap[stateType]; ok {
		return parseBeadsStatus(statusStr)
	}

	// Then try to match by state name (for custom workflow states)
	stateName := strings.ToLower(state.Name)
	if statusStr, ok := config.StateMap[stateName]; ok {
		return parseBeadsStatus(statusStr)
	}

	// Default fallback
	return types.StatusOpen
}

// parseBeadsStatus converts a status string to types.Status.
func parseBeadsStatus(s string) types.Status {
	switch strings.ToLower(s) {
	case "open":
		return types.StatusOpen
	case "in_progress", "in-progress", "inprogress":
		return types.StatusInProgress
	case "blocked":
		return types.StatusBlocked
	case "closed":
		return types.StatusClosed
	default:
		return types.StatusOpen
	}
}

// beadsStatusToLinearStateType converts Beads status to Linear state type for filtering.
// This is used when pushing issues to Linear to find the appropriate state.
func beadsStatusToLinearStateType(status types.Status) string {
	switch status {
	case types.StatusOpen:
		return "unstarted"
	case types.StatusInProgress:
		return "started"
	case types.StatusBlocked:
		return "started" // Linear doesn't have blocked state type
	case types.StatusClosed:
		return "completed"
	default:
		return "unstarted"
	}
}

// linearLabelToIssueType infers issue type from label names.
// Uses configurable mapping from linear.label_type_map.* config.
func linearLabelToIssueType(labels *LinearLabels, config *LinearMappingConfig) types.IssueType {
	if labels == nil {
		return types.TypeTask
	}

	for _, label := range labels.Nodes {
		labelName := strings.ToLower(label.Name)

		// Check exact match first
		if issueType, ok := config.LabelTypeMap[labelName]; ok {
			return parseIssueType(issueType)
		}

		// Check if label contains any mapped keyword
		for keyword, issueType := range config.LabelTypeMap {
			if strings.Contains(labelName, keyword) {
				return parseIssueType(issueType)
			}
		}
	}

	return types.TypeTask // Default
}

// parseIssueType converts an issue type string to types.IssueType.
func parseIssueType(s string) types.IssueType {
	switch strings.ToLower(s) {
	case "bug":
		return types.TypeBug
	case "feature":
		return types.TypeFeature
	case "task":
		return types.TypeTask
	case "epic":
		return types.TypeEpic
	case "chore":
		return types.TypeChore
	default:
		return types.TypeTask
	}
}

// linearRelationToBeadsDep converts a Linear relation to a Beads dependency type.
// Uses configurable mapping from linear.relation_map.* config.
func linearRelationToBeadsDep(relationType string, config *LinearMappingConfig) string {
	if depType, ok := config.RelationMap[relationType]; ok {
		return depType
	}
	return "related" // Default fallback
}

// LinearIssueConversion holds the result of converting a Linear issue to Beads.
// It includes the issue and any dependencies that should be created.
type LinearIssueConversion struct {
	Issue        *types.Issue
	Dependencies []LinearDependencyInfo
}

// LinearDependencyInfo represents a dependency to be created after issue import.
// Stored separately since we need all issues imported before linking dependencies.
type LinearDependencyInfo struct {
	FromLinearID string // Linear identifier of the dependent issue (e.g., "TEAM-123")
	ToLinearID   string // Linear identifier of the dependency target
	Type         string // Beads dependency type (blocks, related, duplicates, parent-child)
}

// linearIssueToBeads converts a Linear issue to a Beads issue.
// Uses configurable mappings loaded from beads config.
func linearIssueToBeads(ctx context.Context, li *LinearIssue) *LinearIssueConversion {
	config := loadLinearMappingConfig(ctx)

	createdAt, err := time.Parse(time.RFC3339, li.CreatedAt)
	if err != nil {
		createdAt = time.Now()
	}

	updatedAt, err := time.Parse(time.RFC3339, li.UpdatedAt)
	if err != nil {
		updatedAt = time.Now()
	}

	issue := &types.Issue{
		Title:       li.Title,
		Description: li.Description,
		Priority:    linearPriorityToBeads(li.Priority, config),
		IssueType:   linearLabelToIssueType(li.Labels, config),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}

	// Map state using configurable mapping
	issue.Status = linearStateToBeadsStatus(li.State, config)

	if li.CompletedAt != "" {
		completedAt, err := time.Parse(time.RFC3339, li.CompletedAt)
		if err == nil {
			issue.ClosedAt = &completedAt
		}
	}

	if li.Assignee != nil {
		if li.Assignee.Email != "" {
			issue.Assignee = li.Assignee.Email
		} else {
			issue.Assignee = li.Assignee.Name
		}
	}

	// Copy labels (bidirectional sync preserves all labels)
	if li.Labels != nil {
		for _, label := range li.Labels.Nodes {
			issue.Labels = append(issue.Labels, label.Name)
		}
	}

	externalRef := li.URL
	issue.ExternalRef = &externalRef

	// Collect dependencies to be created after all issues are imported
	var deps []LinearDependencyInfo

	// Map parent-child relationship
	if li.Parent != nil {
		deps = append(deps, LinearDependencyInfo{
			FromLinearID: li.Identifier,
			ToLinearID:   li.Parent.Identifier,
			Type:         "parent-child",
		})
	}

	// Map relations to dependencies
	if li.Relations != nil {
		for _, rel := range li.Relations.Nodes {
			depType := linearRelationToBeadsDep(rel.Type, config)

			// For "blockedBy", we invert the direction since the related issue blocks this one
			if rel.Type == "blockedBy" {
				deps = append(deps, LinearDependencyInfo{
					FromLinearID: li.Identifier,
					ToLinearID:   rel.RelatedIssue.Identifier,
					Type:         depType,
				})
			} else {
				// For blocks, duplicate, related - this issue is the source
				deps = append(deps, LinearDependencyInfo{
					FromLinearID: rel.RelatedIssue.Identifier,
					ToLinearID:   li.Identifier,
					Type:         depType,
				})
			}
		}
	}

	return &LinearIssueConversion{
		Issue:        issue,
		Dependencies: deps,
	}
}

// linearStateCache caches workflow states for the team to avoid repeated API calls.
type linearStateCache struct {
	states      []LinearState
	statesByID  map[string]LinearState
	openStateID string // First "unstarted" or "backlog" state
}

// buildStateCache fetches and caches team states.
func buildStateCache(ctx context.Context, client *LinearClient) (*linearStateCache, error) {
	states, err := client.GetTeamStates(ctx)
	if err != nil {
		return nil, err
	}

	cache := &linearStateCache{
		states:     states,
		statesByID: make(map[string]LinearState),
	}

	for _, s := range states {
		cache.statesByID[s.ID] = s
		if cache.openStateID == "" && (s.Type == "unstarted" || s.Type == "backlog") {
			cache.openStateID = s.ID
		}
	}

	return cache, nil
}

// findStateForBeadsStatus returns the best Linear state ID for a Beads status.
func (sc *linearStateCache) findStateForBeadsStatus(status types.Status) string {
	targetType := ""
	switch status {
	case types.StatusOpen:
		targetType = "unstarted"
	case types.StatusInProgress:
		targetType = "started"
	case types.StatusBlocked:
		targetType = "started"
	case types.StatusClosed:
		targetType = "completed"
	default:
		targetType = "unstarted"
	}

	for _, s := range sc.states {
		if s.Type == targetType {
			return s.ID
		}
	}

	if len(sc.states) > 0 {
		return sc.states[0].ID
	}

	return ""
}

// getLinearClient creates a configured Linear client from beads config.
func getLinearClient(ctx context.Context) (*LinearClient, error) {
	apiKey, _ := store.GetConfig(ctx, "linear.api_key")
	if apiKey == "" {
		apiKey = os.Getenv("LINEAR_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("Linear API key not configured")
	}

	teamID, _ := store.GetConfig(ctx, "linear.team_id")
	if teamID == "" {
		return nil, fmt.Errorf("Linear team ID not configured")
	}

	return NewLinearClient(apiKey, teamID), nil
}

// LinearSyncStats tracks statistics for a Linear sync operation.
type LinearSyncStats struct {
	Pulled    int `json:"pulled"`
	Pushed    int `json:"pushed"`
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Skipped   int `json:"skipped"`
	Errors    int `json:"errors"`
	Conflicts int `json:"conflicts"`
}

// LinearSyncResult represents the result of a Linear sync operation.
type LinearSyncResult struct {
	Success  bool            `json:"success"`
	Stats    LinearSyncStats `json:"stats"`
	LastSync string          `json:"last_sync,omitempty"`
	Error    string          `json:"error,omitempty"`
	Warnings []string        `json:"warnings,omitempty"`
}

var linearCmd = &cobra.Command{
	Use:   "linear",
	Short: "Linear integration commands",
	Long: `Synchronize issues between beads and Linear.

Configuration:
  bd config set linear.api_key "YOUR_API_KEY"
  bd config set linear.team_id "TEAM_ID"

Environment variables (alternative to config):
  LINEAR_API_KEY - Linear API key

Data Mapping (optional, sensible defaults provided):
  Priority mapping (Linear 0-4 to Beads 0-4):
    bd config set linear.priority_map.0 4    # No priority -> Backlog
    bd config set linear.priority_map.1 0    # Urgent -> Critical
    bd config set linear.priority_map.2 1    # High -> High
    bd config set linear.priority_map.3 2    # Medium -> Medium
    bd config set linear.priority_map.4 3    # Low -> Low

  State mapping (Linear state type to Beads status):
    bd config set linear.state_map.backlog open
    bd config set linear.state_map.unstarted open
    bd config set linear.state_map.started in_progress
    bd config set linear.state_map.completed closed
    bd config set linear.state_map.canceled closed
    bd config set linear.state_map.my_custom_state in_progress  # Custom state names

  Label to issue type mapping:
    bd config set linear.label_type_map.bug bug
    bd config set linear.label_type_map.feature feature
    bd config set linear.label_type_map.epic epic

  Relation type mapping (Linear relations to Beads dependencies):
    bd config set linear.relation_map.blocks blocks
    bd config set linear.relation_map.blockedBy blocks
    bd config set linear.relation_map.duplicate duplicates
    bd config set linear.relation_map.related related

Examples:
  bd linear sync --pull         # Import issues from Linear
  bd linear sync --push         # Export issues to Linear
  bd linear sync                # Bidirectional sync (pull then push)
  bd linear sync --dry-run      # Preview sync without changes
  bd linear status              # Show sync status`,
}

var linearSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronize issues with Linear",
	Long: `Synchronize issues between beads and Linear.

Modes:
  --pull         Import issues from Linear into beads
  --push         Export issues from beads to Linear
  (no flags)     Bidirectional sync: pull then push, with conflict resolution

Conflict Resolution:
  By default, newer timestamp wins. Override with:
  --prefer-local    Always prefer local beads version
  --prefer-linear   Always prefer Linear version

Examples:
  bd linear sync --pull                # Import from Linear
  bd linear sync --push --create-only  # Push new issues only
  bd linear sync --dry-run             # Preview without changes
  bd linear sync --prefer-local        # Bidirectional, local wins`,
	Run: func(cmd *cobra.Command, args []string) {
		// Parse flags (errors are unlikely but check to ensure cobra is working)
		pull, _ := cmd.Flags().GetBool("pull")
		push, _ := cmd.Flags().GetBool("push")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		preferLocal, _ := cmd.Flags().GetBool("prefer-local")
		preferLinear, _ := cmd.Flags().GetBool("prefer-linear")
		createOnly, _ := cmd.Flags().GetBool("create-only")
		updateRefs, _ := cmd.Flags().GetBool("update-refs")
		state, _ := cmd.Flags().GetString("state")

		if !dryRun {
			CheckReadonly("linear sync")
		}

		if preferLocal && preferLinear {
			fmt.Fprintf(os.Stderr, "Error: cannot use both --prefer-local and --prefer-linear\n")
			os.Exit(1)
		}

		if err := ensureStoreActive(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: database not available: %v\n", err)
			os.Exit(1)
		}

		if err := validateLinearConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if !pull && !push {
			pull = true
			push = true
		}

		ctx := rootCtx
		result := &LinearSyncResult{Success: true}

		if pull {
			if dryRun {
				fmt.Println("→ [DRY RUN] Would pull issues from Linear")
			} else {
				fmt.Println("→ Pulling issues from Linear...")
			}

			pullStats, err := doPullFromLinear(ctx, dryRun, state)
			if err != nil {
				result.Success = false
				result.Error = err.Error()
				if jsonOutput {
					outputJSON(result)
				} else {
					fmt.Fprintf(os.Stderr, "Error pulling from Linear: %v\n", err)
				}
				os.Exit(1)
			}

			result.Stats.Pulled = pullStats.Created + pullStats.Updated
			result.Stats.Created += pullStats.Created
			result.Stats.Updated += pullStats.Updated
			result.Stats.Skipped += pullStats.Skipped

			if !dryRun {
				fmt.Printf("✓ Pulled %d issues (%d created, %d updated)\n",
					result.Stats.Pulled, pullStats.Created, pullStats.Updated)
			}
		}

		if pull && push && !dryRun {
			conflicts, err := detectLinearConflicts(ctx)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("conflict detection failed: %v", err))
			} else if len(conflicts) > 0 {
				result.Stats.Conflicts = len(conflicts)
				if preferLocal {
					fmt.Printf("→ Resolving %d conflicts (preferring local)\n", len(conflicts))
					// Local wins - no action needed, push will overwrite
				} else if preferLinear {
					fmt.Printf("→ Resolving %d conflicts (preferring Linear)\n", len(conflicts))
					// Linear wins - re-import conflicting issues
					if err := reimportLinearConflicts(ctx, conflicts); err != nil {
						result.Warnings = append(result.Warnings, fmt.Sprintf("conflict resolution failed: %v", err))
					}
				} else {
					// Default: timestamp-based (newer wins)
					fmt.Printf("→ Resolving %d conflicts (newer wins)\n", len(conflicts))
					if err := resolveLinearConflictsByTimestamp(ctx, conflicts); err != nil {
						result.Warnings = append(result.Warnings, fmt.Sprintf("conflict resolution failed: %v", err))
					}
				}
			}
		}

		if push {
			if dryRun {
				fmt.Println("→ [DRY RUN] Would push issues to Linear")
			} else {
				fmt.Println("→ Pushing issues to Linear...")
			}

			pushStats, err := doPushToLinear(ctx, dryRun, createOnly, updateRefs)
			if err != nil {
				result.Success = false
				result.Error = err.Error()
				if jsonOutput {
					outputJSON(result)
				} else {
					fmt.Fprintf(os.Stderr, "Error pushing to Linear: %v\n", err)
				}
				os.Exit(1)
			}

			result.Stats.Pushed = pushStats.Created + pushStats.Updated
			result.Stats.Created += pushStats.Created
			result.Stats.Updated += pushStats.Updated
			result.Stats.Skipped += pushStats.Skipped
			result.Stats.Errors += pushStats.Errors

			if !dryRun {
				fmt.Printf("✓ Pushed %d issues (%d created, %d updated)\n",
					result.Stats.Pushed, pushStats.Created, pushStats.Updated)
			}
		}

		if !dryRun && result.Success {
			result.LastSync = time.Now().Format(time.RFC3339)
			if err := store.SetConfig(ctx, "linear.last_sync", result.LastSync); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("failed to update last_sync: %v", err))
			}
		}

		if jsonOutput {
			outputJSON(result)
		} else if dryRun {
			fmt.Println("\n✓ Dry run complete (no changes made)")
		} else {
			fmt.Println("\n✓ Linear sync complete")
			if len(result.Warnings) > 0 {
				fmt.Println("\nWarnings:")
				for _, w := range result.Warnings {
					fmt.Printf("  - %s\n", w)
				}
			}
		}
	},
}

var linearStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Linear sync status",
	Long: `Show the current Linear sync status, including:
  - Last sync timestamp
  - Configuration status
  - Number of issues with Linear links
  - Issues pending push (no external_ref)`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		if err := ensureStoreActive(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		apiKey, _ := store.GetConfig(ctx, "linear.api_key")
		teamID, _ := store.GetConfig(ctx, "linear.team_id")
		lastSync, _ := store.GetConfig(ctx, "linear.last_sync")

		if apiKey == "" {
			apiKey = os.Getenv("LINEAR_API_KEY")
		}

		configured := apiKey != "" && teamID != ""

		allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		withLinearRef := 0
		pendingPush := 0
		for _, issue := range allIssues {
			if issue.ExternalRef != nil && isLinearExternalRef(*issue.ExternalRef) {
				withLinearRef++
			} else if issue.ExternalRef == nil {
				pendingPush++
			}
		}

		if jsonOutput {
			hasAPIKey := apiKey != ""
			outputJSON(map[string]interface{}{
				"configured":      configured,
				"has_api_key":     hasAPIKey,
				"team_id":         teamID,
				"last_sync":       lastSync,
				"total_issues":    len(allIssues),
				"with_linear_ref": withLinearRef,
				"pending_push":    pendingPush,
			})
			return
		}

		fmt.Println("Linear Sync Status")
		fmt.Println("==================")
		fmt.Println()

		if !configured {
			fmt.Println("Status: Not configured")
			fmt.Println()
			fmt.Println("To configure Linear integration:")
			fmt.Println("  bd config set linear.api_key \"YOUR_API_KEY\"")
			fmt.Println("  bd config set linear.team_id \"TEAM_ID\"")
			fmt.Println()
			fmt.Println("Or use environment variables:")
			fmt.Println("  export LINEAR_API_KEY=\"YOUR_API_KEY\"")
			return
		}

		fmt.Printf("Team ID:      %s\n", teamID)
		fmt.Printf("API Key:      %s\n", maskAPIKey(apiKey))
		if lastSync != "" {
			fmt.Printf("Last Sync:    %s\n", lastSync)
		} else {
			fmt.Println("Last Sync:    Never")
		}
		fmt.Println()
		fmt.Printf("Total Issues: %d\n", len(allIssues))
		fmt.Printf("With Linear:  %d\n", withLinearRef)
		fmt.Printf("Local Only:   %d\n", pendingPush)

		if pendingPush > 0 {
			fmt.Println()
			fmt.Printf("Run 'bd linear sync --push' to push %d local issue(s) to Linear\n", pendingPush)
		}
	},
}

func init() {
	linearSyncCmd.Flags().Bool("pull", false, "Pull issues from Linear")
	linearSyncCmd.Flags().Bool("push", false, "Push issues to Linear")
	linearSyncCmd.Flags().Bool("dry-run", false, "Preview sync without making changes")
	linearSyncCmd.Flags().Bool("prefer-local", false, "Prefer local version on conflicts")
	linearSyncCmd.Flags().Bool("prefer-linear", false, "Prefer Linear version on conflicts")
	linearSyncCmd.Flags().Bool("create-only", false, "Only create new issues, don't update existing")
	linearSyncCmd.Flags().Bool("update-refs", true, "Update external_ref after creating Linear issues")
	linearSyncCmd.Flags().String("state", "all", "Issue state to sync: open, closed, all")

	linearCmd.AddCommand(linearSyncCmd)
	linearCmd.AddCommand(linearStatusCmd)
	rootCmd.AddCommand(linearCmd)
}

// validateLinearConfig checks that required Linear configuration is present.
func validateLinearConfig() error {
	if err := ensureStoreActive(); err != nil {
		return fmt.Errorf("database not available: %w", err)
	}

	ctx := rootCtx

	apiKey, _ := store.GetConfig(ctx, "linear.api_key")
	if apiKey == "" && os.Getenv("LINEAR_API_KEY") == "" {
		return fmt.Errorf("Linear API key not configured\nRun: bd config set linear.api_key \"YOUR_API_KEY\"\nOr: export LINEAR_API_KEY=YOUR_API_KEY")
	}

	teamID, _ := store.GetConfig(ctx, "linear.team_id")
	if teamID == "" {
		return fmt.Errorf("linear.team_id not configured\nRun: bd config set linear.team_id \"TEAM_ID\"")
	}

	return nil
}

// maskAPIKey returns a masked version of an API key for display.
// Shows first 4 and last 4 characters, with dots in between.
func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// isLinearExternalRef checks if an external_ref URL is a Linear issue URL.
func isLinearExternalRef(externalRef string) bool {
	return strings.Contains(externalRef, "linear.app/") && strings.Contains(externalRef, "/issue/")
}

// LinearConflict represents a conflict between local and Linear versions.
// A conflict occurs when both the local and Linear versions have been modified
// since the last sync.
type LinearConflict struct {
	IssueID           string    // Beads issue ID
	LocalUpdated      time.Time // When the local version was last modified
	LinearUpdated     time.Time // When the Linear version was last modified
	LinearExternalRef string    // URL to the Linear issue
	LinearIdentifier  string    // Linear issue identifier (e.g., "TEAM-123")
	LinearInternalID  string    // Linear's internal UUID (for API updates)
}

// detectLinearConflicts finds issues that have been modified both locally and in Linear
// since the last sync. This is a more expensive operation as it fetches individual
// issue timestamps from Linear.
func detectLinearConflicts(ctx context.Context) ([]LinearConflict, error) {
	lastSyncStr, _ := store.GetConfig(ctx, "linear.last_sync")
	if lastSyncStr == "" {
		// No previous sync - no conflicts possible
		return nil, nil
	}

	lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
	if err != nil {
		return nil, fmt.Errorf("invalid last_sync timestamp: %w", err)
	}

	// Get Linear client for fetching remote timestamps
	client, err := getLinearClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create Linear client: %w", err)
	}

	// Get all local issues with Linear external refs
	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return nil, err
	}

	var conflicts []LinearConflict

	for _, issue := range allIssues {
		if issue.ExternalRef == nil || !isLinearExternalRef(*issue.ExternalRef) {
			continue
		}

		// Only check issues that have been modified locally since last sync
		if !issue.UpdatedAt.After(lastSync) {
			continue
		}

		// Extract Linear identifier from external_ref URL
		linearIdentifier := extractLinearIdentifier(*issue.ExternalRef)
		if linearIdentifier == "" {
			continue
		}

		// Fetch the Linear issue to get its current UpdatedAt timestamp
		linearIssue, err := client.FetchIssueByIdentifier(ctx, linearIdentifier)
		if err != nil {
			// Log warning but continue checking other issues
			fmt.Fprintf(os.Stderr, "Warning: failed to fetch Linear issue %s for conflict check: %v\n",
				linearIdentifier, err)
			continue
		}
		if linearIssue == nil {
			// Issue doesn't exist in Linear anymore - not a conflict
			continue
		}

		// Parse Linear's UpdatedAt timestamp
		linearUpdatedAt, err := time.Parse(time.RFC3339, linearIssue.UpdatedAt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse Linear UpdatedAt for %s: %v\n",
				linearIdentifier, err)
			continue
		}

		// Check if Linear was also modified since last sync (true conflict)
		if linearUpdatedAt.After(lastSync) {
			conflicts = append(conflicts, LinearConflict{
				IssueID:           issue.ID,
				LocalUpdated:      issue.UpdatedAt,
				LinearUpdated:     linearUpdatedAt,
				LinearExternalRef: *issue.ExternalRef,
				LinearIdentifier:  linearIdentifier,
				LinearInternalID:  linearIssue.ID,
			})
		}
	}

	return conflicts, nil
}

// reimportLinearConflicts re-imports conflicting issues from Linear (Linear wins).
// For each conflict, fetches the current state from Linear and updates the local copy.
func reimportLinearConflicts(ctx context.Context, conflicts []LinearConflict) error {
	if len(conflicts) == 0 {
		return nil
	}

	client, err := getLinearClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Linear client: %w", err)
	}

	config := loadLinearMappingConfig(ctx)
	resolved := 0
	failed := 0

	for _, conflict := range conflicts {
		// Fetch the current state of the Linear issue
		linearIssue, err := client.FetchIssueByIdentifier(ctx, conflict.LinearIdentifier)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: failed to fetch %s for resolution: %v\n",
				conflict.LinearIdentifier, err)
			failed++
			continue
		}
		if linearIssue == nil {
			fmt.Fprintf(os.Stderr, "  Warning: Linear issue %s not found, skipping\n",
				conflict.LinearIdentifier)
			failed++
			continue
		}

		// Convert Linear issue to updates for the local issue
		updates := buildLinearToLocalUpdates(linearIssue, config)

		// Apply updates to the local issue
		err = store.UpdateIssue(ctx, conflict.IssueID, updates, actor)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: failed to update local issue %s: %v\n",
				conflict.IssueID, err)
			failed++
			continue
		}

		fmt.Printf("  Resolved: %s <- %s (Linear wins)\n", conflict.IssueID, conflict.LinearIdentifier)
		resolved++
	}

	if failed > 0 {
		return fmt.Errorf("%d conflict(s) failed to resolve", failed)
	}

	fmt.Printf("  Resolved %d conflict(s) by keeping Linear version\n", resolved)
	return nil
}

// buildLinearToLocalUpdates creates an updates map from a Linear issue
// to apply to a local Beads issue. This is used when Linear wins a conflict.
func buildLinearToLocalUpdates(li *LinearIssue, config *LinearMappingConfig) map[string]interface{} {
	updates := make(map[string]interface{})

	// Update title
	updates["title"] = li.Title

	// Update description
	updates["description"] = li.Description

	// Update priority using configured mapping
	updates["priority"] = linearPriorityToBeads(li.Priority, config)

	// Update status using configured mapping
	updates["status"] = string(linearStateToBeadsStatus(li.State, config))

	// Update assignee if present
	if li.Assignee != nil {
		if li.Assignee.Email != "" {
			updates["assignee"] = li.Assignee.Email
		} else {
			updates["assignee"] = li.Assignee.Name
		}
	} else {
		updates["assignee"] = ""
	}

	// Update labels from Linear
	if li.Labels != nil {
		var labels []string
		for _, label := range li.Labels.Nodes {
			labels = append(labels, label.Name)
		}
		updates["labels"] = labels
	}

	// Update timestamps
	if li.UpdatedAt != "" {
		if updatedAt, err := time.Parse(time.RFC3339, li.UpdatedAt); err == nil {
			updates["updated_at"] = updatedAt
		}
	}

	// Handle closed state
	if li.CompletedAt != "" {
		if closedAt, err := time.Parse(time.RFC3339, li.CompletedAt); err == nil {
			updates["closed_at"] = closedAt
		}
	}

	return updates
}

// resolveLinearConflictsByTimestamp resolves conflicts by keeping the newer version.
// For each conflict, compares local and Linear UpdatedAt timestamps.
// If Linear is newer, re-imports from Linear. If local is newer, push will overwrite.
func resolveLinearConflictsByTimestamp(ctx context.Context, conflicts []LinearConflict) error {
	if len(conflicts) == 0 {
		return nil
	}

	// Separate conflicts into "Linear wins" vs "Local wins" based on timestamps
	var linearWins []LinearConflict
	var localWins []LinearConflict

	for _, conflict := range conflicts {
		// Compare timestamps: use the newer one
		if conflict.LinearUpdated.After(conflict.LocalUpdated) {
			linearWins = append(linearWins, conflict)
		} else {
			localWins = append(localWins, conflict)
		}
	}

	// Report what we're doing
	if len(linearWins) > 0 {
		fmt.Printf("  %d conflict(s): Linear is newer, will re-import\n", len(linearWins))
	}
	if len(localWins) > 0 {
		fmt.Printf("  %d conflict(s): Local is newer, will push to Linear\n", len(localWins))
	}

	// For conflicts where Linear wins, re-import from Linear
	if len(linearWins) > 0 {
		err := reimportLinearConflicts(ctx, linearWins)
		if err != nil {
			return fmt.Errorf("failed to re-import Linear-wins conflicts: %w", err)
		}
	}

	// For conflicts where local wins, we mark them to be skipped during push check
	// The push phase will naturally handle these since local timestamps are newer
	// We need to track these so push doesn't skip them due to conflict detection
	if len(localWins) > 0 {
		// Store the resolved conflict IDs so push knows to proceed
		// We use a simple in-memory approach since conflicts are processed in same sync
		for _, conflict := range localWins {
			fmt.Printf("  Resolved: %s -> %s (local wins, will push)\n",
				conflict.IssueID, conflict.LinearIdentifier)
		}
	}

	return nil
}

// LinearPullStats tracks pull operation statistics.
type LinearPullStats struct {
	Created     int
	Updated     int
	Skipped     int
	Incremental bool   // Whether this was an incremental sync
	SyncedSince string // Timestamp we synced since (if incremental)
}

// doPullFromLinear imports issues from Linear using the GraphQL API.
// Supports incremental sync by checking linear.last_sync config and only fetching
// issues updated since that timestamp.
func doPullFromLinear(ctx context.Context, dryRun bool, state string) (*LinearPullStats, error) {
	stats := &LinearPullStats{}

	client, err := getLinearClient(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to create Linear client: %w", err)
	}

	// Check for last sync timestamp to enable incremental sync
	var linearIssues []LinearIssue
	lastSyncStr, _ := store.GetConfig(ctx, "linear.last_sync")

	if lastSyncStr != "" {
		// Parse the last sync timestamp
		lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
		if err != nil {
			// Invalid timestamp - fall back to full sync
			fmt.Fprintf(os.Stderr, "Warning: invalid linear.last_sync timestamp, doing full sync\n")
			linearIssues, err = client.FetchIssues(ctx, state)
			if err != nil {
				return stats, fmt.Errorf("failed to fetch issues from Linear: %w", err)
			}
		} else {
			// Incremental sync: fetch only issues updated since last sync
			stats.Incremental = true
			stats.SyncedSince = lastSyncStr
			linearIssues, err = client.FetchIssuesSince(ctx, state, lastSync)
			if err != nil {
				return stats, fmt.Errorf("failed to fetch issues from Linear (incremental): %w", err)
			}
			if !dryRun {
				fmt.Printf("  Incremental sync since %s\n", lastSync.Format("2006-01-02 15:04:05"))
			}
		}
	} else {
		// No last sync - do a full sync
		linearIssues, err = client.FetchIssues(ctx, state)
		if err != nil {
			return stats, fmt.Errorf("failed to fetch issues from Linear: %w", err)
		}
		if !dryRun {
			fmt.Println("  Full sync (no previous sync timestamp)")
		}
	}

	if dryRun {
		if stats.Incremental {
			fmt.Printf("  Would import %d issues from Linear (incremental since %s)\n", len(linearIssues), stats.SyncedSince)
		} else {
			fmt.Printf("  Would import %d issues from Linear (full sync)\n", len(linearIssues))
		}
		return stats, nil
	}

	// Convert all Linear issues and collect dependency information
	var beadsIssues []*types.Issue
	var allDeps []LinearDependencyInfo
	linearIDToBeadsID := make(map[string]string) // Maps Linear identifier to Beads ID

	for _, li := range linearIssues {
		conversion := linearIssueToBeads(ctx, &li)
		beadsIssues = append(beadsIssues, conversion.Issue)
		allDeps = append(allDeps, conversion.Dependencies...)

		// We'll populate linearIDToBeadsID after import when we have the actual IDs
	}

	if len(beadsIssues) == 0 {
		fmt.Println("  No issues to import")
		return stats, nil
	}

	opts := ImportOptions{
		DryRun:     false,
		SkipUpdate: false,
	}

	result, err := importIssuesCore(ctx, dbPath, store, beadsIssues, opts)
	if err != nil {
		return stats, fmt.Errorf("import failed: %w", err)
	}

	stats.Created = result.Created
	stats.Updated = result.Updated
	stats.Skipped = result.Skipped

	// Build mapping from Linear identifier to Beads ID using external_ref
	// After import, re-fetch all issues to get the mapping
	allBeadsIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to fetch issues for dependency mapping: %v\n", err)
		return stats, nil
	}

	for _, issue := range allBeadsIssues {
		if issue.ExternalRef != nil && isLinearExternalRef(*issue.ExternalRef) {
			// Extract Linear identifier from URL
			linearID := extractLinearIdentifier(*issue.ExternalRef)
			if linearID != "" {
				linearIDToBeadsID[linearID] = issue.ID
			}
		}
	}

	// Create dependencies between imported issues
	depsCreated := 0
	for _, dep := range allDeps {
		fromID, fromOK := linearIDToBeadsID[dep.FromLinearID]
		toID, toOK := linearIDToBeadsID[dep.ToLinearID]

		if !fromOK || !toOK {
			// One or both issues not found - skip silently (may be in different team/project)
			continue
		}

		// Create the dependency using types.Dependency
		dependency := &types.Dependency{
			IssueID:     fromID,
			DependsOnID: toID,
			Type:        types.DependencyType(dep.Type),
			CreatedAt:   time.Now(),
		}
		err := store.AddDependency(ctx, dependency, actor)
		if err != nil {
			// Dependency might already exist, that's OK
			if !strings.Contains(err.Error(), "already exists") &&
				!strings.Contains(err.Error(), "duplicate") {
				fmt.Fprintf(os.Stderr, "Warning: failed to create dependency %s -> %s (%s): %v\n",
					fromID, toID, dep.Type, err)
			}
		} else {
			depsCreated++
		}
	}

	if depsCreated > 0 {
		fmt.Printf("  Created %d dependencies from Linear relations\n", depsCreated)
	}

	return stats, nil
}

// extractLinearIdentifier extracts the Linear issue identifier (e.g., "TEAM-123") from a Linear URL.
func extractLinearIdentifier(url string) string {
	// Linear URLs look like: https://linear.app/team/issue/TEAM-123/title
	// We want to extract "TEAM-123"
	parts := strings.Split(url, "/")
	for i, part := range parts {
		if part == "issue" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// FetchIssueByIdentifier retrieves a single issue from Linear by its identifier (e.g., "TEAM-123").
// Returns nil if the issue is not found.
func (c *LinearClient) FetchIssueByIdentifier(ctx context.Context, identifier string) (*LinearIssue, error) {
	query := `
		query IssueByIdentifier($filter: IssueFilter!) {
			issues(filter: $filter, first: 1) {
				nodes {
					id
					identifier
					title
					description
					url
					priority
					state {
						id
						name
						type
					}
					assignee {
						id
						name
						email
						displayName
					}
					labels {
						nodes {
							id
							name
						}
					}
					createdAt
					updatedAt
					completedAt
				}
			}
		}
	`

	// Build filter to search by identifier number and team prefix
	// Linear identifiers look like "TEAM-123", we filter by number
	// and validate the full identifier in the results
	variables := map[string]interface{}{
		"filter": map[string]interface{}{
			"team": map[string]interface{}{
				"id": map[string]interface{}{
					"eq": c.teamID,
				},
			},
		},
	}

	// Extract the issue number from identifier (e.g., "123" from "TEAM-123")
	parts := strings.Split(identifier, "-")
	if len(parts) >= 2 {
		// Add number filter for more precise matching
		variables["filter"].(map[string]interface{})["number"] = map[string]interface{}{
			"eq": parts[len(parts)-1],
		}
	}

	req := &GraphQLRequest{
		Query:     query,
		Variables: variables,
	}

	resp, err := c.execute(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch issue by identifier: %w", err)
	}

	var issuesResp LinearIssuesResponse
	if err := json.Unmarshal(resp.Data, &issuesResp); err != nil {
		return nil, fmt.Errorf("failed to parse issues response: %w", err)
	}

	// Find the exact match by identifier (in case of partial matches)
	for _, issue := range issuesResp.Issues.Nodes {
		if issue.Identifier == identifier {
			return &issue, nil
		}
	}

	return nil, nil // Issue not found
}

// LinearPushStats tracks push operation statistics.
type LinearPushStats struct {
	Created int
	Updated int
	Skipped int
	Errors  int
}

// doPushToLinear exports issues to Linear using the GraphQL API.
func doPushToLinear(ctx context.Context, dryRun bool, createOnly bool, updateRefs bool) (*LinearPushStats, error) {
	stats := &LinearPushStats{}

	client, err := getLinearClient(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to create Linear client: %w", err)
	}

	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return stats, fmt.Errorf("failed to get local issues: %w", err)
	}

	var toCreate []*types.Issue
	var toUpdate []*types.Issue

	for _, issue := range allIssues {
		if issue.IsTombstone() {
			continue
		}

		if issue.ExternalRef != nil && isLinearExternalRef(*issue.ExternalRef) {
			if !createOnly {
				toUpdate = append(toUpdate, issue)
			}
		} else if issue.ExternalRef == nil {
			toCreate = append(toCreate, issue)
		}
	}

	if dryRun {
		fmt.Printf("  Would create %d issues in Linear\n", len(toCreate))
		if !createOnly {
			fmt.Printf("  Would update %d issues in Linear\n", len(toUpdate))
		}
		return stats, nil
	}

	stateCache, err := buildStateCache(ctx, client)
	if err != nil {
		return stats, fmt.Errorf("failed to fetch team states: %w", err)
	}

	// Load mapping configuration for priority conversion
	mappingConfig := loadLinearMappingConfig(ctx)

	for _, issue := range toCreate {
		linearPriority := beadsPriorityToLinear(issue.Priority, mappingConfig)
		stateID := stateCache.findStateForBeadsStatus(issue.Status)

		description := issue.Description
		if issue.AcceptanceCriteria != "" {
			description += "\n\n## Acceptance Criteria\n" + issue.AcceptanceCriteria
		}
		if issue.Design != "" {
			description += "\n\n## Design\n" + issue.Design
		}
		if issue.Notes != "" {
			description += "\n\n## Notes\n" + issue.Notes
		}

		linearIssue, err := client.CreateIssue(ctx, issue.Title, description, linearPriority, stateID, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create issue '%s' in Linear: %v\n", issue.Title, err)
			stats.Errors++
			continue
		}

		stats.Created++
		fmt.Printf("  Created: %s -> %s\n", issue.ID, linearIssue.Identifier)

		if updateRefs && linearIssue.URL != "" {
			updates := map[string]interface{}{
				"external_ref": linearIssue.URL,
			}
			if err := store.UpdateIssue(ctx, issue.ID, updates, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update external_ref for %s: %v\n", issue.ID, err)
				stats.Errors++
			}
		}
	}

	// Process updates for existing Linear issues
	if len(toUpdate) > 0 && !createOnly {
		for _, issue := range toUpdate {
			// Extract Linear identifier from external_ref URL
			linearIdentifier := extractLinearIdentifier(*issue.ExternalRef)
			if linearIdentifier == "" {
				fmt.Fprintf(os.Stderr, "Warning: could not extract Linear identifier from %s: %s\n",
					issue.ID, *issue.ExternalRef)
				stats.Errors++
				continue
			}

			// Fetch the Linear issue to get its internal ID and UpdatedAt timestamp
			linearIssue, err := client.FetchIssueByIdentifier(ctx, linearIdentifier)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to fetch Linear issue %s: %v\n",
					linearIdentifier, err)
				stats.Errors++
				continue
			}
			if linearIssue == nil {
				fmt.Fprintf(os.Stderr, "Warning: Linear issue %s not found (may have been deleted)\n",
					linearIdentifier)
				stats.Skipped++
				continue
			}

			// Parse Linear's UpdatedAt timestamp (RFC3339 format)
			linearUpdatedAt, err := time.Parse(time.RFC3339, linearIssue.UpdatedAt)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to parse Linear UpdatedAt for %s: %v\n",
					linearIdentifier, err)
				stats.Errors++
				continue
			}

			// Compare timestamps: only update if local is newer
			if !issue.UpdatedAt.After(linearUpdatedAt) {
				// Linear is newer or same, skip update
				stats.Skipped++
				continue
			}

			// Build description from all beads text fields
			description := issue.Description
			if issue.AcceptanceCriteria != "" {
				description += "\n\n## Acceptance Criteria\n" + issue.AcceptanceCriteria
			}
			if issue.Design != "" {
				description += "\n\n## Design\n" + issue.Design
			}
			if issue.Notes != "" {
				description += "\n\n## Notes\n" + issue.Notes
			}

			// Build update payload
			updatePayload := map[string]interface{}{
				"title":       issue.Title,
				"description": description,
			}

			// Add priority if set
			linearPriority := beadsPriorityToLinear(issue.Priority, mappingConfig)
			if linearPriority > 0 {
				updatePayload["priority"] = linearPriority
			}

			// Add state if we can map it
			stateID := stateCache.findStateForBeadsStatus(issue.Status)
			if stateID != "" {
				updatePayload["stateId"] = stateID
			}

			// Perform the update using Linear's internal issue ID
			updatedLinearIssue, err := client.UpdateIssue(ctx, linearIssue.ID, updatePayload)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update Linear issue %s: %v\n",
					linearIdentifier, err)
				stats.Errors++
				continue
			}

			stats.Updated++
			fmt.Printf("  Updated: %s -> %s\n", issue.ID, updatedLinearIssue.Identifier)
		}
	}

	return stats, nil
}
