package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/timeparsing"
	"github.com/steveyegge/beads/internal/types"
)

var eventsCmd = &cobra.Command{
	Use:     "events [issue-id]",
	GroupID: "maint",
	Short:   "Show audit events for an issue or the whole rig",
	Long: `Show the audit trail events for an issue, or list all events since a point in time.

When an issue-id is given, shows events for that issue.
When --since is given (no issue-id), lists all rig events newer than that time.
When --limit 0 is combined with --json, output is streamed via the iterator path.

Examples:
  bd events bd-123              # Show last 50 events for issue bd-123
  bd events bd-123 --limit 0   # Show all events (streaming JSON with --json)
  bd events bd-123 --json       # JSON array of events
  bd events --since 30d --json  # All rig events in the last 30 days (streaming)`,
	RunE: runEvents,
}

func runEvents(cmd *cobra.Command, args []string) error {
	ctx := rootCtx
	limit, _ := cmd.Flags().GetInt("limit")
	sinceStr, _ := cmd.Flags().GetString("since")

	if store == nil {
		return fmt.Errorf("no database connection")
	}

	// --since path: all rig events since a relative time, always streams via IterAllEventsSince.
	if sinceStr != "" {
		since, err := timeparsing.ParseRelativeTime(sinceStr, time.Now())
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", sinceStr, err)
		}
		if jsonOutput {
			it, err := store.IterAllEventsSince(ctx, since)
			if err != nil {
				return err
			}
			streamEventsJSON(it)
			return nil
		}
		events, err := store.GetAllEventsSince(ctx, since)
		if err != nil {
			return err
		}
		printEventsText(events)
		return nil
	}

	// Per-issue path.
	if len(args) == 0 {
		return fmt.Errorf("issue ID required (or use --since to list all recent events)")
	}
	issueID := args[0]

	// --limit 0 --json uses IterEvents to avoid materializing the full slice (be-ritg7o).
	if jsonOutput && limit == 0 {
		it, err := store.IterEvents(ctx, issueID, 0)
		if err != nil {
			return err
		}
		streamEventsJSON(it)
		return nil
	}

	events, err := store.GetEvents(ctx, issueID, limit)
	if err != nil {
		return err
	}
	if jsonOutput {
		outputJSON(events)
		return nil
	}
	printEventsText(events)
	return nil
}

// streamEventsJSON streams a types.Event iterator as a JSON array to stdout.
func streamEventsJSON(it storage.Iter[types.Event]) {
	w := os.Stdout
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if jsonEnvelopeEnabled() {
		fmt.Fprintf(w, `{"schema_version":%d,"data":[`, JSONSchemaVersion)
	} else {
		_, _ = fmt.Fprint(w, "[")
	}
	first := true
	for it.Next(rootCtx) {
		if !first {
			_, _ = fmt.Fprint(w, ",")
		}
		first = false
		ev := it.Value()
		_ = enc.Encode(ev)
	}
	if err := it.Err(); err != nil {
		FatalError("%v", err)
	}
	if jsonEnvelopeEnabled() {
		fmt.Fprintln(w, "]}")
	} else {
		fmt.Fprintln(w, "]")
		emitEnvelopeDeprecation()
	}
}

// printEventsText renders events as human-readable text to stdout.
func printEventsText(events []*types.Event) {
	if len(events) == 0 {
		fmt.Println("No events found")
		return
	}
	for _, ev := range events {
		fmt.Printf("%s  %s  %s  %s\n",
			ev.CreatedAt.Format("2006-01-02 15:04:05"),
			ev.IssueID,
			ev.Actor,
			ev.EventType,
		)
		if ev.OldValue != nil || ev.NewValue != nil {
			old := ""
			if ev.OldValue != nil {
				old = *ev.OldValue
			}
			nw := ""
			if ev.NewValue != nil {
				nw = *ev.NewValue
			}
			fmt.Printf("    %s → %s\n", old, nw)
		}
		if ev.Comment != nil {
			fmt.Printf("    %s\n", *ev.Comment)
		}
	}
}

func init() {
	eventsCmd.Flags().Int("limit", 50, "Limit results (0 = unlimited, streams via IterEvents with --json)")
	eventsCmd.Flags().String("since", "", "List all rig events newer than this duration (e.g., 30d, 1h); always streams with --json")
	rootCmd.AddCommand(eventsCmd)
}
