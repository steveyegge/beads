package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/formula"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// cookCmd compiles a formula JSON into a proto bead.
var cookCmd = &cobra.Command{
	Use:   "cook <formula-file>",
	Short: "Compile a formula into a proto (ephemeral by default)",
	Long: `Cook transforms a .formula.json file into a proto.

By default, cook outputs the resolved formula as JSON to stdout for
ephemeral use. The output can be inspected, piped, or saved to a file.

Two cooking modes are available:

  COMPILE-TIME (default, --mode=compile):
    Produces a proto with {{variable}} placeholders intact.
    Use for: modeling, estimation, contractor handoff, planning.
    Variables are NOT substituted - the output shows the template structure.

  RUNTIME (--mode=runtime or when --var flags provided):
    Produces a fully-resolved proto with variables substituted.
    Use for: final validation before pour, seeing exact output.
    Requires all variables to have values (via --var or defaults).

Formulas are high-level workflow templates that support:
  - Variable definitions with defaults and validation
  - Step definitions that become issue hierarchies
  - Composition rules for bonding formulas together
  - Inheritance via extends

The --persist flag enables the legacy behavior of writing the proto
to the database. This is useful when you want to reuse the same
proto multiple times without re-cooking.

For most workflows, prefer ephemeral protos: pour and wisp commands
accept formula names directly and cook inline.

Examples:
  bd cook mol-feature.formula.json                    # Compile-time: keep {{vars}}
  bd cook mol-feature --var name=auth                 # Runtime: substitute vars
  bd cook mol-feature --mode=runtime --var name=auth  # Explicit runtime mode
  bd cook mol-feature --dry-run                       # Preview steps
  bd cook mol-release.formula.json --persist          # Write to database
  bd cook mol-release.formula.json --persist --force  # Replace existing

Output (default):
  JSON representation of the resolved formula with all steps.

Output (--persist):
  Creates a proto bead in the database with:
  - ID matching the formula name (e.g., mol-feature)
  - The "template" label for proto identification
  - Child issues for each step
  - Dependencies matching depends_on relationships`,
	Args: cobra.ExactArgs(1),
	Run:  runCook,
}

// cookResult holds the result of cooking a formula
type cookResult struct {
	ProtoID    string   `json:"proto_id"`
	Formula    string   `json:"formula"`
	Created    int      `json:"created"`
	Variables  []string `json:"variables"`
	BondPoints []string `json:"bond_points,omitempty"`
}

// cookFlags holds parsed command-line flags for the cook command
type cookFlags struct {
	dryRun      bool
	persist     bool
	force       bool
	searchPaths []string
	prefix      string
	inputVars   map[string]string
	runtimeMode bool
	formulaPath string
}

// parseCookFlags parses and validates cook command flags
func parseCookFlags(cmd *cobra.Command, args []string) (*cookFlags, error) {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	persist, _ := cmd.Flags().GetBool("persist")
	force, _ := cmd.Flags().GetBool("force")
	searchPaths, _ := cmd.Flags().GetStringSlice("search-path")
	prefix, _ := cmd.Flags().GetString("prefix")
	varFlags, _ := cmd.Flags().GetStringArray("var")
	mode, _ := cmd.Flags().GetString("mode")

	// Parse variables
	inputVars := make(map[string]string)
	for _, v := range varFlags {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid variable format '%s', expected 'key=value'", v)
		}
		inputVars[parts[0]] = parts[1]
	}

	// Validate mode
	if mode != "" && mode != "compile" && mode != "runtime" {
		return nil, fmt.Errorf("invalid mode '%s', must be 'compile' or 'runtime'", mode)
	}

	// Runtime mode is triggered by: explicit --mode=runtime OR providing --var flags
	runtimeMode := mode == "runtime" || len(inputVars) > 0

	return &cookFlags{
		dryRun:      dryRun,
		persist:     persist,
		force:       force,
		searchPaths: searchPaths,
		prefix:      prefix,
		inputVars:   inputVars,
		runtimeMode: runtimeMode,
		formulaPath: args[0],
	}, nil
}

// loadAndResolveFormula parses a formula file and applies all transformations.
// Delegates to formula.LoadAndResolve.
func loadAndResolveFormula(formulaPath string, searchPaths []string) (*formula.Formula, error) {
	return formula.LoadAndResolve(formulaPath, searchPaths)
}

