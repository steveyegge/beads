package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
)

var gitResetCmd = &cobra.Command{
	Use:   "reset [git-reset-args...]",
	Short: "Git reset with immediate Dolt sync check",
	Long: `Wraps git reset and immediately checks beads refs for git-Dolt mismatch.

This is the recommended way to time-travel with beads. After git reset
completes, bd reset runs mismatch detection and takes action per your
branch_strategy.* settings (silent, auto-reset, or interactive prompt).

All arguments are passed through to git reset.

Examples:
  bd reset --hard HEAD~1              # Reset one commit, sync Dolt
  bd reset --hard <commit-hash>       # Reset to specific commit, sync Dolt
  bd reset --soft HEAD~1              # Soft reset (no working tree change)`,
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		gitArgs := append([]string{"reset"}, args...)
		gitCmd := exec.CommandContext(rootCtx, "git", gitArgs...) //nolint:gosec // args are user-provided git reset flags

		// Capture git's stdout so we can prefix it with "git: " in the prompt
		var gitOut bytes.Buffer
		gitCmd.Stdout = &gitOut
		gitCmd.Stderr = os.Stderr
		gitCmd.Stdin = os.Stdin

		if err := gitCmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			fmt.Fprintf(os.Stderr, "bd reset: git reset failed: %v\n", err)
			os.Exit(1)
		}

		// Print git output, prefixing "HEAD is now at" with "git: "
		// and passing it to the sync check for context.
		var gitResetLine string
		for _, line := range strings.Split(gitOut.String(), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if strings.Contains(line, "HEAD is now at") {
				gitResetLine = trimmed
				fmt.Fprintf(os.Stderr, "git:  %s\n", gitResetLine)
			} else {
				fmt.Println(line)
			}
		}

		// Immediately check refs for mismatch
		if store != nil && !store.IsClosed() {
			checkBeadsRefSyncWithGitLine(rootCtx, store, gitResetLine)
		}
	},
}

var checkRefsCmd = &cobra.Command{
	Use:   "check-refs",
	Short: "Check beads refs for consistency with git-dolt history",
	Long: `Compares .beads/refs against the current dolt commit hash
and takes action based on branch_strategy.* settings.

If in sync, does nothing. On mismatch:
  - prompt=false: follows default strategy silently
  - prompt=true:  prompt for determination, default strategy as suggested answer

The strategy (reset_dolt_with_git) controls whether dolt is reset to
match git history or whether histories are intentionally allowed to diverge.

Useful for shell integration:

  git() {
    command git "$@"
    if [[ "$1" == "reset" ]]; then
      bd check-refs
    fi
  }`,
	Run: func(cmd *cobra.Command, args []string) {
		if !config.IsBranchStrategyEnabled() {
			fmt.Fprintf(os.Stderr, "beads: refs disabled (no branch_strategy section in config.yaml)\n")
			return
		}
		if store != nil && !store.IsClosed() {
			checkBeadsRefSync(rootCtx, store)
		}
	},
}

func init() {
	rootCmd.AddCommand(gitResetCmd)
	rootCmd.AddCommand(checkRefsCmd)
}
