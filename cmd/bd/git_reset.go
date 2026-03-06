package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
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
		gitCmd.Stdout = os.Stdout
		gitCmd.Stderr = os.Stderr
		gitCmd.Stdin = os.Stdin

		if err := gitCmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			fmt.Fprintf(os.Stderr, "bd reset: git reset failed: %v\n", err)
			os.Exit(1)
		}

		// Immediately check refs for mismatch
		if store != nil && !store.IsClosed() {
			checkBeadsRefSync(rootCtx, store)
		}
	},
}

var checkRefsCmd = &cobra.Command{
	Use:   "check-refs",
	Short: "Check beads refs for git-Dolt mismatch",
	Long: `Runs mismatch detection against beads ref files. Respects branch_strategy.*
settings — does not force a sync.

Useful for shell integration:

  git() {
    command git "$@"
    if [[ "$1" == "reset" ]]; then
      bd check-refs
    fi
  }

Exit codes:
  0  In sync, or action taken (reset/prompt answered)
  1  Mismatch detected but no action (silent mode)`,
	Run: func(cmd *cobra.Command, args []string) {
		if store != nil && !store.IsClosed() {
			checkBeadsRefSync(rootCtx, store)
		}
	},
}

func init() {
	rootCmd.AddCommand(gitResetCmd)
	rootCmd.AddCommand(checkRefsCmd)
}