// outputCookDryRun displays a dry-run preview of what would be cooked
func outputCookDryRun(resolved *formula.Formula, protoID string, runtimeMode bool, inputVars map[string]string, vars, bondPoints []string) {
	modeLabel := "compile-time"
	if runtimeMode {
		modeLabel = "runtime"
		// Apply defaults for runtime mode display
		for name, def := range resolved.Vars {
			if _, provided := inputVars[name]; !provided && def.Default != "" {
				inputVars[name] = def.Default
			}
		}
	}

	fmt.Printf("\nDry run: would cook formula %s as proto %s (%s mode)\n\n", resolved.Formula, protoID, modeLabel)

	// In runtime mode, show substituted steps
	if runtimeMode {
		substituteFormulaVars(resolved, inputVars)
		fmt.Printf("Steps (%d) [variables substituted]:\n", len(resolved.Steps))
	} else {
		fmt.Printf("Steps (%d) [{{variables}} shown as placeholders]:\n", len(resolved.Steps))
	}
	printFormulaSteps(resolved.Steps, "  ")

	if len(vars) > 0 {
		fmt.Printf("\nVariables used: %s\n", strings.Join(vars, ", "))
	}

	// Show variable values in runtime mode
	if runtimeMode && len(inputVars) > 0 {
		fmt.Printf("\nVariable values:\n")
		for name, value := range inputVars {
			fmt.Printf("  {{%s}} = %s\n", name, value)
		}
	}

	if len(bondPoints) > 0 {
		fmt.Printf("Bond points: %s\n", strings.Join(bondPoints, ", "))
	}

	// Show variable definitions (more useful in compile-time mode)
	if !runtimeMode && len(resolved.Vars) > 0 {
		fmt.Printf("\nVariable definitions:\n")
		for name, def := range resolved.Vars {
			attrs := []string{}
			if def.Required {
				attrs = append(attrs, "required")
			}
			if def.Default != "" {
				attrs = append(attrs, fmt.Sprintf("default=%s", def.Default))
			}
			if len(def.Enum) > 0 {
				attrs = append(attrs, fmt.Sprintf("enum=[%s]", strings.Join(def.Enum, ",")))
			}
			attrStr := ""
			if len(attrs) > 0 {
				attrStr = fmt.Sprintf(" (%s)", strings.Join(attrs, ", "))
			}
			fmt.Printf("  {{%s}}: %s%s\n", name, def.Description, attrStr)
		}
	}
}

// outputCookEphemeral outputs the resolved formula as JSON (ephemeral mode)
func outputCookEphemeral(resolved *formula.Formula, runtimeMode bool, inputVars map[string]string, vars []string) error {
	if runtimeMode {
		// Apply defaults from formula variable definitions
		for name, def := range resolved.Vars {
			if _, provided := inputVars[name]; !provided && def.Default != "" {
				inputVars[name] = def.Default
			}
		}

		// Check for missing required variables (gt-ink2c fix)
		// Only require variables that are DEFINED in the formula's [vars] section.
		// Variables used as {{placeholder}} in descriptions but not defined in [vars]
		// are output placeholders (e.g., {{timestamp}}) and should be left unsubstituted.
		var missingVars []string
		for name, def := range resolved.Vars {
			if _, ok := inputVars[name]; !ok {
				// Variable is defined but has no value - it's missing if it has no default
				if def.Default == "" {
					missingVars = append(missingVars, name)
				}
			}
		}
		if len(missingVars) > 0 {
			return fmt.Errorf("runtime mode requires all defined variables to have values\nMissing: %s\nProvide with: --var %s=<value>",
				strings.Join(missingVars, ", "), missingVars[0])
		}

		// Substitute variables in the formula
		substituteFormulaVars(resolved, inputVars)
	}
	outputJSON(resolved)
	return nil
}

