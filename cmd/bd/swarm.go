// Package main implements the bd CLI swarm management commands.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/swarm"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

// Type aliases for backward compatibility with tests and other files in cmd/bd.
type SwarmAnalysis = swarm.Analysis
type ReadyFront = swarm.ReadyFront
type IssueNode = swarm.IssueNode
type SwarmStorage = swarm.Storage
type SwarmStatus = swarm.Status
type StatusIssue = swarm.StatusIssue

var swarmCmd = &cobra.Command{
	Use:     "swarm",
	GroupID: "deps",
	Short:   "Swarm management for structured epics",
	Long: `Swarm management commands for coordinating parallel work on epics.

A swarm is a structured body of work defined by an epic and its children,
with dependencies forming a DAG (directed acyclic graph) of work.`,
}

// findExistingSwarm delegates to swarm.FindExistingSwarm.
func findExistingSwarm(ctx context.Context, s SwarmStorage, epicID string) (*types.Issue, error) {
	return swarm.FindExistingSwarm(ctx, s, epicID)
}

// getEpicChildren delegates to swarm.GetEpicChildren.
func getEpicChildren(ctx context.Context, s SwarmStorage, epicID string) ([]*types.Issue, error) {
	return swarm.GetEpicChildren(ctx, s, epicID)
}

// analyzeEpicForSwarm delegates to swarm.AnalyzeEpicForSwarm.
func analyzeEpicForSwarm(ctx context.Context, s SwarmStorage, epic *types.Issue) (*SwarmAnalysis, error) {
	return swarm.AnalyzeEpicForSwarm(ctx, s, epic)
}

// getSwarmStatus delegates to swarm.GetSwarmStatus.
func getSwarmStatus(ctx context.Context, s SwarmStorage, epic *types.Issue) (*SwarmStatus, error) {
	return swarm.GetSwarmStatus(ctx, s, epic)
}

var swarmValidateCmd = &cobra.Command{
	Use:   "validate [epic-id]",
	Short: "Validate epic structure for swarming",
	Long: `Validate an epic's structure to ensure it's ready for swarm execution.

Checks for:
- Correct dependency direction (requirement-based, not temporal)
- Orphaned issues (roots with no dependents)
- Missing dependencies (leaves that should depend on something)
- Cycles (impossible to resolve)
- Disconnected subgraphs

Reports:
- Ready fronts (waves of parallel work)
- Estimated worker-sessions
- Maximum parallelism
- Warnings for potential issues

Examples:
  bd swarm validate gt-epic-123           # Validate epic structure
  bd swarm validate gt-epic-123 --verbose # Include detailed issue graph`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx
		verbose, _ := cmd.Flags().GetBool("verbose")

		// Swarm commands require direct store access
		if store == nil {
			FatalErrorRespectJSON("no database connection")
		}

		// Resolve epic ID
		epicID, err := utils.ResolvePartialID(ctx, store, args[0])
		if err != nil {
			FatalErrorRespectJSON("epic '%s' not found: %v", args[0], err)
		}

		// Get the epic
		epic, err := store.GetIssue(ctx, epicID)
		if err != nil {
			FatalErrorRespectJSON("failed to get epic: %v", err)
		}
		if epic == nil {
			FatalErrorRespectJSON("epic '%s' not found", epicID)
		}

		// Verify it's an epic
		if epic.IssueType != types.TypeEpic && epic.IssueType != "molecule" {
			FatalErrorRespectJSON("'%s' is not an epic or molecule (type: %s)", epicID, epic.IssueType)
		}

		// Analyze the epic structure
		analysis, err := analyzeEpicForSwarm(ctx, store, epic)
		if err != nil {
			FatalErrorRespectJSON("failed to analyze epic: %v", err)
		}

		// Include detailed graph only in verbose mode
		if !verbose {
			analysis.Issues = nil
		}

		if jsonOutput {
			outputJSON(analysis)
			if !analysis.Swarmable {
				os.Exit(1)
			}
			return
		}

		// Human-readable output
		renderSwarmAnalysis(analysis)

		if !analysis.Swarmable {
			os.Exit(1)
		}
	},
}

