package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/resolver"
	"github.com/steveyegge/beads/internal/types"
)

var resourceCmd = &cobra.Command{
	Use:     "resource",
	GroupID: "advanced",
	Short:   "Manage resources (models, agents, skills)",
	Long: `Manage resources such as models, agents, and skills.

Resources are stored in the database and can be tagged for easy filtering.
Use the resolver to find the best resource for a given profile (cheap, performance, balanced).`,
}

var resourceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all resources",
	Long: `List all resources with optional filters.

Examples:
  bd resource list                           # List all resources
  bd resource list --type model              # List only models
  bd resource list --tag cheap               # List resources with 'cheap' tag
  bd resource list --source local            # List locally configured resources
  bd resource list --json                    # Output as JSON`,
	Run: func(cmd *cobra.Command, args []string) {
		resourceType, _ := cmd.Flags().GetString("type")
		tags, _ := cmd.Flags().GetStringSlice("tag")
		source, _ := cmd.Flags().GetString("source")

		ctx := rootCtx
		requireFreshDB(ctx)

		filter := types.ResourceFilter{}
		if resourceType != "" {
			filter.Type = &resourceType
		}
		if source != "" {
			filter.Source = &source
		}
		if len(tags) > 0 {
			filter.Tags = tags
		}

		resources, err := store.ListResources(ctx, filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing resources: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(resources)
			return
		}

		if len(resources) == 0 {
			fmt.Println("No resources found")
			return
		}

		fmt.Printf("Found %d resources:\n\n", len(resources))
		for _, res := range resources {
			status := "active"
			if !res.IsActive {
				status = "inactive"
			}
			tagStr := ""
			if len(res.Tags) > 0 {
				tagStr = fmt.Sprintf(" [%s]", strings.Join(res.Tags, ", "))
			}
			fmt.Printf("  %s (%s) - %s%s - %s\n",
				res.Identifier,
				res.Type,
				res.Name,
				tagStr,
				status,
			)
		}
	},
}

var resourceAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new resource",
	Long: `Add a new resource (model, agent, or skill).

Examples:
  bd resource add --name "GPT-4" --type model --identifier gpt-4 --source local
  bd resource add --name "Claude 3.5 Sonnet" --type model --identifier claude-3-5-sonnet --tag smart --tag expensive
  bd resource add --name "Sisyphus" --type agent --identifier sisyphus --source config`,
	Run: func(cmd *cobra.Command, args []string) {
		name, _ := cmd.Flags().GetString("name")
		resourceType, _ := cmd.Flags().GetString("type")
		identifier, _ := cmd.Flags().GetString("identifier")
		source, _ := cmd.Flags().GetString("source")
		tags, _ := cmd.Flags().GetStringSlice("tag")
		configJSON, _ := cmd.Flags().GetString("config-json")

		if name == "" || resourceType == "" || identifier == "" {
			fmt.Fprintf(os.Stderr, "Error: name, type, and identifier are required\n")
			os.Exit(1)
		}

		validTypes := []string{types.ResourceTypeModel, types.ResourceTypeAgent, types.ResourceTypeSkill}
		if !stringSliceContains(validTypes, resourceType) {
			fmt.Fprintf(os.Stderr, "Error: type must be one of: %s\n", strings.Join(validTypes, ", "))
			os.Exit(1)
		}

		if source == "" {
			source = types.ResourceSourceLocal
		}

		ctx := rootCtx
		requireFreshDB(ctx)

		resource := &types.Resource{
			Type:       resourceType,
			Name:       name,
			Identifier: identifier,
			Source:     source,
			Config:     configJSON,
			IsActive:   true,
			Tags:       tags,
		}

		err := store.SaveResource(ctx, resource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error adding resource: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(resource)
			return
		}

		fmt.Printf("Added resource: %s (%s)\n", identifier, name)
	},
}

