// Package main implements the bd CLI dependency management commands.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/deps"
	"github.com/steveyegge/beads/internal/routing"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

// getBeadsDir returns the .beads directory path, derived from the global dbPath.
func getBeadsDir() string {
	if dbPath != "" {
		return filepath.Dir(dbPath)
	}
	return ""
}

// isChildOf delegates to deps.IsChildOf.
func isChildOf(childID, parentID string) bool {
	return deps.IsChildOf(childID, parentID)
}

// warnIfCyclesExist checks for dependency cycles and prints a warning if found.
func warnIfCyclesExist(s *dolt.DoltStore) {
	if s == nil {
		return // Skip cycle check if store is not available
	}
	cycles, err := s.DetectCycles(rootCtx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to check for cycles: %v\n", err)
		return
	}
	if len(cycles) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "\n%s Warning: Dependency cycle detected!\n", ui.RenderWarn("⚠"))
	fmt.Fprintf(os.Stderr, "This can hide issues from the ready work list and cause confusion.\n\n")
	fmt.Fprintf(os.Stderr, "Cycle path:\n")
	for _, cycle := range cycles {
		for j, issue := range cycle {
			if j == 0 {
				fmt.Fprintf(os.Stderr, "  %s", issue.ID)
			} else {
				fmt.Fprintf(os.Stderr, " → %s", issue.ID)
			}
		}
		if len(cycle) > 0 {
			fmt.Fprintf(os.Stderr, " → %s", cycle[0].ID)
		}
		fmt.Fprintf(os.Stderr, "\n")
	}
	fmt.Fprintf(os.Stderr, "\nRun 'bd dep cycles' for detailed analysis.\n\n")
}

var depCmd = &cobra.Command{
	Use:     "dep [issue-id]",
	GroupID: "deps",
	Short:   "Manage dependencies",
	Long: `Manage dependencies between issues.

When called with an issue ID and --blocks flag, creates a blocking dependency:
  bd dep <blocker-id> --blocks <blocked-id>

This is equivalent to:
  bd dep add <blocked-id> <blocker-id>

Examples:
  bd dep bd-xyz --blocks bd-abc    # bd-xyz blocks bd-abc
  bd dep add bd-abc bd-xyz         # Same as above (bd-abc depends on bd-xyz)`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		blocksID, _ := cmd.Flags().GetString("blocks")

		// If no args and no flags, show help
		if len(args) == 0 && blocksID == "" {
			_ = cmd.Help() // Help() always returns nil for cobra commands
			return
		}

		// If --blocks flag is provided, create a blocking dependency
		if blocksID != "" {
			if len(args) != 1 {
				FatalErrorRespectJSON("--blocks requires exactly one issue ID argument")
			}
			blockerID := args[0]

			CheckReadonly("dep --blocks")

			ctx := rootCtx
			depType := "blocks"

			// Resolve partial IDs first
			var fromID, toID string
			var err error
			fromID, err = utils.ResolvePartialID(ctx, store, blocksID)
			if err != nil {
				FatalErrorRespectJSON("resolving issue ID %s: %v", blocksID, err)
			}

			toID, err = utils.ResolvePartialID(ctx, store, blockerID)
			if err != nil {
				FatalErrorRespectJSON("resolving issue ID %s: %v", blockerID, err)
			}

			// Check for child→parent dependency anti-pattern
			if isChildOf(fromID, toID) {
				FatalErrorRespectJSON("cannot add dependency: %s is already a child of %s. Children inherit dependency on parent completion via hierarchy. Adding an explicit dependency would create a deadlock", fromID, toID)
			}

			// Direct mode
			dep := &types.Dependency{
				IssueID:     fromID,
				DependsOnID: toID,
				Type:        types.DependencyType(depType),
			}

			if err := store.AddDependency(ctx, dep, actor); err != nil {
				FatalErrorRespectJSON("%v", err)
			}

			// Check for cycles after adding dependency (both daemon and direct mode)
			warnIfCyclesExist(store)

			if jsonOutput {
				outputJSON(map[string]interface{}{
					"status":     "added",
					"blocker_id": toID,
					"blocked_id": fromID,
					"type":       depType,
				})
				return
			}

			fmt.Printf("%s Added dependency: %s blocks %s\n",
				ui.RenderPass("✓"), toID, fromID)
			return
		}

		// If we have an arg but no --blocks flag, show help
		_ = cmd.Help() // Help() always returns nil for cobra commands
	},
}

