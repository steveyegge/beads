package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	gh "github.com/steveyegge/beads/internal/github"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// GitHubConfig holds GitHub connection configuration.
type GitHubConfig struct {
	Token string // Personal access token
	Owner string // Repository owner (user or org)
	Repo  string // Repository name
}

// githubCmd is the root command for GitHub operations.
var githubCmd = &cobra.Command{
	Use:   "github",
	Short: "GitHub integration commands",
	Long: `Commands for syncing issues between beads and GitHub.

Configuration can be set via 'bd config' or environment variables:
  github.owner / GITHUB_OWNER     - Repository owner (user or org)
  github.repo / GITHUB_REPO       - Repository name
  github.token / GITHUB_TOKEN     - Personal access token`,
}

// githubSyncCmd synchronizes issues between beads and GitHub.
var githubSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync issues with GitHub",
	Long: `Synchronize issues between beads and GitHub.

By default, performs bidirectional sync:
- Pulls new/updated issues from GitHub to beads
- Pushes local beads issues to GitHub

Use --pull-only or --push-only to limit direction.`,
	RunE: runGitHubSync,
}

// githubStatusCmd displays GitHub configuration and sync status.
var githubStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show GitHub sync status",
	Long:  `Display current GitHub configuration and sync status.`,
	RunE:  runGitHubStatus,
}

// githubReposCmd lists accessible GitHub repositories.
var githubReposCmd = &cobra.Command{
	Use:   "repos",
	Short: "List accessible GitHub repositories",
	Long:  `List GitHub repositories that the configured token has access to.`,
	RunE:  runGitHubRepos,
}

var (
	githubSyncDryRun   bool
	githubSyncPullOnly bool
	githubSyncPushOnly bool
	githubPreferLocal  bool
	githubPreferGitHub bool
	githubPreferNewer  bool
)

func init() {
	githubCmd.AddCommand(githubSyncCmd)
	githubCmd.AddCommand(githubStatusCmd)
	githubCmd.AddCommand(githubReposCmd)

	githubSyncCmd.Flags().BoolVar(&githubSyncDryRun, "dry-run", false, "Show what would be synced without making changes")
	githubSyncCmd.Flags().BoolVar(&githubSyncPullOnly, "pull-only", false, "Only pull issues from GitHub")
	githubSyncCmd.Flags().BoolVar(&githubSyncPushOnly, "push-only", false, "Only push issues to GitHub")

	githubSyncCmd.Flags().BoolVar(&githubPreferLocal, "prefer-local", false, "On conflict, keep local beads version")
	githubSyncCmd.Flags().BoolVar(&githubPreferGitHub, "prefer-github", false, "On conflict, use GitHub version")
	githubSyncCmd.Flags().BoolVar(&githubPreferNewer, "prefer-newer", false, "On conflict, use most recent version (default)")

	rootCmd.AddCommand(githubCmd)
}

// getGitHubConfig returns GitHub configuration from bd config or environment.
func getGitHubConfig() GitHubConfig {
	ctx := context.Background()
	config := GitHubConfig{}

	config.Token = getGitHubConfigValue(ctx, "github.token")
	config.Owner = getGitHubConfigValue(ctx, "github.owner")
	config.Repo = getGitHubConfigValue(ctx, "github.repo")

	return config
}

// getGitHubConfigValue reads a GitHub configuration value from store or environment.
func getGitHubConfigValue(ctx context.Context, key string) string {
	if store != nil {
		value, _ := store.GetConfig(ctx, key)
		if value != "" {
			return value
		}
	} else if dbPath != "" {
		tempStore, err := sqlite.NewWithTimeout(ctx, dbPath, 5*time.Second)
		if err == nil {
			defer func() { _ = tempStore.Close() }()
			value, _ := tempStore.GetConfig(ctx, key)
			if value != "" {
				return value
			}
		}
	}

	envKey := githubConfigToEnvVar(key)
	if envKey != "" {
		if value := os.Getenv(envKey); value != "" {
			return value
		}
	}

	return ""
}

// githubConfigToEnvVar maps GitHub config keys to their environment variable names.
func githubConfigToEnvVar(key string) string {
	switch key {
	case "github.token":
		return "GITHUB_TOKEN"
	case "github.owner":
		return "GITHUB_OWNER"
	case "github.repo":
		return "GITHUB_REPO"
	default:
		return ""
	}
}

// validateGitHubConfig checks that required configuration is present.
func validateGitHubConfig(config GitHubConfig) error {
	if config.Token == "" {
		return fmt.Errorf("github.token is not configured. Set via 'bd config set github.token <token>' or GITHUB_TOKEN environment variable")
	}
	if config.Owner == "" {
		return fmt.Errorf("github.owner is not configured. Set via 'bd config set github.owner <owner>' or GITHUB_OWNER environment variable")
	}
	if config.Repo == "" {
		return fmt.Errorf("github.repo is not configured. Set via 'bd config set github.repo <repo>' or GITHUB_REPO environment variable")
	}
	return nil
}

