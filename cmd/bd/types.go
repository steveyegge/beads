package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

// coreWorkTypes are the built-in types that beads validates without configuration.
var coreWorkTypes = []struct {
	Type        types.IssueType
	Description string
}{
	{types.TypeTask, "General work item (default)"},
	{types.TypeBug, "Bug report or defect"},
	{types.TypeFeature, "New feature or enhancement"},
	{types.TypeChore, "Maintenance or housekeeping"},
	{types.TypeEpic, "Large body of work spanning multiple issues"},
}

var typesCmd = &cobra.Command{
	Use:     "types",
	GroupID: "views",
	Short:   "List valid issue types",
	Long: `List all valid issue types that can be used with bd create --type.

Core work types (bug, task, feature, chore, epic) are always valid.
Additional types require configuration via types.custom in .beads/config.yaml.

Examples:
  bd types              # List all types with descriptions
  bd types --json       # Output as JSON
`,
	Run: func(cmd *cobra.Command, args []string) {
		// Use daemon RPC when available (bd-s091)
		if daemonClient != nil {
			runTypesViaDaemon()
			return
		}

		// Fallback to direct store access
		if store == nil {
			fmt.Fprintf(os.Stderr, "Error: no database connection available\n")
			fmt.Fprintf(os.Stderr, "Hint: start the daemon with 'bd daemon start' or run in a beads workspace\n")
			os.Exit(1)
		}

		// Get custom types from config
		var customTypes []string
		ctx := context.Background()
		if ct, err := store.GetCustomTypes(ctx); err == nil {
			customTypes = ct
		}

		if jsonOutput {
			result := struct {
				CoreTypes   []typeInfo `json:"core_types"`
				CustomTypes []string   `json:"custom_types,omitempty"`
			}{}

			for _, t := range coreWorkTypes {
				result.CoreTypes = append(result.CoreTypes, typeInfo{
					Name:        string(t.Type),
					Description: t.Description,
				})
			}
			result.CustomTypes = customTypes
			outputJSON(result)
			return
		}

		// Text output
		fmt.Println("Core work types (built-in):")
		for _, t := range coreWorkTypes {
			fmt.Printf("  %-14s %s\n", t.Type, t.Description)
		}

		if len(customTypes) > 0 {
			fmt.Println("\nConfigured custom types:")
			for _, t := range customTypes {
				fmt.Printf("  %s\n", t)
			}
		} else {
			fmt.Println("\nNo custom types configured.")
			fmt.Println("Configure with: bd config set types.custom \"type1,type2,...\"")
		}
	},
}

type typeInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func init() {
	rootCmd.AddCommand(typesCmd)
}

// runTypesViaDaemon executes types via daemon RPC (bd-s091)
func runTypesViaDaemon() {
	result, err := daemonClient.Types(&rpc.TypesArgs{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		outputJSON(result)
		return
	}

	// Text output
	fmt.Println("Core work types (built-in):")
	for _, t := range result.CoreTypes {
		fmt.Printf("  %-14s %s\n", t.Name, t.Description)
	}

	if len(result.CustomTypes) > 0 {
		fmt.Println("\nConfigured custom types:")
		for _, t := range result.CustomTypes {
			fmt.Printf("  %s\n", t)
		}
	} else {
		fmt.Println("\nNo custom types configured.")
		fmt.Println("Configure with: bd config set types.custom \"type1,type2,...\"")
	}
}
