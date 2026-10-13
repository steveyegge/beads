package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/timeparsing"
	"github.com/steveyegge/beads/internal/types"
)

// recurrenceFlags holds the parsed --repeat/--repeat-start/--repeat-end
// triple. The zero value means "not recurring".
type recurrenceFlags struct {
	pattern string
	start   *time.Time
	end     *time.Time
}

// gatherRecurrenceFlags parses and cross-validates the recurrence flags.
//
// Bounds without a pattern are refused rather than silently ignored: a bare
// --repeat-end reads as a --repeat that did not make it onto the command line,
// and accepting it would store a bound that can never apply.
func gatherRecurrenceFlags(cmd *cobra.Command) (recurrenceFlags, error) {
	var out recurrenceFlags
	out.pattern, _ = cmd.Flags().GetString("repeat")

	now := time.Now()
	for _, bound := range []struct {
		flag string
		dest **time.Time
	}{
		{"repeat-start", &out.start},
		{"repeat-end", &out.end},
	} {
		raw, _ := cmd.Flags().GetString(bound.flag)
		if raw == "" {
			continue
		}
		t, err := timeparsing.ParseRelativeTime(raw, now)
		if err != nil {
			return out, HandleError("invalid --%s format %q. %s", bound.flag, raw, deferUntilFormatHint)
		}
		t = t.UTC()
		*bound.dest = &t
	}

	if out.pattern == "" {
		if out.start != nil || out.end != nil {
			return out, HandleError("--repeat-start/--repeat-end require --repeat")
		}
		return out, nil
	}
	if _, err := timeparsing.ParseRepeat(out.pattern); err != nil {
		return out, HandleError("invalid --repeat pattern %q: %v\n  Interval: +1d, +2w, +1m, +1y\n  Cron:     \"0 9 * * 1\" (min hour day-of-month month day-of-week)", out.pattern, err)
	}
	warnSubHourlyRepeat(out.pattern)
	if out.start != nil && out.end != nil && out.end.Before(*out.start) {
		return out, HandleError("--repeat-end (%s) is before --repeat-start (%s)",
			out.end.Format(time.RFC3339), out.start.Format(time.RFC3339))
	}
	return out, nil
}

// repeatPeriod returns the gap between two consecutive occurrences of a
// pattern. It prices a close-spawn cadence at authoring time: closing a
// recurring bead files a NEW row for the successor (SpawnRecurrenceInTx), so a
// sub-hourly heartbeat files identical history rows all day while an open one
// is re-fired by the due sweep for free. Measured 2026-10-05: a */10 chain
// filed 53 beads in 9 hours (seat billing-learn).
func repeatPeriod(pattern string) (time.Duration, bool) {
	probe := &types.Issue{RepeatPattern: pattern}
	now := time.Now().UTC()
	t1, ok, err := probe.NextOccurrence(now)
	if err != nil || !ok {
		return 0, false
	}
	t2, ok, err := probe.NextOccurrence(t1.Add(time.Second))
	if err != nil || !ok {
		return 0, false
	}
	return t2.Sub(t1), true
}

// warnSubHourlyRepeat states the row cost on stderr, where the author reads
// it, and names the row-free alternative. Warning only: a close-spawn cadence
// is legal, and refusing it would break heartbeat series that already work.
func warnSubHourlyRepeat(pattern string) {
	period, ok := repeatPeriod(pattern)
	if !ok || period <= 0 || period >= time.Hour {
		return
	}
	// Concatenation, not Fprintf: gosec taint flags user input reaching a
	// format function even as an argument (G705), and nothing here needs a format.
	fmt.Fprintln(os.Stderr, //nolint:gosec // G705: stderr, not a browser context
		"warning: --repeat "+strconv.Quote(pattern)+" runs about every "+period.Round(time.Minute).String()+
			", and closing a recurring bead files a new row each time (about "+
			strconv.Itoa(int((24*time.Hour)/period))+
			" identical beads/day). Keep the bead OPEN and comment per fire so the due sweep re-fires it without spawning, or use a period of at least 1h.")
}

