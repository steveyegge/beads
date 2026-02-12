package main
import (
	"encoding/json"
	"fmt"
	"os"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)
var epicCmd = &cobra.Command{
	Use:     "epic",
	GroupID: "deps",
	Short:   "Epic management commands",
}
var epicStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show epic completion status",
	Long: `Show completion status for all open epics.

To see only epics eligible for closure, use:
  bd epic close-eligible --dry-run`,
	Run: func(cmd *cobra.Command, args []string) {
		requireDaemon("epic status")

		// Use global jsonOutput set by PersistentPreRun
		var epics []*types.EpicStatus
		resp, err := daemonClient.EpicStatus(&rpc.EpicStatusArgs{
			EligibleOnly: false,
		})
		if err != nil {
			FatalErrorRespectJSON("communicating with daemon: %v", err)
		}
		if !resp.Success {
			FatalErrorRespectJSON("getting epic status: %s", resp.Error)
		}
		if err := json.Unmarshal(resp.Data, &epics); err != nil {
			FatalErrorRespectJSON("parsing response: %v", err)
		}
		if jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(epics); err != nil {
				FatalErrorRespectJSON("encoding JSON: %v", err)
			}
			return
		}
		// Human-readable output
		if len(epics) == 0 {
			fmt.Println("No open epics found")
			return
		}
		for _, epicStatus := range epics {
			epic := epicStatus.Epic
			percentage := 0
			if epicStatus.TotalChildren > 0 {
				percentage = (epicStatus.ClosedChildren * 100) / epicStatus.TotalChildren
			}
			statusIcon := ""
			if epicStatus.EligibleForClose {
				statusIcon = ui.RenderPass("✓")
			} else if percentage > 0 {
				statusIcon = ui.RenderWarn("○")
			} else {
				statusIcon = "○"
			}
			fmt.Printf("%s %s %s\n", statusIcon, ui.RenderAccent(epic.ID), ui.RenderBold(epic.Title))
			fmt.Printf("   Progress: %d/%d children closed (%d%%)\n",
				epicStatus.ClosedChildren, epicStatus.TotalChildren, percentage)
			if epicStatus.EligibleForClose {
				fmt.Printf("   %s\n", ui.RenderPass("Eligible for closure"))
			}
			fmt.Println()
		}
	},
}
var closeEligibleEpicsCmd = &cobra.Command{
	Use:   "close-eligible",
	Short: "Close epics where all children are complete",
	Run: func(cmd *cobra.Command, args []string) {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		// Block writes in readonly mode (closing modifies data)
		if !dryRun {
			CheckReadonly("epic close-eligible")
		}
		requireDaemon("epic close-eligible")

		// Use global jsonOutput set by PersistentPreRun
		var eligibleEpics []*types.EpicStatus
		resp, err := daemonClient.EpicStatus(&rpc.EpicStatusArgs{
			EligibleOnly: true,
		})
		if err != nil {
			FatalErrorRespectJSON("communicating with daemon: %v", err)
		}
		if !resp.Success {
			FatalErrorRespectJSON("getting eligible epics: %s", resp.Error)
		}
		if err := json.Unmarshal(resp.Data, &eligibleEpics); err != nil {
			FatalErrorRespectJSON("parsing response: %v", err)
		}
		if len(eligibleEpics) == 0 {
			if !jsonOutput {
				fmt.Println("No epics eligible for closure")
			} else {
				fmt.Println("[]")
			}
			return
		}
		if dryRun {
			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(eligibleEpics); err != nil {
					FatalErrorRespectJSON("encoding JSON: %v", err)
				}
			} else {
				fmt.Printf("Would close %d epic(s):\n", len(eligibleEpics))
				for _, epicStatus := range eligibleEpics {
					fmt.Printf("  - %s: %s\n", epicStatus.Epic.ID, epicStatus.Epic.Title)
				}
			}
			return
		}
		// Actually close the epics via daemon RPC
		closedIDs := []string{}
		for _, epicStatus := range eligibleEpics {
			closeResp, closeErr := daemonClient.CloseIssue(&rpc.CloseArgs{
				ID:     epicStatus.Epic.ID,
				Reason: "All children completed",
			})
			if closeErr != nil || !closeResp.Success {
				errMsg := ""
				if closeErr != nil {
					errMsg = closeErr.Error()
				} else if !closeResp.Success {
					errMsg = closeResp.Error
				}
				fmt.Fprintf(os.Stderr, "Error closing %s: %s\n", epicStatus.Epic.ID, errMsg)
				continue
			}
			closedIDs = append(closedIDs, epicStatus.Epic.ID)
		}
		if jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(map[string]interface{}{
				"closed": closedIDs,
				"count":  len(closedIDs),
			}); err != nil {
				FatalErrorRespectJSON("encoding JSON: %v", err)
			}
		} else {
			fmt.Printf("✓ Closed %d epic(s)\n", len(closedIDs))
			for _, id := range closedIDs {
				fmt.Printf("  - %s\n", id)
			}
		}
	},
}
// OrphanedChild represents an issue whose parent is closed or missing
type OrphanedChild struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	ParentID string `json:"parent_id"`
	Reason   string `json:"reason"` // "parent_closed" or "parent_not_found"
}