var depAddCmd = &cobra.Command{
	Use:   "add [issue-id] [depends-on-id]",
	Short: "Add a dependency",
	Long: `Add a dependency between two issues.

The depends-on-id can be provided as:
  - A positional argument: bd dep add issue-123 issue-456
  - A flag: bd dep add issue-123 --blocked-by issue-456
  - A flag: bd dep add issue-123 --depends-on issue-456

The --blocked-by and --depends-on flags are aliases and both mean "issue-123
depends on (is blocked by) the specified issue."

The depends-on-id can be:
  - A local issue ID (e.g., bd-xyz)
  - An external reference: external:<project>:<capability>

External references are stored as-is and resolved at query time using
the external_projects config. They block the issue until the capability
is "shipped" in the target project.

Examples:
  bd dep add bd-42 bd-41                              # Positional args
  bd dep add bd-42 --blocked-by bd-41                 # Flag syntax (same effect)
  bd dep add bd-42 --depends-on bd-41                 # Alias (same effect)
  bd dep add gt-xyz external:beads:mol-run-assignee   # Cross-project dependency`,
	Args: func(cmd *cobra.Command, args []string) error {
		blockedBy, _ := cmd.Flags().GetString("blocked-by")
		dependsOn, _ := cmd.Flags().GetString("depends-on")
		hasFlag := blockedBy != "" || dependsOn != ""

		// If a flag is provided, we only need 1 positional arg (the dependent issue)
		if hasFlag {
			if len(args) < 1 {
				return fmt.Errorf("requires at least 1 arg(s), only received %d", len(args))
			}
			if len(args) > 1 {
				return fmt.Errorf("cannot use both positional depends-on-id and --blocked-by/--depends-on flag")
			}
			return nil
		}
		// No flag provided, need exactly 2 positional args
		if len(args) != 2 {
			return fmt.Errorf("requires 2 arg(s), only received %d (or use --blocked-by/--depends-on flag)", len(args))
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("dep add")
		depType, _ := cmd.Flags().GetString("type")

		// Get the dependency target from flag or positional arg
		blockedBy, _ := cmd.Flags().GetString("blocked-by")
		dependsOn, _ := cmd.Flags().GetString("depends-on")

		var dependsOnArg string
		if blockedBy != "" {
			dependsOnArg = blockedBy
		} else if dependsOn != "" {
			dependsOnArg = dependsOn
		} else {
			dependsOnArg = args[1]
		}

		ctx := rootCtx

		// Resolve partial IDs first
		var fromID, toID string

		// Check if toID is an external reference (don't resolve it)
		isExternalRef := strings.HasPrefix(dependsOnArg, "external:")

		var err error
		fromID, err = utils.ResolvePartialID(ctx, store, args[0])
		if err != nil {
			FatalErrorRespectJSON("resolving issue ID %s: %v", args[0], err)
		}

		if isExternalRef {
			// External references are stored as-is
			toID = dependsOnArg
			// Validate format: external:<project>:<capability>
			if err := validateExternalRef(toID); err != nil {
				FatalErrorRespectJSON("%v", err)
			}
		} else {
			toID, err = utils.ResolvePartialID(ctx, store, dependsOnArg)
			if err != nil {
				// Resolution failed - try auto-converting to external ref
				beadsDir := getBeadsDir()
				if extRef := routing.ResolveToExternalRef(dependsOnArg, beadsDir); extRef != "" {
					toID = extRef
					isExternalRef = true
				} else {
					FatalErrorRespectJSON("resolving dependency ID %s: %v", dependsOnArg, err)
				}
			}
		}

		// Check for child→parent dependency anti-pattern
		// This creates a deadlock: child can't start (parent open), parent can't close (children not done)
		if isChildOf(fromID, toID) {
			FatalErrorRespectJSON("cannot add dependency: %s is already a child of %s. Children inherit dependency on parent completion via hierarchy. Adding an explicit dependency would create a deadlock", fromID, toID)
		}

		// Direct mode
		dep := &types.Dependency{
			IssueID:     fromID,
			DependsOnID: toID,
			Type:        types.DependencyType(depType),
		}

		if err := store.AddDependency(ctx, dep, actor); err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		// Check for cycles after adding dependency
		warnIfCyclesExist(store)

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"status":        "added",
				"issue_id":      fromID,
				"depends_on_id": toID,
				"type":          depType,
			})
			return
		}

		fmt.Printf("%s Added dependency: %s depends on %s (%s)\n",
			ui.RenderPass("✓"), fromID, toID, depType)
	},
}

