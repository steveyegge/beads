// cmd/bd/vikunja.go
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/vikunja"
)

// vikunjaCmd is the root command for Vikunja integration.
var vikunjaCmd = &cobra.Command{
	Use:     "vikunja",
	GroupID: "advanced",
	Short:   "Vikunja integration commands",
	Long: `Synchronize issues between beads and Vikunja.

Configuration:
  bd config set vikunja.api_url "https://vikunja.example.com/api/v1"
  bd config set vikunja.api_token "YOUR_API_TOKEN"
  bd config set vikunja.project_id "123"
  bd config set vikunja.view_id "456"

Environment variables (alternative to config):
  VIKUNJA_API_URL   - Vikunja API base URL
  VIKUNJA_API_TOKEN - Vikunja API token

Data Mapping (optional, sensible defaults provided):
  Priority mapping (Vikunja 0-4 to Beads 0-4):
    bd config set vikunja.priority_map.0 4    # Unset -> Backlog
    bd config set vikunja.priority_map.1 3    # Low -> Low
    bd config set vikunja.priority_map.2 2    # Medium -> Medium
    bd config set vikunja.priority_map.3 1    # High -> High
    bd config set vikunja.priority_map.4 0    # Urgent -> Critical

  Label to issue type mapping:
    bd config set vikunja.label_type_map.bug bug
    bd config set vikunja.label_type_map.feature feature

  Relation type mapping:
    bd config set vikunja.relation_map.blocking blocks
    bd config set vikunja.relation_map.subtask parent-child

Examples:
  bd vikunja sync --pull         # Import tasks from Vikunja
  bd vikunja sync --push         # Export issues to Vikunja
  bd vikunja sync                # Bidirectional sync
  bd vikunja sync --dry-run      # Preview sync without changes
  bd vikunja status              # Show sync status
  bd vikunja projects            # List available projects`,
}

// vikunjaSyncCmd handles synchronization with Vikunja.
var vikunjaSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronize issues with Vikunja",
	Long: `Synchronize issues between beads and Vikunja.

Modes:
  --pull         Import tasks from Vikunja into beads
  --push         Export issues from beads to Vikunja
  (no flags)     Bidirectional sync: pull then push, with conflict resolution

Type Filtering (--push only):
  --type task,feature    Only sync issues of these types
  --exclude-type wisp    Exclude issues of these types

Conflict Resolution:
  By default, newer timestamp wins. Override with:
  --prefer-local     Always prefer local beads version
  --prefer-vikunja   Always prefer Vikunja version

Examples:
  bd vikunja sync --pull                     # Import from Vikunja
  bd vikunja sync --push --create-only       # Push new issues only
  bd vikunja sync --push --type=task,feature # Push only tasks and features
  bd vikunja sync --dry-run                  # Preview without changes
  bd vikunja sync --prefer-local             # Bidirectional, local wins`,
	Run: runVikunjaSync,
}

// vikunjaStatusCmd shows the current sync status.
var vikunjaStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Vikunja sync status",
	Long: `Show the current Vikunja sync status, including:
  - Last sync timestamp
  - Configuration status
  - Number of issues with Vikunja links
  - Issues pending push (no external_ref)`,
	Run: runVikunjaStatus,
}

// vikunjaProjectsCmd lists available projects.
var vikunjaProjectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List available Vikunja projects",
	Long: `List all projects accessible with your Vikunja API token.

Use this to find the project ID needed for configuration.

Example:
  bd vikunja projects
  bd config set vikunja.project_id "123"
  bd config set vikunja.view_id "456"`,
	Run: runVikunjaProjects,
}

func init() {
	vikunjaSyncCmd.Flags().Bool("pull", false, "Pull tasks from Vikunja")
	vikunjaSyncCmd.Flags().Bool("push", false, "Push issues to Vikunja")
	vikunjaSyncCmd.Flags().Bool("dry-run", false, "Preview sync without making changes")
	vikunjaSyncCmd.Flags().Bool("prefer-local", false, "Prefer local version on conflicts")
	vikunjaSyncCmd.Flags().Bool("prefer-vikunja", false, "Prefer Vikunja version on conflicts")
	vikunjaSyncCmd.Flags().Bool("create-only", false, "Only create new issues, don't update existing")
	vikunjaSyncCmd.Flags().Bool("update-refs", true, "Update external_ref after creating Vikunja tasks")
	vikunjaSyncCmd.Flags().String("state", "all", "Task state to sync: open, closed, all")
	vikunjaSyncCmd.Flags().StringSlice("type", nil, "Only sync issues of these types (can be repeated)")
	vikunjaSyncCmd.Flags().StringSlice("exclude-type", nil, "Exclude issues of these types (can be repeated)")

	vikunjaCmd.AddCommand(vikunjaSyncCmd)
	vikunjaCmd.AddCommand(vikunjaStatusCmd)
	vikunjaCmd.AddCommand(vikunjaProjectsCmd)
	rootCmd.AddCommand(vikunjaCmd)
}