// persistCookFormula creates a proto bead in the database (persist mode)
func persistCookFormula(ctx context.Context, resolved *formula.Formula, protoID string, force bool, vars, bondPoints []string) error {
	// Check if proto already exists
	existingProto, err := store.GetIssue(ctx, protoID)
	if err == nil && existingProto != nil {
		if !force {
			return fmt.Errorf("proto %s already exists (use --force to replace)", protoID)
		}
		// Delete existing proto and its children
		if err := deleteProtoSubgraph(ctx, store, protoID); err != nil {
			return fmt.Errorf("deleting existing proto: %w", err)
		}
	}

	// Create the proto bead from the formula
	result, err := cookFormula(ctx, store, resolved, protoID)
	if err != nil {
		return fmt.Errorf("cooking formula: %w", err)
	}

	// Schedule auto-flush
	markDirtyAndScheduleFlush()

	if jsonOutput {
		outputJSON(cookResult{
			ProtoID:    result.ProtoID,
			Formula:    resolved.Formula,
			Created:    result.Created,
			Variables:  vars,
			BondPoints: bondPoints,
		})
		return nil
	}

	fmt.Printf("%s Cooked proto: %s\n", ui.RenderPass("✓"), result.ProtoID)
	fmt.Printf("  Created %d issues\n", result.Created)
	if len(vars) > 0 {
		fmt.Printf("  Variables: %s\n", strings.Join(vars, ", "))
	}
	if len(bondPoints) > 0 {
		fmt.Printf("  Bond points: %s\n", strings.Join(bondPoints, ", "))
	}
	fmt.Printf("\nTo use: bd mol pour %s --var <name>=<value>\n", result.ProtoID)
	return nil
}

