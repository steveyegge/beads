//go:build cgo

package bdhttp_test

import (
	"testing"
)

// TestE2E_SearchOverHTTP is S26. `bd search` called the raw SearchIssues
// method, which the http backend refuses ("partial-id search is not available
// over HTTP"); it now asks Reader.List with the text in ListRequest.Query,
// which travels as listIssues' `q` behind issues.list.search.
func TestE2E_SearchOverHTTP(t *testing.T) {
	bd := connectE2EWorkspace(t, "worker")

	routed := createE2EIssue(t, bd, "Routed pool work")
	alsoRouted := createE2EIssue(t, bd, "re-routed later")
	closedRouted := createE2EIssue(t, bd, "routed and done")
	other := createE2EIssue(t, bd, "something else")
	mustRun(t, bd, "close", closedRouted, "--json")

	// A refusal of the raw SearchIssuesWithCounts seam is the S26 regression
	// itself, not a missing dependency: S6c (the externaldeps reader
	// passthrough) is underneath, so nothing on this route reaches that seam.
	r := bd("search", "ROUTED", "--json")
	if r.code != 0 {
		t.Fatalf("bd search failed (exit %d): stdout=%s stderr=%s", r.code, r.stdout, r.stderr)
	}
	got := jsonIDSet(t, r.stdout)
	// Case-insensitive, every status (search includes closed by default).
	for _, id := range []string{routed, alsoRouted, closedRouted} {
		if !got[id] {
			t.Errorf("bd search ROUTED missed %s: %s", id, r.stdout)
		}
	}
	if got[other] {
		t.Errorf("bd search ROUTED matched %s, whose title does not contain it: %s", other, r.stdout)
	}

	// --status narrows, as on a local workspace.
	open := jsonIDSet(t, mustRun(t, bd, "search", "routed", "--status", "open", "--json").stdout)
	if open[closedRouted] || !open[routed] {
		t.Errorf("bd search routed --status open = %v, want %s without %s", open, routed, closedRouted)
	}

	// An id matches even when the title does not contain it.
	byID := jsonIDSet(t, mustRun(t, bd, "search", other, "--json").stdout)
	if !byID[other] {
		t.Errorf("bd search %s did not match the issue by its id: %v", other, byID)
	}

	// A flag listIssues publishes nothing for refuses rather than widening the
	// search.
	if r := bd("search", "routed", "--desc-contains", "pool", "--json"); r.code == 0 {
		t.Errorf("bd search --desc-contains succeeded over http; the wire cannot carry it, so it must refuse: %s", r.stdout)
	}
}