// getVikunjaConfig reads a Vikunja configuration value.
// Priority: project config > environment variable.
func getVikunjaConfig(ctx context.Context, key string) (value string, source string) {
	// Try to read from store
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
	envKey := vikunjaConfigToEnvVar(key)
	if envKey != "" {
		value = os.Getenv(envKey)
		if value != "" {
			return value, fmt.Sprintf("environment variable (%s)", envKey)
		}
	}

	return "", ""
}

// vikunjaConfigToEnvVar maps Vikunja config keys to environment variable names.
func vikunjaConfigToEnvVar(key string) string {
	switch key {
	case "vikunja.api_url":
		return "VIKUNJA_API_URL"
	case "vikunja.api_token":
		return "VIKUNJA_API_TOKEN"
	default:
		return ""
	}
}

// validateVikunjaConfig checks that required configuration is present.
func validateVikunjaConfig() error {
	if err := ensureStoreActive(); err != nil {
		return fmt.Errorf("database not available: %w", err)
	}

	ctx := rootCtx

	apiURL, _ := getVikunjaConfig(ctx, "vikunja.api_url")
	if apiURL == "" {
		return fmt.Errorf("Vikunja API URL not configured\nRun: bd config set vikunja.api_url \"https://vikunja.example.com/api/v1\"\nOr: export VIKUNJA_API_URL=https://vikunja.example.com/api/v1")
	}

	apiToken, _ := getVikunjaConfig(ctx, "vikunja.api_token")
	if apiToken == "" {
		return fmt.Errorf("Vikunja API token not configured\nRun: bd config set vikunja.api_token \"YOUR_TOKEN\"\nOr: export VIKUNJA_API_TOKEN=YOUR_TOKEN")
	}

	return nil
}

// normalizeVikunjaURL ensures the API URL ends with /api/v1.
func normalizeVikunjaURL(url string) string {
	// Remove trailing slash
	url = strings.TrimSuffix(url, "/")

	// Check if URL already ends with /api/v1
	if strings.HasSuffix(url, "/api/v1") {
		return url
	}

	// Check if URL ends with /api (missing /v1)
	if strings.HasSuffix(url, "/api") {
		return url + "/v1"
	}

	// Append /api/v1
	return url + "/api/v1"
}

// getVikunjaClient creates a configured Vikunja client.
func getVikunjaClient(ctx context.Context) (*vikunja.Client, error) {
	apiURL, _ := getVikunjaConfig(ctx, "vikunja.api_url")
	if apiURL == "" {
		return nil, fmt.Errorf("Vikunja API URL not configured")
	}

	// Normalize URL to ensure it ends with /api/v1
	apiURL = normalizeVikunjaURL(apiURL)

	apiToken, _ := getVikunjaConfig(ctx, "vikunja.api_token")
	if apiToken == "" {
		return nil, fmt.Errorf("Vikunja API token not configured")
	}

	client := vikunja.NewClient(apiURL, apiToken)

	// Apply optional project/view config
	if store != nil {
		if projectIDStr, _ := store.GetConfig(ctx, "vikunja.project_id"); projectIDStr != "" {
			var projectID int64
			if _, err := fmt.Sscanf(projectIDStr, "%d", &projectID); err == nil {
				client = client.WithProjectID(projectID)
			}
		}
		if viewIDStr, _ := store.GetConfig(ctx, "vikunja.view_id"); viewIDStr != "" {
			var viewID int64
			if _, err := fmt.Sscanf(viewIDStr, "%d", &viewID); err == nil {
				client = client.WithViewID(viewID)
			}
		}
	}

	return client, nil
}

// vikunjaConfigLoader adapts store for ConfigLoader interface.
type vikunjaConfigLoader struct {
	ctx context.Context
}

func (l *vikunjaConfigLoader) GetAllConfig() (map[string]string, error) {
	return store.GetAllConfig(l.ctx)
}

func loadVikunjaMappingConfig(ctx context.Context) *vikunja.MappingConfig {
	if store == nil {
		return vikunja.DefaultMappingConfig()
	}
	return vikunja.LoadMappingConfig(&vikunjaConfigLoader{ctx: ctx})
}

