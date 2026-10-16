//go:build cgo

package httpclient

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/steveyegge/beads/backend/conformance"
	"github.com/steveyegge/beads/internal/httpapi/apigen"
	"github.com/steveyegge/beads/internal/httpclient/wire"
	"github.com/steveyegge/beads/issueops"
)

// The Heartbeater contract, run against a real bd serve through
// POST /v0/beads/issues/{id}:heartbeat .
//
// THE SUBJECT IS THE SERVED OPERATION, NOT YET THIS CLIENT'S ACCESSOR. The
// client leg (httpclient.Store.Heartbeater, its preflight, local validation
// and problem mapping) lands in the next commit; until then Store's
// accessor is the generated refusing shell and Heartbeater sits on
// completeness_test's unsupported allowlist. servedWireHeartbeater below is the
// thinnest dial that lets the contract reach the server: it builds the
// documented request, sends it through this store's own dispatch (so the
// capability preflight and the wire's problem mapping are the real ones), and
// rebuilds the role's typed errors from the documented members. The next commit replaces
// it with env.subject.Heartbeater() and deletes it.
//
// Seeding and every precondition (the claim, the close, the release, the
// reclaim, the status move) go through the REFERENCE store's own roles, never
// the wire, per the harness's binding rule; the post-state is read raw.
//
// NO HOOK CASE YET: the served route's hook is the CLIENT's decorator around
// the client's accessor (served_lease_reclaimer_test.go's shape), which is the
// next commit's accessor. HookedHeartbeater is left nil, so that one
// assertion skips loudly; the next commit wires it.
func newServedHeartbeaterFixture(t *testing.T, prefix string) conformance.HeartbeaterFixture {
	t.Helper()
	env := newServedEnv(t, prefix)
	ref := env.reference
	claimer, err := ref.IssueClaimer()
	if err != nil {
		t.Fatalf("IssueClaimer(): %v", err)
	}
	lifecycle, err := ref.IssueLifecycle()
	if err != nil {
		t.Fatalf("IssueLifecycle(): %v", err)
	}
	releaser, err := ref.Releaser()
	if err != nil {
		t.Fatalf("Releaser(): %v", err)
	}
	reclaimer, err := ref.LeaseReclaimer()
	if err != nil {
		t.Fatalf("LeaseReclaimer(): %v", err)
	}
	return conformance.HeartbeaterFixture{
		IssuePrefix:                env.prefix,
		Heartbeater:                servedWireHeartbeater{store: env.subject},
		CreateIssue:                env.createIssue,
		CreateWisp:                 env.createWisp,
		Claimer:                    claimer,
		Lifecycle:                  lifecycle,
		Releaser:                   releaser,
		LeaseReclaimer:             reclaimer,
		Exec:                       env.exec,
		QueryScalar:                env.queryScalar,
		CountHistory:               env.countHistory,
		SetJournalEnabled:          ref.SetEventsJournalEnabled,
		SetVersionedHistoryEnabled: ref.SetVersionedHistoryEnabled,
	}
}

// servedWireHeartbeater dials issues.heartbeat for the contract above. It is
// test scaffolding until the client accessor lands; see newServedHeartbeaterFixture.
type servedWireHeartbeater struct{ store *Store }

func (h servedWireHeartbeater) Heartbeat(ctx context.Context, req issueops.HeartbeatRequest) (issueops.HeartbeatResult, error) {
	// The one rule the wire cannot carry: the id is a PATH segment, and an
	// empty one names the collection rather than an issue. Every other rule
	// (the actor's) is the server's to refuse.
	if req.IssueID == "" {
		return issueops.HeartbeatResult{}, &issueops.HeartbeatFieldError{
			Field: issueops.HeartbeatFieldIssueID, Detail: "validation failed: heartbeat requires an issue id",
		}
	}
	path, err := wire.IssueMethodPath(req.IssueID, wire.MethodHeartbeat)
	if err != nil {
		return issueops.HeartbeatResult{}, err
	}
	var out apigen.HeartbeatIssueResponse
	if err := h.store.dispatch(ctx, wire.Request{
		Op:      wire.OpHeartbeatIssue,
		Method:  http.MethodPost,
		Path:    path,
		Body:    apigen.HeartbeatIssueRequest{Actor: req.Actor},
		IssueID: req.IssueID,
		Actor:   req.Actor,
	}, &out); err != nil {
		return issueops.HeartbeatResult{}, servedHeartbeatError(err)
	}
	return issueops.HeartbeatResult{
		IssueID:        out.Id,
		Holder:         out.Holder,
		HeartbeatAt:    out.HeartbeatAt,
		LeaseExpiresAt: out.LeaseExpiresAt,
		Rearmed:        out.Rearmed,
	}, nil
}

