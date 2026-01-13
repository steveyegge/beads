package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/namespace"
	"github.com/steveyegge/beads/internal/ui"
)

var sourcesCmd = &cobra.Command{
	Use:     "sources",
	GroupID: "config",
	Short:   "Manage project source configuration (.beads/sources.yaml)",
	Long: `Manage project source configuration for branch-based namespaces.

The sources.yaml file defines where each project's issues come from (upstream, fork, or local).
This enables coordination across multiple repositories and forks.

Examples:
  bd sources list                                    # Show all configured sources
  bd sources add beads github.com/steveyegge/beads   # Add new upstream project
  bd sources set-fork beads github.com/matt/beads    # Set fork for project
  bd sources set-local beads /local/path/to/beads    # Set local override`,
	Args: cobra.MinimumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		// Delegate to subcommands
		if len(args) == 0 {
			_ = cmd.Help()
			return
		}

		subcommand := args[0]
		switch subcommand {
		case "list":
			sourcesListCmd(cmd)
		case "add":
			if len(args) < 3 {
				FatalErrorRespectJSON("sources add requires <project> and <upstream-url>")
			}
			sourcesAddCmd(cmd, args[1], args[2])
		case "set-fork":
			if len(args) < 3 {
				FatalErrorRespectJSON("sources set-fork requires <project> and <fork-url>")
			}
			sourcesSetForkCmd(cmd, args[1], args[2])
		case "set-local":
			if len(args) < 3 {
				FatalErrorRespectJSON("sources set-local requires <project> and <local-path>")
			}
			sourcesSetLocalCmd(cmd, args[1], args[2])
		case "show":
			if len(args) < 2 {
				FatalErrorRespectJSON("sources show requires <project>")
			}
			sourcesShowCmd(cmd, args[1])
		default:
			FatalErrorRespectJSON("unknown sources subcommand: %s", subcommand)
		}
	},
}

func getSourcesConfigPath() (string, string, error) {
	// Find .beads directory (walk up from current directory)
	dir, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to get working directory: %v", err)
	}

	for {
		beadsDir := filepath.Join(dir, ".beads")
		if _, err := os.Stat(beadsDir); err == nil {
			// Found .beads directory
			return beadsDir, filepath.Join(beadsDir, "sources.yaml"), nil
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root - .beads not found
			return "", "", fmt.Errorf("no .beads directory found in current or parent directories")
		}
		dir = parent
	}
}

func sourcesListCmd(cmd *cobra.Command) {
	beadsDir, _, err := getSourcesConfigPath()
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	cfg, err := namespace.LoadSourcesConfig(beadsDir)
	if err != nil {
		FatalErrorRespectJSON("failed to load sources config: %v", err)
	}

	if len(cfg.Sources) == 0 {
		if jsonOutput {
			outputJSON(map[string]interface{}{
				"sources": map[string]interface{}{},
			})
		} else {
			fmt.Println("No sources configured")
		}
		return
	}

	if jsonOutput {
		// Convert to JSON-serializable format
		sourcesData := make(map[string]interface{})
		for project, source := range cfg.Sources {
			sourcesData[project] = map[string]interface{}{
				"upstream": source.Upstream,
				"fork":     source.Fork,
				"local":    source.Local,
			}
		}
		outputJSON(sourcesData)
	} else {
		fmt.Println("Configured sources:")
		for project, source := range cfg.Sources {
			fmt.Printf("\n  %s:\n", ui.RenderAccent(project))
			fmt.Printf("    Upstream: %s\n", source.Upstream)
			if source.Fork != "" {
				fmt.Printf("    Fork:     %s\n", source.Fork)
			}
			if source.Local != "" {
				fmt.Printf("    Local:    %s\n", source.Local)
			}
		}
	}
}

func sourcesShowCmd(cmd *cobra.Command, project string) {
	beadsDir, _, err := getSourcesConfigPath()
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	cfg, err := namespace.LoadSourcesConfig(beadsDir)
	if err != nil {
		FatalErrorRespectJSON("failed to load sources config: %v", err)
	}

	source, err := cfg.GetProject(project)
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"project":  project,
			"upstream": source.Upstream,
			"fork":     source.Fork,
			"local":    source.Local,
		})
	} else {
		fmt.Printf("%s configuration:\n", project)
		fmt.Printf("  Upstream: %s\n", source.Upstream)
		if source.Fork != "" {
			fmt.Printf("  Fork:     %s\n", source.Fork)
		} else {
			fmt.Printf("  Fork:     (not set)\n")
		}
		if source.Local != "" {
			fmt.Printf("  Local:    %s\n", source.Local)
		} else {
			fmt.Printf("  Local:    (not set)\n")
		}
		fmt.Printf("\n  Effective URL: %s\n", source.GetSourceURL())
	}
}

func sourcesAddCmd(cmd *cobra.Command, project, upstream string) {
	beadsDir, _, err := getSourcesConfigPath()
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	cfg, err := namespace.LoadSourcesConfig(beadsDir)
	if err != nil {
		FatalErrorRespectJSON("failed to load sources config: %v", err)
	}

	if err := cfg.AddProject(project, upstream); err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	if err := namespace.SaveSourcesConfig(beadsDir, cfg); err != nil {
		FatalErrorRespectJSON("failed to save sources config: %v", err)
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"action":   "add",
			"project":  project,
			"upstream": upstream,
		})
	} else {
		fmt.Printf("%s Added project %q with upstream %q\n", ui.RenderPass("✓"), project, upstream)
	}
}

func sourcesSetForkCmd(cmd *cobra.Command, project, fork string) {
	beadsDir, _, err := getSourcesConfigPath()
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	cfg, err := namespace.LoadSourcesConfig(beadsDir)
	if err != nil {
		FatalErrorRespectJSON("failed to load sources config: %v", err)
	}

	if err := cfg.SetProjectFork(project, fork); err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	if err := namespace.SaveSourcesConfig(beadsDir, cfg); err != nil {
		FatalErrorRespectJSON("failed to save sources config: %v", err)
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"action":  "set-fork",
			"project": project,
			"fork":    fork,
		})
	} else {
		fmt.Printf("%s Set fork for %q to %q\n", ui.RenderPass("✓"), project, fork)
	}
}

func sourcesSetLocalCmd(cmd *cobra.Command, project, local string) {
	beadsDir, _, err := getSourcesConfigPath()
	if err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	cfg, err := namespace.LoadSourcesConfig(beadsDir)
	if err != nil {
		FatalErrorRespectJSON("failed to load sources config: %v", err)
	}

	if err := cfg.SetProjectLocal(project, local); err != nil {
		FatalErrorRespectJSON("%v", err)
	}

	if err := namespace.SaveSourcesConfig(beadsDir, cfg); err != nil {
		FatalErrorRespectJSON("failed to save sources config: %v", err)
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"action":  "set-local",
			"project": project,
			"local":   local,
		})
	} else {
		fmt.Printf("%s Set local override for %q to %q\n", ui.RenderPass("✓"), project, local)
	}
}

func init() {
	rootCmd.AddCommand(sourcesCmd)
}