// maskGitHubToken masks a token for safe display.
func maskGitHubToken(token string) string {
	if token == "" {
		return "(not set)"
	}
	if len(token) <= 4 {
		return "****"
	}
	return token[:4] + "****"
}

// getGitHubClient creates a GitHub client from the current configuration.
func getGitHubClient(config GitHubConfig) *gh.Client {
	return gh.NewClient(config.Token, config.Owner, config.Repo)
}

// runGitHubStatus implements the github status command.
func runGitHubStatus(cmd *cobra.Command, args []string) error {
	config := getGitHubConfig()

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintln(out, "GitHub Configuration")
	_, _ = fmt.Fprintln(out, "====================")
	_, _ = fmt.Fprintf(out, "Owner: %s\n", config.Owner)
	_, _ = fmt.Fprintf(out, "Repo:  %s\n", config.Repo)
	_, _ = fmt.Fprintf(out, "Token: %s\n", maskGitHubToken(config.Token))

	if err := validateGitHubConfig(config); err != nil {
		_, _ = fmt.Fprintf(out, "\nStatus: ❌ Not configured\n")
		_, _ = fmt.Fprintf(out, "Error: %v\n", err)
		return nil
	}

	_, _ = fmt.Fprintf(out, "\nStatus: ✓ Configured\n")
	return nil
}

// runGitHubRepos implements the github repos command.
func runGitHubRepos(cmd *cobra.Command, args []string) error {
	config := getGitHubConfig()
	if config.Token == "" {
		return fmt.Errorf("github.token is required to list repositories")
	}

	out := cmd.OutOrStdout()
	client := getGitHubClient(config)
	ctx := context.Background()

	repos, err := client.ListRepositories(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch repositories: %w", err)
	}

	_, _ = fmt.Fprintln(out, "Accessible GitHub Repositories")
	_, _ = fmt.Fprintln(out, "==============================")
	for _, r := range repos {
		_, _ = fmt.Fprintf(out, "Name: %s\n", r.FullName)
		_, _ = fmt.Fprintf(out, "  URL:     %s\n", r.HTMLURL)
		if r.Description != "" {
			_, _ = fmt.Fprintf(out, "  Desc:    %s\n", r.Description)
		}
		_, _ = fmt.Fprintf(out, "  Private: %v\n", r.Private)
		_, _ = fmt.Fprintln(out)
	}

	if len(repos) == 0 {
		_, _ = fmt.Fprintln(out, "No repositories found (or no access)")
	}

	return nil
}

