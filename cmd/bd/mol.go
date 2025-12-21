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
//   - Proto: Uninstantiated template (easter egg: 'protomolecule' alias)
//   - Molecule: A spawned instance of a proto
//   - Spawn: Instantiate a proto, creating real issues from the template
//   - Bond: Polymorphic combine operation (proto+proto, proto+mol, mol+mol)
//   - Distill: Extract ad-hoc epic → reusable proto
//   - Compound: Result of bonding
//
// Usage:
//   bd mol catalog                        # List available protos
//   bd mol show <id>                      # Show proto/molecule structure
//   bd mol spawn <id> --var key=value     # Instantiate proto → molecule

// MoleculeLabel is the label used to identify molecules (templates)
// Molecules use the same label as templates - they ARE templates with workflow semantics
const MoleculeLabel = BeadsTemplateLabel

// MoleculeSubgraph is an alias for TemplateSubgraph
// Molecules and templates share the same subgraph structure
type MoleculeSubgraph = TemplateSubgraph

var molCmd = &cobra.Command{
	Use:     "mol",
	Aliases: []string{"protomolecule"}, // Easter egg for The Expanse fans
	Short:   "Molecule commands (work templates)",
	Long: `Manage molecules - work templates for agent workflows.

Protos are template epics with the "template" label. They define a DAG of work
that can be spawned to create real issues (molecules).

The molecule metaphor:
  - A proto is an uninstantiated template (reusable work pattern)
  - Spawning creates a molecule (real issues) from the proto
  - Variables ({{key}}) are substituted during spawning
  - Bonding combines protos or molecules into compounds
  - Distilling extracts a proto from an ad-hoc epic

Commands:
  catalog  List available protos
  show     Show proto/molecule structure and variables
  spawn    Instantiate a proto → molecule
  run      Spawn + assign + pin for durable execution`,
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
			fmt.Println("No protos available.")
			fmt.Println("\nTo create a proto:")
			fmt.Println("  1. Create an epic with child issues")
			fmt.Println("  2. Add the 'template' label: bd label add <epic-id> template")
			fmt.Println("  3. Use {{variable}} placeholders in titles/descriptions")
			fmt.Println("\nTo spawn (instantiate) a molecule from a proto:")
			fmt.Println("  bd mol spawn <id> --var key=value")
			return
		}

		fmt.Printf("%s\n", ui.RenderPass("Protos (for bd mol spawn):"))
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

var molSpawnCmd = &cobra.Command{
	Use:   "spawn <proto-id>",
	Short: "Instantiate a proto into a molecule",
	Long: `Spawn a molecule by instantiating a proto template into real issues.

Variables are specified with --var key=value flags. The proto's {{key}}
placeholders will be replaced with the corresponding values.

Example:
  bd mol spawn mol-code-review --var pr=123 --var repo=myproject
  bd mol spawn bd-abc123 --var version=1.2.0 --assignee=worker-1`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("mol spawn")

		ctx := rootCtx

		// mol spawn requires direct store access for subgraph loading and cloning
		if store == nil {
			if daemonClient != nil {
				fmt.Fprintf(os.Stderr, "Error: mol spawn requires direct database access\n")
				fmt.Fprintf(os.Stderr, "Hint: use --no-daemon flag: bd --no-daemon mol spawn %s ...\n", args[0])
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

		// Clone the subgraph (spawn the molecule)
		result, err := spawnMolecule(ctx, store, subgraph, vars, assignee, actor)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error spawning molecule: %v\n", err)
			os.Exit(1)
		}

		// Schedule auto-flush
		markDirtyAndScheduleFlush()

		if jsonOutput {
			outputJSON(result)
			return
		}

		fmt.Printf("%s Spawned molecule: created %d issues\n", ui.RenderPass("✓"), result.Created)
		fmt.Printf("  Root issue: %s\n", result.NewEpicID)
	},
}

