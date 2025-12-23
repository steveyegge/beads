package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage/sqlite"
)

var repoCmd = &cobra.Command{
	Use:     "repo",
	GroupID: "advanced",
	Short:   "Manage multiple repository configuration",
	Long: `Configure and manage multiple repository support for multi-repo hydration.

Multi-repo support allows hydrating issues from multiple beads repositories
into a single database for unified cross-repo issue tracking.

Configuration is stored in .beads/config.yaml under the 'repos' section:

  repos:
    primary: "."
    additional:
      - ~/beads-planning
      - ~/work-repo

Examples:
  bd repo add ~/beads-planning       # Add planning repo
  bd repo add ../other-repo          # Add relative path repo
  bd repo list                       # Show all configured repos
  bd repo remove ~/beads-planning    # Remove by path
  bd repo sync                       # Sync from all configured repos`,
}

var repoAddCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Add an additional repository to sync",
	Long: `Add a repository path to the repos.additional list in config.yaml.

The path should point to a directory containing a .beads folder.
Paths can be absolute or relative (they are stored as-is).

This modifies .beads/config.yaml, which is version-controlled and
shared across all clones of this repository.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoPath := args[0]

		// Expand ~ to home directory for validation and display
		expandedPath := repoPath
		if len(repoPath) > 0 && repoPath[0] == '~' {
			home, err := os.UserHomeDir()
			if err == nil {
				expandedPath = filepath.Join(home, repoPath[1:])
			}
		}

		// Validate the repo path exists and has .beads
		beadsDir := filepath.Join(expandedPath, ".beads")
		if _, err := os.Stat(beadsDir); os.IsNotExist(err) {
			return fmt.Errorf("no .beads directory found at %s - is this a beads repository?", expandedPath)
		}

		// Find config.yaml
		configPath, err := config.FindConfigYAMLPath()
		if err != nil {
			return fmt.Errorf("failed to find config.yaml: %w", err)
		}

		// Add the repo (use original path to preserve ~ etc.)
		if err := config.AddRepo(configPath, repoPath); err != nil {
			return fmt.Errorf("failed to add repository: %w", err)
		}

		if jsonOutput {
			result := map[string]interface{}{
				"added": true,
				"path":  repoPath,
			}
			return json.NewEncoder(os.Stdout).Encode(result)
		}

		fmt.Printf("Added repository: %s\n", repoPath)
		fmt.Printf("Run 'bd repo sync' to hydrate issues from this repository.\n")
		return nil
	},
}

var repoRemoveCmd = &cobra.Command{
	Use:   "remove <path>",
	Short: "Remove a repository from sync configuration",
	Long: `Remove a repository path from the repos.additional list in config.yaml.

The path must exactly match what was added (e.g., if you added "~/foo",
you must remove "~/foo", not "/home/user/foo").

This command also removes any previously-hydrated issues from the database
that came from the removed repository.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoPath := args[0]

		// Ensure we have direct database access for cleanup
		if err := ensureDirectMode("repo remove requires direct database access"); err != nil {
			return err
		}

		ctx := rootCtx

		// Delete issues from the removed repo before removing from config
		// The source_repo field uses the original path (e.g., "~/foo")
		deletedCount := 0
		if sqliteStore, ok := store.(*sqlite.SQLiteStorage); ok {
			count, err := sqliteStore.DeleteIssuesBySourceRepo(ctx, repoPath)
			if err != nil {
				return fmt.Errorf("failed to delete issues from repo: %w", err)
			}
			deletedCount = count

			// Also clear the mtime cache entry
			if err := sqliteStore.ClearRepoMtime(ctx, repoPath); err != nil {
				// Non-fatal: just log a warning
				fmt.Fprintf(os.Stderr, "Warning: failed to clear mtime cache: %v\n", err)
			}
		}

		// Find config.yaml
		configPath, err := config.FindConfigYAMLPath()
		if err != nil {
			return fmt.Errorf("failed to find config.yaml: %w", err)
		}

		// Remove the repo from config
		if err := config.RemoveRepo(configPath, repoPath); err != nil {
			return fmt.Errorf("failed to remove repository: %w", err)
		}

		if jsonOutput {
			result := map[string]interface{}{
				"removed":        true,
				"path":           repoPath,
				"issues_deleted": deletedCount,
			}
			return json.NewEncoder(os.Stdout).Encode(result)
		}

		fmt.Printf("Removed repository: %s\n", repoPath)
		if deletedCount > 0 {
			fmt.Printf("Deleted %d issue(s) from the database\n", deletedCount)
		}
		return nil
	},
}

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured repositories",
	Long: `List all repositories configured in .beads/config.yaml.

Shows the primary repository (always ".") and any additional
repositories configured for hydration.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Find config.yaml
		configPath, err := config.FindConfigYAMLPath()
		if err != nil {
			return fmt.Errorf("failed to find config.yaml: %w", err)
		}

		// Get repos from YAML
		repos, err := config.ListRepos(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if jsonOutput {
			primary := repos.Primary
			if primary == "" {
				primary = "."
			}
			result := map[string]interface{}{
				"primary":    primary,
				"additional": repos.Additional,
			}
			return json.NewEncoder(os.Stdout).Encode(result)
		}

		primary := repos.Primary
		if primary == "" {
			primary = "."
		}
		fmt.Printf("Primary repository: %s\n", primary)
		if len(repos.Additional) == 0 {
			fmt.Println("No additional repositories configured")
		} else {
			fmt.Println("\nAdditional repositories:")
			for _, path := range repos.Additional {
				fmt.Printf("  - %s\n", path)
			}
		}
		return nil
	},
}

var repoSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Manually trigger multi-repo sync",
	Long: `Trigger synchronization from all configured repositories.

This hydrates issues from all repos in repos.additional into the
local database, then exports any local changes back to JSONL.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureDirectMode("repo sync requires direct database access"); err != nil {
			return err
		}

		ctx := rootCtx

		// Import from all repos
		jsonlPath := findJSONLPath()
		if err := importToJSONLWithStore(ctx, store, jsonlPath); err != nil {
			return fmt.Errorf("import failed: %w", err)
		}

		// Export to all repos
		if err := exportToJSONLWithStore(ctx, store, jsonlPath); err != nil {
			return fmt.Errorf("export failed: %w", err)
		}

		if jsonOutput {
			result := map[string]interface{}{
				"synced": true,
			}
			return json.NewEncoder(os.Stdout).Encode(result)
		}

		fmt.Println("Multi-repo sync complete")
		return nil
	},
}

func init() {
	repoCmd.AddCommand(repoAddCmd)
	repoCmd.AddCommand(repoRemoveCmd)
	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoSyncCmd)

	repoAddCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output JSON")
	repoRemoveCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output JSON")
	repoListCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output JSON")
	repoSyncCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output JSON")

	rootCmd.AddCommand(repoCmd)
}