func runCook(cmd *cobra.Command, args []string) {
	// Parse and validate flags
	flags, err := parseCookFlags(cmd, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Validate store access for persist mode
	if flags.persist {
		CheckReadonly("cook --persist")
		if daemonClient != nil {
			cookViaDaemon(flags)
			return
		}
		if store == nil {
			fmt.Fprintf(os.Stderr, "Error: cook --persist requires direct database access\n")
			fmt.Fprintf(os.Stderr, "Hint: cook --persist does not yet support daemon mode\n")
			os.Exit(1)
		}
	}

	// Load and resolve the formula
	resolved, err := loadAndResolveFormula(flags.formulaPath, flags.searchPaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Apply prefix to proto ID if specified
	protoID := resolved.Formula
	if flags.prefix != "" {
		protoID = flags.prefix + resolved.Formula
	}

	// Extract variables and bond points
	vars := formula.ExtractVariables(resolved)
	var bondPoints []string
	if resolved.Compose != nil {
		for _, bp := range resolved.Compose.BondPoints {
			bondPoints = append(bondPoints, bp.ID)
		}
	}

	// Handle dry-run mode
	if flags.dryRun {
		outputCookDryRun(resolved, protoID, flags.runtimeMode, flags.inputVars, vars, bondPoints)
		return
	}

	// Handle ephemeral mode (default)
	if !flags.persist {
		if err := outputCookEphemeral(resolved, flags.runtimeMode, flags.inputVars, vars); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Handle persist mode
	if err := persistCookFormula(rootCtx, resolved, protoID, flags.force, vars, bondPoints); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// cookViaDaemon sends a cook request to the RPC daemon (bd-wj80).
func cookViaDaemon(flags *cookFlags) {
	args := &rpc.CookArgs{
		FormulaName: flags.formulaPath,
		DryRun:      flags.dryRun,
		Persist:     flags.persist,
		Force:       flags.force,
		Prefix:      flags.prefix,
		Vars:        flags.inputVars,
	}
	if flags.runtimeMode {
		args.Mode = "runtime"
	}

	result, err := daemonClient.Cook(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		outputJSON(result)
	} else {
		fmt.Printf("%s Cooked proto: %s (%d issues created)\n", ui.RenderPass("✓"), result.ProtoID, result.Created)
	}
}

// cookFormulaResult holds the result of cooking
type cookFormulaResult struct {
	ProtoID string
	Created int
}

// cookFormulaToSubgraph creates an in-memory TemplateSubgraph from a resolved formula.
// Delegates to formula.CookToSubgraph.
func cookFormulaToSubgraph(f *formula.Formula, protoID string) (*formula.TemplateSubgraph, error) {
	return formula.CookToSubgraph(f, protoID)
}



// resolveAndCookFormula loads a formula by name, resolves it, applies all transformations,
// and returns an in-memory TemplateSubgraph ready for instantiation.
// Delegates to formula.ResolveAndCook.
func resolveAndCookFormula(formulaName string, searchPaths []string) (*formula.TemplateSubgraph, error) {
	return formula.ResolveAndCook(formulaName, searchPaths)
}

// resolveAndCookFormulaWithVars loads a formula and optionally filters steps by condition.
// Delegates to formula.ResolveAndCookWithVars.
func resolveAndCookFormulaWithVars(formulaName string, searchPaths []string, conditionVars map[string]string) (*formula.TemplateSubgraph, error) {
	return formula.ResolveAndCookWithVars(formulaName, searchPaths, conditionVars)
}

// cookFormulaToSubgraphWithVars creates an in-memory subgraph with variable info attached.
// Delegates to formula.CookToSubgraphWithVars.
func cookFormulaToSubgraphWithVars(f *formula.Formula, protoID string, vars map[string]*formula.VarDef) (*formula.TemplateSubgraph, error) {
	return formula.CookToSubgraphWithVars(f, protoID, vars)
}

// cookFormula creates a proto bead from a resolved formula.
// protoID is the final ID for the proto (may include a prefix).
func cookFormula(ctx context.Context, s storage.Storage, f *formula.Formula, protoID string) (*cookFormulaResult, error) {
	if s == nil {
		return nil, fmt.Errorf("no database connection")
	}

	// Check for SQLite store (needed for batch create with skip prefix)
	sqliteStore, ok := s.(*sqlite.SQLiteStorage)
	if !ok {
		return nil, fmt.Errorf("cook requires SQLite storage")
	}

	// Map step ID -> created issue ID
	idMapping := make(map[string]string)

	// Collect all issues and dependencies
	var issues []*types.Issue
	var deps []*types.Dependency
	var labels []struct{ issueID, label string }

	// Determine root title: use {{title}} placeholder if the variable is defined,
	// otherwise fall back to formula name (GH#852)
	rootTitle := f.Formula
	if _, hasTitle := f.Vars["title"]; hasTitle {
		rootTitle = "{{title}}"
	}

	// Determine root description: use {{desc}} placeholder if the variable is defined,
	// otherwise fall back to formula description (GH#852)
	rootDesc := f.Description
	if _, hasDesc := f.Vars["desc"]; hasDesc {
		rootDesc = "{{desc}}"
	}

	// Create root proto epic using provided protoID (may include prefix)
	rootIssue := &types.Issue{
		ID:          protoID,
		Title:       rootTitle,
		Description: rootDesc,
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeEpic,
		IsTemplate:  true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	issues = append(issues, rootIssue)
	labels = append(labels, struct{ issueID, label string }{protoID, MoleculeLabel})

	// Collect issues for each step (use protoID as parent for step IDs)
	// Use labelHandler to extract labels for separate DB storage
	formula.CollectSteps(f.Steps, protoID, idMapping, nil, &issues, &deps, func(issueID, label string) {
		labels = append(labels, struct{ issueID, label string }{issueID, label})
	}, nil)

	// Collect dependencies from depends_on
	for _, step := range f.Steps {
		formula.CollectDependencies(step, idMapping, &deps)
	}

	// Create all issues using batch with skip prefix validation
	opts := sqlite.BatchCreateOptions{
		SkipPrefixValidation: true, // Molecules use mol-* prefix
	}
	if err := sqliteStore.CreateIssuesWithFullOptions(ctx, issues, actor, opts); err != nil {
		return nil, fmt.Errorf("failed to create issues: %w", err)
	}

	// Track if we need cleanup on failure
	issuesCreated := true

	// Add labels and dependencies in a transaction
	err := s.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Add labels
		for _, l := range labels {
			if err := tx.AddLabel(ctx, l.issueID, l.label, actor); err != nil {
				return fmt.Errorf("failed to add label %s to %s: %w", l.label, l.issueID, err)
			}
		}

		// Add dependencies
		for _, dep := range deps {
			if err := tx.AddDependency(ctx, dep, actor); err != nil {
				return fmt.Errorf("failed to create dependency: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		// Clean up: delete the issues we created since labels/deps failed
		if issuesCreated {
			cleanupErr := s.RunInTransaction(ctx, func(tx storage.Transaction) error {
				for i := len(issues) - 1; i >= 0; i-- {
					_ = tx.DeleteIssue(ctx, issues[i].ID) // Best effort cleanup
				}
				return nil
			})
			if cleanupErr != nil {
				return nil, fmt.Errorf("%w (cleanup also failed: %v)", err, cleanupErr)
			}
		}
		return nil, err
	}

	return &cookFormulaResult{
		ProtoID: protoID,
		Created: len(issues),
	}, nil
}


// deleteProtoSubgraph deletes a proto and all its children.
func deleteProtoSubgraph(ctx context.Context, s storage.Storage, protoID string) error {
	// Load the subgraph
	subgraph, err := loadTemplateSubgraph(ctx, s, protoID)
	if err != nil {
		return fmt.Errorf("load proto: %w", err)
	}

	// Delete in reverse order (children first)
	return s.RunInTransaction(ctx, func(tx storage.Transaction) error {
		for i := len(subgraph.Issues) - 1; i >= 0; i-- {
			issue := subgraph.Issues[i]
			if err := tx.DeleteIssue(ctx, issue.ID); err != nil {
				return fmt.Errorf("delete %s: %w", issue.ID, err)
			}
		}
		return nil
	})
}

// printFormulaSteps prints steps in a tree format.
func printFormulaSteps(steps []*formula.Step, indent string) {
	for i, step := range steps {
		connector := "├──"
		if i == len(steps)-1 {
			connector = "└──"
		}

		// Collect dependency info
		var depParts []string
		if len(step.DependsOn) > 0 {
			depParts = append(depParts, fmt.Sprintf("depends: %s", strings.Join(step.DependsOn, ", ")))
		}
		if len(step.Needs) > 0 {
			depParts = append(depParts, fmt.Sprintf("needs: %s", strings.Join(step.Needs, ", ")))
		}
		if step.WaitsFor != "" {
			depParts = append(depParts, fmt.Sprintf("waits_for: %s", step.WaitsFor))
		}

		depStr := ""
		if len(depParts) > 0 {
			depStr = fmt.Sprintf(" [%s]", strings.Join(depParts, ", "))
		}

		typeStr := ""
		if step.Type != "" && step.Type != "task" {
			typeStr = fmt.Sprintf(" (%s)", step.Type)
		}

		// Source tracing info
		sourceStr := ""
		if step.SourceFormula != "" || step.SourceLocation != "" {
			sourceStr = fmt.Sprintf(" [from: %s@%s]", step.SourceFormula, step.SourceLocation)
		}

		fmt.Printf("%s%s %s: %s%s%s%s\n", indent, connector, step.ID, step.Title, typeStr, depStr, sourceStr)

		if len(step.Children) > 0 {
			childIndent := indent
			if i == len(steps)-1 {
				childIndent += "    "
			} else {
				childIndent += "│   "
			}
			printFormulaSteps(step.Children, childIndent)
		}
	}
}

// substituteFormulaVars substitutes {{variable}} placeholders in a formula.
// Delegates to formula.SubstituteFormulaVars.
func substituteFormulaVars(f *formula.Formula, vars map[string]string) {
	formula.SubstituteFormulaVars(f, vars)
}

// substituteStepVars substitutes {{variable}} placeholders in steps recursively.
// Delegates to formula.SubstituteStepVars.
func substituteStepVars(steps []*formula.Step, vars map[string]string) {
	formula.SubstituteStepVars(steps, vars)
}

// createGateIssue creates a gate issue for a step with a Gate field.
// Delegates to formula.CreateGateIssue.
func createGateIssue(step *formula.Step, parentID string) *types.Issue {
	return formula.CreateGateIssue(step, parentID)
}

// createDecisionIssue creates a decision issue for a step with a Decision field.
// Delegates to formula.CreateDecisionIssue.
func createDecisionIssue(step *formula.Step, parentID string) (*types.Issue, *types.DecisionPoint) {
	return formula.CreateDecisionIssue(step, parentID)
}

func init() {
	cookCmd.Flags().Bool("dry-run", false, "Preview what would be created")
	cookCmd.Flags().Bool("persist", false, "Persist proto to database (legacy behavior)")
	cookCmd.Flags().Bool("force", false, "Replace existing proto if it exists (requires --persist)")
	cookCmd.Flags().StringSlice("search-path", []string{}, "Additional paths to search for formula inheritance")
	cookCmd.Flags().String("prefix", "", "Prefix to prepend to proto ID (e.g., 'gt-' creates 'gt-mol-feature')")
	cookCmd.Flags().StringArray("var", []string{}, "Variable substitution (key=value), enables runtime mode")
	cookCmd.Flags().String("mode", "", "Cooking mode: compile (keep placeholders) or runtime (substitute vars)")

	rootCmd.AddCommand(cookCmd)
}
