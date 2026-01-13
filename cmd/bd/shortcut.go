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
	"github.com/steveyegge/beads/internal/shortcut"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// shortcutCmd is the root command for Shortcut integration.
var shortcutCmd = &cobra.Command{
	Use:     "shortcut",
	GroupID: "advanced",
	Short:   "Shortcut integration commands",
	Long: `Synchronize issues between beads and Shortcut.

Configuration:
  bd config set shortcut.api_token "YOUR_API_TOKEN"
  bd config set shortcut.team_id "TEAM_UUID"   # Use 'bd shortcut teams' to find the UUID

Environment variables (alternative to config):
  SHORTCUT_API_TOKEN - Shortcut API token

Data Mapping (optional, sensible defaults provided):
  Priority mapping (Shortcut priority to Beads 0-4):
    bd config set shortcut.priority_map.none 4      # No priority -> Backlog
    bd config set shortcut.priority_map.urgent 0    # Urgent -> Critical
    bd config set shortcut.priority_map.high 1      # High -> High
    bd config set shortcut.priority_map.medium 2    # Medium -> Medium
    bd config set shortcut.priority_map.low 3       # Low -> Low

  State mapping (Shortcut workflow state to Beads status):
    bd config set shortcut.state_map.Backlog open
    bd config set shortcut.state_map."Ready for Dev" open
    bd config set shortcut.state_map."In Progress" in_progress
    bd config set shortcut.state_map."In Review" in_progress
    bd config set shortcut.state_map.Done closed

  Type mapping (Shortcut story type to Beads issue type):
    bd config set shortcut.type_map.feature feature
    bd config set shortcut.type_map.bug bug
    bd config set shortcut.type_map.chore task

Examples:
  bd shortcut sync --pull         # Import stories from Shortcut
  bd shortcut sync --push         # Export issues to Shortcut
  bd shortcut sync                # Bidirectional sync (pull then push)
  bd shortcut sync --dry-run      # Preview sync without changes
  bd shortcut status              # Show sync status`,
}

// shortcutSyncCmd handles synchronization with Shortcut.
var shortcutSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronize issues with Shortcut",
	Long: `Synchronize issues between beads and Shortcut.

Modes:
  --pull         Import stories from Shortcut into beads
  --push         Export issues from beads to Shortcut
  (no flags)     Bidirectional sync: pull then push

Conflict Resolution:
  By default, newer timestamp wins. Override with:
  --prefer-local      Always prefer local beads version
  --prefer-shortcut   Always prefer Shortcut version

Examples:
  bd shortcut sync --pull                # Import from Shortcut
  bd shortcut sync --push --create-only  # Push new issues only
  bd shortcut sync --dry-run             # Preview without changes
  bd shortcut sync --prefer-local        # Bidirectional, local wins`,
	Run: runShortcutSync,
}

// shortcutStatusCmd shows the current sync status.
var shortcutStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Shortcut sync status",
	Long: `Show the current Shortcut sync status, including:
  - Last sync timestamp
  - Configuration status
  - Number of issues with Shortcut links
  - Issues pending push (no external_ref)`,
	Run: runShortcutStatus,
}

// shortcutTeamsCmd lists available teams.
var shortcutTeamsCmd = &cobra.Command{
	Use:   "teams",
	Short: "List available Shortcut teams",
	Long: `List all teams (groups) accessible with your Shortcut API token.

Use this to find the team UUID needed for configuration.

Example:
  bd shortcut teams
  bd config set shortcut.team_id "5e330b96-ac5f-44b1-9c7e-a034627c81c8"`,
	Run: runShortcutTeams,
}

