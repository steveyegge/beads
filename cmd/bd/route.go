package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/rpc"
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

	// Check for existing route beads to avoid duplicates
	requireDaemon("route migrate")
	existingRoutes := make(map[string]bool)

	// Query existing route beads via daemon
	listArgs := &rpc.ListArgs{
		IssueType: "route",
		Status:    "open",
	}
	resp, err := daemonClient.List(listArgs)
	if err == nil {
		var issues []*types.IssueWithCounts
		if json.Unmarshal(resp.Data, &issues) == nil {
			for _, iwc := range issues {
				route := routing.ParseRouteFromTitle(iwc.Issue.Title)
				if route.Prefix != "" {
					existingRoutes[route.Prefix] = true
				}
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

		// Create route bead - use daemon if available
		title := fmt.Sprintf("%s → %s", r.Prefix, r.Path)
		description := fmt.Sprintf("Route for prefix %s to path %s", r.Prefix, r.Path)

		// Create route bead via daemon RPC
		var createdID string
		createArgs := &rpc.CreateArgs{
			Title:       title,
			Description: description,
			IssueType:   "route",
			Priority:    2,
		}
		createResp, err := daemonClient.Create(createArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error creating route bead for %s: %v\n", r.Prefix, err)
			continue
		}
		var createResult struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(createResp.Data, &createResult); err != nil {
			fmt.Fprintf(os.Stderr, "  Error parsing response for %s: %v\n", r.Prefix, err)
			continue
		}
		createdID = createResult.ID

		fmt.Printf("  Created %s: %s\n", createdID, title)
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