var depListCmd = &cobra.Command{
	Use:   "list [issue-id]",
	Short: "List dependencies or dependents of an issue",
	Long: `List dependencies or dependents of an issue with optional type filtering.

By default shows dependencies (what this issue depends on). Use --direction to control:
  - down: Show dependencies (what this issue depends on) - default
  - up:   Show dependents (what depends on this issue)

Use --type to filter by dependency type (e.g., tracks, blocks, parent-child).

Examples:
  bd dep list gt-abc                     # Show what gt-abc depends on
  bd dep list gt-abc --direction=up      # Show what depends on gt-abc
  bd dep list gt-abc --direction=up -t tracks  # Show what tracks gt-abc (convoy tracking)`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		// Resolve partial ID with cross-rig routing support
		var fullID string
		var depStore *dolt.DoltStore // store to query dependencies from
		var routedResult *RoutedResult
		defer func() {
			if routedResult != nil {
				routedResult.Close()
			}
		}()

		// Direct mode - use routing-aware resolution
		var err error
		routedResult, err = resolveAndGetIssueWithRouting(ctx, store, args[0])
		if err != nil {
			FatalErrorRespectJSON("resolving %s: %v", args[0], err)
		}
		if routedResult == nil || routedResult.Issue == nil {
			FatalErrorRespectJSON("no issue found: %s", args[0])
		}
		fullID = routedResult.ResolvedID
		if routedResult.Routed {
			depStore = routedResult.Store
		}

		// If no routed store was used, use local storage
		if depStore == nil {
			depStore = store
		}

		direction, _ := cmd.Flags().GetString("direction")
		typeFilter, _ := cmd.Flags().GetString("type")

		if direction == "" {
			direction = "down"
		}

		var issues []*types.IssueWithDependencyMetadata

		if direction == "up" {
			issues, err = depStore.GetDependentsWithMetadata(ctx, fullID)
		} else {
			issues, err = depStore.GetDependenciesWithMetadata(ctx, fullID)
		}
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		// Resolve external references (cross-rig dependencies)
		// GetDependenciesWithMetadata only returns local issues, so we need to
		// fetch raw dependency records and resolve external refs separately
		if direction == "down" {
			externalIssues := resolveExternalDependencies(ctx, depStore, fullID, typeFilter)
			issues = append(issues, externalIssues...)
		}

		// Apply type filter if specified
		if typeFilter != "" {
			var filtered []*types.IssueWithDependencyMetadata
			for _, iss := range issues {
				if string(iss.DependencyType) == typeFilter {
					filtered = append(filtered, iss)
				}
			}
			issues = filtered
		}

		if jsonOutput {
			if issues == nil {
				issues = []*types.IssueWithDependencyMetadata{}
			}
			outputJSON(issues)
			return
		}

		if len(issues) == 0 {
			if typeFilter != "" {
				if direction == "up" {
					fmt.Printf("\nNo issues depend on %s with type '%s'\n", fullID, typeFilter)
				} else {
					fmt.Printf("\n%s has no dependencies of type '%s'\n", fullID, typeFilter)
				}
			} else {
				if direction == "up" {
					fmt.Printf("\nNo issues depend on %s\n", fullID)
				} else {
					fmt.Printf("\n%s has no dependencies\n", fullID)
				}
			}
			return
		}

		if direction == "up" {
			fmt.Printf("\n%s Issues that depend on %s:\n\n", ui.RenderAccent("📋"), fullID)
		} else {
			fmt.Printf("\n%s %s depends on:\n\n", ui.RenderAccent("📋"), fullID)
		}

		for _, iss := range issues {
			// Color the ID based on status
			var idStr string
			switch iss.Status {
			case types.StatusOpen:
				idStr = ui.StatusOpenStyle.Render(iss.ID)
			case types.StatusInProgress:
				idStr = ui.StatusInProgressStyle.Render(iss.ID)
			case types.StatusBlocked:
				idStr = ui.StatusBlockedStyle.Render(iss.ID)
			case types.StatusClosed:
				idStr = ui.StatusClosedStyle.Render(iss.ID)
			default:
				idStr = iss.ID
			}

			fmt.Printf("  %s: %s [P%d] (%s) via %s\n",
				idStr, iss.Title, iss.Priority, iss.Status, iss.DependencyType)
		}
		fmt.Println()
	},
}