func init() {
	shortcutSyncCmd.Flags().Bool("pull", false, "Pull stories from Shortcut")
	shortcutSyncCmd.Flags().Bool("push", false, "Push issues to Shortcut")
	shortcutSyncCmd.Flags().Bool("dry-run", false, "Preview sync without making changes")
	shortcutSyncCmd.Flags().Bool("prefer-local", false, "Prefer local version on conflicts")
	shortcutSyncCmd.Flags().Bool("prefer-shortcut", false, "Prefer Shortcut version on conflicts")
	shortcutSyncCmd.Flags().Bool("create-only", false, "Only create new issues, don't update existing")
	shortcutSyncCmd.Flags().Bool("update-refs", true, "Update external_ref after creating Shortcut stories")
	shortcutSyncCmd.Flags().String("state", "all", "Story state to sync: open, closed, all")

	shortcutCmd.AddCommand(shortcutSyncCmd)
	shortcutCmd.AddCommand(shortcutStatusCmd)
	shortcutCmd.AddCommand(shortcutTeamsCmd)
	rootCmd.AddCommand(shortcutCmd)
}

func runShortcutSync(cmd *cobra.Command, args []string) {
	pull, _ := cmd.Flags().GetBool("pull")
	push, _ := cmd.Flags().GetBool("push")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	preferLocal, _ := cmd.Flags().GetBool("prefer-local")
	preferShortcut, _ := cmd.Flags().GetBool("prefer-shortcut")
	createOnly, _ := cmd.Flags().GetBool("create-only")
	updateRefs, _ := cmd.Flags().GetBool("update-refs")
	state, _ := cmd.Flags().GetString("state")

	if !dryRun {
		CheckReadonly("shortcut sync")
	}

	if preferLocal && preferShortcut {
		fmt.Fprintf(os.Stderr, "Error: cannot use both --prefer-local and --prefer-shortcut\n")
		os.Exit(1)
	}

	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: database not available: %v\n", err)
		os.Exit(1)
	}

	if err := validateShortcutConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Default to both pull and push if neither specified
	if !pull && !push {
		pull = true
		push = true
	}

	ctx := rootCtx
	result := &shortcut.SyncResult{Success: true}
	var forceUpdateIDs map[string]bool
	var skipUpdateIDs map[string]bool
	var prePullConflicts []shortcut.Conflict
	var prePullSkipStoryIDs map[int64]bool

	if pull {
		if preferLocal || preferShortcut {
			conflicts, err := detectShortcutConflicts(ctx)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("conflict detection failed: %v", err))
			} else if len(conflicts) > 0 {
				prePullConflicts = conflicts
				if preferLocal {
					prePullSkipStoryIDs = make(map[int64]bool, len(conflicts))
					forceUpdateIDs = make(map[string]bool, len(conflicts))
					for _, conflict := range conflicts {
						prePullSkipStoryIDs[conflict.ShortcutStoryID] = true
						forceUpdateIDs[conflict.IssueID] = true
					}
				} else if preferShortcut {
					skipUpdateIDs = make(map[string]bool, len(conflicts))
					for _, conflict := range conflicts {
						skipUpdateIDs[conflict.IssueID] = true
					}
				}
			}
		}

		if dryRun {
			fmt.Println("→ [DRY RUN] Would pull stories from Shortcut")
		} else {
			fmt.Println("→ Pulling stories from Shortcut...")
		}

		pullStats, err := doPullFromShortcut(ctx, dryRun, state, prePullSkipStoryIDs)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			if jsonOutput {
				outputJSON(result)
			} else {
				fmt.Fprintf(os.Stderr, "Error pulling from Shortcut: %v\n", err)
			}
			os.Exit(1)
		}

		result.Stats.Pulled = pullStats.Created + pullStats.Updated
		result.Stats.Created += pullStats.Created
		result.Stats.Updated += pullStats.Updated
		result.Stats.Skipped += pullStats.Skipped

		if !dryRun {
			fmt.Printf("✓ Pulled %d stories (%d created, %d updated)\n",
				result.Stats.Pulled, pullStats.Created, pullStats.Updated)
		}
	}

	if pull && push {
		conflicts := prePullConflicts
		var err error
		if conflicts == nil {
			conflicts, err = detectShortcutConflicts(ctx)
		}
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("conflict detection failed: %v", err))
		} else if len(conflicts) > 0 {
			result.Stats.Conflicts = len(conflicts)
			if dryRun {
				if preferLocal {
					fmt.Printf("→ [DRY RUN] Would resolve %d conflicts (preferring local)\n", len(conflicts))
					forceUpdateIDs = make(map[string]bool, len(conflicts))
					for _, conflict := range conflicts {
						forceUpdateIDs[conflict.IssueID] = true
					}
				} else if preferShortcut {
					fmt.Printf("→ [DRY RUN] Would resolve %d conflicts (preferring Shortcut)\n", len(conflicts))
					skipUpdateIDs = make(map[string]bool, len(conflicts))
					for _, conflict := range conflicts {
						skipUpdateIDs[conflict.IssueID] = true
					}
				} else {
					fmt.Printf("→ [DRY RUN] Would resolve %d conflicts (newer wins)\n", len(conflicts))
					var shortcutWins []shortcut.Conflict
					var localWins []shortcut.Conflict
					for _, conflict := range conflicts {
						if conflict.ShortcutUpdated.After(conflict.LocalUpdated) {
							shortcutWins = append(shortcutWins, conflict)
						} else {
							localWins = append(localWins, conflict)
						}
					}
					if len(localWins) > 0 {
						forceUpdateIDs = make(map[string]bool, len(localWins))
						for _, conflict := range localWins {
							forceUpdateIDs[conflict.IssueID] = true
						}
					}
					if len(shortcutWins) > 0 {
						skipUpdateIDs = make(map[string]bool, len(shortcutWins))
						for _, conflict := range shortcutWins {
							skipUpdateIDs[conflict.IssueID] = true
						}
					}
				}
			} else if preferLocal {
				fmt.Printf("→ Resolving %d conflicts (preferring local)\n", len(conflicts))
				if forceUpdateIDs == nil {
					forceUpdateIDs = make(map[string]bool, len(conflicts))
					for _, conflict := range conflicts {
						forceUpdateIDs[conflict.IssueID] = true
					}
				}
			} else if preferShortcut {
				fmt.Printf("→ Resolving %d conflicts (preferring Shortcut)\n", len(conflicts))
				if skipUpdateIDs == nil {
					skipUpdateIDs = make(map[string]bool, len(conflicts))
					for _, conflict := range conflicts {
						skipUpdateIDs[conflict.IssueID] = true
					}
				}
				if prePullConflicts == nil {
					if err := reimportShortcutConflicts(ctx, conflicts); err != nil {
						result.Warnings = append(result.Warnings, fmt.Sprintf("conflict resolution failed: %v", err))
					}
				}
			} else {
				fmt.Printf("→ Resolving %d conflicts (newer wins)\n", len(conflicts))
				if err := resolveShortcutConflictsByTimestamp(ctx, conflicts); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("conflict resolution failed: %v", err))
				}
			}
		}
	}

	if push {
		if dryRun {
			fmt.Println("→ [DRY RUN] Would push issues to Shortcut")
		} else {
			fmt.Println("→ Pushing issues to Shortcut...")
		}

		pushStats, err := doPushToShortcut(ctx, dryRun, createOnly, updateRefs, forceUpdateIDs, skipUpdateIDs)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			if jsonOutput {
				outputJSON(result)
			} else {
				fmt.Fprintf(os.Stderr, "Error pushing to Shortcut: %v\n", err)
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
		if err := store.SetConfig(ctx, "shortcut.last_sync", result.LastSync); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to update last_sync: %v", err))
		}
	}

	if jsonOutput {
		outputJSON(result)
	} else if dryRun {
		fmt.Println("\n✓ Dry run complete (no changes made)")
	} else {
		fmt.Println("\n✓ Shortcut sync complete")
		if len(result.Warnings) > 0 {
			fmt.Println("\nWarnings:")
			for _, w := range result.Warnings {
				fmt.Printf("  - %s\n", w)
			}
		}
	}
}

