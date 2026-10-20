package uow

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/steveyegge/beads/backend/conformance"
	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
	publicops "github.com/steveyegge/beads/issueops"
)

// TestCounterContract runs the Counter contract against the unit-of-work
// provider — the one Counter implementation that does not hand back
// internal/workapi/storecounter. The two store backends share that body between
// them, which makes this the SECOND of two votes rather than the third.
//
// One provider for the whole suite (each newUOWRoleFixtureProvider boots a
// real Dolt sql-server) and NO t.Parallel: this backend has no per-test
// copy-on-write branch, so dolt_log and the issues table are database-global
// and a parallel subtest would corrupt another subtest's history delta.
func TestCounterContract(t *testing.T) {
	ctx := context.Background()
	fixture := newUOWCounterFixture(t, ctx, "cnt")

	t.Run("CountsTheDurablePlaneByDefault", func(t *testing.T) {
		conformance.RunCounterCountsTheDurablePlaneByDefault(t, ctx, fixture)
	})
	t.Run("IncludeInfraMergesTheWispTier", func(t *testing.T) {
		conformance.RunCounterIncludeInfraMergesTheWispTier(t, ctx, fixture)
	})
	t.Run("IncludeInfraExcludesGates", func(t *testing.T) {
		conformance.RunCounterIncludeInfraExcludesGates(t, ctx, fixture)
	})
	t.Run("CountsClosedRows", func(t *testing.T) {
		conformance.RunCounterCountsClosedRows(t, ctx, fixture)
	})
	t.Run("AnUnknownStatusMatchesNothing", func(t *testing.T) {
		conformance.RunCounterAnUnknownStatusMatchesNothing(t, ctx, fixture)
	})
	t.Run("GroupsPartitionTheScalarSet", func(t *testing.T) {
		conformance.RunCounterGroupsPartitionTheScalarSet(t, ctx, fixture)
	})
	t.Run("LabelBucketsOverlapSoTotalIsNotTheirSum", func(t *testing.T) {
		conformance.RunCounterLabelBucketsOverlapSoTotalIsNotTheirSum(t, ctx, fixture)
	})
	t.Run("NamesTheEmptyBuckets", func(t *testing.T) {
		conformance.RunCounterNamesTheEmptyBuckets(t, ctx, fixture)
	})
	t.Run("PrefixesPriorityBuckets", func(t *testing.T) {
		conformance.RunCounterPrefixesPriorityBuckets(t, ctx, fixture)
	})
	t.Run("PriorityBucketsCountZeroAndCountEveryRow", func(t *testing.T) {
		conformance.RunCounterPriorityBucketsCountZeroAndCountEveryRow(t, ctx, fixture)
	})
	t.Run("TheNoLabelBucketIsAbsentWhenEveryRowIsLabeled", func(t *testing.T) {
		conformance.RunCounterTheNoLabelBucketIsAbsentWhenEveryRowIsLabeled(t, ctx, fixture)
	})
	t.Run("TypeBucketsAreTheRawTypeNames", func(t *testing.T) {
		conformance.RunCounterTypeBucketsAreTheRawTypeNames(t, ctx, fixture)
	})
	t.Run("RefusesAnUnknownGroup", func(t *testing.T) {
		conformance.RunCounterRefusesAnUnknownGroup(t, ctx, fixture)
	})
	t.Run("NormalizesLabelsAndLeavesTheRequestAlone", func(t *testing.T) {
		conformance.RunCounterNormalizesLabelsAndLeavesTheRequestAlone(t, ctx, fixture)
	})
	t.Run("WritesNothing", func(t *testing.T) {
		conformance.RunCounterWritesNothing(t, ctx, fixture)
	})
	t.Run("ParentIDScopesToChildren", func(t *testing.T) {
		conformance.RunCounterParentIDScopesToChildren(t, ctx, fixture)
	})
	t.Run("NoParentExcludesChildren", func(t *testing.T) {
		conformance.RunCounterNoParentExcludesChildren(t, ctx, fixture)
	})
	t.Run("ExcludeTypesNarrowsThePredicate", func(t *testing.T) {
		conformance.RunCounterExcludeTypesNarrowsThePredicate(t, ctx, fixture)
	})
	t.Run("ExcludeStatusNarrowsThePredicate", func(t *testing.T) {
		conformance.RunCounterExcludeStatusNarrowsThePredicate(t, ctx, fixture)
	})
	t.Run("ParentIDMatchesListCardinality", func(t *testing.T) {
		conformance.RunCounterParentIDMatchesListCardinality(t, ctx, fixture)
	})
	t.Run("NoParentMatchesListCardinality", func(t *testing.T) {
		conformance.RunCounterNoParentMatchesListCardinality(t, ctx, fixture)
	})
	t.Run("ExcludeTypesMatchesListCardinality", func(t *testing.T) {
		conformance.RunCounterExcludeTypesMatchesListCardinality(t, ctx, fixture)
	})
	t.Run("ParentIDIncludesAWispChild", func(t *testing.T) {
		conformance.RunCounterParentIDIncludesAWispChild(t, ctx, fixture)
	})
	t.Run("ParentIDAndExcludeStatusComposeOnAClosedChild", func(t *testing.T) {
		conformance.RunCounterParentIDAndExcludeStatusComposeOnAClosedChild(t, ctx, fixture)
	})
}