var depRemoveCmd = &cobra.Command{
	Use:     "remove [issue-id] [depends-on-id]",
	Aliases: []string{"rm"},
	Short:   "Remove a dependency",
	Args:    cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("dep remove")
		ctx := rootCtx

		// Resolve partial IDs first
		var fromID, toID string
		var err error
		fromID, err = utils.ResolvePartialID(ctx, store, args[0])
		if err != nil {
			FatalErrorRespectJSON("resolving issue ID %s: %v", args[0], err)
		}

		toID, err = utils.ResolvePartialID(ctx, store, args[1])
		if err != nil {
			FatalErrorRespectJSON("resolving dependency ID %s: %v", args[1], err)
		}

		// Direct mode
		fullFromID := fromID
		fullToID := toID

		if err := store.RemoveDependency(ctx, fullFromID, fullToID, actor); err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"status":        "removed",
				"issue_id":      fullFromID,
				"depends_on_id": fullToID,
			})
			return
		}

		fmt.Printf("%s Removed dependency: %s no longer depends on %s\n",
			ui.RenderPass("✓"), fullFromID, fullToID)
	},
}

var depTreeCmd = &cobra.Command{
	Use:   "tree [issue-id]",
	Short: "Show dependency tree",
	Long: `Show dependency tree rooted at the given issue.

By default, shows dependencies (what blocks this issue). Use --direction to control:
  - down: Show dependencies (what blocks this issue) - default
  - up:   Show dependents (what this issue blocks)
  - both: Show full graph in both directions

Examples:
  bd dep tree gt-0iqq                    # Show what blocks gt-0iqq
  bd dep tree gt-0iqq --direction=up     # Show what gt-0iqq blocks
  bd dep tree gt-0iqq --status=open      # Only show open issues
  bd dep tree gt-0iqq --depth=3          # Limit to 3 levels deep`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		// Resolve partial ID first
		var fullID string
		var err error
		fullID, err = utils.ResolvePartialID(ctx, store, args[0])
		if err != nil {
			FatalErrorRespectJSON("resolving %s: %v", args[0], err)
		}

		showAllPaths, _ := cmd.Flags().GetBool("show-all-paths")
		maxDepth, _ := cmd.Flags().GetInt("max-depth")
		reverse, _ := cmd.Flags().GetBool("reverse")
		direction, _ := cmd.Flags().GetString("direction")
		statusFilter, _ := cmd.Flags().GetString("status")
		formatStr, _ := cmd.Flags().GetString("format")

		// Handle --direction flag (takes precedence over deprecated --reverse)
		if direction == "" && reverse {
			direction = "up"
		} else if direction == "" {
			direction = "down"
		}

		// Validate direction
		if direction != "down" && direction != "up" && direction != "both" {
			FatalErrorRespectJSON("--direction must be 'down', 'up', or 'both'")
		}

		if maxDepth < 1 {
			FatalErrorRespectJSON("--max-depth must be >= 1")
		}

		// For "both" direction, we need to fetch both trees and merge them
		var tree []*types.TreeNode

		if direction == "both" {
			// Get dependencies (down) - what blocks this issue
			downTree, err := store.GetDependencyTree(ctx, fullID, maxDepth, showAllPaths, false)
			if err != nil {
				FatalErrorRespectJSON("%v", err)
			}

			// Get dependents (up) - what this issue blocks
			upTree, err := store.GetDependencyTree(ctx, fullID, maxDepth, showAllPaths, true)
			if err != nil {
				FatalErrorRespectJSON("%v", err)
			}

			// Merge: root appears once, dependencies below, dependents above
			// We'll show dependents first (with negative-like positioning conceptually),
			// then root, then dependencies
			tree = mergeBidirectionalTrees(downTree, upTree, fullID)
		} else {
			tree, err = store.GetDependencyTree(ctx, fullID, maxDepth, showAllPaths, direction == "up")
			if err != nil {
				FatalErrorRespectJSON("%v", err)
			}
		}

		// Apply status filter if specified
		if statusFilter != "" {
			tree = filterTreeByStatus(tree, types.Status(statusFilter))
		}

		// Handle mermaid format
		if formatStr == "mermaid" {
			outputMermaidTree(tree, args[0])
			return
		}

		if jsonOutput {
			// Always output array, even if empty
			if tree == nil {
				tree = []*types.TreeNode{}
			}
			outputJSON(tree)
			return
		}

		if len(tree) == 0 {
			switch direction {
			case "up":
				fmt.Printf("\n%s has no dependents\n", fullID)
			case "both":
				fmt.Printf("\n%s has no dependencies or dependents\n", fullID)
			default:
				fmt.Printf("\n%s has no dependencies\n", fullID)
			}
			return
		}

		switch direction {
		case "up":
			fmt.Printf("\n%s Dependent tree for %s:\n\n", ui.RenderAccent("🌲"), fullID)
		case "both":
			fmt.Printf("\n%s Full dependency graph for %s:\n\n", ui.RenderAccent("🌲"), fullID)
		default:
			fmt.Printf("\n%s Dependency tree for %s:\n\n", ui.RenderAccent("🌲"), fullID)
		}

		// Render tree with proper connectors
		renderTree(tree, maxDepth, direction)
		fmt.Println()
	},
}

