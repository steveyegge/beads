//go:build cgo

package httpclient

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/steveyegge/beads/internal/httpapi"
	"github.com/steveyegge/beads/internal/storage/externaldeps"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/issueops"
)

// lazyBlockedLister defers building the server's blocked lister until the
// first request, because the served harness opens the reference store only
// after its config options have run.
type lazyBlockedLister struct {
	get func() (issueops.BlockedLister, error)
}

func (l lazyBlockedLister) ListBlocked(ctx context.Context, req issueops.BlockedRequest) (issueops.BlockedResult, error) {
	lister, err := l.get()
	if err != nil {
		return issueops.BlockedResult{}, err
	}
	return lister.ListBlocked(ctx, req)
}

// newPolicyServedEnv is a served workspace composed the way `bd serve` composes
// its store-shaped source: the blocked lister comes from the
// external-dependency decorator over the reference store
// (externaldeps.Store.BlockedLister), and the handshake advertises
// policy.external_dependencies because that layer is really there
// (externaldeps.Applied).
func newPolicyServedEnv(t *testing.T, prefix string) *servedEnv {
	t.Helper()
	var env *servedEnv
	env = newServedEnv(t, prefix, func(cfg *httpapi.Config) {
		cfg.ExternalDependencyPolicy = true
		cfg.BlockedLister = lazyBlockedLister{get: func() (issueops.BlockedLister, error) {
			decorated := externaldeps.New(env.reference, nil, nil)
			if !externaldeps.Applied(decorated) {
				t.Errorf("the server's store is not composed through the external-dependency policy")
			}
			return decorated.BlockedLister()
		}}
	})
	return env
}

// seedExternalBlocked seeds a local blocker, a row blocked by it AND an
// unsatisfied external ref (mixed), a row blocked ONLY by an external ref, and
// a free row. It returns the listing every route must answer.
func seedExternalBlocked(t *testing.T, env *servedEnv) map[string][]string {
	t.Helper()
	ctx := t.Context()
	p := env.prefix
	for i, id := range []string{"blocker", "mixed", "extonly", "free"} {
		if err := env.createIssue(ctx, &types.Issue{
			ID: p + "-" + id, Title: id, Status: types.StatusOpen, Priority: i, IssueType: types.TypeTask,
		}, "seed"); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	for _, dep := range []*types.Dependency{
		{IssueID: p + "-mixed", DependsOnID: p + "-blocker", Type: types.DepBlocks},
		{IssueID: p + "-mixed", DependsOnID: "external:otherproj:billing", Type: types.DepBlocks},
		{IssueID: p + "-extonly", DependsOnID: "external:otherproj:payments", Type: types.DepBlocks},
	} {
		if err := env.addDependency(ctx, dep, "seed"); err != nil {
			t.Fatalf("seed %s -> %s: %v", dep.IssueID, dep.DependsOnID, err)
		}
	}
	want := map[string][]string{
		p + "-mixed":   {"external:otherproj:billing", p + "-blocker"},
		p + "-extonly": {"external:otherproj:payments"},
	}
	for _, blockers := range want {
		slices.Sort(blockers)
	}
	return want
}

func blockedRowsByID(t *testing.T, items []*types.BlockedIssue) map[string][]string {
	t.Helper()
	got := map[string][]string{}
	for _, item := range items {
		blockers := slices.Clone(item.BlockedBy)
		slices.Sort(blockers)
		got[item.ID] = blockers
		if item.BlockedByCount != len(item.BlockedBy) {
			t.Errorf("%s: BlockedByCount = %d, len(BlockedBy) = %d", item.ID, item.BlockedByCount, len(item.BlockedBy))
		}
	}
	return got
}

// TestServedBlockedListerCarriesExternalRefsWhenTheServerEnforces pins the
// server-side decoration: against a server that advertises
// policy.external_dependencies, the operation's own answer (the raw client
// role, no client decoration at all) already lists the external-only row and
// names the external ref on the mixed row, and the client's decorated chain,
// which stands down to that server, answers the same.
func TestServedBlockedListerCarriesExternalRefsWhenTheServerEnforces(t *testing.T) {
	env := newPolicyServedEnv(t, "bxe")
	want := seedExternalBlocked(t, env)
	ctx := t.Context()

	if enforced, err := env.subject.ServerEnforcesExternalDependencyPolicy(ctx); err != nil || !enforced {
		t.Fatalf("ServerEnforcesExternalDependencyPolicy = %v, %v; want true", enforced, err)
	}

	raw, err := env.subject.BlockedLister()
	if err != nil {
		t.Fatalf("BlockedLister: %v", err)
	}
	served, err := raw.ListBlocked(ctx, issueops.BlockedRequest{})
	if err != nil {
		t.Fatalf("raw ListBlocked: %v", err)
	}
	if got := blockedRowsByID(t, served.Items); !reflect.DeepEqual(got, want) {
		t.Fatalf("the server's own listing = %v, want %v (external refs decorated server-side)", got, want)
	}

	chain, err := externaldeps.New(env.subject, nil, nil).BlockedLister()
	if err != nil {
		t.Fatalf("decorated BlockedLister: %v", err)
	}
	decorated, err := chain.ListBlocked(ctx, issueops.BlockedRequest{})
	if err != nil {
		t.Fatalf("decorated ListBlocked: %v", err)
	}
	if !reflect.DeepEqual(decorated.Items, served.Items) {
		t.Errorf("the client's decorated chain answered %v, want the server's listing %v unchanged",
			blockedRowsByID(t, decorated.Items), want)
	}
}

// TestServedLegacyGetBlockedIssuesThroughTheDecorator pins the legacy blocked
// door on the CLI's chain (externaldeps over the http store), which `bd ready
// --explain`, `bd mol stale` and the wisp commands read through. It used to
// fail over http on the raw multi-id read the decoration makes; it now takes
// the served roles, against a server that applies the policy and one that
// does not, and answers the same listing as the role, paged as asked.
func TestServedLegacyGetBlockedIssuesThroughTheDecorator(t *testing.T) {
	for _, tc := range []struct {
		name     string
		env      func(*testing.T) *servedEnv
		enforced bool
	}{
		{"server enforces", func(t *testing.T) *servedEnv { return newPolicyServedEnv(t, "bxl") }, true},
		{"client enforces", func(t *testing.T) *servedEnv { return newServedEnv(t, "bxm") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := tc.env(t)
			want := seedExternalBlocked(t, env)
			ctx := t.Context()
			if enforced, err := env.subject.ServerEnforcesExternalDependencyPolicy(ctx); err != nil || enforced != tc.enforced {
				t.Fatalf("ServerEnforcesExternalDependencyPolicy = %v, %v; want %v", enforced, err, tc.enforced)
			}
			decorated := externaldeps.New(env.subject, nil, nil)
			got, err := decorated.GetBlockedIssues(ctx, types.WorkFilter{})
			if err != nil {
				t.Fatalf("GetBlockedIssues over http: %v", err)
			}
			if rows := blockedRowsByID(t, got); !reflect.DeepEqual(rows, want) {
				t.Fatalf("GetBlockedIssues = %v, want %v", rows, want)
			}
			page, err := decorated.GetBlockedIssues(ctx, types.WorkFilter{Limit: 1})
			if err != nil {
				t.Fatalf("GetBlockedIssues(Limit=1): %v", err)
			}
			if len(page) != 1 || page[0].ID != got[0].ID {
				t.Errorf("GetBlockedIssues(Limit=1) = %v, want the first row %s", blockedRowsByID(t, page), got[0].ID)
			}
		})
	}
}