// renderSwarmAnalysis outputs human-readable analysis.
func renderSwarmAnalysis(analysis *SwarmAnalysis) {
	fmt.Printf("\n%s Swarm Analysis: %s\n", ui.RenderAccent("🐝"), analysis.EpicTitle)
	fmt.Printf("   Epic ID: %s\n", analysis.EpicID)
	fmt.Printf("   Total issues: %d (%d closed)\n", analysis.TotalIssues, analysis.ClosedIssues)

	if analysis.TotalIssues == 0 {
		fmt.Printf("\n%s Epic has no children to swarm\n\n", ui.RenderWarn("⚠"))
		return
	}

	// Ready fronts
	if len(analysis.ReadyFronts) > 0 {
		fmt.Printf("\n%s Ready Fronts (waves of parallel work):\n", ui.RenderPass("📊"))
		for _, front := range analysis.ReadyFronts {
			fmt.Printf("   Wave %d: %d issues\n", front.Wave+1, len(front.Issues))
			for i, id := range front.Issues {
				title := ""
				if i < len(front.Titles) {
					title = front.Titles[i]
				}
				fmt.Printf("      • %s: %s\n", ui.RenderID(id), title)
			}
		}
	}

	// Summary stats
	fmt.Printf("\n%s Summary:\n", ui.RenderAccent("📈"))
	fmt.Printf("   Estimated worker-sessions: %d\n", analysis.EstimatedSessions)
	fmt.Printf("   Max parallelism: %d\n", analysis.MaxParallelism)
	fmt.Printf("   Total waves: %d\n", len(analysis.ReadyFronts))

	// Warnings
	if len(analysis.Warnings) > 0 {
		fmt.Printf("\n%s Warnings:\n", ui.RenderWarn("⚠"))
		for _, warning := range analysis.Warnings {
			fmt.Printf("   • %s\n", warning)
		}
	}

	// Errors
	if len(analysis.Errors) > 0 {
		fmt.Printf("\n%s Errors:\n", ui.RenderFail("❌"))
		for _, err := range analysis.Errors {
			fmt.Printf("   • %s\n", err)
		}
	}

	// Final verdict
	fmt.Println()
	if analysis.Swarmable {
		fmt.Printf("%s Swarmable: YES\n\n", ui.RenderPass("✓"))
	} else {
		fmt.Printf("%s Swarmable: NO (fix errors first)\n\n", ui.RenderFail("✗"))
	}
}

var swarmStatusCmd = &cobra.Command{
	Use:   "status [epic-or-swarm-id]",
	Short: "Show current swarm status",
	Long: `Show the current status of a swarm, computed from beads.

Accepts either:
- An epic ID (shows status for that epic's children)
- A swarm molecule ID (follows the link to find the epic)

Displays issues grouped by state:
- Completed: Closed issues
- Active: Issues currently in_progress (with assignee)
- Ready: Open issues with all dependencies satisfied
- Blocked: Open issues waiting on dependencies

The status is COMPUTED from beads, not stored separately.
If beads changes, status changes.

Examples:
  bd swarm status gt-epic-123       # Show swarm status by epic
  bd swarm status gt-swarm-456      # Show status via swarm molecule
  bd swarm status gt-epic-123 --json  # Machine-readable output`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		// Swarm commands require direct store access
		if store == nil {
			FatalErrorRespectJSON("no database connection")
		}

		// Resolve ID
		issueID, err := utils.ResolvePartialID(ctx, store, args[0])
		if err != nil {
			FatalErrorRespectJSON("issue '%s' not found: %v", args[0], err)
		}

		// Get the issue
		issue, err := store.GetIssue(ctx, issueID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				FatalErrorRespectJSON("issue '%s' not found", issueID)
			}
			FatalErrorRespectJSON("failed to get issue: %v", err)
		}

		var epic *types.Issue

		// Check if it's a swarm molecule - if so, follow the link to the epic
		if issue.IssueType == "molecule" && issue.MolType == types.MolTypeSwarm {
			// Find linked epic via relates-to dependency
			deps, err := store.GetDependencyRecords(ctx, issue.ID)
			if err != nil {
				FatalErrorRespectJSON("failed to get swarm dependencies: %v", err)
			}
			for _, dep := range deps {
				if dep.Type == types.DepRelatesTo {
					epic, err = store.GetIssue(ctx, dep.DependsOnID)
					if err != nil {
						FatalErrorRespectJSON("failed to get linked epic: %v", err)
					}
					break
				}
			}
			if epic == nil {
				FatalErrorRespectJSON("swarm molecule '%s' has no linked epic", issueID)
			}
		} else if issue.IssueType == types.TypeEpic || issue.IssueType == "molecule" {
			epic = issue
		} else {
			FatalErrorRespectJSON("'%s' is not an epic or swarm molecule (type: %s)", issueID, issue.IssueType)
		}

		// Get swarm status
		status, err := getSwarmStatus(ctx, store, epic)
		if err != nil {
			FatalErrorRespectJSON("failed to get swarm status: %v", err)
		}

		if jsonOutput {
			outputJSON(status)
			return
		}

		// Human-readable output
		renderSwarmStatus(status)
	},
}