var orphanedChildrenCmd = &cobra.Command{
	Use:   "orphaned-children",
	Short: "Find children whose parent epic is closed or missing",
	Long: `Find issues that are orphaned - their parent epic is either closed or doesn't exist.

Orphaned children may indicate:
  - Work that was abandoned when an epic was closed
  - Data corruption or missing parent beads
  - Issues that should be reparented to a new epic

Note: This differs from 'bd orphans' which finds issues mentioned in commits but not closed.

Examples:
  bd epic orphaned-children              # Find all orphaned children
  bd epic orphaned-children --json       # Machine-readable output`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		if store == nil {
			FatalErrorRespectJSON("no database connection")
		}

		// Get all dependency records
		allDeps, err := store.GetAllDependencyRecords(ctx)
		if err != nil {
			FatalErrorRespectJSON("getting dependencies: %v", err)
		}

		// Build map of child -> parent from parent-child dependencies
		childToParent := make(map[string]string)
		for issueID, deps := range allDeps {
			for _, dep := range deps {
				if dep.Type == types.DepParentChild {
					// issueID depends on dep.DependsOnID with parent-child type
					// meaning dep.DependsOnID is the parent of issueID
					childToParent[issueID] = dep.DependsOnID
				}
			}
		}

		// Check each child's parent
		var orphans []OrphanedChild
		for childID, parentID := range childToParent {
			// Get child issue to include title
			child, err := store.GetIssue(ctx, childID)
			if err != nil || child == nil {
				continue // Skip if child doesn't exist
			}

			// Skip closed children - they're not really orphaned
			if child.Status == types.StatusClosed {
				continue
			}

			// Check parent status
			parent, err := store.GetIssue(ctx, parentID)
			if err != nil || parent == nil {
				orphans = append(orphans, OrphanedChild{
					ID:       childID,
					Title:    child.Title,
					ParentID: parentID,
					Reason:   "parent_not_found",
				})
			} else if parent.Status == types.StatusClosed {
				orphans = append(orphans, OrphanedChild{
					ID:       childID,
					Title:    child.Title,
					ParentID: parentID,
					Reason:   "parent_closed",
				})
			}
		}

		if jsonOutput {
			outputJSON(orphans)
			return
		}

		if len(orphans) == 0 {
			fmt.Println("No orphaned children found")
			return
		}

		fmt.Printf("Found %d orphaned child issue(s):\n\n", len(orphans))
		for _, orphan := range orphans {
			reason := ""
			switch orphan.Reason {
			case "parent_closed":
				reason = fmt.Sprintf("parent %s closed", orphan.ParentID)
			case "parent_not_found":
				reason = fmt.Sprintf("parent %s not found", orphan.ParentID)
			}
			fmt.Printf("  %s %s - %s\n", ui.RenderWarn("⚠"), ui.RenderID(orphan.ID), orphan.Title)
			fmt.Printf("    %s\n", ui.RenderMuted(reason))
		}

		fmt.Printf("\nHint: Use 'bd update <id> --parent <new-parent>' to reparent orphaned issues\n")
	},
}

