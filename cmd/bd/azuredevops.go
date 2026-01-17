package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/tracker/azuredevops"
	"github.com/steveyegge/beads/internal/types"
)

// azuredevopsCmd is the root command for Azure DevOps integration.
var azuredevopsCmd = &cobra.Command{
	Use:     "azuredevops",
	Aliases: []string{"ado"},
	GroupID: "advanced",
	Short:   "Azure DevOps integration commands",
	Long: `Synchronize issues between beads and Azure DevOps.

Configuration:
  bd config set azuredevops.organization "myorg"
  bd config set azuredevops.project "myproject"
  bd config set azuredevops.pat "PERSONAL_ACCESS_TOKEN"

Environment variables (alternative to config):
  AZURE_DEVOPS_PAT           - Azure DevOps Personal Access Token
  AZURE_DEVOPS_ORGANIZATION  - Azure DevOps organization name
  AZURE_DEVOPS_PROJECT       - Azure DevOps project name

Examples:
  bd azuredevops sync --pull         # Import work items from Azure DevOps
  bd azuredevops sync --push         # Export issues to Azure DevOps
  bd azuredevops sync                # Bidirectional sync (pull then push)
  bd azuredevops sync --dry-run      # Preview sync without changes
  bd azuredevops status              # Show sync status
  bd ado status                      # Short alias`,
}

// azuredevopsSyncCmd handles synchronization with Azure DevOps.
var azuredevopsSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronize issues with Azure DevOps",
	Long: `Synchronize issues between beads and Azure DevOps.

Modes:
  --pull         Import work items from Azure DevOps into beads
  --push         Export issues from beads to Azure DevOps
  (no flags)     Bidirectional sync: pull then push, with conflict resolution

Conflict Resolution:
  By default, newer timestamp wins. Override with:
  --prefer-local   Always prefer local beads version
  --prefer-ado     Always prefer Azure DevOps version

Examples:
  bd azuredevops sync --pull                # Import from Azure DevOps
  bd azuredevops sync --push --create-only  # Push new issues only
  bd azuredevops sync --dry-run             # Preview without changes
  bd azuredevops sync --prefer-local        # Bidirectional, local wins
  bd ado sync --pull                        # Using the short alias`,
	Run: runAzureDevOpsSync,
}

// azuredevopsStatusCmd shows the current sync status.
var azuredevopsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Azure DevOps sync status",
	Long: `Show the current Azure DevOps sync status, including:
  - Last sync timestamp
  - Configuration status
  - Number of issues with Azure DevOps links
  - Issues pending push (no external_ref)`,
	Run: runAzureDevOpsStatus,
}

// azuredevopsProjectsCmd lists available projects.
var azuredevopsProjectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List available Azure DevOps projects",
	Long: `List all projects accessible with your Azure DevOps PAT.

Use this to find the project name needed for configuration.

Example:
  bd azuredevops projects
  bd config set azuredevops.project "MyProject"`,
	Run: runAzureDevOpsProjects,
}

func init() {
	azuredevopsSyncCmd.Flags().Bool("pull", false, "Pull work items from Azure DevOps")
	azuredevopsSyncCmd.Flags().Bool("push", false, "Push issues to Azure DevOps")
	azuredevopsSyncCmd.Flags().Bool("dry-run", false, "Preview sync without making changes")
	azuredevopsSyncCmd.Flags().Bool("prefer-local", false, "Prefer local version on conflicts")
	azuredevopsSyncCmd.Flags().Bool("prefer-ado", false, "Prefer Azure DevOps version on conflicts")
	azuredevopsSyncCmd.Flags().Bool("create-only", false, "Only create new issues, don't update existing")
	azuredevopsSyncCmd.Flags().Bool("update-refs", true, "Update external_ref after creating Azure DevOps work items")
	azuredevopsSyncCmd.Flags().String("state", "all", "Issue state to sync: open, closed, all")

	azuredevopsCmd.AddCommand(azuredevopsSyncCmd)
	azuredevopsCmd.AddCommand(azuredevopsStatusCmd)
	azuredevopsCmd.AddCommand(azuredevopsProjectsCmd)
	rootCmd.AddCommand(azuredevopsCmd)
}