var depCyclesCmd = &cobra.Command{
	Use:   "cycles",
	Short: "Detect dependency cycles",
	Run: func(cmd *cobra.Command, args []string) {

		ctx := rootCtx
		cycles, err := store.DetectCycles(ctx)
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		if jsonOutput {
			// Always output array, even if empty
			if cycles == nil {
				cycles = [][]*types.Issue{}
			}
			outputJSON(cycles)
			return
		}

		if len(cycles) == 0 {
			fmt.Printf("\n%s No dependency cycles detected\n\n", ui.RenderPass("✓"))
			return
		}

		fmt.Printf("\n%s Found %d dependency cycles:\n\n", ui.RenderFail("⚠"), len(cycles))
		for i, cycle := range cycles {
			fmt.Printf("%d. Cycle involving:\n", i+1)
			for _, issue := range cycle {
				fmt.Printf("   - %s: %s\n", issue.ID, issue.Title)
			}
			fmt.Println()
		}
	},
}

// outputMermaidTree delegates to deps.OutputMermaidTree.
func outputMermaidTree(tree []*types.TreeNode, rootID string) {
	deps.OutputMermaidTree(tree, rootID)
}

// getStatusEmoji delegates to deps.GetStatusEmoji.
func getStatusEmoji(status types.Status) string {
	return deps.GetStatusEmoji(status)
}

// renderTree renders the tree with proper box-drawing connectors using deps.TreeRenderer.
func renderTree(tree []*types.TreeNode, maxDepth int, direction string) {
	if len(tree) == 0 {
		return
	}

	r := deps.NewTreeRenderer(maxDepth, direction)
	r.MutedFunc = func(s string) string { return ui.RenderMuted(s) }
	r.WarnFunc = func(s string) string { return ui.RenderWarn(s) }
	r.StyleFunc = func(status types.Status, text string) string {
		switch status {
		case types.StatusOpen:
			return ui.StatusOpenStyle.Render(text)
		case types.StatusInProgress:
			return ui.StatusInProgressStyle.Render(text)
		case types.StatusBlocked:
			return ui.StatusBlockedStyle.Render(text)
		case types.StatusClosed:
			return ui.StatusClosedStyle.Render(text)
		default:
			return text
		}
	}
	r.PassStyleBold = func(s string) string { return ui.PassStyle.Bold(true).Render(s) }
	r.IsExternalRef = deps.IsExternalRef
	r.RenderTree(tree)
}

