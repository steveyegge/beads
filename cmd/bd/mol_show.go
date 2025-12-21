package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

var molShowCmd = &cobra.Command{
	Use:   "show <molecule-id>",
	Short: "Show molecule details",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		// mol show requires direct store access for subgraph loading
		if store == nil {
			if daemonClient != nil {
				fmt.Fprintf(os.Stderr, "Error: mol show requires direct database access\n")
				fmt.Fprintf(os.Stderr, "Hint: use --no-daemon flag: bd --no-daemon mol show %s\n", args[0])
			} else {
				fmt.Fprintf(os.Stderr, "Error: no database connection\n")
			}
			os.Exit(1)
		}

		moleculeID, err := utils.ResolvePartialID(ctx, store, args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: molecule '%s' not found\n", args[0])
			os.Exit(1)
		}

		subgraph, err := loadTemplateSubgraph(ctx, store, moleculeID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading molecule: %v\n", err)
			os.Exit(1)
		}

		showMolecule(subgraph)
	},
}

func showMolecule(subgraph *MoleculeSubgraph) {
	if jsonOutput {
		outputJSON(map[string]interface{}{
			"root":         subgraph.Root,
			"issues":       subgraph.Issues,
			"dependencies": subgraph.Dependencies,
			"variables":    extractAllVariables(subgraph),
		})
		return
	}

	fmt.Printf("\n%s Molecule: %s\n", ui.RenderAccent("🧪"), subgraph.Root.Title)
	fmt.Printf("   ID: %s\n", subgraph.Root.ID)
	fmt.Printf("   Steps: %d\n", len(subgraph.Issues))

	vars := extractAllVariables(subgraph)
	if len(vars) > 0 {
		fmt.Printf("\n%s Variables:\n", ui.RenderWarn("📝"))
		for _, v := range vars {
			fmt.Printf("   {{%s}}\n", v)
		}
	}

	fmt.Printf("\n%s Structure:\n", ui.RenderPass("🌲"))
	printMoleculeTree(subgraph, subgraph.Root.ID, 0, true)
	fmt.Println()
}

func init() {
	molCmd.AddCommand(molShowCmd)
}
