package uow

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/steveyegge/beads/backend/conformance"
	"github.com/steveyegge/beads/internal/storage"
	publicops "github.com/steveyegge/beads/issueops"
)

// TestHeartbeaterContract runs the Heartbeater contract against the
// unit-of-work provider, which reaches internal/storage/issueops.
// ExecuteHeartbeatInTx through domain.IssueUseCase.RenewLease inside one unit
// of work committed with RunTxScoped: the SQL-only form for a refresh, the
// scoped commit for a re-arm.
func TestHeartbeaterContract(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture := newUOWHeartbeaterFixture(t, ctx, "hbc")
	t.Run("RefreshesTheHoldersLease", func(t *testing.T) {
		conformance.RunHeartbeaterRefreshesTheHoldersLease(t, ctx, fixture)
	})
	t.Run("ChangesNothingOnTheRow", func(t *testing.T) {
		conformance.RunHeartbeaterChangesNothingOnTheRow(t, ctx, fixture)
	})
	t.Run("AGuardedWriteWithThePreHeartbeatRevisionStillWins", func(t *testing.T) {
		conformance.RunHeartbeaterAGuardedWriteWithThePreHeartbeatRevisionStillWins(t, ctx, fixture)
	})
	t.Run("RefusesAForeignHolder", func(t *testing.T) {
		conformance.RunHeartbeaterRefusesAForeignHolder(t, ctx, fixture)
	})
	t.Run("RefusesAfterClose", func(t *testing.T) {
		conformance.RunHeartbeaterRefusesAfterClose(t, ctx, fixture)
	})
	t.Run("RefusesAfterRelease", func(t *testing.T) {
		conformance.RunHeartbeaterRefusesAfterRelease(t, ctx, fixture)
	})
	t.Run("RefusesAfterReclaim", func(t *testing.T) {
		conformance.RunHeartbeaterRefusesAfterReclaim(t, ctx, fixture)
	})
	t.Run("RefusesAfterStatusMovedOffInProgress", func(t *testing.T) {
		conformance.RunHeartbeaterRefusesAfterStatusMovedOffInProgress(t, ctx, fixture)
	})
	t.Run("RefusesAWisp", func(t *testing.T) {
		conformance.RunHeartbeaterRefusesAWisp(t, ctx, fixture)
	})
	t.Run("RearmsAHandDoledClaim", func(t *testing.T) {
		conformance.RunHeartbeaterRearmsAHandDoledClaim(t, ctx, fixture)
	})
	t.Run("AnswersNotFoundForAnAbsentID", func(t *testing.T) {
		conformance.RunHeartbeaterAnswersNotFoundForAnAbsentID(t, ctx, fixture)
	})
	t.Run("RefusesABlankOrOverLongActor", func(t *testing.T) {
		conformance.RunHeartbeaterRefusesABlankOrOverLongActor(t, ctx, fixture)
	})
	t.Run("DoesNotMutateTheRequest", func(t *testing.T) {
		conformance.RunHeartbeaterDoesNotMutateTheRequest(t, ctx, fixture)
	})
	t.Run("CommitsOnlyItsOwnTables", func(t *testing.T) {
		conformance.RunHeartbeaterCommitsOnlyItsOwnTables(t, ctx, fixture)
	})
	t.Run("ASameSecondHeartbeatIsNotARearm", func(t *testing.T) {
		conformance.RunHeartbeaterASameSecondHeartbeatIsNotARearm(t, ctx, fixture)
	})
	t.Run("RefusesAnOrphanedLeaseOnAReleasedIssue", func(t *testing.T) {
		conformance.RunHeartbeaterRefusesAnOrphanedLeaseOnAReleasedIssue(t, ctx, fixture)
	})
	t.Run("RefusesAnOrphanedLeaseOnAClosedIssue", func(t *testing.T) {
		conformance.RunHeartbeaterRefusesAnOrphanedLeaseOnAClosedIssue(t, ctx, fixture)
	})
	t.Run("RefusesAnOrphanedLeaseOnAnIssueSomeoneElseHolds", func(t *testing.T) {
		conformance.RunHeartbeaterRefusesAnOrphanedLeaseOnAnIssueSomeoneElseHolds(t, ctx, fixture)
	})
}