// formatTreeNode formats a single tree node with status, ready indicator, etc.
// This wrapper preserves the original function signature used by tests.
func formatTreeNode(node *types.TreeNode) string {
	styleFunc := func(status types.Status, text string) string {
		switch status {
		case types.StatusOpen:
			return ui.StatusOpenStyle.Render(text)
		case types.StatusInProgress:
			return ui.StatusInProgressStyle.Render(text)
		case types.StatusBlocked:
			return ui.StatusBlockedStyle.Render(text)
		case types.StatusClosed:
			return ui.StatusClosedStyle.Render(text)
		default:
			return text
		}
	}
	passStyleBold := func(s string) string { return ui.PassStyle.Bold(true).Render(s) }
	warnFunc := func(s string) string { return ui.RenderWarn(s) }
	return deps.FormatTreeNode(node, styleFunc, passStyleBold, warnFunc, deps.IsExternalRef)
}

// filterTreeByStatus delegates to deps.FilterTreeByStatus.
func filterTreeByStatus(tree []*types.TreeNode, status types.Status) []*types.TreeNode {
	return deps.FilterTreeByStatus(tree, status)
}

// mergeBidirectionalTrees delegates to deps.MergeBidirectionalTrees.
func mergeBidirectionalTrees(downTree, upTree []*types.TreeNode, rootID string) []*types.TreeNode {
	return deps.MergeBidirectionalTrees(downTree, upTree, rootID)
}

// validateExternalRef delegates to deps.ValidateExternalRef.
func validateExternalRef(ref string) error {
	return deps.ValidateExternalRef(ref)
}

// IsExternalRef delegates to deps.IsExternalRef.
func IsExternalRef(ref string) bool {
	return deps.IsExternalRef(ref)
}

// ParseExternalRef delegates to deps.ParseExternalRef.
func ParseExternalRef(ref string) (project, capability string) {
	return deps.ParseExternalRef(ref)
}

// resolveExternalDependencies fetches issue metadata for external (cross-rig) dependencies.
// It queries raw dependency records, finds external refs, and resolves them via routing.
func resolveExternalDependencies(ctx context.Context, depStore *dolt.DoltStore, issueID string, typeFilter string) []*types.IssueWithDependencyMetadata {
	if depStore == nil {
		return nil
	}

	// Get raw dependency records to find external refs
	depRecords, err := depStore.GetDependencyRecords(ctx, issueID)
	if err != nil {
		if isVerbose() {
			fmt.Fprintf(os.Stderr, "[external-deps] GetDependencyRecords error: %v\n", err)
		}
		return nil // Silently fail - local deps still work
	}

	if isVerbose() {
		fmt.Fprintf(os.Stderr, "[external-deps] found %d raw deps for %s\n", len(depRecords), issueID)
	}

	var result []*types.IssueWithDependencyMetadata
	beadsDir := getBeadsDir()

	for _, dep := range depRecords {
		if isVerbose() {
			fmt.Fprintf(os.Stderr, "[external-deps] checking dep: %s -> %s (%s)\n", dep.IssueID, dep.DependsOnID, dep.Type)
		}

		// Skip non-external refs (already handled by GetDependenciesWithMetadata)
		if !deps.IsExternalRef(dep.DependsOnID) {
			continue
		}

		// Apply type filter early if specified
		if typeFilter != "" && string(dep.Type) != typeFilter {
			if isVerbose() {
				fmt.Fprintf(os.Stderr, "[external-deps] skipping due to type filter: %s != %s\n", dep.Type, typeFilter)
			}
			continue
		}

		// Parse external ref: external:<project>:<issue-id>
		project, targetID := deps.ParseExternalRef(dep.DependsOnID)
		if project == "" || targetID == "" {
			if isVerbose() {
				fmt.Fprintf(os.Stderr, "[external-deps] failed to parse external ref: %s\n", dep.DependsOnID)
			}
			continue
		}

		if isVerbose() {
			fmt.Fprintf(os.Stderr, "[external-deps] parsed: project=%s, targetID=%s\n", project, targetID)
		}

		// Resolve the beads directory for this project via routing
		targetBeadsDir, _, err := routing.ResolveBeadsDirForRig(project, beadsDir)
		if err != nil {
			if isVerbose() {
				fmt.Fprintf(os.Stderr, "[external-deps] routing error for %s: %v\n", project, err)
			}
			continue // Project not configured in routes
		}

		if isVerbose() {
			fmt.Fprintf(os.Stderr, "[external-deps] resolved beads dir: %s\n", targetBeadsDir)
		}

		// Open storage for the target rig (auto-detect backend from metadata.json)
		targetStore, err := dolt.NewFromConfig(ctx, targetBeadsDir)
		if err != nil {
			if isVerbose() {
				fmt.Fprintf(os.Stderr, "[external-deps] failed to open target db %s: %v\n", targetBeadsDir, err)
			}
			continue // Can't open target database
		}

		// Fetch the issue from the target rig
		issue, err := targetStore.GetIssue(ctx, targetID)
		_ = targetStore.Close() // Best effort cleanup
		if err != nil || issue == nil {
			if isVerbose() {
				fmt.Fprintf(os.Stderr, "[external-deps] issue not found: %s (err=%v)\n", targetID, err)
			}
			continue // Issue not found in target
		}

		if isVerbose() {
			fmt.Fprintf(os.Stderr, "[external-deps] resolved issue: %s - %s\n", issue.ID, issue.Title)
		}

		// Convert to IssueWithDependencyMetadata
		result = append(result, &types.IssueWithDependencyMetadata{
			Issue:          *issue,
			DependencyType: dep.Type,
		})
	}

	return result
}