func runShortcutStatus(cmd *cobra.Command, args []string) {
	ctx := rootCtx

	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	apiToken, _ := getShortcutConfig(ctx, "shortcut.api_token")
	teamID, _ := getShortcutConfig(ctx, "shortcut.team_id")
	lastSync, _ := store.GetConfig(ctx, "shortcut.last_sync")

	configured := apiToken != ""

	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	withShortcutRef := 0
	pendingPush := 0
	for _, issue := range allIssues {
		if issue.ExternalRef != nil && shortcut.IsShortcutExternalRef(*issue.ExternalRef) {
			withShortcutRef++
		} else if issue.ExternalRef == nil {
			pendingPush++
		}
	}

	if jsonOutput {
		hasAPIToken := apiToken != ""
		outputJSON(map[string]interface{}{
			"configured":        configured,
			"has_api_token":     hasAPIToken,
			"team_id":           teamID,
			"last_sync":         lastSync,
			"total_issues":      len(allIssues),
			"with_shortcut_ref": withShortcutRef,
			"pending_push":      pendingPush,
		})
		return
	}

	fmt.Println("Shortcut Sync Status")
	fmt.Println("====================")
	fmt.Println()

	if !configured {
		fmt.Println("Status: Not configured")
		fmt.Println()
		fmt.Println("To configure Shortcut integration:")
		fmt.Println("  bd config set shortcut.api_token \"YOUR_API_TOKEN\"")
		fmt.Println("  bd config set shortcut.team_id \"TEAM_UUID\"  # Use 'bd shortcut teams' to find UUID")
		fmt.Println()
		fmt.Println("Or use environment variables:")
		fmt.Println("  export SHORTCUT_API_TOKEN=\"YOUR_API_TOKEN\"")
		return
	}

	fmt.Printf("Team ID:      %s\n", teamID)
	fmt.Printf("API Token:    %s\n", maskAPIToken(apiToken))
	if lastSync != "" {
		fmt.Printf("Last Sync:    %s\n", lastSync)
	} else {
		fmt.Println("Last Sync:    Never")
	}
	fmt.Println()
	fmt.Printf("Total Issues: %d\n", len(allIssues))
	fmt.Printf("With Shortcut: %d\n", withShortcutRef)
	fmt.Printf("Local Only:   %d\n", pendingPush)

	if pendingPush > 0 {
		fmt.Println()
		fmt.Printf("Run 'bd shortcut sync --push' to push %d local issue(s) to Shortcut\n", pendingPush)
	}
}

