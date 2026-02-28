package protocol

import "testing"

// TestProtocol_BlockedStatusIsStoredNotComputed documents that
// dependency-blocked issues keep stored status "open" — they appear in
// 'bd blocked' but NOT in 'bd list --status blocked'. The --status flag
// filters by stored status, not computed dependency state.
func TestProtocol_BlockedStatusIsStoredNotComputed(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)

	blocker := w.create("Blocker")
	blocked := w.create("Blocked issue")
	w.run("dep", "add", blocked, blocker, "--type=blocks")

	// bd blocked should find the dependency-blocked issue
	blockedOut := w.run("blocked", "--json")
	blockedIssues := parseJSONOutput(t, blockedOut)
	found := false
	for _, issue := range blockedIssues {
		if id, _ := issue["id"].(string); id == blocked {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("'bd blocked' should find %s but didn't.\nOutput: %s", blocked, blockedOut)
	}

	// bd list --status blocked should NOT find it (status is still "open")
	listOut := w.run("list", "--status", "blocked", "--json", "-n", "0")
	listIssues := parseJSONOutput(t, listOut)
	for _, issue := range listIssues {
		if id, _ := issue["id"].(string); id == blocked {
			t.Errorf("'bd list --status blocked' should NOT find %s (its stored status is 'open')", blocked)
		}
	}
}
