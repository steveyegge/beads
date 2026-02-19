package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

var deferCmd = &cobra.Command{
	Use:   "defer [id...]",
	Short: "Defer one or more issues for later",
	Long: `Defer issues to put them on ice for later.

Deferred issues are deliberately set aside - not blocked by anything specific,
just postponed for future consideration. Unlike blocked issues, there's no
dependency keeping them from being worked. Unlike closed issues, they will
be revisited.

Deferred issues don't show in 'bd ready' but remain visible in 'bd list'.

Examples:
  bd defer bd-abc                  # Defer a single issue (status-based)
  bd defer bd-abc --until=tomorrow # Defer until specific time
  bd defer bd-abc bd-def           # Defer multiple issues`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("defer")

		// Parse --until flag (GH#820)
		untilStr, _ := cmd.Flags().GetString("until")
		allowUnbounded, _ := cmd.Flags().GetBool("allow-unbounded")
		if err := validateDeferLiveness(untilStr, allowUnbounded); err != nil {
			if jsonOutput {
				finishEnvelope(commandEnvelope{
					OK:      false,
					Command: "defer",
					Result:  "policy_violation",
					Details: map[string]interface{}{
						"message": err.Error(),
					},
					RecoveryCommand: "bd defer <id> --until <time>",
					Events:          []string{"defer_rejected"},
				}, exitCodePolicyViolation)
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(exitCodePolicyViolation)
		}
		deferUntil, err := parseSchedulingFlag("until", untilStr, time.Now())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		ctx := rootCtx

		// Resolve partial IDs
		_, err = utils.ResolvePartialIDs(ctx, store, args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		deferredIssues := []*types.Issue{}

		// Direct storage access
		if store == nil {
			FatalErrorWithHint("database not initialized",
				"run 'bd init' to create a database, or use 'bd --no-db' for JSONL-only mode")
		}

		for _, id := range args {
			fullID, err := utils.ResolvePartialID(ctx, store, id)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error resolving %s: %v\n", id, err)
				continue
			}

			updates := map[string]interface{}{
				"status": string(types.StatusDeferred),
			}
			// Add defer_until if --until specified (GH#820)
			if deferUntil != nil {
				updates["defer_until"] = *deferUntil
			}

			if err := store.UpdateIssue(ctx, fullID, updates, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Error deferring %s: %v\n", fullID, err)
				continue
			}

			if jsonOutput {
				issue, _ := store.GetIssue(ctx, fullID)
				if issue != nil {
					deferredIssues = append(deferredIssues, issue)
				}
			} else {
				fmt.Printf("%s Deferred %s\n", ui.RenderAccent("*"), fullID)
			}
		}

		if jsonOutput && len(deferredIssues) > 0 {
			outputJSON(deferredIssues)
		}
	},
}

func init() {
	// Time-based scheduling flag (GH#820)
	deferCmd.Flags().String("until", "", "Defer until specific time (e.g., +1h, tomorrow, next monday)")
	deferCmd.Flags().Bool("allow-unbounded", false, "Allow defer without --until (manual/exceptional use)")
	deferCmd.ValidArgsFunction = issueIDCompletion
	rootCmd.AddCommand(deferCmd)
}

func validateDeferLiveness(untilRaw string, allowUnbounded bool) error {
	if strings.TrimSpace(untilRaw) != "" {
		return nil
	}
	if allowUnbounded {
		return nil
	}
	return fmt.Errorf("unbounded defer is blocked; provide --until <time> or pass --allow-unbounded")
}