func newUOWCounterFixture(t *testing.T, ctx context.Context, prefix string) conformance.CounterFixture {
	t.Helper()
	provider := newUOWRoleFixtureProvider(t, ctx, prefix)
	// Through the capability accessor, not NewCounter: a provider that stopped
	// offering the role is the regression, and a constructor call would hide it.
	source, ok := provider.(CounterSource)
	if !ok {
		t.Fatalf("provider %T does not offer the Counter accessor", provider)
	}
	counter, err := source.Counter()
	if err != nil {
		t.Fatalf("Counter(): %v", err)
	}
	readerSource, ok := provider.(IssueReaderSource)
	if !ok {
		t.Fatalf("provider %T does not offer the IssueReader accessor", provider)
	}
	reader, err := readerSource.IssueReader()
	if err != nil {
		t.Fatalf("IssueReader(): %v", err)
	}
	kit := newUOWRoleFixtureKit(provider, prefix)
	return conformance.CounterFixture{
		IssuePrefix:   kit.IssuePrefix,
		Counter:       counter,
		CreateIssue:   kit.CreateIssue,
		CreateWisp:    kit.CreateWisp,
		CountHistory:  kit.CountHistory,
		AddDependency: kit.AddDependency,
		List:          reader.List,
	}
}

// TestCounterExcludeStatusAcceptsAWorkspaceCustomStatusWithoutIncludeInfra
// pins the PR #7199 review's blocker fix on the unit-of-work leg: countFilter
// must load the workspace's list configuration when ExcludeStatus is
// non-empty, not only when IncludeInfra is set, so BuildCountFilter's
// validation sees the workspace's own custom statuses. A leg-level test, not
// a conformance case, because CounterFixture cannot drive a
// workspace-configured custom status (counter_contract.go's CounterFixture
// doc) — this needs a real provider with status.custom set.
//
// Its own provider (not newUOWCounterFixture's), so status.custom does not
// leak into the other subtests sharing that fixture's one database.
func TestCounterExcludeStatusAcceptsAWorkspaceCustomStatusWithoutIncludeInfra(t *testing.T) {
	ctx := context.Background()
	provider := newUOWRoleFixtureProvider(t, ctx, "cntcs")

	if err := RunTx(ctx, provider, func(ctx context.Context, uw UnitOfWork) (string, error) {
		return "set status.custom", uw.ConfigUseCase().SetConfig(ctx, "status.custom", "review,qa")
	}); err != nil {
		t.Fatalf("SetConfig(status.custom): %v", err)
	}

	const openID = "cntcs-open"
	const reviewID = "cntcs-review"
	for _, id := range []string{openID, reviewID} {
		id := id
		if err := RunTx(ctx, provider, func(ctx context.Context, uw UnitOfWork) (string, error) {
			_, err := uw.IssueUseCase().CreateIssue(ctx, domain.CreateIssueParams{
				Issue: &types.Issue{
					ID: id, Title: id, Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask,
				},
				ExplicitID: id,
				CreateOnly: true,
			}, "tester")
			return "seed " + id, err
		}); err != nil {
			t.Fatalf("CreateIssue(%s): %v", id, err)
		}
	}
	if err := RunTx(ctx, provider, func(ctx context.Context, uw UnitOfWork) (string, error) {
		return "move to review", uw.IssueUseCase().UpdateIssue(ctx, reviewID, map[string]any{
			"status": types.Status("review"),
		}, "tester")
	}); err != nil {
		t.Fatalf("UpdateIssue(%s) to review: %v", reviewID, err)
	}

	source, ok := provider.(CounterSource)
	if !ok {
		t.Fatalf("provider %T does not offer the Counter accessor", provider)
	}
	counter, err := source.Counter()
	if err != nil {
		t.Fatalf("Counter(): %v", err)
	}

	scope := publicops.CountRequest{IDFilter: openID + "," + reviewID}
	total, err := counter.Count(ctx, scope)
	if err != nil {
		t.Fatalf("Count(unfiltered): %v", err)
	}
	if total.Total != 2 {
		t.Fatalf("Count(unfiltered).Total = %d, want 2", total.Total)
	}

	// IncludeInfra left UNSET: before the fix this refused with "invalid
	// exclude-status \"review\"" because the config load was gated on
	// IncludeInfra alone.
	scope.ExcludeStatus = []string{"review"}
	result, err := counter.Count(ctx, scope)
	if err != nil {
		t.Fatalf("Count(exclude-status=review, IncludeInfra unset): %v, want the custom status accepted", err)
	}
	if result.Total != 1 {
		t.Fatalf("Count(exclude-status=review).Total = %d, want 1", result.Total)
	}
}