// renderSwarmStatus outputs human-readable swarm status.
func renderSwarmStatus(status *SwarmStatus) {
	fmt.Printf("\n%s Ready Front Analysis: %s\n\n", ui.RenderAccent("🐝"), status.EpicTitle)

	// Completed
	fmt.Printf("Completed:     ")
	if len(status.Completed) == 0 {
		fmt.Printf("(none)\n")
	} else {
		for i, issue := range status.Completed {
			if i > 0 {
				fmt.Printf("               ")
			}
			fmt.Printf("%s %s\n", ui.RenderPass("✓"), ui.RenderID(issue.ID))
		}
	}

	// Active
	fmt.Printf("Active:        ")
	if len(status.Active) == 0 {
		fmt.Printf("(none)\n")
	} else {
		var parts []string
		for _, issue := range status.Active {
			part := fmt.Sprintf("⟳ %s", issue.ID)
			if issue.Assignee != "" {
				part += fmt.Sprintf(" [%s]", issue.Assignee)
			}
			parts = append(parts, part)
		}
		fmt.Printf("%s\n", strings.Join(parts, ", "))
	}

	// Ready
	fmt.Printf("Ready:         ")
	if len(status.Ready) == 0 {
		if len(status.Blocked) > 0 {
			// Find what's blocking
			needed := make(map[string]bool)
			for _, b := range status.Blocked {
				for _, dep := range b.BlockedBy {
					needed[dep] = true
				}
			}
			var neededList []string
			for dep := range needed {
				neededList = append(neededList, dep)
			}
			sort.Strings(neededList)
			fmt.Printf("(none - waiting for %s)\n", strings.Join(neededList, ", "))
		} else {
			fmt.Printf("(none)\n")
		}
	} else {
		var parts []string
		for _, issue := range status.Ready {
			parts = append(parts, fmt.Sprintf("○ %s", issue.ID))
		}
		fmt.Printf("%s\n", strings.Join(parts, ", "))
	}

	// Blocked
	fmt.Printf("Blocked:       ")
	if len(status.Blocked) == 0 {
		fmt.Printf("(none)\n")
	} else {
		for i, issue := range status.Blocked {
			if i > 0 {
				fmt.Printf("               ")
			}
			blockerStr := strings.Join(issue.BlockedBy, ", ")
			fmt.Printf("◌ %s (needs %s)\n", issue.ID, blockerStr)
		}
	}

	// Progress summary
	fmt.Printf("\nProgress: %d/%d complete", len(status.Completed), status.TotalIssues)
	if status.ActiveCount > 0 {
		fmt.Printf(", %d/%d active", status.ActiveCount, status.TotalIssues)
	}
	fmt.Printf(" (%.0f%%)\n\n", status.Progress)
}