var dashboardCmd = &cobra.Command{
	Use:   "dashboard <epic-id>",
	Short: "Show epic dashboard with progress visualization",
	Long: `Display a visual dashboard for an epic showing progress, children status, and summary.

Example:
  bd epic dashboard hq-5881b3

Shows:
  - Progress bar with percentage
  - List of children with status
  - Summary counts (blocked, ready, complete)`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx
		epicID := args[0]

		if store == nil {
			FatalErrorRespectJSON("no database connection")
		}

		// Get epic (store.GetIssue handles partial ID resolution)
		epic, err := store.GetIssue(ctx, epicID)
		if err != nil {
			FatalErrorRespectJSON("getting epic: %v", err)
		}
		if epic == nil {
			FatalErrorRespectJSON("epic not found: %s", epicID)
		}
		epicID = epic.ID // Use resolved ID

		// Get children via dependents with metadata
		dependents, err := store.GetDependentsWithMetadata(ctx, epicID)
		if err != nil {
			FatalErrorRespectJSON("getting children: %v", err)
		}

		// Filter for parent-child relationships (children of this epic)
		var children []*types.IssueWithDependencyMetadata
		for _, dep := range dependents {
			if dep.DependencyType == types.DepParentChild {
				children = append(children, dep)
			}
		}

		// Count statuses
		var complete, inProgress, blocked, open int
		for _, child := range children {
			switch child.Status {
			case types.StatusClosed:
				complete++
			case types.StatusInProgress:
				inProgress++
			case types.StatusBlocked:
				blocked++
			default:
				open++
			}
		}

		total := len(children)
		percentage := 0
		if total > 0 {
			percentage = (complete * 100) / total
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"epic":        epic,
				"children":    children,
				"total":       total,
				"complete":    complete,
				"in_progress": inProgress,
				"blocked":     blocked,
				"open":        open,
				"percentage":  percentage,
			})
			return
		}

		// Render dashboard
		width := 50
		titleLine := fmt.Sprintf("Epic: %s - %s", epicID, epic.Title)
		if len(titleLine) > width-4 {
			titleLine = titleLine[:width-7] + "..."
		}

		// Box top
		fmt.Println("╭" + repeatStr("─", width) + "╮")
		fmt.Printf("│ %-*s │\n", width-2, titleLine)
		fmt.Println("├" + repeatStr("─", width) + "┤")

		// Progress bar
		barWidth := width - 20
		filledWidth := (percentage * barWidth) / 100
		emptyWidth := barWidth - filledWidth
		progressBar := repeatStr("█", filledWidth) + repeatStr("░", emptyWidth)
		fmt.Printf("│ Progress: %s %3d%% (%d/%d) │\n", progressBar, percentage, complete, total)

		// Status
		fmt.Printf("│ Status: %-*s │\n", width-11, epic.Status)

		// Created date
		fmt.Printf("│ Created: %-*s │\n", width-12, epic.CreatedAt.Format("2006-01-02"))

		// Separator
		fmt.Println("│" + repeatStr(" ", width) + "│")

		// Children header
		if len(children) > 0 {
			fmt.Printf("│ %-*s │\n", width-2, "Children:")
			for _, child := range children {
				icon := "○"
				switch child.Status {
				case types.StatusClosed:
					icon = "✓"
				case types.StatusInProgress:
					icon = "◐"
				case types.StatusBlocked:
					icon = "✗"
				}
				childLine := fmt.Sprintf("  %s %s", icon, child.ID)
				titlePart := child.Title
				maxTitleLen := width - len(childLine) - 5
				if len(titlePart) > maxTitleLen {
					titlePart = titlePart[:maxTitleLen-3] + "..."
				}
				fullLine := fmt.Sprintf("%s %s", childLine, titlePart)
				fmt.Printf("│ %-*s │\n", width-2, fullLine)
			}
		} else {
			fmt.Printf("│ %-*s │\n", width-2, "(no children)")
		}

		// Separator
		fmt.Println("│" + repeatStr(" ", width) + "│")

		// Summary
		summary := fmt.Sprintf("Blocked: %d | In Progress: %d | Open: %d | Done: %d", blocked, inProgress, open, complete)
		fmt.Printf("│ %-*s │\n", width-2, summary)

		// Box bottom
		fmt.Println("╰" + repeatStr("─", width) + "╯")
	},
}

// repeatStr repeats a string n times
func repeatStr(s string, n int) string {
	if n <= 0 {
		return ""
	}
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}

func init() {
	epicCmd.AddCommand(epicStatusCmd)
	epicCmd.AddCommand(closeEligibleEpicsCmd)
	epicCmd.AddCommand(orphanedChildrenCmd)
	epicCmd.AddCommand(dashboardCmd)
	closeEligibleEpicsCmd.Flags().Bool("dry-run", false, "Preview what would be closed without making changes")
	rootCmd.AddCommand(epicCmd)
}
