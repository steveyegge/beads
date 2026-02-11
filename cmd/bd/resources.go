package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/discovery"
	"github.com/steveyegge/beads/internal/resolver"
	"github.com/steveyegge/beads/internal/types"
)


var resourcesCmd = &cobra.Command{
	Use:     "resources",
	Short:   "Manage agents, skills, and models",
	Long:    "Commands for managing available resources (agents, skills, models) and syncing them from external sources.",
	GroupID: GroupMaintenance,
}

var resourcesSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronize resources from configured sources",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		store := getStore()
		if store == nil {
			return fmt.Errorf("storage not initialized")
		}

		// 1. Discover resources from all configured sources
		resources, err := discovery.DiscoverResources(ctx)
		if err != nil {
			return fmt.Errorf("failed to discover resources: %w", err)
		}

		// 2. Sync to database
		// Group by source for efficient sync
		bySource := make(map[string][]*types.Resource)
		for _, r := range resources {
			bySource[r.Source] = append(bySource[r.Source], r)
		}

		// Iterate through all known sources (and any others found)
		// We explicitly check for known sources to ensure we clear them if empty
		knownSources := []string{types.ResourceSourceLocal, types.ResourceSourceLinear}

		// Add any other sources found during discovery
		for src := range bySource {
			found := false
			for _, k := range knownSources {
				if k == src {
					found = true
					break
				}
			}
			if !found {
				knownSources = append(knownSources, src)
			}
		}

		for _, src := range knownSources {
			list, ok := bySource[src]
			if !ok {
				list = []*types.Resource{} // Empty list to clear old resources
			}

			if err := store.SyncResources(ctx, src, list); err != nil {
				return fmt.Errorf("failed to sync resources for source %s: %w", src, err)
			}
			fmt.Printf("Synced %d resources from %s\n", len(list), src)
		}

		return nil
	},
}

var (
	listType   string
	listSource string
	listTags   []string
)

var resourcesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available resources",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		filter := types.ResourceFilter{
			Tags: listTags,
		}
		if listType != "" {
			filter.Type = &listType
		}
		if listSource != "" {
			filter.Source = &listSource
		}

		store := getStore()
		if store == nil {
			return fmt.Errorf("storage not initialized")
		}
		resources, err := store.ListResources(ctx, filter)
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "TYPE\tIDENTIFIER\tNAME\tSOURCE\tTAGS")
		for _, r := range resources {
			tags := ""
			if len(r.Tags) > 0 {
				tags = fmt.Sprintf("%v", r.Tags)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Type, r.Identifier, r.Name, r.Source, tags)
		}
		w.Flush()

		return nil
	},
}

var (
	matchTags    []string
	matchProfile string
	matchType    string
)

var resourcesMatchCmd = &cobra.Command{
	Use:   "match",
	Short: "Test resource matching logic",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		store := getStore()
		if store == nil {
			return fmt.Errorf("storage not initialized")
		}

		// 1. Fetch all candidate resources (or filter by type if provided)
		filter := types.ResourceFilter{}
		if matchType != "" {
			filter.Type = &matchType
		}
		resources, err := store.ListResources(ctx, filter)
		if err != nil {
			return err
		}

		// 2. Run resolver
		req := resolver.Requirement{
			Type:    matchType,
			Tags:    matchTags,
			Profile: matchProfile,
		}

		r := resolver.NewStandardResolver()
		matches := r.ResolveAll(resources, req)

		if len(matches) == 0 {
			fmt.Println("No matching resources found.")
			return nil
		}

		fmt.Printf("Found %d matches for requirement: %+v\n\n", len(matches), req)

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "RANK\tSCORE\tIDENTIFIER\tNAME\tTAGS")

		// Re-calculate score just for display purposes (since ResolveAll doesn't return scores)
		// Ideally we'd refactor resolver to return ScoredResource struct
		for i, res := range matches {
			// Quick hack: just display rank
			tags := fmt.Sprintf("%v", res.Tags)
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", i+1, "-", res.Identifier, res.Name, tags)
		}
		w.Flush()

		return nil
	},
}

func init() {
	resourcesListCmd.Flags().StringVar(&listType, "type", "", "Filter by resource type")
	resourcesListCmd.Flags().StringVar(&listSource, "source", "", "Filter by source")
	resourcesListCmd.Flags().StringSliceVar(&listTags, "tag", nil, "Filter by tags (AND match)")

	resourcesMatchCmd.Flags().StringVar(&matchType, "type", "", "Resource type to match")
	resourcesMatchCmd.Flags().StringSliceVar(&matchTags, "tags", nil, "Required tags")
	resourcesMatchCmd.Flags().StringVar(&matchProfile, "profile", "", "Matching profile (cheap, performance, balanced)")

	resourcesCmd.AddCommand(resourcesSyncCmd)
	resourcesCmd.AddCommand(resourcesListCmd)
	resourcesCmd.AddCommand(resourcesMatchCmd)
	rootCmd.AddCommand(resourcesCmd)
}