func runShortcutTeams(cmd *cobra.Command, args []string) {
	ctx := rootCtx

	apiToken, apiTokenSource := getShortcutConfig(ctx, "shortcut.api_token")
	if apiToken == "" {
		fmt.Fprintf(os.Stderr, "Error: Shortcut API token not configured\n")
		fmt.Fprintf(os.Stderr, "Run: bd config set shortcut.api_token \"YOUR_API_TOKEN\"\n")
		fmt.Fprintf(os.Stderr, "Or:  export SHORTCUT_API_TOKEN=YOUR_API_TOKEN\n")
		os.Exit(1)
	}

	debug.Logf("Using API token from %s", apiTokenSource)

	client := shortcut.NewClient(apiToken, "")

	teams, err := client.GetTeams(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching teams: %v\n", err)
		os.Exit(1)
	}

	if len(teams) == 0 {
		fmt.Println("No teams found (check your API token permissions)")
		return
	}

	if jsonOutput {
		outputJSON(teams)
		return
	}

	fmt.Println("Available Shortcut Teams")
	fmt.Println("========================")
	fmt.Println()
	fmt.Printf("%-40s  %-20s  %s\n", "ID (use this for team_id)", "Mention Name", "Name")
	fmt.Printf("%-40s  %-20s  %s\n", "----------------------------------------", "--------------------", "----")
	for _, team := range teams {
		fmt.Printf("%-40s  %-20s  %s\n", team.ID, team.MentionName, team.Name)
	}
	fmt.Println()
	fmt.Println("To configure, use the ID (UUID) from above:")
	fmt.Println("  bd config set shortcut.team_id \"<ID>\"")
}