// registerRecurrenceFlags adds the recurrence flags to a create-shaped command.
func registerRecurrenceFlags(cmd *cobra.Command) {
	cmd.Flags().String("repeat", "", "Recurrence rule: an interval (+1d, +2w, +1m) or a 5-field cron expression (\"0 9 * * 1\"). Closing a recurring bead is refused while recurrence.close_guard is on (the default); end the series with --repeat \"\" first. With the guard off, closing spawns the next instance under the same parent (peer dependencies are not copied). A +1m/+1y series clamps to the last day of a shorter month (Jan 31 -> Feb 28 -> Mar 31)")
	cmd.Flags().String("repeat-start", "", "Earliest occurrence of a recurring bead. Same formats as --due")
	cmd.Flags().String("repeat-end", "", "Last occurrence of a recurring bead; the series stops after it. Same formats as --due")
}

// dueSourceFromFlag reads the --due-source provenance flag. Empty keeps the
// CLI's explicit default; anything else must name a valid due source.
func dueSourceFromFlag(cmd *cobra.Command) (types.DueSource, error) {
	raw, _ := cmd.Flags().GetString("due-source")
	if raw == "" {
		return types.DueSourceExplicit, nil
	}
	source := types.DueSource(raw)
	if !source.IsValid() {
		return "", HandleError("invalid --due-source %q (must be %s)", raw, types.ValidDueSourceNames())
	}
	return source, nil
}

// firstOccurrenceDue derives the due date of a recurring bead's FIRST instance
// when the caller gave --repeat without --due: the rule alone already says when
// the work is next expected, so asking for a redundant --due would be busywork.
//
// Returns nil when there is nothing to derive (not recurring, or a --due was
// supplied), leaving the bead's due date exactly as the caller left it.
func firstOccurrenceDue(r recurrenceFlags, dueAt *time.Time, now time.Time) (*time.Time, error) {
	if dueAt != nil || r.pattern == "" {
		return nil, nil
	}
	probe := &types.Issue{RepeatPattern: r.pattern, RepeatStart: r.start, RepeatEnd: r.end}
	next, ok, err := probe.NextOccurrence(now)
	if err != nil {
		return nil, HandleError("invalid --repeat pattern %q: %v", r.pattern, err)
	}
	if !ok {
		return nil, HandleError("--repeat series has no occurrence before --repeat-end (%s)", r.end.Format(time.RFC3339))
	}
	return &next, nil
}

// applyRecurrenceUpdateFlags folds --repeat/--repeat-start/--repeat-end into an
// update field map. Only flags the caller actually passed are written, so an
// update that names none leaves the bead's recurrence alone.
//
// An empty --repeat="" stops the series. That needs no --force gate (unlike
// clearing a due date): a bead that stops repeating still has its own deadline,
// so nothing the mandatory-due invariant protects is lost.
func applyRecurrenceUpdateFlags(cmd *cobra.Command, fields map[string]any) error {
	if cmd.Flags().Changed("repeat") {
		pattern, _ := cmd.Flags().GetString("repeat")
		if pattern != "" {
			if _, err := timeparsing.ParseRepeat(pattern); err != nil {
				return HandleErrorRespectJSON("invalid --repeat pattern %q: %v\n  Interval: +1d, +2w, +1m, +1y\n  Cron:     \"0 9 * * 1\" (min hour day-of-month month day-of-week)", pattern, err)
			}
			warnSubHourlyRepeat(pattern)
		}
		fields["repeat_pattern"] = pattern
	}
	now := time.Now()
	for _, bound := range []struct{ flag, key string }{
		{"repeat-start", "repeat_start"},
		{"repeat-end", "repeat_end"},
	} {
		if !cmd.Flags().Changed(bound.flag) {
			continue
		}
		raw, _ := cmd.Flags().GetString(bound.flag)
		if raw == "" {
			fields[bound.key] = nil
			continue
		}
		t, err := timeparsing.ParseRelativeTime(raw, now)
		if err != nil {
			return HandleErrorRespectJSON("invalid --%s format %q. %s", bound.flag, raw, deferUntilFormatHint)
		}
		fields[bound.key] = t.UTC()
	}
	return nil
}

// registerRecurrenceUpdateFlags adds the recurrence flags to `bd update`. The
// descriptions differ from create's: here empty clears.
func registerRecurrenceUpdateFlags(cmd *cobra.Command) {
	cmd.Flags().String("repeat", "", "Recurrence rule, empty to stop repeating: an interval (+1d, +2w) or 5-field cron (\"0 9 * * 1\")")
	cmd.Flags().String("repeat-start", "", "Earliest occurrence (empty to clear). Same formats as --due")
	cmd.Flags().String("repeat-end", "", "Last occurrence (empty to clear). Same formats as --due")
}