func runAzureDevOpsSync(cmd *cobra.Command, args []string) {
	// 1. Parse flags
	pull, _ := cmd.Flags().GetBool("pull")
	push, _ := cmd.Flags().GetBool("push")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	preferLocal, _ := cmd.Flags().GetBool("prefer-local")
	preferADO, _ := cmd.Flags().GetBool("prefer-ado")
	createOnly, _ := cmd.Flags().GetBool("create-only")
	updateRefs, _ := cmd.Flags().GetBool("update-refs")
	state, _ := cmd.Flags().GetString("state")

	// 2. Validation
	if !dryRun {
		CheckReadonly("azuredevops sync")
	}

	if preferLocal && preferADO {
		fmt.Fprintf(os.Stderr, "Error: cannot use both --prefer-local and --prefer-ado\n")
		os.Exit(1)
	}

	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: database not available: %v\n", err)
		os.Exit(1)
	}

	if err := validateAzureDevOpsConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := rootCtx

	// 3. Create tracker and engine using the plugin framework
	adoTracker, err := tracker.NewTracker("azuredevops")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create Azure DevOps tracker: %v\n", err)
		os.Exit(1)
	}

	cfg := tracker.NewConfig(ctx, "azuredevops", newConfigStoreAdapter(store))
	if err := adoTracker.Init(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to initialize Azure DevOps tracker: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = adoTracker.Close() }()

	engine := tracker.NewSyncEngine(adoTracker, cfg, newSyncStoreAdapter(store), actor)
	engine.OnMessage = func(msg string) { fmt.Println("->", msg) }
	engine.OnWarning = func(msg string) { fmt.Fprintln(os.Stderr, "Warning:", msg) }

	// 4. Build SyncOptions from flags
	opts := tracker.SyncOptions{
		Pull:       pull,
		Push:       push,
		DryRun:     dryRun,
		CreateOnly: createOnly,
		UpdateRefs: updateRefs,
		State:      state,
	}
	if preferLocal {
		opts.ConflictResolution = tracker.ConflictResolutionLocal
	} else if preferADO {
		opts.ConflictResolution = tracker.ConflictResolutionExternal
	}

	// 5. Execute sync
	result, err := engine.Sync(ctx, opts)
	if err != nil {
		if jsonOutput {
			outputJSON(result)
		} else {
			fmt.Fprintf(os.Stderr, "Error: sync failed: %v\n", err)
		}
		os.Exit(1)
	}

	// 6. Output results
	printSyncResult(result, dryRun, "Azure DevOps")
}

func runAzureDevOpsStatus(cmd *cobra.Command, args []string) {
	ctx := rootCtx

	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	organization, _ := getAzureDevOpsConfig(ctx, "azuredevops.organization")
	project, _ := getAzureDevOpsConfig(ctx, "azuredevops.project")
	lastSync, _ := store.GetConfig(ctx, "azuredevops.last_sync")

	configured := organization != "" && project != ""

	allIssues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	withADORef := 0
	pendingPush := 0
	for _, issue := range allIssues {
		if issue.ExternalRef != nil && isAzureDevOpsExternalRef(*issue.ExternalRef) {
			withADORef++
		} else if issue.ExternalRef == nil {
			pendingPush++
		}
	}

	if jsonOutput {
		hasPAT := false
		pat, _ := getAzureDevOpsConfig(ctx, "azuredevops.pat")
		if pat != "" {
			hasPAT = true
		}
		outputJSON(map[string]interface{}{
			"configured":   configured,
			"has_pat":      hasPAT,
			"organization": organization,
			"project":      project,
			"last_sync":    lastSync,
			"total_issues": len(allIssues),
			"with_ado_ref": withADORef,
			"pending_push": pendingPush,
		})
		return
	}

	fmt.Println("Azure DevOps Sync Status")
	fmt.Println("========================")
	fmt.Println()

	if !configured {
		fmt.Println("Status: Not configured")
		fmt.Println()
		fmt.Println("To configure Azure DevOps integration:")
		fmt.Println("  bd config set azuredevops.organization \"myorg\"")
		fmt.Println("  bd config set azuredevops.project \"myproject\"")
		fmt.Println("  bd config set azuredevops.pat \"YOUR_PAT\"")
		fmt.Println()
		fmt.Println("Or use environment variables:")
		fmt.Println("  export AZURE_DEVOPS_ORGANIZATION=\"myorg\"")
		fmt.Println("  export AZURE_DEVOPS_PROJECT=\"myproject\"")
		fmt.Println("  export AZURE_DEVOPS_PAT=\"YOUR_PAT\"")
		return
	}

	pat, _ := getAzureDevOpsConfig(ctx, "azuredevops.pat")
	fmt.Printf("Organization: %s\n", organization)
	fmt.Printf("Project:      %s\n", project)
	fmt.Printf("PAT:          %s\n", maskAPIKey(pat))
	if lastSync != "" {
		fmt.Printf("Last Sync:    %s\n", lastSync)
	} else {
		fmt.Println("Last Sync:    Never")
	}
	fmt.Println()
	fmt.Printf("Total Issues: %d\n", len(allIssues))
	fmt.Printf("With ADO:     %d\n", withADORef)
	fmt.Printf("Local Only:   %d\n", pendingPush)

	if pendingPush > 0 {
		fmt.Println()
		fmt.Printf("Run 'bd azuredevops sync --push' to push %d local issue(s) to Azure DevOps\n", pendingPush)
	}
}