var resourceUpdateCmd = &cobra.Command{
	Use:   "update <identifier>",
	Short: "Update a resource",
	Long: `Update a resource by identifier.

Examples:
  bd resource update gpt-4 --name "GPT-4 Turbo"
  bd resource update claude-3-5-sonnet --config-json '{"max_tokens": 8000}'
  bd resource update sisyphus --deactivate`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		identifier := args[0]

		ctx := rootCtx
		requireFreshDB(ctx)

		resource, err := store.GetResource(ctx, identifier)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching resource: %v\n", err)
			os.Exit(1)
		}
		if resource == nil {
			fmt.Fprintf(os.Stderr, "Error: resource not found: %s\n", identifier)
			os.Exit(1)
		}

		if cmd.Flags().Changed("name") {
			name, _ := cmd.Flags().GetString("name")
			resource.Name = name
		}
		if cmd.Flags().Changed("config-json") {
			configJSON, _ := cmd.Flags().GetString("config-json")
			resource.Config = configJSON
		}
		if cmd.Flags().Changed("deactivate") {
			deactivate, _ := cmd.Flags().GetBool("deactivate")
			resource.IsActive = !deactivate
		}
		if cmd.Flags().Changed("activate") {
			activate, _ := cmd.Flags().GetBool("activate")
			resource.IsActive = activate
		}

		err = store.SaveResource(ctx, resource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error updating resource: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(resource)
			return
		}

		fmt.Printf("Updated resource: %s\n", identifier)
	},
}

var resourceTagAddCmd = &cobra.Command{
	Use:   "add <identifier> <tag>",
	Short: "Add a tag to a resource",
	Long: `Add a tag to a resource.

Examples:
  bd resource tag add gpt-4 expensive
  bd resource tag add claude-3-5-sonnet smart`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		identifier := args[0]
		tag := args[1]

		ctx := rootCtx
		requireFreshDB(ctx)

		resource, err := store.GetResource(ctx, identifier)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching resource: %v\n", err)
			os.Exit(1)
		}
		if resource == nil {
			fmt.Fprintf(os.Stderr, "Error: resource not found: %s\n", identifier)
			os.Exit(1)
		}

		for _, t := range resource.Tags {
			if t == tag {
				fmt.Printf("Tag '%s' already exists on resource %s\n", tag, identifier)
				return
			}
		}

		resource.Tags = append(resource.Tags, tag)

		err = store.SaveResource(ctx, resource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error adding tag: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(resource)
			return
		}

		fmt.Printf("Added tag '%s' to resource: %s\n", tag, identifier)
	},
}

var resourceTagRemoveCmd = &cobra.Command{
	Use:   "remove <identifier> <tag>",
	Short: "Remove a tag from a resource",
	Long: `Remove a tag from a resource.

Examples:
  bd resource tag remove gpt-4 expensive
  bd resource tag remove claude-3-5-sonnet smart`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		identifier := args[0]
		tag := args[1]

		ctx := rootCtx
		requireFreshDB(ctx)

		resource, err := store.GetResource(ctx, identifier)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching resource: %v\n", err)
			os.Exit(1)
		}
		if resource == nil {
			fmt.Fprintf(os.Stderr, "Error: resource not found: %s\n", identifier)
			os.Exit(1)
		}

		found := false
		newTags := make([]string, 0, len(resource.Tags))
		for _, t := range resource.Tags {
			if t == tag {
				found = true
				continue
			}
			newTags = append(newTags, t)
		}

		if !found {
			fmt.Fprintf(os.Stderr, "Error: tag '%s' not found on resource %s\n", tag, identifier)
			os.Exit(1)
		}

		resource.Tags = newTags

		err = store.SaveResource(ctx, resource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error removing tag: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(resource)
			return
		}

		fmt.Printf("Removed tag '%s' from resource: %s\n", tag, identifier)
	},
}

var resourceDeleteCmd = &cobra.Command{
	Use:   "delete <identifier>",
	Short: "Delete a resource (soft delete)",
	Long: `Delete a resource by setting is_active to false.

Examples:
  bd resource delete gpt-4
  bd resource delete sisyphus`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		identifier := args[0]

		ctx := rootCtx
		requireFreshDB(ctx)

		resource, err := store.GetResource(ctx, identifier)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching resource: %v\n", err)
			os.Exit(1)
		}
		if resource == nil {
			fmt.Fprintf(os.Stderr, "Error: resource not found: %s\n", identifier)
			os.Exit(1)
		}

		resource.IsActive = false

		err = store.SaveResource(ctx, resource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting resource: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(resource)
			return
		}

		fmt.Printf("Deleted resource: %s\n", identifier)
	},
}

