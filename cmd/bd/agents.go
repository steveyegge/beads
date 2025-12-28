// Package main implements the bd CLI agents management commands.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/ui"
)

var agentsCmd = &cobra.Command{
	Use:     "agents",
	GroupID: "setup",
	Short:   "Manage agent marketplace integration",
	Long: `Manage agent marketplace plugins and configuration.

The agents command provides integration with the Claude Code agents marketplace,
allowing you to enable/disable plugins and view agent configuration.

Configuration is stored in the beads database (project-level) and config.yaml
(user preferences).

Examples:
  bd agents list                    # List available agents from marketplace
  bd agents enable beads-workflows  # Enable a plugin
  bd agents disable beads-workflows # Disable a plugin
  bd agents status                  # Show current agent configuration`,
}

var agentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available agents from marketplace",
	Long: `List agents available in the configured marketplace repository.

Reads the marketplace.json file from the configured agents.marketplace.repo path
and displays available plugins with their agents.`,
	Run: func(cmd *cobra.Command, args []string) {
		repoPath := config.GetString("agents.marketplace.repo")
		if repoPath == "" {
			// Also check database config
			ctx := rootCtx
			if store != nil {
				if val, err := store.GetConfig(ctx, "agents.marketplace.repo"); err == nil && val != "" {
					repoPath = val
				}
			}
		}

		if repoPath == "" {
			fmt.Fprintln(os.Stderr, "No agents marketplace configured.")
			fmt.Fprintln(os.Stderr, "Set with: bd config set agents.marketplace.repo \"/path/to/agents\"")
			os.Exit(1)
		}

		// Read marketplace.json
		marketplacePath := filepath.Join(repoPath, ".claude-plugin", "marketplace.json")
		data, err := os.ReadFile(marketplacePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading marketplace: %v\n", err)
			fmt.Fprintf(os.Stderr, "Expected file at: %s\n", marketplacePath)
			os.Exit(1)
		}

		var marketplace []struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Version     string   `json:"version"`
			Category    string   `json:"category"`
			Agents      []string `json:"agents"`
			Skills      []string `json:"skills"`
			Commands    []string `json:"commands"`
		}

		if err := json.Unmarshal(data, &marketplace); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing marketplace.json: %v\n", err)
			os.Exit(1)
		}

		// Get enabled plugins
		enabledStr := getEnabledPlugins()
		enabled := make(map[string]bool)
		for _, p := range strings.Split(enabledStr, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				enabled[p] = true
			}
		}

		if jsonOutput {
			output := make([]map[string]interface{}, 0, len(marketplace))
			for _, p := range marketplace {
				output = append(output, map[string]interface{}{
					"name":        p.Name,
					"description": p.Description,
					"version":     p.Version,
					"category":    p.Category,
					"enabled":     enabled[p.Name],
					"agents":      len(p.Agents),
					"skills":      len(p.Skills),
					"commands":    len(p.Commands),
				})
			}
			outputJSON(output)
			return
		}

		fmt.Printf("\n%s Agents Marketplace (%d plugins)\n\n", ui.RenderPass("📦"), len(marketplace))

		// Sort by category then name
		sort.Slice(marketplace, func(i, j int) bool {
			if marketplace[i].Category != marketplace[j].Category {
				return marketplace[i].Category < marketplace[j].Category
			}
			return marketplace[i].Name < marketplace[j].Name
		})

		currentCategory := ""
		for _, p := range marketplace {
			if p.Category != currentCategory {
				currentCategory = p.Category
				fmt.Printf("  %s:\n", strings.Title(currentCategory))
			}

			status := "  "
			if enabled[p.Name] {
				status = ui.RenderPass("✓")
			}

			fmt.Printf("    %s %s", status, p.Name)
			if p.Version != "" {
				fmt.Printf(" (v%s)", p.Version)
			}
			fmt.Println()

			if p.Description != "" {
				fmt.Printf("      %s\n", p.Description)
			}

			counts := []string{}
			if len(p.Agents) > 0 {
				counts = append(counts, fmt.Sprintf("%d agents", len(p.Agents)))
			}
			if len(p.Skills) > 0 {
				counts = append(counts, fmt.Sprintf("%d skills", len(p.Skills)))
			}
			if len(p.Commands) > 0 {
				counts = append(counts, fmt.Sprintf("%d commands", len(p.Commands)))
			}
			if len(counts) > 0 {
				fmt.Printf("      [%s]\n", strings.Join(counts, ", "))
			}
		}
		fmt.Println()
	},
}

var agentsEnableCmd = &cobra.Command{
	Use:   "enable <plugin-name>",
	Short: "Enable a marketplace plugin",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pluginName := args[0]

		enabledStr := getEnabledPlugins()
		enabled := parsePluginList(enabledStr)

		// Check if already enabled
		for _, p := range enabled {
			if p == pluginName {
				if jsonOutput {
					outputJSON(map[string]interface{}{
						"plugin":  pluginName,
						"status":  "already_enabled",
						"enabled": enabled,
					})
				} else {
					fmt.Printf("Plugin '%s' is already enabled\n", pluginName)
				}
				return
			}
		}

		// Add to enabled list
		enabled = append(enabled, pluginName)
		newEnabledStr := strings.Join(enabled, ",")

		// Save to database config
		ctx := rootCtx
		if err := store.SetConfig(ctx, "agents.marketplace.enabled", newEnabledStr); err != nil {
			fmt.Fprintf(os.Stderr, "Error enabling plugin: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"plugin":  pluginName,
				"status":  "enabled",
				"enabled": enabled,
			})
		} else {
			fmt.Printf("%s Enabled plugin '%s'\n", ui.RenderPass("✓"), pluginName)
		}
	},
}

