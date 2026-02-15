package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/types"
)

var hooksEventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Manage config-driven event hooks",
	Long: `Manage event hooks defined in .beads/config.yaml.

Event hooks fire after bead write operations (create, update, close, comment).
They can run external commands with ${BEAD_*} variable substitution.

Configuration example (.beads/config.yaml):

  event-hooks:
    - event: post-write
      command: "bobbin index-bead --id ${BEAD_ID} --rig ${BEAD_RIG}"
      async: true
    - event: post-create
      filter: "priority:P0,P1"
      command: "curl -s -d '${BEAD_TITLE}' http://ntfy.svc/aegis"
      async: true

Events: post-create, post-update, post-close, post-comment, post-write (any)
Variables: BEAD_ID, BEAD_RIG, BEAD_TITLE, BEAD_PRIORITY, BEAD_TYPE, BEAD_EVENT, BEAD_STATUS`,
}

var hooksEventsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured event hooks",
	Run: func(cmd *cobra.Command, args []string) {
		eventHooks, err := hooks.LoadEventHooks(config.GetViper())
		if err != nil {
			if jsonOutput {
				outputJSON(map[string]string{"error": err.Error()})
			} else {
				fmt.Fprintf(os.Stderr, "Error loading event hooks: %v\n", err)
			}
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"event_hooks": eventHooks,
				"count":       len(eventHooks),
			})
			return
		}

		if len(eventHooks) == 0 {
			fmt.Println("No event hooks configured.")
			fmt.Println()
			fmt.Println("Add hooks to .beads/config.yaml:")
			fmt.Println("  event-hooks:")
			fmt.Println("    - event: post-write")
			fmt.Println("      command: \"echo ${BEAD_ID}\"")
			fmt.Println("      async: true")
			return
		}

		fmt.Printf("Event hooks (%d configured):\n\n", len(eventHooks))
		for i, h := range eventHooks {
			fmt.Printf("  %d. [%s]", i+1, h.Event)
			if h.Filter != "" {
				fmt.Printf(" filter=%s", h.Filter)
			}
			if h.Async {
				fmt.Printf(" (async)")
			}
			fmt.Println()
			fmt.Printf("     command: %s\n", h.Command)
		}
	},
}

var hooksEventsTestCmd = &cobra.Command{
	Use:   "test <event> [issue-id]",
	Short: "Test fire an event hook (dry-run or live)",
	Long: `Test an event hook by simulating or actually firing it.

By default, shows what would be executed (dry-run).
Use --live to actually execute the hook commands.

Examples:
  bd hooks events test post-create         # dry-run with synthetic issue
  bd hooks events test post-create bd-123  # dry-run with real issue
  bd hooks events test post-write --live   # actually fire hooks`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		event := args[0]
		live, _ := cmd.Flags().GetBool("live")

		if !hooks.IsValidEventName(event) {
			fmt.Fprintf(os.Stderr, "Unknown event: %s\nValid events: %s\n",
				event, strings.Join(hooks.ValidEvents, ", "))
			os.Exit(1)
		}

		eventHooks, err := hooks.LoadEventHooks(config.GetViper())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading hooks: %v\n", err)
			os.Exit(1)
		}

		if len(eventHooks) == 0 {
			fmt.Println("No event hooks configured.")
			return
		}

		// Build issue (real or synthetic)
		var issue *types.Issue
		if len(args) >= 2 {
			// Try to load real issue
			if store != nil {
				ctx := rootCtx
				if loaded, err := store.GetIssue(ctx, args[1]); err == nil {
					issue = loaded
				}
			}
		}
		if issue == nil {
			issue = &types.Issue{
				ID:        "test-abc",
				Title:     "Test Hook Issue",
				Priority:  1,
				IssueType: "task",
				Status:    "open",
			}
		}

		// Strip "post-" prefix to match dispatcher convention
		eventName := strings.TrimPrefix(event, "post-")

		vars := hooks.VarsFromIssue(issue, eventName)

		matched := 0
		for _, h := range eventHooks {
			// Check if this hook would fire
			hookEvent := strings.TrimPrefix(h.Event, "post-")
			if h.Event != "post-write" && hookEvent != eventName {
				continue
			}

			expanded := hooks.ExpandCommand(h.Command, vars)

			if jsonOutput {
				outputJSON(map[string]interface{}{
					"hook":     h,
					"expanded": expanded,
					"vars":     vars,
					"live":     live,
				})
			} else {
				asyncLabel := ""
				if h.Async {
					asyncLabel = " (async)"
				}
				fmt.Printf("Hook: [%s]%s\n", h.Event, asyncLabel)
				fmt.Printf("  Expanded: %s\n", expanded)
			}

			if live {
				d := hooks.NewDispatcher([]hooks.EventHook{h})
				d.Fire(eventName, issue)
				if !jsonOutput {
					fmt.Println("  Status: fired")
				}
			} else if !jsonOutput {
				fmt.Println("  Status: dry-run (use --live to execute)")
			}
			fmt.Println()
			matched++
		}

		if matched == 0 && !jsonOutput {
			fmt.Printf("No hooks match event %q\n", event)
		} else if !jsonOutput {
			fmt.Printf("%d hook(s) matched\n", matched)
		}
	},
}

// IsValidEventName is a convenience wrapper exported for CLI use.
func init() {
	hooksEventsTestCmd.Flags().Bool("live", false, "Actually execute the hook commands")

	hooksEventsCmd.AddCommand(hooksEventsListCmd)
	hooksEventsCmd.AddCommand(hooksEventsTestCmd)
	hooksCmd.AddCommand(hooksEventsCmd)
}