func runVikunjaSync(cmd *cobra.Command, args []string) {
	pull, _ := cmd.Flags().GetBool("pull")
	push, _ := cmd.Flags().GetBool("push")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	preferLocal, _ := cmd.Flags().GetBool("prefer-local")
	preferVikunja, _ := cmd.Flags().GetBool("prefer-vikunja")
	createOnly, _ := cmd.Flags().GetBool("create-only")
	updateRefs, _ := cmd.Flags().GetBool("update-refs")
	state, _ := cmd.Flags().GetString("state")
	typeFilters, _ := cmd.Flags().GetStringSlice("type")
	excludeTypes, _ := cmd.Flags().GetStringSlice("exclude-type")

	if !dryRun {
		CheckReadonly("vikunja sync")
	}

	if preferLocal && preferVikunja {
		fmt.Fprintf(os.Stderr, "Error: cannot use both --prefer-local and --prefer-vikunja\n")
		os.Exit(1)
	}

	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: database not available: %v\n", err)
		os.Exit(1)
	}

	if err := validateVikunjaConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if !pull && !push {
		pull = true
		push = true
	}

	ctx := rootCtx
	result := &vikunja.SyncResult{Success: true}
	var forceUpdateIDs map[string]bool
	var skipUpdateIDs map[string]bool

	// Reserve preferLocal and preferVikunja for future conflict resolution
	_ = preferLocal
	_ = preferVikunja

	if pull {
		if dryRun {
			fmt.Println("-> [DRY RUN] Would pull tasks from Vikunja")
		} else {
			fmt.Println("-> Pulling tasks from Vikunja...")
		}

		pullStats, err := doPullFromVikunja(ctx, dryRun, state)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			fmt.Fprintf(os.Stderr, "Error pulling from Vikunja: %v\n", err)
			os.Exit(1)
		}

		result.Stats.Pulled = pullStats.Created + pullStats.Updated
		result.Stats.Created += pullStats.Created
		result.Stats.Updated += pullStats.Updated
		result.Stats.Skipped += pullStats.Skipped

		if !dryRun {
			fmt.Printf("Pulled %d tasks (%d created, %d updated)\n",
				result.Stats.Pulled, pullStats.Created, pullStats.Updated)
		}
	}

	if push {
		if dryRun {
			fmt.Println("-> [DRY RUN] Would push issues to Vikunja")
		} else {
			fmt.Println("-> Pushing issues to Vikunja...")
		}

		pushStats, err := doPushToVikunja(ctx, dryRun, createOnly, updateRefs,
			forceUpdateIDs, skipUpdateIDs, typeFilters, excludeTypes)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			fmt.Fprintf(os.Stderr, "Error pushing to Vikunja: %v\n", err)
			os.Exit(1)
		}

		result.Stats.Pushed = pushStats.Created + pushStats.Updated
		result.Stats.Created += pushStats.Created
		result.Stats.Updated += pushStats.Updated
		result.Stats.Skipped += pushStats.Skipped
		result.Stats.Errors += pushStats.Errors

		if !dryRun {
			fmt.Printf("Pushed %d issues (%d created, %d updated)\n",
				result.Stats.Pushed, pushStats.Created, pushStats.Updated)
		}
	}

	if result.Success && !dryRun {
		fmt.Println("Sync completed successfully")
	}
}

func runVikunjaStatus(cmd *cobra.Command, args []string) {
	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: database not available: %v\n", err)
		os.Exit(1)
	}

	ctx := rootCtx

	fmt.Println("Vikunja Sync Status")
	fmt.Println("===================")

	// Check configuration
	apiURL, urlSource := getVikunjaConfig(ctx, "vikunja.api_url")
	apiToken, tokenSource := getVikunjaConfig(ctx, "vikunja.api_token")

	if apiURL != "" {
		fmt.Printf("API URL: %s (%s)\n", apiURL, urlSource)
	} else {
		fmt.Println("API URL: not configured")
	}

	if apiToken != "" {
		fmt.Printf("API Token: ***configured*** (%s)\n", tokenSource)
	} else {
		fmt.Println("API Token: not configured")
	}

	if store != nil {
		if projectID, _ := store.GetConfig(ctx, "vikunja.project_id"); projectID != "" {
			fmt.Printf("Project ID: %s\n", projectID)
		}
		if viewID, _ := store.GetConfig(ctx, "vikunja.view_id"); viewID != "" {
			fmt.Printf("View ID: %s\n", viewID)
		}
		if lastSync, _ := store.GetConfig(ctx, "vikunja.last_sync"); lastSync != "" {
			fmt.Printf("Last Sync: %s\n", lastSync)
		} else {
			fmt.Println("Last Sync: never")
		}
	}
}

func runVikunjaProjects(cmd *cobra.Command, args []string) {
	if err := validateVikunjaConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := rootCtx
	client, err := getVikunjaClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	projects, err := client.FetchProjects(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching projects: %v\n", err)
		os.Exit(1)
	}

	if len(projects) == 0 {
		fmt.Println("No projects found")
		return
	}

	fmt.Println("Available Vikunja Projects")
	fmt.Println("==========================")
	for _, project := range projects {
		fmt.Printf("\nProject: %s (ID: %d)\n", project.Title, project.ID)
		if project.Identifier != "" {
			fmt.Printf("  Identifier: %s\n", project.Identifier)
		}
		if project.Description != "" {
			fmt.Printf("  Description: %s\n", project.Description)
		}
		if len(project.Views) > 0 {
			fmt.Println("  Views:")
			for _, view := range project.Views {
				fmt.Printf("    - %s (ID: %d, Type: %s)\n", view.Title, view.ID, view.ViewKind)
			}
		}
	}

	fmt.Println("\nTo configure sync, run:")
	fmt.Println("  bd config set vikunja.project_id <PROJECT_ID>")
	fmt.Println("  bd config set vikunja.view_id <VIEW_ID>")
}