// TestCounterInfraTypeCountMatchesList is the unit-of-work twin of the
// embedded leg's test of the same name (GH#6258): countFilter — the body
// behind proxied and served counts — must load the workspace configuration
// for a named IssueType, so a type in types.infra reads the plane a listing of
// it reads, and a default infra type the workspace dropped does not.
//
// Its own provider, so types.custom and types.infra do not leak into the
// other subtests sharing newUOWCounterFixture's one database.
func TestCounterInfraTypeCountMatchesList(t *testing.T) {
	ctx := context.Background()
	provider := newUOWRoleFixtureProvider(t, ctx, "cntinf")

	for _, kv := range [][2]string{{"types.custom", "robot,agent"}, {"types.infra", "robot"}} {
		kv := kv
		if err := RunTx(ctx, provider, func(ctx context.Context, uw UnitOfWork) (string, error) {
			return "set " + kv[0], uw.ConfigUseCase().SetConfig(ctx, kv[0], kv[1])
		}); err != nil {
			t.Fatalf("SetConfig(%s): %v", kv[0], err)
		}
	}

	const (
		wispA     = "cntinf-robot-wisp-a"
		wispB     = "cntinf-robot-wisp-b"
		retyped   = "cntinf-robot-durable"
		agentRow  = "cntinf-agent"
		robotType = types.IssueType("robot")
	)
	for _, seed := range []struct {
		issue *types.Issue
		wisp  bool
	}{
		{&types.Issue{ID: wispA, Title: wispA, Status: types.StatusOpen, Priority: 2, IssueType: robotType, Ephemeral: true}, true},
		{&types.Issue{ID: wispB, Title: wispB, Status: types.StatusOpen, Priority: 2, IssueType: robotType, Ephemeral: true}, true},
		{&types.Issue{ID: retyped, Title: retyped, Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}, false},
		{&types.Issue{ID: agentRow, Title: agentRow, Status: types.StatusOpen, Priority: 2, IssueType: types.IssueType("agent")}, false},
	} {
		seed := seed
		if err := RunTx(ctx, provider, func(ctx context.Context, uw UnitOfWork) (string, error) {
			params := domain.CreateIssueParams{Issue: seed.issue, ExplicitID: seed.issue.ID, CreateOnly: true}
			var err error
			if seed.wisp {
				_, err = uw.IssueUseCase().CreateWisp(ctx, params, "tester")
			} else {
				_, err = uw.IssueUseCase().CreateIssue(ctx, params, "tester")
			}
			return "seed " + seed.issue.ID, err
		}); err != nil {
			t.Fatalf("seed %s: %v", seed.issue.ID, err)
		}
	}
	// Retyping after create keeps the row durable: create-time infra routing
	// does not run on update.
	if err := RunTx(ctx, provider, func(ctx context.Context, uw UnitOfWork) (string, error) {
		return "retype to robot", uw.IssueUseCase().UpdateIssue(ctx, retyped, map[string]any{
			"issue_type": "robot",
		}, "tester")
	}); err != nil {
		t.Fatalf("UpdateIssue(%s) to robot: %v", retyped, err)
	}

	source, ok := provider.(CounterSource)
	if !ok {
		t.Fatalf("provider %T does not offer the Counter accessor", provider)
	}
	counter, err := source.Counter()
	if err != nil {
		t.Fatalf("Counter(): %v", err)
	}
	readerSource, ok := provider.(IssueReaderSource)
	if !ok {
		t.Fatalf("provider %T does not offer the IssueReader accessor", provider)
	}
	reader, err := readerSource.IssueReader()
	if err != nil {
		t.Fatalf("IssueReader(): %v", err)
	}
	ids := strings.Join([]string{wispA, wispB, retyped, agentRow}, ",")

	for _, tt := range []struct {
		issueType                      string
		includeEphemeral, includeInfra bool
		want                           int64
	}{
		{"robot", false, false, 2},
		{"robot", true, false, 3},
		{"robot", false, true, 2},
		{"robot", true, true, 3},
		{"agent", false, false, 1},
		{"agent", true, false, 1},
		{"agent", false, true, 1},
		{"agent", true, true, 1},
	} {
		name := fmt.Sprintf("type=%s/include-ephemeral=%v/include-infra=%v",
			tt.issueType, tt.includeEphemeral, tt.includeInfra)
		t.Run(name, func(t *testing.T) {
			req := publicops.CountRequest{
				IDFilter: ids, IssueType: tt.issueType,
				IncludeEphemeral: tt.includeEphemeral, IncludeInfra: tt.includeInfra,
			}
			got, err := counter.Count(ctx, req)
			if err != nil {
				t.Fatalf("Count: %v", err)
			}
			page, err := reader.List(ctx, publicops.ListRequest{
				IDFilter: ids, IssueType: tt.issueType,
				IncludeEphemeral: tt.includeEphemeral, IncludeInfra: tt.includeInfra,
				AllFlag: true, IncludeTemplates: true,
			})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if got.Total != int64(len(page.Items)) {
				t.Errorf("Count = %d, len(List) = %d: count and list read different planes", got.Total, len(page.Items))
			}
			if got.Total != tt.want {
				t.Errorf("Count = %d, want %d", got.Total, tt.want)
			}
			grouped, err := counter.CountByGroup(ctx, publicops.CountByGroupRequest{
				Filter: req, GroupBy: publicops.CountGroupStatus,
			})
			if err != nil {
				t.Fatalf("CountByGroup: %v", err)
			}
			if grouped.Total != int64(len(page.Items)) {
				t.Errorf("CountByGroup.Total = %d, len(List) = %d", grouped.Total, len(page.Items))
			}
		})
	}

	// An untyped count stays on the durable plane: the retyped robot and the
	// agent, never the robot wisps.
	untyped, err := counter.Count(ctx, publicops.CountRequest{IDFilter: ids})
	if err != nil {
		t.Fatalf("Count(untyped): %v", err)
	}
	if untyped.Total != 2 {
		t.Errorf("Count(untyped) = %d, want 2", untyped.Total)
	}
}
