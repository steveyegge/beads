package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// FeedEntry represents a single change in the feed output.
type FeedEntry struct {
	Timestamp time.Time       `json:"timestamp"`
	IssueID   string          `json:"issue_id"`
	EventType types.EventType `json:"event_type"`
	Actor     string          `json:"actor"`
	OldValue  *string         `json:"old_value,omitempty"`
	NewValue  *string         `json:"new_value,omitempty"`
	Comment   *string         `json:"comment,omitempty"`
}

// eventTypeIcon returns a display icon for each event type.
var eventTypeIcon = map[types.EventType]string{
	types.EventCreated:           "+",
	types.EventUpdated:           "~",
	types.EventStatusChanged:     ">",
	types.EventCommented:         "#",
	types.EventClosed:            "x",
	types.EventReopened:          "o",
	types.EventDependencyAdded:   "^",
	types.EventDependencyRemoved: "v",
	types.EventLabelAdded:        "t",
	types.EventLabelRemoved:      "t",
	types.EventCompacted:         "c",
}

var feedCmd = &cobra.Command{
	Use:     "feed",
	GroupID: "views",
	Short:   "Show recent changes as a chronological feed",
	Long: `Show a chronological feed of creates, updates, closes, and comments.

Reads from the events table to build a timeline of what changed.

Time range flags:
  --since     Show events after a timestamp (RFC3339 or YYYY-MM-DD)
  --last      Show events from the last duration (e.g., 24h, 7d, 1w)
  --limit     Maximum number of events to show (default: 50)

Watch mode:
  --watch     Continuously poll for new events
  --interval  Poll interval in watch mode (default: 30s)

Examples:
  bd feed                              # last 50 events
  bd feed --since 2026-02-14           # events since date
  bd feed --last 24h                   # events from last 24 hours
  bd feed --last 7d --json             # JSON output
  bd feed --watch --interval 10s       # live tail`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		sinceStr, _ := cmd.Flags().GetString("since")
		lastStr, _ := cmd.Flags().GetString("last")
		limit, _ := cmd.Flags().GetInt("limit")
		watch, _ := cmd.Flags().GetBool("watch")
		intervalStr, _ := cmd.Flags().GetString("interval")
		eventFilter, _ := cmd.Flags().GetString("type")

		if sinceStr != "" && lastStr != "" {
			fmt.Fprintf(os.Stderr, "Error: cannot use both --since and --last\n")
			os.Exit(1)
		}

		// Parse time threshold
		var since time.Time
		if sinceStr != "" {
			t, err := parseTimeFlag(sinceStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing --since: %v\n", err)
				os.Exit(1)
			}
			since = t
		} else if lastStr != "" {
			d, err := parseDuration(lastStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing --last: %v\n", err)
				os.Exit(1)
			}
			since = time.Now().UTC().Add(-d)
		}

		// Parse watch interval
		interval := 30 * time.Second
		if intervalStr != "" {
			d, err := parseDuration(intervalStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing --interval: %v\n", err)
				os.Exit(1)
			}
			interval = d
		}

		// Track last seen event ID for watch mode
		var lastSeenID int64

		for {
			events, err := store.GetAllEventsSince(ctx, lastSeenID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching events: %v\n", err)
				os.Exit(1)
			}

			// Filter by time
			var filtered []*types.Event
			for _, e := range events {
				if !since.IsZero() && e.CreatedAt.Before(since) {
					continue
				}
				if eventFilter != "" && string(e.EventType) != eventFilter {
					continue
				}
				filtered = append(filtered, e)
			}

			// Apply limit (take last N events, most recent)
			if !watch && limit > 0 && len(filtered) > limit {
				filtered = filtered[len(filtered)-limit:]
			}

			// Update last seen ID
			for _, e := range events {
				if e.ID > lastSeenID {
					lastSeenID = e.ID
				}
			}

			if len(filtered) > 0 {
				if jsonOutput {
					entries := make([]FeedEntry, 0, len(filtered))
					for _, e := range filtered {
						entries = append(entries, FeedEntry{
							Timestamp: e.CreatedAt,
							IssueID:   e.IssueID,
							EventType: e.EventType,
							Actor:     e.Actor,
							OldValue:  e.OldValue,
							NewValue:  e.NewValue,
							Comment:   e.Comment,
						})
					}
					data, _ := json.MarshalIndent(entries, "", "  ")
					fmt.Println(string(data))
				} else {
					renderFeedEvents(filtered)
				}
			} else if !watch {
				fmt.Println("No events found.")
			}

			if !watch {
				break
			}

			time.Sleep(interval)
		}
	},
}