func runAzureDevOpsProjects(cmd *cobra.Command, args []string) {
	ctx := rootCtx

	// Validate config - only need PAT and organization for listing projects
	pat, _ := getAzureDevOpsConfig(ctx, "azuredevops.pat")
	if pat == "" {
		fmt.Fprintf(os.Stderr, "Error: Azure DevOps PAT not configured\n")
		fmt.Fprintf(os.Stderr, "Run: bd config set azuredevops.pat \"YOUR_PAT\"\n")
		fmt.Fprintf(os.Stderr, "Or:  export AZURE_DEVOPS_PAT=YOUR_PAT\n")
		os.Exit(1)
	}

	organization, _ := getAzureDevOpsConfig(ctx, "azuredevops.organization")
	if organization == "" {
		fmt.Fprintf(os.Stderr, "Error: Azure DevOps organization not configured\n")
		fmt.Fprintf(os.Stderr, "Run: bd config set azuredevops.organization \"myorg\"\n")
		fmt.Fprintf(os.Stderr, "Or:  export AZURE_DEVOPS_ORGANIZATION=myorg\n")
		os.Exit(1)
	}

	// Create client directly (don't need full tracker for listing projects)
	client := azuredevops.NewClient(organization, "", pat)

	projects, err := client.ListProjects(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching projects: %v\n", err)
		os.Exit(1)
	}

	if len(projects) == 0 {
		fmt.Println("No projects found (check your PAT permissions)")
		return
	}

	if jsonOutput {
		outputJSON(projects)
		return
	}

	fmt.Println("Available Azure DevOps Projects")
	fmt.Println("================================")
	fmt.Println()
	fmt.Printf("%-40s  %-12s  %s\n", "Name (use for azuredevops.project)", "State", "Visibility")
	fmt.Printf("%-40s  %-12s  %s\n", "----------------------------------------", "------------", "----------")
	for _, project := range projects {
		fmt.Printf("%-40s  %-12s  %s\n", project.Name, project.State, project.Visibility)
	}
	fmt.Println()
	fmt.Println("To configure:")
	fmt.Println("  bd config set azuredevops.project \"<Name>\"")
}

// validateAzureDevOpsConfig checks that required Azure DevOps configuration is present.
func validateAzureDevOpsConfig() error {
	if err := ensureStoreActive(); err != nil {
		return fmt.Errorf("database not available: %w", err)
	}

	ctx := rootCtx

	organization, _ := getAzureDevOpsConfig(ctx, "azuredevops.organization")
	if organization == "" {
		return fmt.Errorf("azuredevops.organization not configured\nRun: bd config set azuredevops.organization \"myorg\"\nOr: export AZURE_DEVOPS_ORGANIZATION=myorg")
	}

	project, _ := getAzureDevOpsConfig(ctx, "azuredevops.project")
	if project == "" {
		return fmt.Errorf("azuredevops.project not configured\nRun: bd config set azuredevops.project \"myproject\"\nOr: export AZURE_DEVOPS_PROJECT=myproject")
	}

	pat, _ := getAzureDevOpsConfig(ctx, "azuredevops.pat")
	if pat == "" {
		return fmt.Errorf("Azure DevOps PAT not configured\nRun: bd config set azuredevops.pat \"YOUR_PAT\"\nOr: export AZURE_DEVOPS_PAT=YOUR_PAT")
	}

	return nil
}

// getAzureDevOpsConfig reads an Azure DevOps configuration value, handling both
// project config and environment variables. Returns the value and its source.
// Priority: project config > environment variable.
func getAzureDevOpsConfig(ctx context.Context, key string) (value string, source string) {
	// Try to read from store
	if store != nil {
		value, _ = store.GetConfig(ctx, key)
		if value != "" {
			return value, "project config (bd config)"
		}
	}

	// Fall back to environment variable
	envKey := azureDevOpsConfigToEnvVar(key)
	if envKey != "" {
		value = os.Getenv(envKey)
		if value != "" {
			return value, fmt.Sprintf("environment variable (%s)", envKey)
		}
	}

	return "", ""
}

// azureDevOpsConfigToEnvVar maps Azure DevOps config keys to their environment variable names.
func azureDevOpsConfigToEnvVar(key string) string {
	switch key {
	case "azuredevops.pat":
		return "AZURE_DEVOPS_PAT"
	case "azuredevops.organization":
		return "AZURE_DEVOPS_ORGANIZATION"
	case "azuredevops.project":
		return "AZURE_DEVOPS_PROJECT"
	default:
		return ""
	}
}

// isAzureDevOpsExternalRef checks if an external_ref URL is for Azure DevOps.
func isAzureDevOpsExternalRef(externalRef string) bool {
	// Azure DevOps URLs contain "/_workitems/edit/" or "dev.azure.com"
	return strings.Contains(externalRef, "/_workitems/edit/") || strings.Contains(externalRef, "dev.azure.com")
}
