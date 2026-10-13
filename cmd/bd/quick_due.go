package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/timeparsing"
	"github.com/steveyegge/beads/internal/types"
)

// quickDueLadderDays is the days-from-now a quick-captured issue is due, by
// priority: the more urgent the work, the sooner the deadline it inherits.
// Indexed by priority 0-4.
var quickDueLadderDays = [5]int{1, 3, 7, 14, 30}

// resolveQuickDue settles `bd q`'s due date: an explicit --due wins, otherwise
// the priority ladder supplies one so quick capture stays a one-liner under a
// workspace that requires due dates.
//
// The ladder only fires when `due.required` is on. With the invariant off this
// is upstream `bd q`, which mints no due date at all, and stays that way.
func resolveQuickDue(cmd *cobra.Command, issue *types.Issue) error {
	dueStr, _ := cmd.Flags().GetString("due")
	if dueStr != "" {
		t, err := timeparsing.ParseRelativeTime(dueStr, time.Now())
		if err != nil {
			return HandleError("invalid --due format %q. %s", dueStr, deferUntilFormatHint)
		}
		issue.DueAt = &t
		return nil
	}
	applyDefaultDue(issue)
	return nil
}

// applyDefaultDue gives a bead that has no due date one from the priority
// ladder, stamped due_source=default, when the workspace requires due dates
// and the bead is held to the rule.
//
// It is what every create surface WITHOUT a --due of its own runs before the
// storage boundary: quick capture, `bd todo add`, `bd gate create`, molecule
// instantiation, and the multi-bead plans (--graph, --file, batch). `bd create`
// itself deliberately does not: the one surface where a human names each bead
// is the one that asks them to name its deadline. A ladder date stays
// distinguishable from a chosen one through its source, so nothing about the
// invariant is hidden — every bead has a deadline, and the column says which
// ones were synthesized.
func applyDefaultDue(issue *types.Issue) {
	if issue == nil || issue.DueAt != nil ||
		!issueops.DueRequiredEnabled() || issueops.DueRequiredExempt(issue) {
		return
	}
	priority := issue.Priority
	if priority < 0 || priority >= len(quickDueLadderDays) {
		priority = len(quickDueLadderDays) - 1
	}
	due := time.Now().AddDate(0, 0, quickDueLadderDays[priority])
	issue.DueAt = &due
	issue.DueSource = types.DueSourceDefault
}

// applyDefaultDues runs applyDefaultDue over a plan of beads.
func applyDefaultDues(issues []*types.Issue) {
	for _, issue := range issues {
		applyDefaultDue(issue)
	}
}

// echoQuickDue reports the due date the ladder resolved, on STDERR: `bd q`
// prints the new issue's ID and nothing else on stdout, because `ID=$(bd q …)`
// is its whole reason for existing.
func echoQuickDue(issue *types.Issue) {
	if issue.DueAt == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "due %s (P%d)\n", issue.DueAt.Format(time.RFC3339), issue.Priority)
}
