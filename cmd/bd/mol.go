package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

// Molecule commands - work templates for agent workflows
//
// Terminology:
//   - Molecule: A template epic with child issues forming a DAG workflow
//   - Bond: Instantiate a molecule, creating real issues from the template
//   - Catalog: List available molecules
//
// Usage:
//   bd mol catalog                        # List available molecules
//   bd mol show <id>                      # Show molecule structure
//   bd mol bond <id> --var key=value      # Create issues from molecule

// MoleculeLabel is the label used to identify molecules (templates)
// Molecules use the same label as templates - they ARE templates with workflow semantics
const MoleculeLabel = BeadsTemplateLabel

// MoleculeSubgraph is an alias for TemplateSubgraph
// Molecules and templates share the same subgraph structure
type MoleculeSubgraph = TemplateSubgraph

var molCmd = &cobra.Command{
	Use:   "mol",
	Short: "Molecule commands (work templates)",
	Long: `Manage molecules - work templates for agent workflows.

Molecules are epics with the "template" label. They define a DAG of work
that can be instantiated ("bonded") to create real issues.

The molecule metaphor:
  - A molecule is a template (reusable work pattern)
  - Bonding creates new issues from the template
  - Variables ({{key}}) are substituted during bonding

Commands:
  catalog  List available molecules
  show     Show molecule structure and variables
  bond     Create issues from a molecule template`,
}

var molCatalogCmd = &cobra.Command{
	Use:     "catalog",
	Aliases: []string{"list", "ls"},
	Short:   "List available molecules",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx
		var molecules []*types.Issue

		if daemonClient != nil {
			resp, err := daemonClient.List(&rpc.ListArgs{})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading molecules: %v\n", err)
				os.Exit(1)
			}
			var allIssues []*types.Issue
			if err := json.Unmarshal(resp.Data, &allIssues); err == nil {
				for _, issue := range allIssues {
					for _, label := range issue.Labels {
						if label == MoleculeLabel {
							molecules = append(molecules, issue)
							break
						}
					}
				}
			}
		} else if store != nil {
			var err error
			molecules, err = store.GetIssuesByLabel(ctx, MoleculeLabel)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading molecules: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Fprintf(os.Stderr, "Error: no database connection\n")
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(molecules)
			return
		}

		if len(molecules) == 0 {
			fmt.Println("No molecules available.")
			fmt.Println("\nTo create a molecule:")
			fmt.Println("  1. Create an epic with child issues")
			fmt.Println("  2. Add the 'template' label: bd label add <epic-id> template")
			fmt.Println("  3. Use {{variable}} placeholders in titles/descriptions")
			fmt.Println("\nTo bond (instantiate) a molecule:")
			fmt.Println("  bd mol bond <id> --var key=value")
			return
		}

		fmt.Printf("%s\n", ui.RenderPass("Molecules (for bd mol bond):"))
		for _, mol := range molecules {
			vars := extractVariables(mol.Title + " " + mol.Description)
			varStr := ""
			if len(vars) > 0 {
				varStr = fmt.Sprintf(" (vars: %s)", strings.Join(vars, ", "))
			}
			fmt.Printf("  %s: %s%s\n", ui.RenderAccent(mol.ID), mol.Title, varStr)
		}
		fmt.Println()
	},
}

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

