package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	beads "github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/linear"
)

// maybeWarnLinearStaleness emits a one-time stderr warning when Linear data
// is stale. Uses a temp file marker to suppress repeat warnings within the
// same shell session. Only warns if LINEAR_API_KEY is configured.
func maybeWarnLinearStaleness(cmd *cobra.Command) {
	// Don't warn during prime (it handles staleness itself) or during sync
	if cmd.Name() == "prime" || (cmd.Parent() != nil && cmd.Parent().Name() == "linear") {
		return
	}

	// Quiet mode suppresses warnings
	if quietFlag {
		return
	}

	apiKey := os.Getenv("LINEAR_API_KEY")
	if apiKey == "" {
		if yamlKey := config.GetString("linear.api_key"); yamlKey == "" {
			return
		}
	}

	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return
	}

	if !linear.IsPullStale(beadsDir, linear.DefaultStaleThreshold) {
		return
	}

	// Session marker: suppress repeated warnings per shell session.
	// Uses the parent shell PID as session identifier.
	ppid := os.Getppid()
	markerPath := filepath.Join(os.TempDir(), fmt.Sprintf("bd-staleness-warned-%d", ppid))
	if _, err := os.Stat(markerPath); err == nil {
		return
	}

	info := linear.GetStalenessInfo(beadsDir, linear.DefaultStaleThreshold)
	if info.NeverPulled {
		fmt.Fprintf(os.Stderr, "⚠ Linear data has never been pulled — run 'bd linear sync --pull' to import\n")
	} else {
		fmt.Fprintf(os.Stderr, "⚠ Linear data is %s stale — run 'bd linear sync --pull' to refresh\n", linear.FormatAge(info.Age))
	}

	// Write session marker to suppress future warnings in this shell
	_ = os.WriteFile(markerPath, []byte("1"), 0600)
}
