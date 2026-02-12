package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// pourCmd is a top-level command for instantiating protos as persistent mols.
//
// In the molecular chemistry metaphor:
//   - Proto (solid) -> pour -> Mol (liquid)
//   - Pour creates persistent, auditable work in .beads/
var pourCmd = &cobra.Command{
	Use:   "pour <proto-id>",
	Short: "Instantiate a proto as a persistent mol (solid -> liquid)",
	Long: `Pour a proto into a persistent mol - like pouring molten metal into a mold.

This is the chemistry-inspired command for creating PERSISTENT work from templates.
The resulting mol lives in .beads/ (permanent storage) and is synced with git.

Phase transition: Proto (solid) -> pour -> Mol (liquid)

WHEN TO USE POUR vs WISP:
  pour (liquid): Persistent work that needs audit trail
    - Feature implementations spanning multiple sessions
    - Work you may need to reference later
    - Anything worth preserving in git history

  wisp (vapor): Ephemeral work that auto-cleans up
    - Release workflows (one-time execution)
    - Patrol cycles (deacon, witness, refinery)
    - Health checks and diagnostics
    - Any operational workflow without audit value

TIP: Formulas can specify phase:"vapor" to recommend wisp usage.
     If you pour a vapor-phase formula, you'll get a warning.

Examples:
  bd mol pour mol-feature --var name=auth    # Persistent feature work
  bd mol pour mol-review --var pr=123        # Persistent code review`,
	Args: cobra.ExactArgs(1),
	Run:  runPour,
}

func runPour(cmd *cobra.Command, args []string) {
	CheckReadonly("pour")
	requireDaemon("pour")
	pourViaDaemon(cmd, args)
}

// pourViaDaemon sends a pour request to the RPC daemon (bd-wj80).
func pourViaDaemon(cmd *cobra.Command, args []string) {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	varFlags, _ := cmd.Flags().GetStringArray("var")
	assignee, _ := cmd.Flags().GetString("assignee")
	attachFlags, _ := cmd.Flags().GetStringSlice("attach")
	attachType, _ := cmd.Flags().GetString("attach-type")

	vars := make(map[string]string)
	for _, v := range varFlags {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "Error: invalid variable format '%s', expected 'key=value'\n", v)
			os.Exit(1)
		}
		vars[parts[0]] = parts[1]
	}

	pourArgs := &rpc.PourArgs{
		ProtoID:     args[0],
		Vars:        vars,
		DryRun:      dryRun,
		Assignee:    assignee,
		Attachments: attachFlags,
		AttachType:  attachType,
	}

	result, err := daemonClient.Pour(pourArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Auto-materialize referenced runbooks on client side (od-dv0.6)
	if len(result.Runbooks) > 0 {
		materializeFormulaRunbooks(result.Runbooks)
	}

	if jsonOutput {
		outputJSON(result)
	} else {
		fmt.Printf("%s Poured mol: created %d issues\n", ui.RenderPass("✓"), result.Created)
		fmt.Printf("  Root issue: %s\n", result.RootID)
		fmt.Printf("  Phase: %s\n", result.Phase)
		if len(result.Runbooks) > 0 {
			fmt.Printf("  Runbooks: %d auto-materialized\n", len(result.Runbooks))
		}
		if result.Attached > 0 {
			fmt.Printf("  Attached: %d issues\n", result.Attached)
		}
	}
}

// materializeFormulaRunbooks auto-materializes runbook beads referenced by a formula (od-dv0.6).
// Each runbook reference is resolved from the database and written to .oj/runbooks/.
// Errors are logged but do not block the pour operation.
func materializeFormulaRunbooks(runbookRefs []string) {
	for _, rbRef := range runbookRefs {
		rb := loadRunbookFromDB(rbRef)
		if rb == nil {
			fmt.Fprintf(os.Stderr, "%s Runbook %q not found in database, skipping materialize\n", ui.RenderWarn("⚠"), rbRef)
			continue
		}

		// Determine output directory
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s Cannot determine working directory: %v\n", ui.RenderWarn("⚠"), err)
			continue
		}
		outDir := filepath.Join(cwd, ".oj", "runbooks")

		err = materializeOne(rb, outDir)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				// Runbook already on disk - this is fine
				continue
			}
			fmt.Fprintf(os.Stderr, "%s Materialize runbook %q: %v\n", ui.RenderWarn("⚠"), rbRef, err)
		}
	}
}

func init() {
	// Pour command flags
	pourCmd.Flags().StringArray("var", []string{}, "Variable substitution (key=value)")
	pourCmd.Flags().Bool("dry-run", false, "Preview what would be created")
	pourCmd.Flags().String("assignee", "", "Assign the root issue to this agent/user")
	pourCmd.Flags().StringSlice("attach", []string{}, "Proto to attach after spawning (repeatable)")
	pourCmd.Flags().String("attach-type", types.BondTypeSequential, "Bond type for attachments: sequential, parallel, or conditional")

	molCmd.AddCommand(pourCmd)
}