var agentsDisableCmd = &cobra.Command{
	Use:   "disable <plugin-name>",
	Short: "Disable a marketplace plugin",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pluginName := args[0]

		enabledStr := getEnabledPlugins()
		enabled := parsePluginList(enabledStr)

		// Find and remove the plugin
		found := false
		newEnabled := make([]string, 0, len(enabled))
		for _, p := range enabled {
			if p == pluginName {
				found = true
			} else {
				newEnabled = append(newEnabled, p)
			}
		}

		if !found {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"plugin":  pluginName,
					"status":  "not_enabled",
					"enabled": enabled,
				})
			} else {
				fmt.Printf("Plugin '%s' is not enabled\n", pluginName)
			}
			return
		}

		// Save updated list
		newEnabledStr := strings.Join(newEnabled, ",")

		ctx := rootCtx
		if err := store.SetConfig(ctx, "agents.marketplace.enabled", newEnabledStr); err != nil {
			fmt.Fprintf(os.Stderr, "Error disabling plugin: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"plugin":  pluginName,
				"status":  "disabled",
				"enabled": newEnabled,
			})
		} else {
			fmt.Printf("%s Disabled plugin '%s'\n", ui.RenderPass("✓"), pluginName)
		}
	},
}

var agentsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current agent configuration",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		// Collect all agent-related config
		status := make(map[string]interface{})

		// Get from viper (yaml config)
		status["prefer_model"] = config.GetString("agents.prefer_model")
		status["context_budget"] = config.GetInt("agents.context_budget")
		status["skill_autoload"] = config.GetBool("agents.skill_autoload")

		// Get from database
		if store != nil {
			if val, err := store.GetConfig(ctx, "agents.marketplace.repo"); err == nil {
				status["marketplace_repo"] = val
			}
			if val, err := store.GetConfig(ctx, "agents.marketplace.enabled"); err == nil {
				status["enabled_plugins"] = parsePluginList(val)
			}
			if val, err := store.GetConfig(ctx, "agents.default_model"); err == nil && val != "" {
				status["default_model"] = val
			}
			if val, err := store.GetConfig(ctx, "agents.session.auto_prime"); err == nil && val != "" {
				status["session_auto_prime"] = val
			}
			if val, err := store.GetConfig(ctx, "agents.session.track_skills"); err == nil && val != "" {
				status["session_track_skills"] = val
			}
			if val, err := store.GetConfig(ctx, "agents.enforcement.description_min_length"); err == nil && val != "" {
				status["enforcement_description_min_length"] = val
			}
			if val, err := store.GetConfig(ctx, "agents.enforcement.dependency_validation"); err == nil && val != "" {
				status["enforcement_dependency_validation"] = val
			}
			if val, err := store.GetConfig(ctx, "agents.enforcement.single_issue_max"); err == nil && val != "" {
				status["enforcement_single_issue_max"] = val
			}
		}

		if jsonOutput {
			outputJSON(status)
			return
		}

		fmt.Printf("\n%s Agent Configuration\n\n", ui.RenderPass("⚙️"))

		// Marketplace
		fmt.Println("  Marketplace:")
		if repo, ok := status["marketplace_repo"].(string); ok && repo != "" {
			fmt.Printf("    repo: %s\n", repo)
		} else {
			fmt.Println("    repo: (not configured)")
		}
		if plugins, ok := status["enabled_plugins"].([]string); ok && len(plugins) > 0 {
			fmt.Printf("    enabled: %s\n", strings.Join(plugins, ", "))
		} else {
			fmt.Println("    enabled: (none)")
		}

		// Model settings
		fmt.Println("\n  Model:")
		if model, ok := status["default_model"].(string); ok && model != "" {
			fmt.Printf("    default: %s\n", model)
		} else {
			fmt.Println("    default: sonnet")
		}
		if pref := status["prefer_model"].(string); pref != "" {
			fmt.Printf("    prefer: %s (user override)\n", pref)
		}
		fmt.Printf("    context_budget: %d tokens\n", status["context_budget"])

		// Session settings
		fmt.Println("\n  Session:")
		fmt.Printf("    auto_prime: %v\n", status["session_auto_prime"])
		fmt.Printf("    track_skills: %v\n", status["session_track_skills"])
		fmt.Printf("    skill_autoload: %v\n", status["skill_autoload"])

		// Enforcement settings
		fmt.Println("\n  Enforcement:")
		fmt.Printf("    description_min_length: %v\n", status["enforcement_description_min_length"])
		fmt.Printf("    dependency_validation: %v\n", status["enforcement_dependency_validation"])
		fmt.Printf("    single_issue_max: %v\n", status["enforcement_single_issue_max"])

		fmt.Println()
	},
}

// getEnabledPlugins returns the comma-separated list of enabled plugins
func getEnabledPlugins() string {
	// First check viper (yaml)
	if val := config.GetString("agents.marketplace.enabled"); val != "" {
		return val
	}

	// Then check database
	ctx := rootCtx
	if store != nil {
		if val, err := store.GetConfig(ctx, "agents.marketplace.enabled"); err == nil {
			return val
		}
	}

	return ""
}

// parsePluginList splits a comma-separated plugin list into a slice
func parsePluginList(s string) []string {
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func init() {
	agentsCmd.AddCommand(agentsListCmd)
	agentsCmd.AddCommand(agentsEnableCmd)
	agentsCmd.AddCommand(agentsDisableCmd)
	agentsCmd.AddCommand(agentsStatusCmd)
	rootCmd.AddCommand(agentsCmd)
}
