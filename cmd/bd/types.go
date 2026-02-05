package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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

// typeDefineCmd defines a schema for an issue type.
var typeDefineCmd = &cobra.Command{
	Use:   "define <typename>",
	Short: "Define a type schema with required fields and labels",
	Long: `Define or update a type schema that enforces mandatory fields and labels.

When a schema is defined for a type, issues of that type must satisfy the
schema's requirements during creation and update.

Examples:
  bd type define config \
    --required-field rig \
    --required-field metadata \
    --required-label "config:*" \
    --description "Configuration beads require scope and payload"

  bd type define bug \
    --required-field description \
    --description "Bug reports must include a description"
`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("type define")

		typeName := args[0]

		requiredFields, _ := cmd.Flags().GetStringSlice("required-field")
		requiredLabels, _ := cmd.Flags().GetStringSlice("required-label")
		description, _ := cmd.Flags().GetString("description")

		if len(requiredFields) == 0 && len(requiredLabels) == 0 {
			FatalError("at least one --required-field or --required-label must be specified")
		}

		schema := &types.TypeSchema{
			RequiredFields: requiredFields,
			RequiredLabels: requiredLabels,
			Description:    description,
		}

		ctx := context.Background()

		if daemonClient != nil {
			// Store via daemon config set
			data, err := json.Marshal(schema)
			if err != nil {
				FatalError("failed to serialize schema: %v", err)
			}
			key := types.TypeSchemaConfigPrefix + typeName
			if _, err := daemonClient.ConfigSet(&rpc.ConfigSetArgs{Key: key, Value: string(data)}); err != nil {
				FatalError("failed to set type schema: %v", err)
			}
		} else if store != nil {
			if err := store.SetTypeSchema(ctx, typeName, schema); err != nil {
				FatalError("failed to set type schema: %v", err)
			}
		} else {
			FatalError("no database connection available")
		}

		if jsonOutput {
			outputJSON(schema)
		} else {
			fmt.Printf("✓ Defined schema for type %q\n", typeName)
			if len(requiredFields) > 0 {
				fmt.Printf("  Required fields: %s\n", strings.Join(requiredFields, ", "))
			}
			if len(requiredLabels) > 0 {
				fmt.Printf("  Required labels: %s\n", strings.Join(requiredLabels, ", "))
			}
			if description != "" {
				fmt.Printf("  Description: %s\n", description)
			}
		}
	},
}

// typeSchemaCmd shows the schema for an issue type.
var typeSchemaCmd = &cobra.Command{
	Use:   "schema <typename>",
	Short: "Show the schema for an issue type",
	Long: `Display the type schema including required fields and labels.

Examples:
  bd type schema config    # Show schema for config type
  bd type schema bug       # Show schema for bug type
`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		typeName := args[0]
		ctx := context.Background()

		var schema *types.TypeSchema
		var err error

		if daemonClient != nil {
			// Read via daemon config get
			key := types.TypeSchemaConfigPrefix + typeName
			var resp *rpc.GetConfigResponse
			resp, err = daemonClient.GetConfig(&rpc.GetConfigArgs{Key: key})
			if err == nil && resp != nil && resp.Value != "" {
				schema = &types.TypeSchema{}
				err = json.Unmarshal([]byte(resp.Value), schema)
			}
		} else if store != nil {
			schema, err = store.GetTypeSchema(ctx, typeName)
		} else {
			FatalError("no database connection available")
		}

		if err != nil {
			FatalError("failed to get type schema: %v", err)
		}

		if schema == nil {
			if jsonOutput {
				outputJSON(nil)
			} else {
				fmt.Printf("No schema defined for type %q\n", typeName)
			}
			return
		}

		if jsonOutput {
			outputJSON(schema)
			return
		}

		fmt.Printf("Schema for type %q:\n", typeName)
		if schema.Description != "" {
			fmt.Printf("  Description: %s\n", schema.Description)
		}
		if len(schema.RequiredFields) > 0 {
			fmt.Printf("  Required fields:\n")
			for _, f := range schema.RequiredFields {
				fmt.Printf("    - %s\n", f)
			}
		}
		if len(schema.RequiredLabels) > 0 {
			fmt.Printf("  Required labels:\n")
			for _, l := range schema.RequiredLabels {
				fmt.Printf("    - %s\n", l)
			}
		}
	},
}

// typeCmd is the parent command for type-related subcommands.
var typeCmd = &cobra.Command{
	Use:     "type",
	GroupID: "setup",
	Short:   "Manage type schemas",
	Long: `Manage type schemas that enforce mandatory fields and labels per issue type.

Subcommands:
  define    Define or update a type schema
  schema    Show a type schema
`,
}

func init() {
	rootCmd.AddCommand(typesCmd)

	// Type schema management
	typeCmd.AddCommand(typeDefineCmd)
	typeCmd.AddCommand(typeSchemaCmd)
	rootCmd.AddCommand(typeCmd)

	// Flags for type define
	typeDefineCmd.Flags().StringSlice("required-field", nil, "Field that must be non-empty (can be repeated)")
	typeDefineCmd.Flags().StringSlice("required-label", nil, "Label pattern that must be present (can be repeated, supports wildcards like 'config:*')")
	typeDefineCmd.Flags().String("description", "", "Human-readable description of the schema requirements")
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
