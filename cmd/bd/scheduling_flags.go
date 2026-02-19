package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/timeparsing"
	"github.com/steveyegge/beads/internal/ui"
)

func parseSchedulingFlag(flagName, raw string, now time.Time) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parsed, err := timeparsing.ParseRelativeTime(raw, now)
	if err != nil {
		return nil, fmt.Errorf("invalid --%s format %q. Examples: %s", flagName, raw, schedulingExamples(flagName))
	}
	return &parsed, nil
}

func schedulingExamples(flagName string) string {
	switch flagName {
	case "due":
		return "+6h, tomorrow, next monday, 2025-01-15"
	default:
		return "+1h, tomorrow, next monday, 2025-01-15"
	}
}

func warnIfPastDeferredTime(parsed *time.Time, enabled bool) {
	if !enabled || parsed == nil {
		return
	}
	if parsed.Before(time.Now()) {
		fmt.Fprintf(os.Stderr, "%s Defer date %q is in the past. Issue will appear in bd ready immediately.\n",
			ui.RenderWarn("!"), parsed.Format("2006-01-02 15:04"))
		fmt.Fprintln(os.Stderr, "  Did you mean a future date? Use --defer=+1h or --defer=tomorrow")
	}
}
