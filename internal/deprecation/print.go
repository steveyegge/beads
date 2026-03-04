package deprecation

import (
	"fmt"
	"os"

	"github.com/steveyegge/beads/internal/ui"
)

// PrintWarnings outputs deprecation warnings to stderr.
// Returns true if any warnings were printed.
// Suppressed when jsonMode is true.
func PrintWarnings(warnings []Warning, jsonMode bool) bool {
	if len(warnings) == 0 || jsonMode {
		return false
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, ui.RenderWarn("WARNING")+": Deprecated configuration detected (will be removed in v1.0.0):")
	fmt.Fprintln(os.Stderr)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "  - %s\n", w.Summary)
		fmt.Fprintf(os.Stderr, "    %s\n", w.Detail)
		fmt.Fprintf(os.Stderr, "    Fix: %s\n", w.Action)
		fmt.Fprintln(os.Stderr)
	}
	fmt.Fprintln(os.Stderr, "Run 'bd doctor' for full diagnostics.")
	fmt.Fprintln(os.Stderr)
	return true
}