// TestHeartbeaterRefusesBeforeOpeningAUOW: a malformed heartbeat is a
// deterministic validation failure and must not cost a database connection to
// discover, as on the dolt and embedded-dolt legs.
func TestHeartbeaterRefusesBeforeOpeningAUOW(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request publicops.HeartbeatRequest
	}{
		{"no actor", publicops.HeartbeatRequest{IssueID: "x"}},
		{"no id", publicops.HeartbeatRequest{Actor: "worker"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &mockUnitOfWorkProvider{newUOWErr: errors.New("unexpected unit-of-work open")}
			heartbeater, err := NewHeartbeater(provider)
			if err != nil {
				t.Fatalf("NewHeartbeater: %v", err)
			}
			if _, err := heartbeater.Heartbeat(context.Background(), tc.request); !errors.Is(err, publicops.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			if provider.newUOWCalls != 0 {
				t.Errorf("an invalid request opened %d units of work", provider.newUOWCalls)
			}
		})
	}
}

// newUOWHeartbeaterFixture takes every role through the provider's own
// capability accessors, and the hooked heartbeater off the notifying provider
// the proxied CLI route wraps around its provider.
func newUOWHeartbeaterFixture(t *testing.T, ctx context.Context, prefix string) conformance.HeartbeaterFixture {
	t.Helper()
	provider := newUOWRoleFixtureProvider(t, ctx, prefix)
	source, ok := provider.(HeartbeaterSource)
	if !ok {
		t.Fatalf("provider %T does not offer the Heartbeater accessor", provider)
	}
	heartbeater, err := source.Heartbeater()
	if err != nil {
		t.Fatalf("Heartbeater(): %v", err)
	}
	claimer, err := provider.(IssueClaimerSource).IssueClaimer()
	if err != nil {
		t.Fatalf("IssueClaimer(): %v", err)
	}
	lifecycle, err := provider.(IssueLifecycleSource).IssueLifecycle()
	if err != nil {
		t.Fatalf("IssueLifecycle(): %v", err)
	}
	releaser, err := provider.(ReleaserSource).Releaser()
	if err != nil {
		t.Fatalf("Releaser(): %v", err)
	}
	reclaimer, err := provider.(LeaseReclaimerSource).LeaseReclaimer()
	if err != nil {
		t.Fatalf("LeaseReclaimer(): %v", err)
	}
	configurer, ok := provider.(storage.EventsJournalConfigurer)
	if !ok {
		t.Fatalf("provider %T does not implement storage.EventsJournalConfigurer", provider)
	}
	versionConfigurer, ok := provider.(storage.VersionedHistoryConfigurer)
	if !ok {
		t.Fatalf("provider %T does not implement storage.VersionedHistoryConfigurer", provider)
	}
	runner, fired := conformance.NewUpdateHookLog(t)
	hooked := heartbeater
	if runner != nil {
		notifying, ok := NewNotifyingProvider(provider, Sinks{Hook: runner}).(HeartbeaterSource)
		if !ok {
			t.Fatal("the notifying provider does not offer the Heartbeater accessor")
		}
		if hooked, err = notifying.Heartbeater(); err != nil {
			t.Fatalf("hooked Heartbeater(): %v", err)
		}
	}
	kit := newUOWRoleFixtureKit(provider, prefix)
	return conformance.HeartbeaterFixture{
		IssuePrefix:    kit.IssuePrefix,
		Heartbeater:    heartbeater,
		CreateIssue:    kit.CreateIssue,
		CreateWisp:     kit.CreateWisp,
		Claimer:        claimer,
		Lifecycle:      lifecycle,
		Releaser:       releaser,
		LeaseReclaimer: reclaimer,
		QueryScalar:    kit.QueryScalar,
		CountHistory:   kit.CountHistory,
		ExecUncommitted: func(ctx context.Context, statements []conformance.SQLStatement) error {
			_, err := RunTxEphemeral(ctx, provider, func(ctx context.Context, uw UnitOfWork) (struct{}, error) {
				for _, stmt := range statements {
					if _, err := uw.RawSQLUseCase().Exec(ctx, stmt.Query, stmt.Args...); err != nil {
						return struct{}{}, fmt.Errorf("%s: %w", stmt.Query, err)
					}
				}
				return struct{}{}, nil
			})
			return err
		},
		Exec: func(ctx context.Context, statements []conformance.SQLStatement) error {
			return RunTx(ctx, provider, func(ctx context.Context, uw UnitOfWork) (string, error) {
				for _, stmt := range statements {
					if _, err := uw.RawSQLUseCase().Exec(ctx, stmt.Query, stmt.Args...); err != nil {
						return "", fmt.Errorf("%s: %w", stmt.Query, err)
					}
				}
				return "seed heartbeater rows", nil
			})
		},
		SetJournalEnabled:          configurer.SetEventsJournalEnabled,
		SetVersionedHistoryEnabled: versionConfigurer.SetVersionedHistoryEnabled,
		HookedHeartbeater:          hooked,
		FiredUpdates:               fired,
	}
}
