package issueops

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/steveyegge/beads/internal/storage/sqlbuild"
	"github.com/steveyegge/beads/internal/types"
	publicops "github.com/steveyegge/beads/issueops"
)

// This file pins the FORK-DEFAULT behavior of the lease-heartbeat machinery,
// which is OFF by default (gastownhall/beads#5681). Every test here runs with
// a bare context.Background() and no BD_HEARTBEAT_ENABLED, so it exercises the
// disabled path exactly as a default deployment sees it:
//
//   - claim still records assignee/status but arms NO lease,
//   - `bd heartbeat` is a successful no-op (workers' timer loops keep exiting 0),
//   - `bd reclaim` reverts nothing.
//
// The ENABLED-path contracts live in public_claim_test.go, lease_reclaimer_test.go,
// and the backend/conformance suite, all of which opt back in explicitly.

// TestHeartbeatDisabledClaimArmsNoLease: with the feature off, a winning claim
// CAS must NOT touch the ephemeral leases table. The mock expects the issues
// UPDATE but no INSERT INTO leases — any lease write would fail
// ExpectationsWereMet.
func TestHeartbeatDisabledClaimArmsNoLease(t *testing.T) {
	_, mock, tx := beginMockTx(t)

	const id = "bd-1"
	expectClaimableRead(mock, id, "", types.StatusOpen)
	mock.ExpectExec(`(?s)UPDATE issues\s+SET assignee`).WillReturnResult(sqlmock.NewResult(0, 1))
	// Deliberately NO lease INSERT expectation here.
	mock.ExpectQuery(`(?s)SELECT id FROM events`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`(?s)INSERT INTO events`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + IssueSelectColumns + " FROM issues " + sqlbuild.LeaseJoin("issues") + " WHERE id = ?")).
		WithArgs(id).
		WillReturnRows(claimIssueRow(id, claimActor, types.StatusInProgress))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT label FROM labels WHERE issue_id = ? ORDER BY label")).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"label"}))

	result, tables, err := ExecuteClaim(context.Background(), tx, publicops.ClaimRequest{Actor: claimActor, IssueID: id})
	if err != nil {
		t.Fatalf("ExecuteClaim: %v", err)
	}
	if !result.Changed {
		t.Error("a won CAS reported no persisted mutation")
	}
	if !tables["issues"] || !tables["events"] {
		t.Errorf("staged %v, want the issues row and the claim event", tables)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("a disabled claim still touched the database unexpectedly: %v", err)
	}
}