var swarmCreateCmd = &cobra.Command{
	Use:   "create [epic-id]",
	Short: "Create a swarm molecule from an epic",
	Long: `Create a swarm molecule to orchestrate parallel work on an epic.

The swarm molecule:
- Links to the epic it orchestrates
- Has mol_type=swarm for discovery
- Specifies a coordinator (optional)
- Can be picked up by any coordinator agent

If given a single issue (not an epic), it will be auto-wrapped:
- Creates an epic with that issue as its only child
- Then creates the swarm molecule for that epic

Examples:
  bd swarm create gt-epic-123                          # Create swarm for epic
  bd swarm create gt-epic-123 --coordinator=witness/   # With specific coordinator
  bd swarm create gt-task-456                          # Auto-wrap single issue`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("swarm create")
		ctx := rootCtx
		coordinator, _ := cmd.Flags().GetString("coordinator")
		force, _ := cmd.Flags().GetBool("force")

		// Swarm commands require direct store access
		if store == nil {
			FatalErrorRespectJSON("no database connection")
		}

		// Resolve the input ID
		inputID, err := utils.ResolvePartialID(ctx, store, args[0])
		if err != nil {
			FatalErrorRespectJSON("issue '%s' not found: %v", args[0], err)
		}

		// Get the issue
		issue, err := store.GetIssue(ctx, inputID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				FatalErrorRespectJSON("issue '%s' not found", inputID)
			}
			FatalErrorRespectJSON("failed to get issue: %v", err)
		}

		var epicID string
		var epicTitle string

		// Check if it's an epic or single issue that needs wrapping
		if issue.IssueType == types.TypeEpic || issue.IssueType == "molecule" {
			epicID = issue.ID
			epicTitle = issue.Title
		} else {
			// Auto-wrap: create an epic with this issue as child
			if !jsonOutput {
				fmt.Printf("Auto-wrapping single issue as epic...\n")
			}

			wrapperEpic := &types.Issue{
				Title:       fmt.Sprintf("Swarm Epic: %s", issue.Title),
				Description: fmt.Sprintf("Auto-generated epic to wrap single issue %s for swarm execution.", issue.ID),
				Status:      types.StatusOpen,
				Priority:    issue.Priority,
				IssueType:   types.TypeEpic,
				CreatedBy:   actor,
			}

			if err := store.CreateIssue(ctx, wrapperEpic, actor); err != nil {
				FatalErrorRespectJSON("failed to create wrapper epic: %v", err)
			}

			// Add parent-child dependency: issue depends on epic (epic is parent)
			dep := &types.Dependency{
				IssueID:     issue.ID,
				DependsOnID: wrapperEpic.ID,
				Type:        types.DepParentChild,
				CreatedBy:   actor,
			}
			if err := store.AddDependency(ctx, dep, actor); err != nil {
				FatalErrorRespectJSON("failed to link issue to epic: %v", err)
			}

			epicID = wrapperEpic.ID
			epicTitle = wrapperEpic.Title

			if !jsonOutput {
				fmt.Printf("Created wrapper epic: %s\n", epicID)
			}
		}

		// Check for existing swarm molecule
		existingSwarm, err := findExistingSwarm(ctx, store, epicID)
		if err != nil {
			FatalErrorRespectJSON("failed to check for existing swarm: %v", err)
		}
		if existingSwarm != nil && !force {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"error":          "swarm already exists",
					"existing_id":    existingSwarm.ID,
					"existing_title": existingSwarm.Title,
				})
			} else {
				fmt.Printf("%s Swarm already exists: %s\n", ui.RenderWarn("⚠"), ui.RenderID(existingSwarm.ID))
				fmt.Printf("   Use --force to create another.\n")
			}
			os.Exit(1)
		}

		// Validate the epic structure
		epic, err := store.GetIssue(ctx, epicID)
		if err != nil {
			FatalErrorRespectJSON("failed to get epic: %v", err)
		}

		analysis, err := analyzeEpicForSwarm(ctx, store, epic)
		if err != nil {
			FatalErrorRespectJSON("failed to analyze epic: %v", err)
		}

		if !analysis.Swarmable {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"error":    "epic is not swarmable",
					"analysis": analysis,
				})
			} else {
				fmt.Printf("\n%s Epic is not swarmable. Fix errors first:\n", ui.RenderFail("✗"))
				for _, e := range analysis.Errors {
					fmt.Printf("  • %s\n", e)
				}
			}
			os.Exit(1)
		}

		// Create the swarm molecule
		swarmMol := &types.Issue{
			Title:       fmt.Sprintf("Swarm: %s", epicTitle),
			Description: fmt.Sprintf("Swarm molecule orchestrating epic %s.\n\nEpic: %s\nCoordinator: %s", epicID, epicID, coordinator),
			Status:      types.StatusOpen,
			Priority:    epic.Priority,
			IssueType:   "molecule",
			MolType:     types.MolTypeSwarm,
			Assignee:    coordinator,
			CreatedBy:   actor,
		}

		if err := store.CreateIssue(ctx, swarmMol, actor); err != nil {
			FatalErrorRespectJSON("failed to create swarm molecule: %v", err)
		}

		// Link swarm molecule to epic with relates-to dependency
		dep := &types.Dependency{
			IssueID:     swarmMol.ID,
			DependsOnID: epicID,
			Type:        types.DepRelatesTo,
			CreatedBy:   actor,
		}
		if err := store.AddDependency(ctx, dep, actor); err != nil {
			FatalErrorRespectJSON("failed to link swarm to epic: %v", err)
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"swarm_id":    swarmMol.ID,
				"epic_id":     epicID,
				"coordinator": coordinator,
				"analysis":    analysis,
			})
		} else {
			fmt.Printf("\n%s Created swarm molecule: %s\n", ui.RenderPass("✓"), ui.RenderID(swarmMol.ID))
			fmt.Printf("   Epic: %s (%s)\n", epicID, epicTitle)
			if coordinator != "" {
				fmt.Printf("   Coordinator: %s\n", coordinator)
			}
			fmt.Printf("   Total issues: %d\n", analysis.TotalIssues)
			fmt.Printf("   Max parallelism: %d\n", analysis.MaxParallelism)
			fmt.Printf("   Waves: %d\n", len(analysis.ReadyFronts))
		}
	},
}

var swarmListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all swarm molecules",
	Long: `List all swarm molecules with their status.

Shows each swarm molecule with:
- Progress (completed/total issues)
- Active workers
- Epic ID and title

Examples:
  bd swarm list         # List all swarms
  bd swarm list --json  # Machine-readable output`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		// Swarm commands require direct store access
		if store == nil {
			FatalErrorRespectJSON("no database connection")
		}

		// Query for all swarm molecules
		swarmType := types.MolTypeSwarm
		filter := types.IssueFilter{
			MolType: &swarmType,
		}
		swarms, err := store.SearchIssues(ctx, "", filter)
		if err != nil {
			FatalErrorRespectJSON("failed to list swarms: %v", err)
		}

		if len(swarms) == 0 {
			if jsonOutput {
				outputJSON(map[string]interface{}{"swarms": []interface{}{}})
			} else {
				fmt.Printf("No swarm molecules found.\n")
			}
			return
		}

		// Build output with status for each swarm
		type SwarmListItem struct {
			ID          string  `json:"id"`
			Title       string  `json:"title"`
			EpicID      string  `json:"epic_id"`
			EpicTitle   string  `json:"epic_title"`
			Status      string  `json:"status"`
			Coordinator string  `json:"coordinator"`
			Total       int     `json:"total_issues"`
			Completed   int     `json:"completed_issues"`
			Active      int     `json:"active_issues"`
			Progress    float64 `json:"progress_percent"`
		}

		var items []SwarmListItem
		for _, s := range swarms {
			item := SwarmListItem{
				ID:          s.ID,
				Title:       s.Title,
				Status:      string(s.Status),
				Coordinator: s.Assignee,
			}

			// Find linked epic via relates-to dependency
			depRecords, err := store.GetDependencyRecords(ctx, s.ID)
			if err == nil {
				for _, dep := range depRecords {
					if dep.Type == types.DepRelatesTo {
						item.EpicID = dep.DependsOnID
						epic, err := store.GetIssue(ctx, dep.DependsOnID)
						if err == nil && epic != nil {
							item.EpicTitle = epic.Title
							// Get swarm status for this epic
							status, err := getSwarmStatus(ctx, store, epic)
							if err == nil {
								item.Total = status.TotalIssues
								item.Completed = len(status.Completed)
								item.Active = status.ActiveCount
								item.Progress = status.Progress
							}
						}
						break
					}
				}
			}

			items = append(items, item)
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{"swarms": items})
			return
		}

		// Human-readable output
		fmt.Printf("\n%s Active Swarms (%d)\n\n", ui.RenderAccent("🐝"), len(items))
		for _, item := range items {
			// Progress indicator
			progressStr := fmt.Sprintf("%d/%d", item.Completed, item.Total)
			if item.Active > 0 {
				progressStr += fmt.Sprintf(", %d active", item.Active)
			}

			fmt.Printf("%s %s\n", ui.RenderID(item.ID), item.Title)
			if item.EpicID != "" {
				fmt.Printf("   Epic: %s (%s)\n", item.EpicID, item.EpicTitle)
			}
			fmt.Printf("   Progress: %s (%.0f%%)\n", progressStr, item.Progress)
			if item.Coordinator != "" {
				fmt.Printf("   Coordinator: %s\n", item.Coordinator)
			}
			fmt.Println()
		}
	},
}

func init() {
	swarmValidateCmd.Flags().Bool("verbose", false, "Include detailed issue graph in output")
	swarmCreateCmd.Flags().String("coordinator", "", "Coordinator address (e.g., gastown/witness)")
	swarmCreateCmd.Flags().Bool("force", false, "Create new swarm even if one already exists")

	swarmCmd.AddCommand(swarmValidateCmd)
	swarmCmd.AddCommand(swarmStatusCmd)
	swarmCmd.AddCommand(swarmCreateCmd)
	swarmCmd.AddCommand(swarmListCmd)
	rootCmd.AddCommand(swarmCmd)
}
