package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/linear"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// linearCmd is the root command for Linear integration.
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

// linearSyncCmd handles synchronization with Linear.
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
	Run: runLinearSync,
}

// linearStatusCmd shows the current sync status.
var linearStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Linear sync status",
	Long: `Show the current Linear sync status, including:
  - Last sync timestamp
  - Configuration status
  - Number of issues with Linear links
  - Issues pending push (no external_ref)`,
	Run: runLinearStatus,
}

// linearTeamsCmd lists available teams.
var linearTeamsCmd = &cobra.Command{
	Use:   "teams",
	Short: "List available Linear teams",
	Long: `List all teams accessible with your Linear API key.

Use this to find the team ID (UUID) needed for configuration.

Example:
  bd linear teams
  bd config set linear.team_id "12345678-1234-1234-1234-123456789abc"`,
	Run: runLinearTeams,
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
	linearCmd.AddCommand(linearTeamsCmd)
	rootCmd.AddCommand(linearCmd)
}

func runLinearSync(cmd *cobra.Command, args []string) {
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
	result := &linear.SyncResult{Success: true}
	var forceUpdateIDs map[string]bool

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
				// Local wins - track conflicts so push will overwrite regardless of timestamps
				forceUpdateIDs = make(map[string]bool, len(conflicts))
				for _, conflict := range conflicts {
					forceUpdateIDs[conflict.IssueID] = true
				}
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

		pushStats, err := doPushToLinear(ctx, dryRun, createOnly, updateRefs, forceUpdateIDs)
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
}

func runLinearStatus(cmd *cobra.Command, args []string) {
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
		if issue.ExternalRef != nil && linear.IsLinearExternalRef(*issue.ExternalRef) {
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
}

func runLinearTeams(cmd *cobra.Command, args []string) {
	ctx := rootCtx

	// Get API key and team ID using the helper function.
	// This handles both daemon mode (where store is nil) and direct mode.
	apiKey, apiKeySource := getLinearConfig(ctx, "linear.api_key")
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: Linear API key not configured\n")
		fmt.Fprintf(os.Stderr, "Run: bd config set linear.api_key \"YOUR_API_KEY\"\n")
		fmt.Fprintf(os.Stderr, "Or:  export LINEAR_API_KEY=YOUR_API_KEY\n")
		os.Exit(1)
	}

	// Show source in verbose mode (helps troubleshoot multi-workspace setups)
	debug.Logf("Using API key from %s", apiKeySource)

	// Create client with empty team ID (not needed for listing teams)
	client := linear.NewClient(apiKey, "")

	teams, err := client.FetchTeams(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching teams: %v\n", err)
		os.Exit(1)
	}

	if len(teams) == 0 {
		fmt.Println("No teams found (check your API key permissions)")
		return
	}

	if jsonOutput {
		outputJSON(teams)
		return
	}

	fmt.Println("Available Linear Teams")
	fmt.Println("======================")
	fmt.Println()
	fmt.Printf("%-40s  %-6s  %s\n", "ID (use this for linear.team_id)", "Key", "Name")
	fmt.Printf("%-40s  %-6s  %s\n", "----------------------------------------", "------", "----")
	for _, team := range teams {
		fmt.Printf("%-40s  %-6s  %s\n", team.ID, team.Key, team.Name)
	}
	fmt.Println()
	fmt.Println("To configure:")
	fmt.Println("  bd config set linear.team_id \"<ID>\"")
}

