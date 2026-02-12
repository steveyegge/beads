package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/timeparsing"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

var deferCmd = &cobra.Command{
	Use:   "defer [id...]",
	Short: "Defer one or more issues for later",
	Long: `Defer issues to put them on ice for later.

Deferred issues are deliberately set aside - not blocked by anything specific,
just postponed for future consideration. Unlike blocked issues, there's no
dependency keeping them from being worked. Unlike closed issues, they will
be revisited.

Deferred issues don't show in 'bd ready' but remain visible in 'bd list'.

Examples:
  bd defer bd-abc                  # Defer a single issue (status-based)
  bd defer bd-abc --until=tomorrow # Defer until specific time
  bd defer bd-abc bd-def           # Defer multiple issues`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("defer")
		requireDaemon("defer")

		// Parse --until flag (GH#820)
		var deferUntil *time.Time
		untilStr, _ := cmd.Flags().GetString("until")
		if untilStr != "" {
			t, err := timeparsing.ParseRelativeTime(untilStr, time.Now())
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid --until format %q. Examples: +1h, tomorrow, next monday, 2025-01-15\n", untilStr)
				os.Exit(1)
			}
			deferUntil = &t
		}

		// Resolve partial IDs via daemon
		var resolvedIDs []string
		for _, id := range args {
			resolveArgs := &rpc.ResolveIDArgs{ID: id}
			resp, err := daemonClient.ResolveID(resolveArgs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error resolving ID %s: %v\n", id, err)
				os.Exit(1)
			}
			var resolvedID string
			if err := json.Unmarshal(resp.Data, &resolvedID); err != nil {
				fmt.Fprintf(os.Stderr, "Error unmarshaling resolved ID: %v\n", err)
				os.Exit(1)
			}
			resolvedIDs = append(resolvedIDs, resolvedID)
		}

		deferredIssues := []*types.Issue{}

		for _, id := range resolvedIDs {
			status := string(types.StatusDeferred)
			updateArgs := &rpc.UpdateArgs{
				ID:     id,
				Status: &status,
			}
			// Add defer_until if --until specified (GH#820)
			if deferUntil != nil {
				s := deferUntil.Format(time.RFC3339)
				updateArgs.DeferUntil = &s
			}

			resp, err := daemonClient.Update(updateArgs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error deferring %s: %v\n", id, err)
				continue
			}

			if jsonOutput {
				var issue types.Issue
				if err := json.Unmarshal(resp.Data, &issue); err == nil {
					deferredIssues = append(deferredIssues, &issue)
				}
			} else {
				fmt.Printf("%s Deferred %s\n", ui.RenderAccent("*"), id)
			}
		}

		if jsonOutput && len(deferredIssues) > 0 {
			outputJSON(deferredIssues)
		}
	},
}

func init() {
	// Time-based scheduling flag (GH#820)
	deferCmd.Flags().String("until", "", "Defer until specific time (e.g., +1h, tomorrow, next monday)")
	deferCmd.ValidArgsFunction = issueIDCompletion
	rootCmd.AddCommand(deferCmd)
}