var molBondCmd = &cobra.Command{
	Use:   "bond <molecule-id>",
	Short: "Create issues from a molecule template",
	Long: `Bond (instantiate) a molecule by creating real issues from its template.

Variables are specified with --var key=value flags. The molecule's {{key}}
placeholders will be replaced with the corresponding values.

Example:
  bd mol bond mol-code-review --var pr=123 --var repo=myproject
  bd mol bond bd-abc123 --var version=1.2.0 --assignee=worker-1`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("mol bond")

		ctx := rootCtx

		// mol bond requires direct store access for subgraph loading and cloning
		if store == nil {
			if daemonClient != nil {
				fmt.Fprintf(os.Stderr, "Error: mol bond requires direct database access\n")
				fmt.Fprintf(os.Stderr, "Hint: use --no-daemon flag: bd --no-daemon mol bond %s ...\n", args[0])
			} else {
				fmt.Fprintf(os.Stderr, "Error: no database connection\n")
			}
			os.Exit(1)
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		varFlags, _ := cmd.Flags().GetStringSlice("var")
		assignee, _ := cmd.Flags().GetString("assignee")

		// Parse variables
		vars := make(map[string]string)
		for _, v := range varFlags {
			parts := strings.SplitN(v, "=", 2)
			if len(parts) != 2 {
				fmt.Fprintf(os.Stderr, "Error: invalid variable format '%s', expected 'key=value'\n", v)
				os.Exit(1)
			}
			vars[parts[0]] = parts[1]
		}

		// Resolve molecule ID
		moleculeID, err := utils.ResolvePartialID(ctx, store, args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error resolving molecule ID %s: %v\n", args[0], err)
			os.Exit(1)
		}

		// Load the molecule subgraph
		subgraph, err := loadTemplateSubgraph(ctx, store, moleculeID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading molecule: %v\n", err)
			os.Exit(1)
		}

		// Check for missing variables
		requiredVars := extractAllVariables(subgraph)
		var missingVars []string
		for _, v := range requiredVars {
			if _, ok := vars[v]; !ok {
				missingVars = append(missingVars, v)
			}
		}
		if len(missingVars) > 0 {
			fmt.Fprintf(os.Stderr, "Error: missing required variables: %s\n", strings.Join(missingVars, ", "))
			fmt.Fprintf(os.Stderr, "Provide them with: --var %s=<value>\n", missingVars[0])
			os.Exit(1)
		}

		if dryRun {
			fmt.Printf("\nDry run: would create %d issues from molecule %s\n\n", len(subgraph.Issues), moleculeID)
			for _, issue := range subgraph.Issues {
				newTitle := substituteVariables(issue.Title, vars)
				suffix := ""
				if issue.ID == subgraph.Root.ID && assignee != "" {
					suffix = fmt.Sprintf(" (assignee: %s)", assignee)
				}
				fmt.Printf("  - %s (from %s)%s\n", newTitle, issue.ID, suffix)
			}
			if len(vars) > 0 {
				fmt.Printf("\nVariables:\n")
				for k, v := range vars {
					fmt.Printf("  {{%s}} = %s\n", k, v)
				}
			}
			return
		}

		// Clone the subgraph (bond the molecule)
		result, err := bondMolecule(ctx, store, subgraph, vars, assignee, actor)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error bonding molecule: %v\n", err)
			os.Exit(1)
		}

		// Schedule auto-flush
		markDirtyAndScheduleFlush()

		if jsonOutput {
			outputJSON(result)
			return
		}

		fmt.Printf("%s Bonded molecule: created %d issues\n", ui.RenderPass("✓"), result.Created)
		fmt.Printf("  Root issue: %s\n", result.NewEpicID)
	},
}

func init() {
	molBondCmd.Flags().StringSlice("var", []string{}, "Variable substitution (key=value)")
	molBondCmd.Flags().Bool("dry-run", false, "Preview what would be created")
	molBondCmd.Flags().String("assignee", "", "Assign the root issue to this agent/user")

	molCmd.AddCommand(molCatalogCmd)
	molCmd.AddCommand(molShowCmd)
	molCmd.AddCommand(molBondCmd)
	rootCmd.AddCommand(molCmd)
}

// =============================================================================
// Molecule Helper Functions
// =============================================================================

// bondMolecule creates new issues from the molecule with variable substitution
// Wraps cloneSubgraph from template.go and returns BondResult
func bondMolecule(ctx context.Context, s storage.Storage, subgraph *MoleculeSubgraph, vars map[string]string, assignee string, actorName string) (*InstantiateResult, error) {
	return cloneSubgraph(ctx, s, subgraph, vars, assignee, actorName)
}

// printMoleculeTree prints the molecule structure as a tree
func printMoleculeTree(subgraph *MoleculeSubgraph, parentID string, depth int, isRoot bool) {
	printTemplateTree(subgraph, parentID, depth, isRoot)
}
