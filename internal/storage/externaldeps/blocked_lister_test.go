package externaldeps

import (
	"reflect"
	"slices"
	"testing"

	"github.com/steveyegge/beads/internal/storage/uow"
	"github.com/steveyegge/beads/internal/types"
	publicops "github.com/steveyegge/beads/issueops"
)

// TestWrapUOWProviderBlockedListerNamesExternalBlockers pins the blocked
// listing on the composition every `bd serve` answers from: the unit-of-work
// provider wrapped by this package (cmd/bd's wireExternalDependencyUOWProvider)
// handing out BlockedLister through its own accessor. The server advertises
// policy.external_dependencies for this composition, so its listing is the one
// a client stands down to, and it must already carry the external refs:
//
//   - a row blocked ONLY by an unsatisfied external ref is listed, with the
//     ref as its blocker (the undecorated listing never names it, because the
//     stored blocked flag knows nothing of other projects);
//   - a row with a local blocker AND an external ref names both.
//
// A lister built over the inner provider rather than this one would lose
// both, and gc reads the missing row as unblocked.
func TestWrapUOWProviderBlockedListerNamesExternalBlockers(t *testing.T) {
	local, mixed, extOnly := issue("be-local"), issue("be-mixed"), issue("be-ext")
	mixed.Priority, extOnly.Priority = 1, 2
	inner := &fakeUOW{
		issues: &fakeIssueUseCase{
			ready: []*types.Issue{local, mixed, extOnly},
			blocked: []*types.BlockedIssue{
				{Issue: *mixed, BlockedByCount: 1, BlockedBy: []string{local.ID}},
			},
		},
		deps: &fakeDependencyUseCase{
			external: map[string][]*types.Dependency{
				mixed.ID:   {externalDep(mixed.ID, "external:remote:billing", types.DepBlocks)},
				extOnly.ID: {externalDep(extOnly.ID, "external:remote:payments", types.DepBlocks)},
			},
		},
	}
	provider := WrapUOWProvider(&fakeUOWProvider{uw: inner}, func(ProjectName) (string, bool) {
		return "", false
	}, nil)
	source, ok := provider.(uow.BlockedListerSource)
	if !ok {
		t.Fatalf("wrapped provider %T does not offer BlockedLister", provider)
	}
	lister, err := source.BlockedLister()
	if err != nil {
		t.Fatalf("BlockedLister: %v", err)
	}
	result, err := lister.ListBlocked(t.Context(), publicops.BlockedRequest{})
	if err != nil {
		t.Fatalf("ListBlocked: %v", err)
	}
	got := map[string][]string{}
	var order []string
	for _, item := range result.Items {
		blockers := slices.Clone(item.BlockedBy)
		slices.Sort(blockers)
		got[item.ID] = blockers
		order = append(order, item.ID)
		if item.BlockedByCount != len(item.BlockedBy) {
			t.Errorf("%s: BlockedByCount = %d, len(BlockedBy) = %d", item.ID, item.BlockedByCount, len(item.BlockedBy))
		}
	}
	want := map[string][]string{
		mixed.ID:   {local.ID, "external:remote:billing"},
		extOnly.ID: {"external:remote:payments"},
	}
	slices.Sort(want[mixed.ID])
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listing = %v, want %v", got, want)
	}
	if !slices.Equal(order, []string{mixed.ID, extOnly.ID}) {
		t.Errorf("order = %v, want priority order [%s %s]", order, mixed.ID, extOnly.ID)
	}
}
