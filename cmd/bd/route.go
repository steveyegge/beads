package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/routing"
	"github.com/steveyegge/beads/internal/types"
)

var routeCmd = &cobra.Command{
	Use:   "route",
	Short: "Manage route beads for prefix-based routing",
	Long:  `Manage route beads that map issue ID prefixes to rig paths.`,
}

var routeMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate routes.jsonl to route beads",
	Long: `Convert existing routes.jsonl entries to route beads.

This command reads routes.jsonl from the current beads directory (or town level)
and creates a route bead for each entry that doesn't already exist.

The migration is idempotent - running it multiple times won't create duplicates.`,
	RunE: runRouteMigrate,
}

var (
	routeMigrateDryRun bool
	routeMigrateBackup bool
	routeMigrateDelete bool
)

func init() {
	rootCmd.AddCommand(routeCmd)
	routeCmd.AddCommand(routeMigrateCmd)

	routeMigrateCmd.Flags().BoolVar(&routeMigrateDryRun, "dry-run", false, "Show what would be migrated without making changes")
	routeMigrateCmd.Flags().BoolVar(&routeMigrateBackup, "backup", true, "Create backup of routes.jsonl before migration")
	routeMigrateCmd.Flags().BoolVar(&routeMigrateDelete, "delete", false, "Delete routes.jsonl after successful migration")
}

func runRouteMigrate(cmd *cobra.Command, args []string) error {
	// Find beads directory
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return fmt.Errorf("no .beads directory found")
	}

	// Also check town-level routes
	townRoot := routing.FindTownRoot(filepath.Dir(beadsDir))
	var townBeadsDir string
	if townRoot != "" {
		townBeadsDir = filepath.Join(townRoot, ".beads")
	}

	// Collect routes from both locations
	var allRoutes []routeWithSource

	// Local routes
	localRoutes, err := routing.LoadRoutesFromFile(beadsDir)
	if err != nil {
		return fmt.Errorf("failed to load local routes: %w", err)
	}
	for _, r := range localRoutes {
		allRoutes = append(allRoutes, routeWithSource{Route: r, Source: beadsDir})
	}

	// Town routes (if different from local)
	if townBeadsDir != "" && townBeadsDir != beadsDir {
		townRoutes, err := routing.LoadRoutesFromFile(townBeadsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load town routes: %v\n", err)
		} else {
			for _, r := range townRoutes {
				allRoutes = append(allRoutes, routeWithSource{Route: r, Source: townBeadsDir})
			}
		}
	}

	if len(allRoutes) == 0 {
		fmt.Println("No routes found in routes.jsonl")
		return nil
	}

	fmt.Printf("Found %d routes to migrate\n", len(allRoutes))

	if routeMigrateDryRun {
		fmt.Println("\nDry run - no changes will be made:")
		for _, r := range allRoutes {
			fmt.Printf("  %s → %s (from %s)\n", r.Prefix, r.Path, r.Source)
		}
		return nil
	}

	// Get storage for creating route beads
	store := getStore()
	if store == nil {
		return fmt.Errorf("storage not available - ensure daemon is running")
	}

	// Check for existing route beads to avoid duplicates
	ctx := cmd.Context()
	existingRoutes := make(map[string]bool)

	// Query existing route beads
	filter := types.IssueFilter{}
	issueType := types.IssueType("route")
	filter.IssueType = &issueType

	existing, err := store.SearchIssues(ctx, "", filter)
	if err == nil {
		for _, issue := range existing {
			// Extract prefix from title
			route := routing.ParseRouteFromTitle(issue.Title)
			if route.Prefix != "" {
				existingRoutes[route.Prefix] = true
			}
		}
	}

	// Backup routes.jsonl files if requested
	if routeMigrateBackup {
		sources := make(map[string]bool)
		for _, r := range allRoutes {
			sources[r.Source] = true
		}
		for source := range sources {
			routesPath := filepath.Join(source, routing.RoutesFileName)
			backupPath := routesPath + ".bak"
			if _, err := os.Stat(routesPath); err == nil {
				data, err := os.ReadFile(routesPath)
				if err != nil {
					return fmt.Errorf("failed to read %s for backup: %w", routesPath, err)
				}
				if err := os.WriteFile(backupPath, data, 0644); err != nil {
					return fmt.Errorf("failed to write backup %s: %w", backupPath, err)
				}
				fmt.Printf("Created backup: %s\n", backupPath)
			}
		}
	}

	// Create route beads
	var created, skipped int
	for _, r := range allRoutes {
		if existingRoutes[r.Prefix] {
			fmt.Printf("  Skipped %s (already exists)\n", r.Prefix)
			skipped++
			continue
		}

		// Create route bead
		title := fmt.Sprintf("%s → %s", r.Prefix, r.Path)
		issue := &types.Issue{
			Title:       title,
			Description: fmt.Sprintf("Route for prefix %s to path %s", r.Prefix, r.Path),
			IssueType:   types.IssueType("route"),
			Status:      types.StatusOpen,
			Priority:    2,
		}

		if err := store.CreateIssue(ctx, issue, getActor()); err != nil {
			fmt.Fprintf(os.Stderr, "  Error creating route bead for %s: %v\n", r.Prefix, err)
			continue
		}

		fmt.Printf("  Created %s: %s\n", issue.ID, title)
		existingRoutes[r.Prefix] = true
		created++
	}

	fmt.Printf("\nMigration complete: %d created, %d skipped\n", created, skipped)

	// Delete routes.jsonl files if requested
	if routeMigrateDelete && created > 0 {
		sources := make(map[string]bool)
		for _, r := range allRoutes {
			sources[r.Source] = true
		}
		for source := range sources {
			routesPath := filepath.Join(source, routing.RoutesFileName)
			if err := os.Remove(routesPath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to delete %s: %v\n", routesPath, err)
			} else {
				fmt.Printf("Deleted: %s\n", routesPath)
			}
		}
	}

	return nil
}

type routeWithSource struct {
	routing.Route
	Source string
}