var resourceResolveCmd = &cobra.Command{
	Use:   "resolve",
	Short: "Find the best resource for a profile",
	Long: `Find the best resource for a given profile using the resolver.

Profiles:
  cheap       - Prioritize cost-effective resources
  performance - Prioritize high-performance resources
  balanced    - Balance cost and performance

Examples:
  bd resource resolve --type model --profile cheap
  bd resource resolve --type agent --tag coding --profile performance
  bd resource resolve --type skill --tag devops`,
	Run: func(cmd *cobra.Command, args []string) {
		resourceType, _ := cmd.Flags().GetString("type")
		tags, _ := cmd.Flags().GetStringSlice("tag")
		profile, _ := cmd.Flags().GetString("profile")

		if resourceType == "" {
			fmt.Fprintf(os.Stderr, "Error: --type is required\n")
			os.Exit(1)
		}

		ctx := rootCtx
		requireFreshDB(ctx)

		filter := types.ResourceFilter{
			Type: &resourceType,
		}
		resources, err := store.ListResources(ctx, filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing resources: %v\n", err)
			os.Exit(1)
		}

		activeResources := make([]*types.Resource, 0, len(resources))
		for _, r := range resources {
			if r.IsActive {
				activeResources = append(activeResources, r)
			}
		}

		if len(activeResources) == 0 {
			fmt.Fprintf(os.Stderr, "No active resources found for type: %s\n", resourceType)
			os.Exit(1)
		}

		req := resolver.Requirement{
			Type:    resourceType,
			Tags:    tags,
			Profile: profile,
		}

		resolverInstance := resolver.NewStandardResolver()
		best := resolverInstance.ResolveBest(activeResources, req)

		if best == nil {
			fmt.Fprintf(os.Stderr, "No matching resource found\n")
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(best)
			return
		}

		tagStr := ""
		if len(best.Tags) > 0 {
			tagStr = fmt.Sprintf(" [%s]", strings.Join(best.Tags, ", "))
		}
		fmt.Printf("Best resource: %s (%s)%s\n", best.Identifier, best.Name, tagStr)
		if best.Config != "" {
			var configMap map[string]interface{}
			if err := json.Unmarshal([]byte(best.Config), &configMap); err == nil {
				fmt.Printf("Config: %s\n", formatJSON(configMap))
			}
		}
	},
}

var resourceTagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Manage resource tags",
}

func init() {
	resourceListCmd.Flags().String("type", "", "Filter by type (model, agent, skill)")
	resourceListCmd.Flags().StringSlice("tag", []string{}, "Filter by tags (AND semantics)")
	resourceListCmd.Flags().String("source", "", "Filter by source (local, linear, jira, config)")

	resourceAddCmd.Flags().String("name", "", "Resource display name (required)")
	resourceAddCmd.Flags().String("type", "", "Resource type: model, agent, or skill (required)")
	resourceAddCmd.Flags().String("identifier", "", "Unique identifier (required)")
	resourceAddCmd.Flags().String("source", "", "Source: local, linear, jira, config (default: local)")
	resourceAddCmd.Flags().StringSlice("tag", []string{}, "Tags for resource (can specify multiple times)")
	resourceAddCmd.Flags().String("config-json", "", "JSON configuration string")

	resourceUpdateCmd.Flags().String("name", "", "Update display name")
	resourceUpdateCmd.Flags().String("config-json", "", "Update JSON configuration")
	resourceUpdateCmd.Flags().Bool("deactivate", false, "Deactivate resource")
	resourceUpdateCmd.Flags().Bool("activate", false, "Activate resource")

	resourceResolveCmd.Flags().String("type", "", "Resource type (required)")
	resourceResolveCmd.Flags().StringSlice("tag", []string{}, "Required tags")
	resourceResolveCmd.Flags().String("profile", "balanced", "Profile: cheap, performance, or balanced")

	resourceTagCmd.AddCommand(resourceTagAddCmd)
	resourceTagCmd.AddCommand(resourceTagRemoveCmd)

	resourceCmd.AddCommand(resourceListCmd)
	resourceCmd.AddCommand(resourceAddCmd)
	resourceCmd.AddCommand(resourceUpdateCmd)
	resourceCmd.AddCommand(resourceTagCmd)
	resourceCmd.AddCommand(resourceDeleteCmd)
	resourceCmd.AddCommand(resourceResolveCmd)

	rootCmd.AddCommand(resourceCmd)
}

func stringSliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func formatJSON(data interface{}) string {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	return string(b)
}