// runGitHubSync implements the github sync command.
func runGitHubSync(cmd *cobra.Command, args []string) error {
	config := getGitHubConfig()
	if err := validateGitHubConfig(config); err != nil {
		return err
	}

	if !githubSyncDryRun {
		CheckReadonly("github sync")
	}

	if githubSyncPullOnly && githubSyncPushOnly {
		return fmt.Errorf("cannot use both --pull-only and --push-only")
	}

	conflictStrategy, err := getGitHubConflictStrategy(githubPreferLocal, githubPreferGitHub, githubPreferNewer)
	if err != nil {
		return fmt.Errorf("%w (--prefer-local, --prefer-github, --prefer-newer)", err)
	}

	if err := ensureStoreActive(); err != nil {
		return fmt.Errorf("database not available: %w", err)
	}

	out := cmd.OutOrStdout()
	client := getGitHubClient(config)
	ctx := context.Background()
	mappingConfig := gh.DefaultMappingConfig()

	syncCtx := NewSyncContext()
	syncCtx.SetStore(store)
	syncCtx.SetActor(actor)
	syncCtx.SetDBPath(dbPath)

	if githubSyncDryRun {
		_, _ = fmt.Fprintln(out, "Dry run mode - no changes will be made")
		_, _ = fmt.Fprintln(out)
	}

	pull := !githubSyncPushOnly
	push := !githubSyncPullOnly

	result := &gh.SyncResult{Success: true}

	// Pull from GitHub
	if pull {
		if githubSyncDryRun {
			_, _ = fmt.Fprintln(out, "→ [DRY RUN] Would pull issues from GitHub")
		} else {
			_, _ = fmt.Fprintln(out, "→ Pulling issues from GitHub...")
		}

		pullStats, err := doPullFromGitHubWithContext(ctx, syncCtx, client, config.Owner, config.Repo, mappingConfig, githubSyncDryRun, "all", nil)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			_, _ = fmt.Fprintf(out, "Error pulling from GitHub: %v\n", err)
			return err
		}

		result.Stats.Pulled = pullStats.Created + pullStats.Updated
		result.Stats.Created += pullStats.Created
		result.Stats.Updated += pullStats.Updated
		result.Stats.Skipped += pullStats.Skipped

		if !githubSyncDryRun {
			_, _ = fmt.Fprintf(out, "✓ Pulled %d issues (%d created, %d updated)\n",
				result.Stats.Pulled, pullStats.Created, pullStats.Updated)
		}
	}

	// Detect conflicts before push
	var conflicts []gh.Conflict
	skipUpdateIDs := make(map[string]bool)
	forceUpdateIDs := make(map[string]bool)

	if pull && push && !githubSyncDryRun {
		var localIssues []*types.Issue
		if syncCtx.Store() != nil {
			var err error
			localIssues, err = syncCtx.Store().SearchIssues(ctx, "", types.IssueFilter{})
			if err != nil {
				_, _ = fmt.Fprintf(out, "Warning: failed to get local issues for conflict detection: %v\n", err)
			} else {
				conflicts, err = detectGitHubConflictsWithContext(ctx, syncCtx, client, localIssues)
				if err != nil {
					_, _ = fmt.Fprintf(out, "Warning: failed to detect conflicts: %v\n", err)
				} else if len(conflicts) > 0 {
					for _, c := range conflicts {
						switch conflictStrategy {
						case GitHubConflictPreferLocal:
							forceUpdateIDs[c.IssueID] = true
						case GitHubConflictPreferGitHub:
							skipUpdateIDs[c.IssueID] = true
						case GitHubConflictPreferNewer:
							if c.LocalUpdated.After(c.GitHubUpdated) {
								forceUpdateIDs[c.IssueID] = true
							} else {
								skipUpdateIDs[c.IssueID] = true
							}
						}
					}
					_, _ = fmt.Fprintf(out, "→ Detected %d conflicts (strategy: %s)\n", len(conflicts), conflictStrategy)
				}
			}
		}
	}

	// Push to GitHub
	if push {
		if githubSyncDryRun {
			_, _ = fmt.Fprintln(out, "→ [DRY RUN] Would push issues to GitHub")
		} else {
			_, _ = fmt.Fprintln(out, "→ Pushing issues to GitHub...")
		}

		var localIssues []*types.Issue
		if syncCtx.Store() != nil {
			var err error
			localIssues, err = syncCtx.Store().SearchIssues(ctx, "", types.IssueFilter{})
			if err != nil {
				return fmt.Errorf("failed to get local issues: %w", err)
			}
		}

		pushStats, err := doPushToGitHubWithContext(ctx, syncCtx, client, config.Owner, config.Repo, mappingConfig, localIssues, githubSyncDryRun, false, forceUpdateIDs, skipUpdateIDs)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			_, _ = fmt.Fprintf(out, "Error pushing to GitHub: %v\n", err)
			return err
		}

		result.Stats.Pushed = pushStats.Created + pushStats.Updated
		result.Stats.Created += pushStats.Created
		result.Stats.Updated += pushStats.Updated
		result.Stats.Skipped += pushStats.Skipped

		if !githubSyncDryRun {
			_, _ = fmt.Fprintf(out, "✓ Pushed %d issues (%d created, %d updated)\n",
				result.Stats.Pushed, pushStats.Created, pushStats.Updated)
		}
	}

	// Resolve conflicts where GitHub won
	if len(skipUpdateIDs) > 0 && !githubSyncDryRun {
		_, _ = fmt.Fprintf(out, "→ Updating %d local issues from GitHub...\n", len(skipUpdateIDs))
		var conflictsToResolve []gh.Conflict
		for _, c := range conflicts {
			if skipUpdateIDs[c.IssueID] {
				conflictsToResolve = append(conflictsToResolve, c)
			}
		}
		if err := resolveGitHubConflictsWithContext(ctx, syncCtx, client, config.Owner, config.Repo, mappingConfig, conflictsToResolve, conflictStrategy); err != nil {
			_, _ = fmt.Fprintf(out, "Warning: failed to resolve some conflicts: %v\n", err)
		} else {
			_, _ = fmt.Fprintf(out, "✓ Updated %d local issues\n", len(conflictsToResolve))
		}
		result.Stats.Conflicts = len(conflicts)
	}

	if githubSyncDryRun {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "Run without --dry-run to apply changes")
	}

	return nil
}

// parseGitHubSourceSystem parses a source system string like "github:owner/repo:42".
// Returns owner, repo, number, and ok (whether it's a valid GitHub source).
func parseGitHubSourceSystem(sourceSystem string) (owner, repo string, number int, ok bool) {
	if !strings.HasPrefix(sourceSystem, "github:") {
		return "", "", 0, false
	}

	parts := strings.Split(sourceSystem, ":")
	if len(parts) != 3 {
		return "", "", 0, false
	}

	ownerRepo := strings.SplitN(parts[1], "/", 2)
	if len(ownerRepo) != 2 {
		return "", "", 0, false
	}

	n, err := fmt.Sscanf(parts[2], "%d", &number)
	if err != nil || n != 1 {
		return "", "", 0, false
	}

	return ownerRepo[0], ownerRepo[1], number, true
}
