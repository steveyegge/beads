package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var listCategory string

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available example scripts",
	Long: `List all available bash example scripts with their metadata.

Examples:
  bd-examples list                    # List all scripts
  bd-examples list --category agents  # List only agent examples
  bd-examples list --json             # Output as JSON`,
	RunE: runList,
}

func init() {
	listCmd.Flags().StringVarP(&listCategory, "category", "c", "", "Filter by category (agents, hooks, compaction)")
}

func runList(cmd *cobra.Command, args []string) error {
	var cat Category
	if listCategory != "" {
		cat = Category(listCategory)
	}

	scripts := GetScriptsByCategory(cat)

	if jsonOutput {
		return listJSON(scripts)
	}

	return listTable(scripts)
}

func listJSON(scripts []Script) error {
	type jsonScript struct {
		Path          string   `json:"path"`
		Category      string   `json:"category"`
		Description   string   `json:"description"`
		Prerequisites []string `json:"prerequisites"`
		DryRunMode    string   `json:"dry_run_mode"`
		Interactive   bool     `json:"interactive"`
	}

	var out []jsonScript
	for _, s := range scripts {
		out = append(out, jsonScript{
			Path:          s.Path,
			Category:      string(s.Category),
			Description:   s.Description,
			Prerequisites: s.Prerequisites,
			DryRunMode:    string(s.DryRunMode),
			Interactive:   s.Interactive,
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func listTable(scripts []Script) error {
	// Group by category
	byCategory := make(map[Category][]Script)
	for _, s := range scripts {
		byCategory[s.Category] = append(byCategory[s.Category], s)
	}

	// Print order
	order := []Category{CategoryAgents, CategoryHooks, CategoryCompaction}

	first := true
	for _, cat := range order {
		catScripts, ok := byCategory[cat]
		if !ok || len(catScripts) == 0 {
			continue
		}

		if !first {
			fmt.Println()
		}
		first = false

		// Category header
		fmt.Printf("%s %s\n", boldStyle.Render(string(cat)), mutedStyle.Render("- "+CategoryDescription(cat)))

		for _, s := range catScripts {
			// Script line
			fmt.Printf("  %s\n", accentStyle.Render(s.Path))

			// Description
			fmt.Printf("    %s\n", s.Description)

			// Details line
			var details []string

			// Prerequisites
			if len(s.Prerequisites) > 0 {
				details = append(details, fmt.Sprintf("Requires: %s", strings.Join(s.Prerequisites, ", ")))
			}

			// Dry-run mode
			var modeStr string
			switch s.DryRunMode {
			case DryRunSafe:
				modeStr = passStyle.Render("safe")
			case DryRunIntercept:
				modeStr = warnStyle.Render("intercept")
			case DryRunNative:
				modeStr = passStyle.Render("native --dry-run")
			case DryRunBlock:
				modeStr = failStyle.Render("blocked")
			}
			details = append(details, fmt.Sprintf("Dry-run: %s", modeStr))

			// Interactive
			if s.Interactive {
				details = append(details, warnStyle.Render("interactive"))
			}

			fmt.Printf("    %s\n", mutedStyle.Render(strings.Join(details, " | ")))
		}
	}

	return nil
}
