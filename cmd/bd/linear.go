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
	ID          string        `json:"id"`
	Identifier  string        `json:"identifier"` // e.g., "TEAM-123"
	Title       string        `json:"title"`
	Description string        `json:"description"`
	URL         string        `json:"url"`
	Priority    int           `json:"priority"` // 0=no priority, 1=urgent, 2=high, 3=medium, 4=low
	State       *LinearState  `json:"state"`
	Assignee    *LinearUser   `json:"assignee"`
	Labels      *LinearLabels `json:"labels"`
	CreatedAt   string        `json:"createdAt"`
	UpdatedAt   string        `json:"updatedAt"`
	CompletedAt string        `json:"completedAt,omitempty"`
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
		resp.Body.Close()
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
func linearPriorityToBeads(linearPriority int) int {
	switch linearPriority {
	case 0:
		return 4 // No priority -> Backlog
	case 1:
		return 0 // Urgent -> Critical
	case 2:
		return 1 // High -> High
	case 3:
		return 2 // Medium -> Medium
	case 4:
		return 3 // Low -> Low
	default:
		return 2 // Default to Medium
	}
}

// beadsPriorityToLinear maps Beads priority (0-4) to Linear priority (0-4).
func beadsPriorityToLinear(beadsPriority int) int {
	switch beadsPriority {
	case 0:
		return 1 // Critical -> Urgent
	case 1:
		return 2 // High -> High
	case 2:
		return 3 // Medium -> Medium
	case 3:
		return 4 // Low -> Low
	case 4:
		return 0 // Backlog -> No priority
	default:
		return 3 // Default to Medium
	}
}

// linearStateToBeadsStatus maps Linear state type to Beads status.
func linearStateToBeadsStatus(stateType string) types.Status {
	switch stateType {
	case "backlog", "unstarted":
		return types.StatusOpen
	case "started":
		return types.StatusInProgress
	case "completed":
		return types.StatusClosed
	case "canceled":
		return types.StatusClosed
	default:
		return types.StatusOpen
	}
}

// linearIssueToBeads converts a Linear issue to a Beads issue.
func linearIssueToBeads(li *LinearIssue) (*types.Issue, error) {
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
		Priority:    linearPriorityToBeads(li.Priority),
		IssueType:   types.TypeTask,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}

	if li.State != nil {
		issue.Status = linearStateToBeadsStatus(li.State.Type)
	} else {
		issue.Status = types.StatusOpen
	}

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

	if li.Labels != nil {
		for _, label := range li.Labels.Nodes {
			issue.Labels = append(issue.Labels, label.Name)

			// Infer issue type from label names
			lowerName := strings.ToLower(label.Name)
			if strings.Contains(lowerName, "bug") {
				issue.IssueType = types.TypeBug
			} else if strings.Contains(lowerName, "feature") {
				issue.IssueType = types.TypeFeature
			} else if strings.Contains(lowerName, "epic") {
				issue.IssueType = types.TypeEpic
			} else if strings.Contains(lowerName, "chore") || strings.Contains(lowerName, "maintenance") {
				issue.IssueType = types.TypeChore
			}
		}
	}

	externalRef := li.URL
	issue.ExternalRef = &externalRef

	return issue, nil
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
type LinearConflict struct {
	IssueID           string
	LocalUpdated      time.Time
	LinearUpdated     time.Time
	LinearExternalRef string
}

// detectLinearConflicts finds issues that have been modified both locally and in Linear.
func detectLinearConflicts(ctx context.Context) ([]LinearConflict, error) {
	lastSyncStr, _ := store.GetConfig(ctx, "linear.last_sync")
	if lastSyncStr == "" {
		return nil, nil
	}

	lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
	if err != nil {
		return nil, fmt.Errorf("invalid last_sync timestamp: %w", err)
	}

	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return nil, err
	}

	var conflicts []LinearConflict
	for _, issue := range allIssues {
		if issue.ExternalRef == nil || !isLinearExternalRef(*issue.ExternalRef) {
			continue
		}

		if issue.UpdatedAt.After(lastSync) {
			conflicts = append(conflicts, LinearConflict{
				IssueID:           issue.ID,
				LocalUpdated:      issue.UpdatedAt,
				LinearExternalRef: *issue.ExternalRef,
			})
		}
	}

	return conflicts, nil
}

// reimportLinearConflicts re-imports conflicting issues from Linear (Linear wins).
// NOTE: This is a placeholder - full implementation requires fetching individual
// issues from Linear API and updating local copies.
func reimportLinearConflicts(_ context.Context, conflicts []LinearConflict) error {
	if len(conflicts) == 0 {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Warning: conflict resolution (--prefer-linear) not fully implemented\n")
	fmt.Fprintf(os.Stderr, "  %d issue(s) may have conflicts that need manual review:\n", len(conflicts))
	for _, c := range conflicts {
		fmt.Fprintf(os.Stderr, "    - %s (local updated: %s)\n", c.IssueID, c.LocalUpdated.Format(time.RFC3339))
	}
	return nil
}

// resolveLinearConflictsByTimestamp resolves conflicts by keeping the newer version.
// NOTE: This is a placeholder - full implementation requires fetching Linear
// timestamps and comparing with local timestamps.
func resolveLinearConflictsByTimestamp(_ context.Context, conflicts []LinearConflict) error {
	if len(conflicts) == 0 {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Warning: timestamp-based conflict resolution not fully implemented\n")
	fmt.Fprintf(os.Stderr, "  %d issue(s) may have conflicts - local version will be pushed:\n", len(conflicts))
	for _, c := range conflicts {
		fmt.Fprintf(os.Stderr, "    - %s\n", c.IssueID)
	}
	return nil
}

// LinearPullStats tracks pull operation statistics.
type LinearPullStats struct {
	Created int
	Updated int
	Skipped int
}

// doPullFromLinear imports issues from Linear using the GraphQL API.
func doPullFromLinear(ctx context.Context, dryRun bool, state string) (*LinearPullStats, error) {
	stats := &LinearPullStats{}

	client, err := getLinearClient(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to create Linear client: %w", err)
	}

	linearIssues, err := client.FetchIssues(ctx, state)
	if err != nil {
		return stats, fmt.Errorf("failed to fetch issues from Linear: %w", err)
	}

	if dryRun {
		fmt.Printf("  Would import %d issues from Linear\n", len(linearIssues))
		return stats, nil
	}

	var beadsIssues []*types.Issue
	for _, li := range linearIssues {
		issue, err := linearIssueToBeads(&li)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to convert issue %s: %v\n", li.Identifier, err)
			stats.Skipped++
			continue
		}
		beadsIssues = append(beadsIssues, issue)
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

	return stats, nil
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

	for _, issue := range toCreate {
		linearPriority := beadsPriorityToLinear(issue.Priority)
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

	if len(toUpdate) > 0 && !createOnly {
		fmt.Fprintf(os.Stderr, "  Note: Updating existing Linear issues is not yet supported (%d skipped)\n", len(toUpdate))
		stats.Skipped += len(toUpdate)
	}

	return stats, nil
}