// uuidRegex matches valid UUID format (with or without hyphens).
var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{12}$`)

func isValidUUID(s string) bool {
	return uuidRegex.MatchString(s)
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

	// Validate team ID format (should be UUID)
	if !isValidUUID(teamID) {
		return fmt.Errorf("linear.team_id appears invalid (expected UUID format like '12345678-1234-1234-1234-123456789abc')\nCurrent value: %s", teamID)
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

// getLinearConfig reads a Linear configuration value, handling both daemon mode
// (where store is nil) and direct mode. Returns the value and its source.
// Priority: project config > environment variable.
func getLinearConfig(ctx context.Context, key string) (value string, source string) {
	// Try to read from store (works in direct mode)
	if store != nil {
		value, _ = store.GetConfig(ctx, key)
		if value != "" {
			return value, "project config (bd config)"
		}
	} else if dbPath != "" {
		// In daemon mode, store is nil. Open a temporary read-only connection
		// to read the config. This is necessary because the RPC protocol
		// doesn't expose GetConfig.
		tempStore, err := sqlite.NewWithTimeout(ctx, dbPath, 5*time.Second)
		if err == nil {
			defer func() { _ = tempStore.Close() }()
			value, _ = tempStore.GetConfig(ctx, key)
			if value != "" {
				return value, "project config (bd config)"
			}
		}
	}

	// Fall back to environment variable
	envKey := linearConfigToEnvVar(key)
	if envKey != "" {
		value = os.Getenv(envKey)
		if value != "" {
			return value, fmt.Sprintf("environment variable (%s)", envKey)
		}
	}

	return "", ""
}

// linearConfigToEnvVar maps Linear config keys to their environment variable names.
func linearConfigToEnvVar(key string) string {
	switch key {
	case "linear.api_key":
		return "LINEAR_API_KEY"
	case "linear.team_id":
		return "LINEAR_TEAM_ID"
	default:
		return ""
	}
}

// getLinearClient creates a configured Linear client from beads config.
func getLinearClient(ctx context.Context) (*linear.Client, error) {
	apiKey, _ := getLinearConfig(ctx, "linear.api_key")
	if apiKey == "" {
		return nil, fmt.Errorf("Linear API key not configured")
	}

	teamID, _ := getLinearConfig(ctx, "linear.team_id")
	if teamID == "" {
		return nil, fmt.Errorf("Linear team ID not configured")
	}

	client := linear.NewClient(apiKey, teamID)

	// Allow custom endpoint for self-hosted instances or testing
	// Note: This still requires store to be available; endpoint override
	// is an advanced feature typically used in direct mode
	if store != nil {
		if endpoint, _ := store.GetConfig(ctx, "linear.api_endpoint"); endpoint != "" {
			client = client.WithEndpoint(endpoint)
		}
	}

	return client, nil
}

// storeConfigLoader adapts the store to the linear.ConfigLoader interface.
type storeConfigLoader struct {
	ctx context.Context
}

func (l *storeConfigLoader) GetAllConfig() (map[string]string, error) {
	return store.GetAllConfig(l.ctx)
}

// loadLinearMappingConfig loads mapping configuration from beads config.
func loadLinearMappingConfig(ctx context.Context) *linear.MappingConfig {
	if store == nil {
		return linear.DefaultMappingConfig()
	}
	return linear.LoadMappingConfig(&storeConfigLoader{ctx: ctx})
}

// detectLinearConflicts finds issues that have been modified both locally and in Linear
// since the last sync. This is a more expensive operation as it fetches individual
// issue timestamps from Linear.
func detectLinearConflicts(ctx context.Context) ([]linear.Conflict, error) {
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

	var conflicts []linear.Conflict

	for _, issue := range allIssues {
		if issue.ExternalRef == nil || !linear.IsLinearExternalRef(*issue.ExternalRef) {
			continue
		}

		// Only check issues that have been modified locally since last sync
		if !issue.UpdatedAt.After(lastSync) {
			continue
		}

		// Extract Linear identifier from external_ref URL
		linearIdentifier := linear.ExtractLinearIdentifier(*issue.ExternalRef)
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
			conflicts = append(conflicts, linear.Conflict{
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
func reimportLinearConflicts(ctx context.Context, conflicts []linear.Conflict) error {
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
		updates := linear.BuildLinearToLocalUpdates(linearIssue, config)

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

// resolveLinearConflictsByTimestamp resolves conflicts by keeping the newer version.
// For each conflict, compares local and Linear UpdatedAt timestamps.
// If Linear is newer, re-imports from Linear. If local is newer, push will overwrite.
func resolveLinearConflictsByTimestamp(ctx context.Context, conflicts []linear.Conflict) error {
	if len(conflicts) == 0 {
		return nil
	}

	// Separate conflicts into "Linear wins" vs "Local wins" based on timestamps
	var linearWins []linear.Conflict
	var localWins []linear.Conflict

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

// doPullFromLinear imports issues from Linear using the GraphQL API.
// Supports incremental sync by checking linear.last_sync config and only fetching
// issues updated since that timestamp.
func doPullFromLinear(ctx context.Context, dryRun bool, state string) (*linear.PullStats, error) {
	stats := &linear.PullStats{}

	client, err := getLinearClient(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to create Linear client: %w", err)
	}

	// Check for last sync timestamp to enable incremental sync
	var linearIssues []linear.Issue
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

	// Load mapping configuration for converting Linear issues to Beads
	mappingConfig := loadLinearMappingConfig(ctx)

	var beadsIssues []*types.Issue
	var allDeps []linear.DependencyInfo
	linearIDToBeadsID := make(map[string]string)

	for i := range linearIssues {
		conversion := linear.IssueToBeads(&linearIssues[i], mappingConfig)
		beadsIssues = append(beadsIssues, conversion.Issue.(*types.Issue))
		allDeps = append(allDeps, conversion.Dependencies...)
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
		if issue.ExternalRef != nil && linear.IsLinearExternalRef(*issue.ExternalRef) {
			// Extract Linear identifier from URL
			linearID := linear.ExtractLinearIdentifier(*issue.ExternalRef)
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

// doPushToLinear exports issues to Linear using the GraphQL API.
func doPushToLinear(ctx context.Context, dryRun bool, createOnly bool, updateRefs bool, forceUpdateIDs map[string]bool) (*linear.PushStats, error) {
	stats := &linear.PushStats{}

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

		if issue.ExternalRef != nil && linear.IsLinearExternalRef(*issue.ExternalRef) {
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

	stateCache, err := linear.BuildStateCache(ctx, client)
	if err != nil {
		return stats, fmt.Errorf("failed to fetch team states: %w", err)
	}

	// Load mapping configuration for priority conversion
	mappingConfig := loadLinearMappingConfig(ctx)

	for _, issue := range toCreate {
		linearPriority := linear.PriorityToLinear(issue.Priority, mappingConfig)
		stateID := stateCache.FindStateForBeadsStatus(issue.Status)

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
			linearIdentifier := linear.ExtractLinearIdentifier(*issue.ExternalRef)
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

			// Compare timestamps: only update if local is newer,
			// unless this issue is in the force-update set.
			if !forceUpdateIDs[issue.ID] && !issue.UpdatedAt.After(linearUpdatedAt) {
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
			linearPriority := linear.PriorityToLinear(issue.Priority, mappingConfig)
			if linearPriority > 0 {
				updatePayload["priority"] = linearPriority
			}

			// Add state if we can map it
			stateID := stateCache.FindStateForBeadsStatus(issue.Status)
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
