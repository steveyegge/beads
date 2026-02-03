package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/types"
)

type specSyncChange struct {
	SpecID       string `json:"spec_id"`
	OldLifecycle string `json:"old_lifecycle"`
	NewLifecycle string `json:"new_lifecycle"`
	OpenCount    int    `json:"open_count"`
	ClosedCount  int    `json:"closed_count"`
}

type specSyncResult struct {
	Changes []specSyncChange `json:"changes"`
	Applied int              `json:"applied"`
	Skipped int              `json:"skipped"`
}

var specSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync spec lifecycle from linked beads",
	Run: func(cmd *cobra.Command, _ []string) {
		apply, _ := cmd.Flags().GetBool("apply")
		yes, _ := cmd.Flags().GetBool("yes")

		if daemonClient != nil {
			FatalErrorRespectJSON("spec sync requires direct access (run with --no-daemon)")
		}

		if err := ensureDatabaseFresh(rootCtx); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		specStore, err := getSpecRegistryStore()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		if store == nil {
			FatalErrorRespectJSON("storage not available")
		}

		entries, err := specStore.ListSpecRegistry(rootCtx)
		if err != nil {
			FatalErrorRespectJSON("list spec registry: %v", err)
		}

		issues, err := store.SearchIssues(rootCtx, "", types.IssueFilter{})
		if err != nil {
			FatalErrorRespectJSON("list issues: %v", err)
		}

		openCounts := make(map[string]int)
		closedCounts := make(map[string]int)
		for _, issue := range issues {
			if issue.SpecID == "" {
				continue
			}
			if issue.Status == types.StatusClosed || issue.Status == types.StatusTombstone {
				closedCounts[issue.SpecID]++
			} else {
				openCounts[issue.SpecID]++
			}
		}

		changes := make([]specSyncChange, 0)
		for _, entry := range entries {
			openCount := openCounts[entry.SpecID]
			closedCount := closedCounts[entry.SpecID]
			newLifecycle := ""
			switch {
			case openCount > 0:
				newLifecycle = "active"
			case closedCount > 0:
				newLifecycle = "done"
			default:
				continue
			}
			if newLifecycle == entry.Lifecycle {
				continue
			}
			changes = append(changes, specSyncChange{
				SpecID:       entry.SpecID,
				OldLifecycle: entry.Lifecycle,
				NewLifecycle: newLifecycle,
				OpenCount:    openCount,
				ClosedCount:  closedCount,
			})
		}

		result := specSyncResult{
			Changes: changes,
			Applied: 0,
			Skipped: 0,
		}

		if !apply {
			if jsonOutput {
				outputJSON(result)
				return
			}
			renderSpecSyncPreview(changes)
			fmt.Println("\nRun with --apply to update spec lifecycle.")
			return
		}

		if jsonOutput && !yes {
			FatalErrorRespectJSON("--yes is required with --json and --apply")
		}

		if !jsonOutput && !yes && len(changes) > 0 {
			fmt.Printf("Apply %d change(s)? [y/N] ", len(changes))
			var response string
			_, _ = fmt.Fscanln(os.Stdin, &response)
			if response != "y" && response != "Y" {
				fmt.Println("Canceled.")
				return
			}
		}

		now := time.Now().UTC().Truncate(time.Second)
		for _, change := range changes {
			update := spec.SpecRegistryUpdate{
				Lifecycle: &change.NewLifecycle,
			}
			if change.NewLifecycle == "done" {
				update.CompletedAt = &now
			}
			if err := specStore.UpdateSpecRegistry(rootCtx, change.SpecID, update); err != nil {
				result.Skipped++
				continue
			}
			result.Applied++
		}

		if jsonOutput {
			outputJSON(result)
			return
		}
		renderSpecSyncApplied(result)
	},
}

func init() {
	specSyncCmd.Flags().Bool("apply", false, "Apply lifecycle changes")
	specSyncCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
	specCmd.AddCommand(specSyncCmd)
}

func renderSpecSyncPreview(changes []specSyncChange) {
	if len(changes) == 0 {
		fmt.Println("No lifecycle changes needed.")
		return
	}
	fmt.Printf("Spec Sync Preview (%d change(s))\n", len(changes))
	for _, change := range changes {
		fmt.Printf("  %s: %s → %s (open=%d closed=%d)\n",
			change.SpecID,
			change.OldLifecycle,
			change.NewLifecycle,
			change.OpenCount,
			change.ClosedCount,
		)
	}
}

func renderSpecSyncApplied(result specSyncResult) {
	fmt.Printf("Applied %d change(s), skipped %d\n", result.Applied, result.Skipped)
	if len(result.Changes) == 0 {
		fmt.Println("No lifecycle changes needed.")
		return
	}
	for _, change := range result.Changes {
		fmt.Printf("  %s: %s → %s\n", change.SpecID, change.OldLifecycle, change.NewLifecycle)
	}
}
