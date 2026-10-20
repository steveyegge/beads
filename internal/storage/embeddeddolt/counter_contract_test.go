//go:build cgo

package embeddeddolt_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/steveyegge/beads/backend/conformance"
	"github.com/steveyegge/beads/internal/types"
	publicops "github.com/steveyegge/beads/issueops"
)

// TestCounterContract runs the Counter contract against the embedded store,
// which hands back the SAME body the server-backed store does
// (internal/workapi/storecounter) and differs only in the engine underneath.
// That is what this wiring catches; it is not an independent vote on the body.
//
// One environment for the whole suite: booting an embedded engine per case
// would dominate the runtime, the ids are prefix-namespaced and every request
// is scoped to them, and the history delta needs the subtests sequential
// anyway.
func TestCounterContract(t *testing.T) {
	skipUnlessEmbeddedDolt(t)
	te := newTestEnv(t, "cnt")
	ctx := t.Context()
	fixture := newEmbeddedCounterFixture(t, te, "cnt")

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

func newEmbeddedCounterFixture(t *testing.T, te *testEnv, prefix string) conformance.CounterFixture {
	t.Helper()
	counter, err := te.store.Counter()
	if err != nil {
		t.Fatalf("Counter(): %v", err)
	}
	reader, err := te.store.IssueReader()
	if err != nil {
		t.Fatalf("IssueReader(): %v", err)
	}
	kit := newEmbeddedRoleFixtureKit(te, prefix)
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
// pins the PR #7199 review's blocker fix: the store-backed Counter must load
// the workspace's list configuration when ExcludeStatus is non-empty, not
// only when IncludeInfra is set, so BuildCountFilter's validation sees the
// workspace's own custom statuses. A leg-level test, not a conformance case,
// because CounterFixture cannot drive a workspace-configured custom status
// (counter_contract.go's CounterFixture doc) — this needs a real store with
// status.custom set.
//
// Reproduces the reviewer's CLI repro directly against the store: with
// status.custom="review,qa", an issue moved to "review", and IncludeInfra
// left unset, `--exclude-status review` must narrow the count instead of
// being refused as an unrecognized status.
func TestCounterExcludeStatusAcceptsAWorkspaceCustomStatusWithoutIncludeInfra(t *testing.T) {
	skipUnlessEmbeddedDolt(t)
	te := newTestEnv(t, "cntcs")
	ctx := t.Context()

	if err := te.store.SetConfig(ctx, "status.custom", "review,qa"); err != nil {
		t.Fatalf("SetConfig(status.custom): %v", err)
	}

	const openID = "cntcs-open"
	const reviewID = "cntcs-review"
	for _, id := range []string{openID, reviewID} {
		if err := te.store.CreateIssue(ctx, &types.Issue{
			ID: id, Title: id, Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask,
		}, "tester"); err != nil {
			t.Fatalf("CreateIssue(%s): %v", id, err)
		}
	}
	if err := te.store.UpdateIssue(ctx, reviewID, map[string]interface{}{
		"status": types.Status("review"),
	}, "tester"); err != nil {
		t.Fatalf("UpdateIssue(%s) to review: %v", reviewID, err)
	}

	counter, err := te.store.Counter()
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

// TestReaderListExcludeStatusAcceptsAWorkspaceCustomStatus is the listing's
// twin of the test above: ListRequest.ExcludeStatus shares the count's
// validation (workapi.ParseExcludeStatus) and must see the workspace's own
// custom statuses, so a custom status is accepted and narrows the listing,
// and a name in no vocabulary is refused. A leg-level test for the same
// reason: ReaderFixture cannot configure a workspace custom status.
func TestReaderListExcludeStatusAcceptsAWorkspaceCustomStatus(t *testing.T) {
	skipUnlessEmbeddedDolt(t)
	te := newTestEnv(t, "lstcs")
	ctx := t.Context()

	if err := te.store.SetConfig(ctx, "status.custom", "review,qa"); err != nil {
		t.Fatalf("SetConfig(status.custom): %v", err)
	}
	const openID, reviewID = "lstcs-open", "lstcs-review"
	for _, id := range []string{openID, reviewID} {
		if err := te.store.CreateIssue(ctx, &types.Issue{
			ID: id, Title: id, Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask,
		}, "tester"); err != nil {
			t.Fatalf("CreateIssue(%s): %v", id, err)
		}
	}
	if err := te.store.UpdateIssue(ctx, reviewID, map[string]interface{}{
		"status": types.Status("review"),
	}, "tester"); err != nil {
		t.Fatalf("UpdateIssue(%s) to review: %v", reviewID, err)
	}
	reader, err := te.store.IssueReader()
	if err != nil {
		t.Fatalf("IssueReader(): %v", err)
	}

	scope := publicops.ListRequest{IDFilter: openID + "," + reviewID, ExcludeStatus: []string{"review"}}
	page, err := reader.List(ctx, scope)
	if err != nil {
		t.Fatalf("List(ExcludeStatus=review): %v, want the custom status accepted", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != openID {
		ids := make([]string, 0, len(page.Items))
		for _, it := range page.Items {
			ids = append(ids, it.ID)
		}
		t.Fatalf("List(ExcludeStatus=review) = %v, want [%s]", ids, openID)
	}

	scope.ExcludeStatus = []string{"reveiw"}
	if _, err := reader.List(ctx, scope); !errors.Is(err, publicops.ErrValidation) {
		t.Fatalf("List(ExcludeStatus=reveiw) = %v, want ErrValidation", err)
	}
}

// TestCounterInfraTypeCountMatchesList pins GH#6258 on the store-backed
// Counter: `bd count --type <infra type>` must answer from the plane `bd list
// --type <infra type>` reads — the type's ephemeral rows — and must judge
// "infra" by the workspace's types.infra, not the built-in names. A leg-level
// test, not a conformance case, because CounterFixture cannot configure a
// workspace infra set, and it owns its environment because types.custom and
// types.infra are workspace-global.
//
// types.infra=robot drops agent from the infra set, so a builder-only fix that
// never loads the configuration fails twice here: robot stays durable-only
// (1, against a list of 2) and agent is narrowed to ephemeral rows it has none
// of (0, against a list of 1).
//
// The listing runs with AllFlag and IncludeTemplates so the comparison is the
// PLANE only; a count and a listing still differ on closed and template rows
// by design (issueops.CountRequest).
func TestCounterInfraTypeCountMatchesList(t *testing.T) {
	skipUnlessEmbeddedDolt(t)
	te := newTestEnv(t, "cntinf")
	ctx := t.Context()

	if err := te.store.SetConfig(ctx, "types.custom", "robot,agent"); err != nil {
		t.Fatalf("SetConfig(types.custom): %v", err)
	}
	if err := te.store.SetConfig(ctx, "types.infra", "robot"); err != nil {
		t.Fatalf("SetConfig(types.infra): %v", err)
	}

	const (
		wispA     = "cntinf-robot-wisp-a"
		wispB     = "cntinf-robot-wisp-b"
		retyped   = "cntinf-robot-durable"
		agentRow  = "cntinf-agent"
		robotType = types.IssueType("robot")
	)
	for _, issue := range []*types.Issue{
		{ID: wispA, Title: wispA, Status: types.StatusOpen, Priority: 2, IssueType: robotType, Ephemeral: true},
		{ID: wispB, Title: wispB, Status: types.StatusOpen, Priority: 2, IssueType: robotType, Ephemeral: true},
		{ID: retyped, Title: retyped, Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: agentRow, Title: agentRow, Status: types.StatusOpen, Priority: 2, IssueType: types.IssueType("agent")},
	} {
		if err := te.store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("CreateIssue(%s): %v", issue.ID, err)
		}
	}
	// Retyping after create keeps the row durable: create-time infra routing
	// does not run on update.
	if err := te.store.UpdateIssue(ctx, retyped, map[string]interface{}{
		"issue_type": "robot",
	}, "tester"); err != nil {
		t.Fatalf("UpdateIssue(%s) to robot: %v", retyped, err)
	}

	counter, err := te.store.Counter()
	if err != nil {
		t.Fatalf("Counter(): %v", err)
	}
	reader, err := te.store.IssueReader()
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
			if grouped.Total != got.Total {
				t.Errorf("CountByGroup.Total = %d, want the scalar %d", grouped.Total, got.Total)
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
