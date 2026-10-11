package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/steveyegge/beads/internal/metrics"
	storeops "github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/ui"
)

var heartbeatCmd = &cobra.Command{
	Use:     "heartbeat <id>",
	Aliases: []string{"hb"},
	GroupID: "issues",
	Short:   "Refresh the lease on an issue you hold in_progress",
	Long: `Refresh the lease on an issue you currently hold in_progress.

This fork ships with the lease machinery OFF (heartbeat.enabled=false): claims
carry no lease, and this command is a successful no-op that keeps worker
heartbeat loops from breaking. Set heartbeat.enabled true in .beads/config.yaml
(or BD_HEARTBEAT_ENABLED=1) to restore lease semantics; the description below
then applies in full.

A claim carries a lease that expires after a TTL. A worker keeps its claim alive
by heartbeating faster than the TTL; once it stops (because it died), the lease
goes stale and 'bd reclaim' reverts the issue to ready so another worker can pick
it up. Heartbeat pushes lease_expires_at forward and stamps heartbeat_at = now.

Only the current owner may heartbeat. If the lease has already been reclaimed or
the issue closed, heartbeat fails so the worker learns to stop.

Leases live in an ephemeral, node-local table: heartbeats write no Dolt commit
and no history, so any cadence comfortably below the TTL is fine. Leases are
only enforceable on the node that granted them; cross-machine claim visibility
rides the issue's status and assignee, which do commit.

Examples:
  bd heartbeat bd-123
  bd hb bd-123`,
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		CheckReadonly("heartbeat")

		evt := metrics.NewCommandEvent("heartbeat")
		defer func() {
			if c := metrics.Global(); c != nil {
				c.CloseEventAndAdd(evt)
			}
		}()

		ctx := rootCtx
		id := args[0]

		// Fork default (heartbeat.enabled=false, gastownhall/beads#5681):
		// heartbeat is an exit-0 no-op. The gate sits BEFORE the proxied
		// dispatch so classic and proxied routes alike short-circuit without
		// opening a store or contacting a server; the storage layer
		// (issueops.HeartbeatIssueInTx) independently refuses to arm or
		// refresh a lease, so a server with the feature off no-ops too.
		if !storeops.HeartbeatEnabled(ctx) {
			return renderHeartbeatDisabled(id)
		}

		if usesProxiedServer() {
			return runHeartbeatProxiedServer(ctx, id)
		}

		result, err := resolveAndGetIssueForMutation(ctx, store, id)
		if err != nil {
			if result != nil {
				result.Close()
			}
			return HandleErrorRespectJSON("resolving %s: %v", id, err)
		}
		if result == nil || result.Issue == nil {
			if result != nil {
				result.Close()
			}
			return HandleErrorRespectJSON("issue %s not found", id)
		}
		defer result.Close()

		issueStore := result.Store
		if err := issueStore.HeartbeatIssue(ctx, result.ResolvedID, currentActor()); err != nil {
			return HandleErrorRespectJSON("heartbeat %s: %v", result.ResolvedID, err)
		}

		if err := commitPendingIfEmbedded(ctx, issueStore, currentActor(), doltAutoCommitParams{
			Command:  "heartbeat",
			IssueIDs: []string{result.ResolvedID},
		}); err != nil {
			return HandleErrorRespectJSON("failed to commit: %v", err)
		}

		SetLastTouchedID(result.ResolvedID)

		return renderHeartbeatSuccess(result.ResolvedID, result.Issue.Title)
	},
}

// renderHeartbeatDisabled is bd heartbeat with heartbeat.enabled=false (this
// fork's default, gastownhall/beads#5681): exit 0, and the SAME --json shape
// as renderHeartbeatSuccess so a worker loop that heartbeats on a timer keeps
// parsing its output. Only the human line differs, and it says why.
func renderHeartbeatDisabled(id string) error {
	if jsonOutput {
		return outputJSON(map[string]string{
			"id":     id,
			"status": "heartbeat",
			"owner":  currentActor(),
		})
	}
	fmt.Printf("%s Heartbeat %s skipped (heartbeat.enabled=false)\n", ui.RenderSkipIcon(), formatFeedbackID(id, ""))
	return nil
}

// renderHeartbeatSuccess prints the success shape both storage routes share —
// the classic path and the proxied-server path (heartbeat_proxied_server.go)
// call exactly this code, so the --json contract workers parse
// ({"id","status":"heartbeat","owner"}) cannot drift between modes.
func renderHeartbeatSuccess(id, title string) error {
	if jsonOutput {
		return outputJSON(map[string]string{
			"id":     id,
			"status": "heartbeat",
			"owner":  currentActor(),
		})
	}
	fmt.Printf("%s Heartbeat %s (lease refreshed)\n", ui.RenderPass("✓"), formatFeedbackID(id, title))
	return nil
}

func init() {
	heartbeatCmd.ValidArgsFunction = issueIDCompletion
	rootCmd.AddCommand(heartbeatCmd)
}
