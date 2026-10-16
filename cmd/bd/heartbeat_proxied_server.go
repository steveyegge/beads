package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
	storageissueops "github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/storage/uow"
	"github.com/steveyegge/beads/internal/workapi"
	"github.com/steveyegge/beads/issueops"
)

// heartbeatProxiedOutcome carries what the render layer needs out of the
// transaction: the fully-resolved ID and the title for the feedback line.
type heartbeatProxiedOutcome struct {
	id      string
	title   string
	refusal error
}

// runHeartbeatProxiedServer routes bd heartbeat through the proxied-server
// plane, on the Heartbeater role's body (IssueUseCase.RenewLease) and its
// commit discipline (uow.RunTxScoped): a REFRESH writes only the dolt_ignored
// leases table (bd-lrgn1) and commits with the plain SQL COMMIT, so it mints
// ZERO Dolt commits; a RE-ARM (a hand-doled claim's first heartbeat) is
// recorded as a mutation and commits exactly its own tables; a refusal
// commits too (it may have deleted the caller's orphaned lease row) and is
// reported after. The exit code
// is the worker contract (workers call this every ~90s and only check rc):
// 0 = lease refreshed; nonzero = the lease is gone (wrong owner, not
// in_progress, closed, reclaimed) and the worker should stop.
func runHeartbeatProxiedServer(ctx context.Context, id string) error {
	if uowProvider == nil {
		return HandleErrorRespectJSON("proxied-server UOW provider not initialized")
	}

	res, err := uow.RunTxScoped(ctx, uowProvider, func(ctx context.Context, uw uow.UnitOfWork) (heartbeatProxiedOutcome, []string, string, error) {
		issue, _, rerr := workapi.GetIssueOrWisp(ctx, workapi.NewUOWDetailSource(uw), id)
		if errors.Is(rerr, storage.ErrNotFound) {
			return heartbeatProxiedOutcome{}, nil, "", fmt.Errorf("issue %s not found", id)
		}
		if rerr != nil {
			return heartbeatProxiedOutcome{}, nil, "", fmt.Errorf("resolving %s: %w", id, rerr)
		}
		// Wisps resolve here too and are refused by the role
		// (ErrLeaseNotHeld, which matches ErrNotClaimable: "is ephemeral").
		result, herr := uw.IssueUseCase().RenewLease(ctx, issueops.HeartbeatRequest{Actor: currentActor(), IssueID: issue.ID})
		if storageissueops.HeartbeatRefusalCommits(herr) {
			return heartbeatProxiedOutcome{refusal: fmt.Errorf("heartbeat %s: %w", issue.ID, herr)}, nil, "", nil
		}
		if herr != nil {
			return heartbeatProxiedOutcome{}, nil, "", fmt.Errorf("heartbeat %s: %w", issue.ID, herr)
		}
		tables, msg := storageissueops.LeaseRearmVersionCommit(issue.ID, result.Rearmed)
		return heartbeatProxiedOutcome{id: issue.ID, title: issue.Title}, tables.Sorted(), msg, nil
	})
	if err != nil {
		return HandleErrorRespectJSON("%v", err)
	}
	if res.refusal != nil {
		return HandleErrorRespectJSON("%v", res.refusal)
	}

	SetLastTouchedID(res.id)
	return renderHeartbeatSuccess(res.id, res.title)
}
