package hooks

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// Dispatcher fires config-driven event hooks after bead writes.
type Dispatcher struct {
	hooks   []EventHook
	timeout time.Duration
}

// NewDispatcher creates a dispatcher from loaded hook configs.
// Returns nil if hooks is empty (no-op dispatcher not needed).
func NewDispatcher(hooks []EventHook) *Dispatcher {
	if len(hooks) == 0 {
		return nil
	}
	return &Dispatcher{
		hooks:   hooks,
		timeout: 10 * time.Second,
	}
}

// Fire triggers all hooks matching the given event and issue.
// Async hooks run in goroutines; sync hooks block until complete or timeout.
// Errors are logged but never propagated — hooks must not break bd commands.
func (d *Dispatcher) Fire(event string, issue *types.Issue) {
	if d == nil {
		return
	}

	vars := VarsFromIssue(issue, event)

	for _, h := range d.hooks {
		if !d.matches(h, event, issue) {
			continue
		}

		expanded := ExpandCommand(h.Command, vars)

		if h.Async {
			go d.run(expanded)
		} else {
			d.run(expanded)
		}
	}
}

// FireComment triggers hooks for comment events with comment-specific vars.
func (d *Dispatcher) FireComment(issue *types.Issue, author, body string) {
	if d == nil {
		return
	}

	vars := VarsFromIssue(issue, "comment")
	vars.CommentAuthor = author
	vars.CommentBody = body

	for _, h := range d.hooks {
		if !d.matches(h, "comment", issue) {
			continue
		}

		expanded := ExpandCommand(h.Command, vars)

		if h.Async {
			go d.run(expanded)
		} else {
			d.run(expanded)
		}
	}
}

// matches checks if a hook should fire for the given event and issue.
func (d *Dispatcher) matches(h EventHook, event string, issue *types.Issue) bool {
	// Event matching: "post-write" matches everything, others match exactly
	if h.Event != "post-write" {
		// Strip "post-" prefix for comparison (event comes in as "create", hook has "post-create")
		hookEvent := strings.TrimPrefix(h.Event, "post-")
		if hookEvent != event {
			return false
		}
	}

	// Filter matching
	if h.Filter != "" && issue != nil {
		if !matchesFilter(h.Filter, issue) {
			return false
		}
	}

	return true
}

// matchesFilter evaluates a simple filter expression against an issue.
// Supported: "priority:P0,P1", "type:bug", "status:open", "rig:aegis"
func matchesFilter(filter string, issue *types.Issue) bool {
	parts := strings.SplitN(filter, ":", 2)
	if len(parts) != 2 {
		return true // Malformed filter — pass through
	}

	field := strings.TrimSpace(parts[0])
	values := strings.Split(parts[1], ",")
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
	}

	switch field {
	case "priority":
		issuePri := priorityString(issue.Priority)
		return containsIgnoreCase(values, issuePri)
	case "type":
		return containsIgnoreCase(values, string(issue.IssueType))
	case "status":
		return containsIgnoreCase(values, string(issue.Status))
	case "rig":
		rig := extractRig(issue.ID)
		return containsIgnoreCase(values, rig)
	default:
		return true // Unknown field — pass through
	}
}

func containsIgnoreCase(haystack []string, needle string) bool {
	needle = strings.ToLower(needle)
	for _, h := range haystack {
		if strings.ToLower(h) == needle {
			return true
		}
	}
	return false
}

// run executes a shell command with timeout. Errors are logged, not returned.
func (d *Dispatcher) run(command string) {
	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
	defer cancel()

	// #nosec G204 — command is from config.yaml, variable values are sanitized
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[hooks] command failed: %s: %v (output: %s)", truncate(command, 80), err, truncate(string(output), 200))
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("...(%d more)", len(s)-max)
}
