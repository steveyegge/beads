package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/steveyegge/beads/internal/metrics"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/uow"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/workapi"
	"github.com/steveyegge/beads/issueops"
)

var heartbeatCmd = &cobra.Command{
	Use:     "heartbeat <id>",
	Aliases: []string{"hb"},
	GroupID: "issues",
	Short:   "Refresh the lease on an issue you hold in_progress",
	Long: `Refresh the lease on an issue you currently hold in_progress.

A claim carries a lease that expires after a TTL. A worker keeps its claim alive
by heartbeating faster than the TTL; once it stops (because it died), the lease
goes stale and 'bd reclaim' reverts the issue to ready so another worker can pick
it up. Heartbeat pushes lease_expires_at forward and stamps heartbeat_at = now.

Only the current owner may heartbeat. If the lease has already been reclaimed or
the issue closed, heartbeat fails so the worker learns to stop.

Leases live in an ephemeral, node-local table: a heartbeat that refreshes your
lease writes no Dolt commit and no history, so any cadence comfortably below
the TTL is fine. Only a re-arm (the first heartbeat of a claim that has no
lease, such as one set by a plain update) is recorded as a change to the issue
and committed, once. Leases are only enforceable on the node that granted them;
cross-machine claim visibility rides the issue's status and assignee, which do
commit.

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

		// One role on every route: the store's own
		// Heartbeater accessor on the direct routes — dolt, embedded dolt and
		// a registered HTTP backend, where it dials issues.heartbeat and an
		// older server is refused before the dial naming the token — and the
		// proxied-server provider's on the proxied one. On every leg a refresh
		// writes ONLY the lease row (no revision remint, no history, no Dolt
		// commit); a re-arm is recorded as a mutation and committed once.
		if usesProxiedServer() {
			return runHeartbeatProxied(ctx, id)
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
		heartbeater, err := issueStore.Heartbeater()
		if err != nil {
			return HandleErrorRespectJSON("heartbeat %s: %v", result.ResolvedID, err)
		}
		if _, err := heartbeater.Heartbeat(ctx, issueops.HeartbeatRequest{
			Actor:   currentActor(),
			IssueID: result.ResolvedID,
		}); err != nil {
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

// runHeartbeatProxied is bd heartbeat on the proxied-server plane: the id is
// resolved exactly, on either plane, in a read unit of work of its own, and
// the refresh goes through the provider's OWN Heartbeater accessor (the
// capability the uow leg commits with RunTxScoped: a refresh names no tables
// and takes its SQL-only form, so it mints exactly ZERO Dolt commits,
// bd-lrgn1; a re-arm mints exactly one). Nothing here sets commandDidWrite.
// The exit code is the worker contract: 0 = lease refreshed; nonzero = the
// lease is gone and the worker should stop.
func runHeartbeatProxied(ctx context.Context, id string) error {
	issue, err := proxiedHeartbeatTarget(ctx, id)
	if err != nil {
		return err
	}
	heartbeater, err := proxiedHeartbeater()
	if err != nil {
		return HandleErrorRespectJSON("heartbeat %s: %v", issue.ID, err)
	}
	if _, err := heartbeater.Heartbeat(ctx, issueops.HeartbeatRequest{
		Actor:   currentActor(),
		IssueID: issue.ID,
	}); err != nil {
		return HandleErrorRespectJSON("heartbeat %s: %v", issue.ID, err)
	}
	SetLastTouchedID(issue.ID)
	return renderHeartbeatSuccess(issue.ID, issue.Title)
}

// proxiedHeartbeatTarget reads the row a proxied heartbeat names, on either
// plane (a wisp resolves here and the role refuses it as ephemeral), for its
// canonical id and the title the feedback line prints.
func proxiedHeartbeatTarget(ctx context.Context, id string) (*types.Issue, error) {
	uw, err := proxiedOpenReadUOW(ctx)
	if err != nil {
		return nil, err
	}
	defer uw.Close(ctx)
	issue, _, err := workapi.GetIssueOrWisp(ctx, workapi.NewUOWDetailSource(uw), id)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, HandleErrorRespectJSON("issue %s not found", id)
	}
	if err != nil {
		return nil, HandleErrorRespectJSON("resolving %s: %v", id, err)
	}
	return issue, nil
}

// proxiedHeartbeater takes the lease refresh through the proxied-server
// provider's own capability accessor, proxiedLeaseReclaimer's two-step and for
// its reason: the accessor is where each layer is added.
func proxiedHeartbeater() (issueops.Heartbeater, error) {
	if uowProvider == nil {
		return nil, errors.New("proxied-server UOW provider not initialized")
	}
	src, ok := uowProvider.(uow.HeartbeaterSource)
	if !ok {
		return nil, fmt.Errorf("proxied-server provider %T does not offer the lease-refresh surface", uowProvider)
	}
	return src.Heartbeater()
}

// renderHeartbeatSuccess prints the success shape every storage route shares —
// the direct path and the proxied-server path (runHeartbeatProxied) call
// exactly this code, so the --json contract workers parse
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