// renderFeedEvents displays events in a human-readable format.
func renderFeedEvents(events []*types.Event) {
	for _, e := range events {
		icon := eventTypeIcon[e.EventType]
		if icon == "" {
			icon = "?"
		}

		ts := e.CreatedAt.Format("15:04:05")
		issueID := ui.StatusOpenStyle.Render(e.IssueID)
		actor := ui.RenderMuted(e.Actor)

		switch e.EventType {
		case types.EventCreated:
			title := ""
			if e.NewValue != nil {
				title = " " + *e.NewValue
			}
			fmt.Printf("[%s] %s %s created by %s%s\n", ts, icon, issueID, actor, title)

		case types.EventClosed:
			reason := ""
			if e.Comment != nil && *e.Comment != "" {
				reason = ": " + feedTruncate(*e.Comment, 60)
			}
			fmt.Printf("[%s] %s %s closed by %s%s\n", ts, icon, issueID, actor, reason)

		case types.EventStatusChanged:
			change := ""
			if e.OldValue != nil && e.NewValue != nil {
				oldStatus := extractJSONField(*e.OldValue, "status")
				newStatus := extractJSONField(*e.NewValue, "status")
				if oldStatus != "" && newStatus != "" {
					change = fmt.Sprintf(" %s -> %s", oldStatus, newStatus)
				}
			}
			fmt.Printf("[%s] %s %s status%s by %s\n", ts, icon, issueID, change, actor)

		case types.EventCommented:
			text := ""
			if e.Comment != nil {
				text = ": " + feedTruncate(*e.Comment, 60)
			}
			fmt.Printf("[%s] %s %s comment by %s%s\n", ts, icon, issueID, actor, text)

		case types.EventReopened:
			fmt.Printf("[%s] %s %s reopened by %s\n", ts, icon, issueID, actor)

		case types.EventDependencyAdded:
			dep := ""
			if e.NewValue != nil {
				dep = " -> " + *e.NewValue
			}
			fmt.Printf("[%s] %s %s dep added%s by %s\n", ts, icon, issueID, dep, actor)

		case types.EventDependencyRemoved:
			dep := ""
			if e.OldValue != nil {
				dep = " x " + *e.OldValue
			}
			fmt.Printf("[%s] %s %s dep removed%s by %s\n", ts, icon, issueID, dep, actor)

		case types.EventLabelAdded:
			label := ""
			if e.NewValue != nil {
				label = " +" + *e.NewValue
			}
			fmt.Printf("[%s] %s %s label%s by %s\n", ts, icon, issueID, label, actor)

		case types.EventLabelRemoved:
			label := ""
			if e.OldValue != nil {
				label = " -" + *e.OldValue
			}
			fmt.Printf("[%s] %s %s label%s by %s\n", ts, icon, issueID, label, actor)

		default:
			fmt.Printf("[%s] %s %s %s by %s\n", ts, icon, issueID, e.EventType, actor)
		}
	}
}

// extractJSONField attempts to extract a string field from a JSON string.
// Returns the raw string if not valid JSON (might be a plain status string).
func extractJSONField(jsonStr, field string) string {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return jsonStr
	}
	if v, ok := m[field]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// feedTruncate shortens a string to maxLen, adding "..." if truncated.
func feedTruncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// parseDuration parses a duration string supporting d (days) and w (weeks)
// in addition to Go's standard time.ParseDuration units.
func parseDuration(s string) (time.Duration, error) {
	// Handle day/week suffixes
	if strings.HasSuffix(s, "d") {
		s = strings.TrimSuffix(s, "d")
		var days float64
		if _, err := fmt.Sscanf(s, "%f", &days); err != nil {
			return 0, fmt.Errorf("invalid duration: %s", s+"d")
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	if strings.HasSuffix(s, "w") {
		s = strings.TrimSuffix(s, "w")
		var weeks float64
		if _, err := fmt.Sscanf(s, "%f", &weeks); err != nil {
			return 0, fmt.Errorf("invalid duration: %s", s+"w")
		}
		return time.Duration(weeks * 7 * float64(24*time.Hour)), nil
	}
	return time.ParseDuration(s)
}

func init() {
	feedCmd.Flags().String("since", "", "Show events after timestamp (RFC3339 or YYYY-MM-DD)")
	feedCmd.Flags().String("last", "", "Show events from the last duration (e.g., 24h, 7d, 1w)")
	feedCmd.Flags().Int("limit", 50, "Maximum number of events to show")
	feedCmd.Flags().Bool("watch", false, "Continuously poll for new events")
	feedCmd.Flags().String("interval", "30s", "Poll interval in watch mode")
	feedCmd.Flags().String("type", "", "Filter by event type (created, closed, commented, status_changed, etc.)")

	rootCmd.AddCommand(feedCmd)
}