// servedHeartbeatError rebuilds the role's typed refusals from the documented
// problem members. `lease_not_held` and `not_found` already arrive typed from
// the wire's code table; `already_claimed` arrives as the claim's conflict
// shape and becomes the heartbeat's, and a 400 naming `actor` becomes the
// role's field error.
func servedHeartbeatError(err error) error {
	var claim *issueops.ClaimConflictError
	if errors.As(err, &claim) && errors.Is(err, issueops.ErrAlreadyClaimed) {
		return &issueops.LeaseConflictError{
			IssueID: claim.IssueID, Holder: claim.Assignee, Status: claim.Status, Err: issueops.ErrAlreadyClaimed,
		}
	}
	var problem *wire.ProblemError
	if errors.As(err, &problem) && problem.Code == "invalid_argument" && problem.Param == "actor" {
		return &issueops.HeartbeatFieldError{Field: issueops.HeartbeatFieldActor, Detail: problem.Detail}
	}
	return err
}

func TestServedHeartbeaterRefreshesTheHoldersLease(t *testing.T) {
	conformance.RunHeartbeaterRefreshesTheHoldersLease(t, t.Context(), newServedHeartbeaterFixture(t, "hhb00"))
}

func TestServedHeartbeaterChangesNothingOnTheRow(t *testing.T) {
	conformance.RunHeartbeaterChangesNothingOnTheRow(t, t.Context(), newServedHeartbeaterFixture(t, "hhb01"))
}

func TestServedHeartbeaterAGuardedWriteWithThePreHeartbeatRevisionStillWins(t *testing.T) {
	conformance.RunHeartbeaterAGuardedWriteWithThePreHeartbeatRevisionStillWins(t, t.Context(), newServedHeartbeaterFixture(t, "hhb02"))
}

func TestServedHeartbeaterRefusesAForeignHolder(t *testing.T) {
	conformance.RunHeartbeaterRefusesAForeignHolder(t, t.Context(), newServedHeartbeaterFixture(t, "hhb03"))
}

func TestServedHeartbeaterRefusesAfterClose(t *testing.T) {
	conformance.RunHeartbeaterRefusesAfterClose(t, t.Context(), newServedHeartbeaterFixture(t, "hhb04"))
}

func TestServedHeartbeaterRefusesAfterRelease(t *testing.T) {
	conformance.RunHeartbeaterRefusesAfterRelease(t, t.Context(), newServedHeartbeaterFixture(t, "hhb05"))
}

func TestServedHeartbeaterRefusesAfterReclaim(t *testing.T) {
	conformance.RunHeartbeaterRefusesAfterReclaim(t, t.Context(), newServedHeartbeaterFixture(t, "hhb06"))
}

func TestServedHeartbeaterRefusesAfterStatusMovedOffInProgress(t *testing.T) {
	conformance.RunHeartbeaterRefusesAfterStatusMovedOffInProgress(t, t.Context(), newServedHeartbeaterFixture(t, "hhb07"))
}

func TestServedHeartbeaterRefusesAWisp(t *testing.T) {
	conformance.RunHeartbeaterRefusesAWisp(t, t.Context(), newServedHeartbeaterFixture(t, "hhb08"))
}

func TestServedHeartbeaterRearmsAHandDoledClaim(t *testing.T) {
	conformance.RunHeartbeaterRearmsAHandDoledClaim(t, t.Context(), newServedHeartbeaterFixture(t, "hhb09"))
}

func TestServedHeartbeaterAnswersNotFoundForAnAbsentID(t *testing.T) {
	conformance.RunHeartbeaterAnswersNotFoundForAnAbsentID(t, t.Context(), newServedHeartbeaterFixture(t, "hhb10"))
}

func TestServedHeartbeaterRefusesABlankOrOverLongActor(t *testing.T) {
	conformance.RunHeartbeaterRefusesABlankOrOverLongActor(t, t.Context(), newServedHeartbeaterFixture(t, "hhb11"))
}

func TestServedHeartbeaterDoesNotMutateTheRequest(t *testing.T) {
	conformance.RunHeartbeaterDoesNotMutateTheRequest(t, t.Context(), newServedHeartbeaterFixture(t, "hhb12"))
}

func TestServedHeartbeaterASameSecondHeartbeatIsNotARearm(t *testing.T) {
	conformance.RunHeartbeaterASameSecondHeartbeatIsNotARearm(t, t.Context(), newServedHeartbeaterFixture(t, "hhb13"))
}

func TestServedHeartbeaterRefusesAnOrphanedLeaseOnAReleasedIssue(t *testing.T) {
	conformance.RunHeartbeaterRefusesAnOrphanedLeaseOnAReleasedIssue(t, t.Context(), newServedHeartbeaterFixture(t, "hhb14"))
}

func TestServedHeartbeaterRefusesAnOrphanedLeaseOnAClosedIssue(t *testing.T) {
	conformance.RunHeartbeaterRefusesAnOrphanedLeaseOnAClosedIssue(t, t.Context(), newServedHeartbeaterFixture(t, "hhb15"))
}

func TestServedHeartbeaterRefusesAnOrphanedLeaseOnAnIssueSomeoneElseHolds(t *testing.T) {
	conformance.RunHeartbeaterRefusesAnOrphanedLeaseOnAnIssueSomeoneElseHolds(t, t.Context(), newServedHeartbeaterFixture(t, "hhb16"))
}

func TestServedHeartbeaterCommitsOnlyItsOwnTables(t *testing.T) {
	conformance.RunHeartbeaterCommitsOnlyItsOwnTables(t, t.Context(), newServedHeartbeaterFixture(t, "hhb17"))
}