// TestHeartbeatDisabledHeartbeatIsSuccessfulNoOp: with the feature off,
// HeartbeatIssueInTx must return nil WITHOUT issuing any SQL. This is the
// exit-0 contract workers' heartbeat timer loops depend on. The mock expects
// nothing, so any query fails the test.
func TestHeartbeatDisabledHeartbeatIsSuccessfulNoOp(t *testing.T) {
	_, mock, tx := beginMockTx(t)

	if err := HeartbeatIssueInTx(context.Background(), tx, "bd-1", claimActor); err != nil {
		t.Fatalf("a disabled heartbeat must succeed as a no-op, got: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("a disabled heartbeat issued SQL: %v", err)
	}
}

// TestHeartbeatDisabledReclaimRevertsNothing: with the feature off,
// ReclaimExpiredLeasesInTx returns an empty result WITHOUT issuing any SQL, so
// `bd reclaim` on a supervisor timer exits 0 having reverted nothing.
func TestHeartbeatDisabledReclaimRevertsNothing(t *testing.T) {
	_, mock, tx := beginMockTx(t)

	cutoff := time.Now().UTC()
	got, err := ReclaimExpiredLeasesInTx(context.Background(), tx, cutoff, types.ReclaimFilter{}, "reaper")
	if err != nil {
		t.Fatalf("a disabled reclaim must succeed with an empty result, got: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a disabled reclaim reverted %d issues, want 0", len(got))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("a disabled reclaim issued SQL: %v", err)
	}
}

// TestHeartbeatEnabledOverrideBeatsDisabledConfig: the per-call override
// (WithHeartbeatEnabled) must win over the disabled default, so a deployment
// that opts back in — and the ENABLED-path tests — get real lease machinery.
// The mock expects the lease INSERT that only the enabled path performs.
func TestHeartbeatEnabledOverrideBeatsDisabledConfig(t *testing.T) {
	_, mock, tx := beginMockTx(t)

	const id = "bd-1"
	expectClaimableRead(mock, id, "", types.StatusOpen)
	mock.ExpectExec(`(?s)UPDATE issues\s+SET assignee`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO leases`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT id FROM events`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`(?s)INSERT INTO events`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + IssueSelectColumns + " FROM issues " + sqlbuild.LeaseJoin("issues") + " WHERE id = ?")).
		WithArgs(id).
		WillReturnRows(claimIssueRow(id, claimActor, types.StatusInProgress))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT label FROM labels WHERE issue_id = ? ORDER BY label")).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"label"}))

	ctx := WithHeartbeatEnabled(context.Background(), true)
	_, _, err := ExecuteClaim(ctx, tx, publicops.ClaimRequest{Actor: claimActor, IssueID: id})
	if err != nil {
		t.Fatalf("ExecuteClaim with override: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("the enabled override did not arm a lease as expected: %v", err)
	}
}

// TestHeartbeatDisabledConflictCopyTellsTheTruth: with the feature off (this
// fork's default), a claim refusal on an OPEN issue held by someone else must
// NOT promise lease expiry - no lease was armed at claim time, so nothing will
// ever surface it for bd reclaim. The copy is composed by
// storage.ClaimHolderSteeringRefusal, shared with the domain-stack twin, so
// this pins both producers.
func TestHeartbeatDisabledConflictCopyTellsTheTruth(t *testing.T) {
	_, mock, tx := beginMockTx(t)

	const id = "bd-1"
	// "held by another actor": the pre-image says the CAS cannot win, so no
	// UPDATE is attempted (ga-v2k49) - only the claimable read and the two
	// state re-reads the refusal carries.
	expectClaimableRead(mock, id, "bob", types.StatusOpen)
	expectClaimStateRead(mock, id, "bob", types.StatusOpen)
	expectClaimStateRead(mock, id, "bob", types.StatusOpen)

	_, _, err := ExecuteClaim(context.Background(), tx, publicops.ClaimRequest{Actor: claimActor, IssueID: id})
	var conflict *publicops.ClaimConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a *ClaimConflictError", err)
	}
	msg := conflict.Err.Error()
	if !strings.Contains(msg, "heartbeat.enabled=false") {
		t.Errorf("refusal = %q, want it to name the disabled flag", msg)
	}
	for _, banned := range []string{"lease expiry", "reclaim"} {
		if strings.Contains(msg, banned) {
			t.Errorf("refusal = %q, must not promise %q while no lease is armed", msg, banned)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

// TestHeartbeatEnabledConflictCopyPromisesLeaseRecovery: the same refusal with
// the machinery opted back in keeps the lease-expiry sentence - an armed lease
// really will go stale and surface the claim for bd reclaim.
func TestHeartbeatEnabledConflictCopyPromisesLeaseRecovery(t *testing.T) {
	_, mock, tx := beginMockTx(t)

	const id = "bd-1"
	expectClaimableRead(mock, id, "bob", types.StatusOpen)
	expectClaimStateRead(mock, id, "bob", types.StatusOpen)
	expectClaimStateRead(mock, id, "bob", types.StatusOpen)

	ctx := WithHeartbeatEnabled(context.Background(), true)
	_, _, err := ExecuteClaim(ctx, tx, publicops.ClaimRequest{Actor: claimActor, IssueID: id})
	var conflict *publicops.ClaimConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a *ClaimConflictError", err)
	}
	if msg := conflict.Err.Error(); !strings.Contains(msg, "lease expiry will surface it for bd reclaim") {
		t.Errorf("refusal = %q, want the lease-expiry recovery sentence", msg)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