func init() {
	// dep command shorthand flag
	depCmd.Flags().StringP("blocks", "b", "", "Issue ID that this issue blocks (shorthand for: bd dep add <blocked> <blocker>)")

	depAddCmd.Flags().StringP("type", "t", "blocks", "Dependency type (blocks|tracks|related|parent-child|discovered-from|until|caused-by|validates|relates-to|supersedes)")
	depAddCmd.Flags().String("blocked-by", "", "Issue ID that blocks the first issue (alternative to positional arg)")
	depAddCmd.Flags().String("depends-on", "", "Issue ID that the first issue depends on (alias for --blocked-by)")

	depTreeCmd.Flags().Bool("show-all-paths", false, "Show all paths to nodes (no deduplication for diamond dependencies)")
	depTreeCmd.Flags().IntP("max-depth", "d", 50, "Maximum tree depth to display (safety limit)")
	depTreeCmd.Flags().Bool("reverse", false, "Show dependent tree (deprecated: use --direction=up)")
	depTreeCmd.Flags().String("direction", "", "Tree direction: 'down' (dependencies), 'up' (dependents), or 'both'")
	depTreeCmd.Flags().String("status", "", "Filter to only show issues with this status (open, in_progress, blocked, deferred, closed)")
	depTreeCmd.Flags().String("format", "", "Output format: 'mermaid' for Mermaid.js flowchart")
	depTreeCmd.Flags().StringP("type", "t", "", "Filter to only show dependencies of this type (e.g., tracks, blocks, parent-child)")

	depListCmd.Flags().String("direction", "down", "Direction: 'down' (dependencies), 'up' (dependents)")
	depListCmd.Flags().StringP("type", "t", "", "Filter by dependency type (e.g., tracks, blocks, parent-child)")

	// Issue ID completions for dep subcommands
	depAddCmd.ValidArgsFunction = issueIDCompletion
	depRemoveCmd.ValidArgsFunction = issueIDCompletion
	depListCmd.ValidArgsFunction = issueIDCompletion
	depTreeCmd.ValidArgsFunction = issueIDCompletion

	depCmd.AddCommand(depAddCmd)
	depCmd.AddCommand(depRemoveCmd)
	depCmd.AddCommand(depListCmd)
	depCmd.AddCommand(depTreeCmd)
	depCmd.AddCommand(depCyclesCmd)
	rootCmd.AddCommand(depCmd)
}