// shortcutUUIDRegex matches valid UUID format (with or without hyphens).
var shortcutUUIDRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{12}$`)

func isValidShortcutUUID(s string) bool {
	return shortcutUUIDRegex.MatchString(s)
}

// validateShortcutConfig checks that required Shortcut configuration is present.
func validateShortcutConfig() error {
	if err := ensureStoreActive(); err != nil {
		return fmt.Errorf("database not available: %w", err)
	}

	ctx := rootCtx

	apiToken, _ := getShortcutConfig(ctx, "shortcut.api_token")
	if apiToken == "" {
		return fmt.Errorf("Shortcut API token not configured\nRun: bd config set shortcut.api_token \"YOUR_API_TOKEN\"\nOr: export SHORTCUT_API_TOKEN=YOUR_API_TOKEN")
	}

	teamID, _ := getShortcutConfig(ctx, "shortcut.team_id")
	if teamID == "" {
		return fmt.Errorf("shortcut.team_id not configured\nRun: bd config set shortcut.team_id \"TEAM_UUID\"\nUse 'bd shortcut teams' to find your team's UUID")
	}

	if !isValidShortcutUUID(teamID) {
		return fmt.Errorf("shortcut.team_id appears invalid (expected UUID format like '5e330b96-ac5f-44b1-9c7e-a034627c81c8')\nCurrent value: %s\nUse 'bd shortcut teams' to find your team's UUID", teamID)
	}

	return nil
}

// maskAPIToken returns a masked version of an API token for display.
func maskAPIToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

// getShortcutConfig reads a Shortcut configuration value, handling both daemon mode
// and direct mode. Returns the value and its source.
func getShortcutConfig(ctx context.Context, key string) (value string, source string) {
	// Try to read from store (works in direct mode)
	if store != nil {
		value, _ = store.GetConfig(ctx, key)
		if value != "" {
			return value, "project config (bd config)"
		}
	} else if dbPath != "" {
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
	envKey := shortcutConfigToEnvVar(key)
	if envKey != "" {
		value = os.Getenv(envKey)
		if value != "" {
			return value, fmt.Sprintf("environment variable (%s)", envKey)
		}
	}

	return "", ""
}

// shortcutConfigToEnvVar maps Shortcut config keys to environment variable names.
func shortcutConfigToEnvVar(key string) string {
	switch key {
	case "shortcut.api_token":
		return "SHORTCUT_API_TOKEN"
	default:
		return ""
	}
}

// getShortcutClient creates a configured Shortcut client from beads config.
func getShortcutClient(ctx context.Context) (*shortcut.Client, error) {
	apiToken, _ := getShortcutConfig(ctx, "shortcut.api_token")
	if apiToken == "" {
		return nil, fmt.Errorf("Shortcut API token not configured")
	}

	teamID, _ := getShortcutConfig(ctx, "shortcut.team_id")

	client := shortcut.NewClient(apiToken, teamID)

	if store != nil {
		if endpoint, _ := store.GetConfig(ctx, "shortcut.api_endpoint"); endpoint != "" {
			client = client.WithEndpoint(endpoint)
		}
	}

	return client, nil
}

// storeConfigLoaderShortcut adapts the store to the shortcut.ConfigLoader interface.
type storeConfigLoaderShortcut struct {
	ctx context.Context
}

func (l *storeConfigLoaderShortcut) GetAllConfig() (map[string]string, error) {
	return store.GetAllConfig(l.ctx)
}

// loadShortcutMappingConfig loads mapping configuration from beads config.
func loadShortcutMappingConfig(ctx context.Context) *shortcut.MappingConfig {
	if store == nil {
		return shortcut.DefaultMappingConfig()
	}
	return shortcut.LoadMappingConfig(&storeConfigLoaderShortcut{ctx: ctx})
}

// getShortcutIDMode returns the configured ID mode for Shortcut imports.
func getShortcutIDMode(ctx context.Context) string {
	mode, _ := getShortcutConfig(ctx, "shortcut.id_mode")
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "hash"
	}
	return mode
}

// getShortcutHashLength returns the configured hash length for Shortcut imports.
func getShortcutHashLength(ctx context.Context) int {
	raw, _ := getShortcutConfig(ctx, "shortcut.hash_length")
	if raw == "" {
		return 6
	}
	var value int
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &value); err != nil {
		return 6
	}
	if value < 3 {
		return 3
	}
	if value > 8 {
		return 8
	}
	return value
}