var molRunCmd = &cobra.Command{
	Use:   "run <proto-id>",
	Short: "Spawn proto and start execution (spawn + assign + pin)",
	Long: `Run a molecule by spawning a proto and setting up for durable execution.

This command:
  1. Spawns the molecule (creates issues from proto template)
  2. Assigns the root issue to the caller
  3. Sets root status to in_progress
  4. Pins the root issue for session recovery

After a crash or session reset, the pinned root issue ensures the agent
can resume from where it left off by checking 'bd ready'.

Example:
  bd mol run mol-version-bump --var version=1.2.0
  bd mol run bd-qqc --var version=0.32.0 --var date=2025-01-01`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("mol run")

		ctx := rootCtx

		// mol run requires direct store access
		if store == nil {
			if daemonClient != nil {
				fmt.Fprintf(os.Stderr, "Error: mol run requires direct database access\n")
				fmt.Fprintf(os.Stderr, "Hint: use --no-daemon flag: bd --no-daemon mol run %s ...\n", args[0])
			} else {
				fmt.Fprintf(os.Stderr, "Error: no database connection\n")
			}
			os.Exit(1)
		}

		varFlags, _ := cmd.Flags().GetStringSlice("var")

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

		// Spawn the molecule with actor as assignee
		result, err := spawnMolecule(ctx, store, subgraph, vars, actor, actor)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error spawning molecule: %v\n", err)
			os.Exit(1)
		}

		// Update root issue: set status=in_progress and pinned=true
		rootID := result.NewEpicID
		updates := map[string]interface{}{
			"status": string(types.StatusInProgress),
			"pinned": true,
		}
		if err := store.UpdateIssue(ctx, rootID, updates, actor); err != nil {
			fmt.Fprintf(os.Stderr, "Error updating root issue: %v\n", err)
			os.Exit(1)
		}

		// Schedule auto-flush
		markDirtyAndScheduleFlush()

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"root_id":   rootID,
				"created":   result.Created,
				"id_mapping": result.IDMapping,
				"pinned":    true,
				"status":    "in_progress",
				"assignee":  actor,
			})
			return
		}

		fmt.Printf("%s Molecule running: created %d issues\n", ui.RenderPass("✓"), result.Created)
		fmt.Printf("  Root issue: %s (pinned, in_progress)\n", rootID)
		fmt.Printf("  Assignee: %s\n", actor)
		fmt.Println("\nNext steps:")
		fmt.Printf("  bd ready                # Find unblocked work in this molecule\n")
		fmt.Printf("  bd show %s       # View molecule status\n", rootID[:8])
	},
}

func init() {
	molSpawnCmd.Flags().StringSlice("var", []string{}, "Variable substitution (key=value)")
	molSpawnCmd.Flags().Bool("dry-run", false, "Preview what would be created")
	molSpawnCmd.Flags().String("assignee", "", "Assign the root issue to this agent/user")

	molRunCmd.Flags().StringSlice("var", []string{}, "Variable substitution (key=value)")

	molCmd.AddCommand(molCatalogCmd)
	molCmd.AddCommand(molShowCmd)
	molCmd.AddCommand(molSpawnCmd)
	molCmd.AddCommand(molRunCmd)
	rootCmd.AddCommand(molCmd)
}

// =============================================================================
// Molecule Helper Functions
// =============================================================================

// spawnMolecule creates new issues from the proto with variable substitution.
// This instantiates a proto (template) into a molecule (real issues).
// Wraps cloneSubgraph from template.go and returns SpawnResult.
func spawnMolecule(ctx context.Context, s storage.Storage, subgraph *MoleculeSubgraph, vars map[string]string, assignee string, actorName string) (*InstantiateResult, error) {
	return cloneSubgraph(ctx, s, subgraph, vars, assignee, actorName)
}

// printMoleculeTree prints the molecule structure as a tree
func printMoleculeTree(subgraph *MoleculeSubgraph, parentID string, depth int, isRoot bool) {
	printTemplateTree(subgraph, parentID, depth, isRoot)
}
