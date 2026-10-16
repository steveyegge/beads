//go:build cgo

package corpus_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/httpapi"
	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
	"github.com/steveyegge/beads/internal/types"
)

// gateCheckRequestLog records every request the reference proxy forwards.
type gateCheckRequestLog struct {
	mu   sync.Mutex
	seen []string
	next http.Handler
}

func (l *gateCheckRequestLog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	l.seen = append(l.seen, r.Method+" "+r.URL.Path)
	l.mu.Unlock()
	l.next.ServeHTTP(w, r)
}

func (l *gateCheckRequestLog) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.seen
	l.seen = nil
	return out
}

func gateCheckWrites(reqs []string) []string {
	var writes []string
	for _, r := range reqs {
		if !strings.HasPrefix(r, http.MethodGet+" ") {
			writes = append(writes, r)
		}
	}
	return writes
}

// TestServedGateCheckOverAwaitTypedGates runs `bd gate check` over HTTP, through
// the reference proxy, against REAL await-typed gates. No wire create accepts
// await_*, so the gates are seeded straight into the served store before it
// listens, and every outcome is read back from that store:
//
//   - --dry-run writes nothing;
//   - an expired timer closes, a pending one stays open;
//   - a bead gate whose target is open records its sighting (issues.update),
//     and one whose target is closed closes;
//   - a merged gh:pr gate closes, a closed one escalates and stays open;
//   - a gh:run gate with a workflow-name hint discovers its run but cannot
//     persist await_id over the wire: that gate's error names the await_id
//     update, not a Go method, and await_id is untouched.
//
// The corpus.yaml shapes prove the listing only (their gates have no
// await_type); this is the test that proves what the sweep does to the gates.
func TestServedGateCheckOverAwaitTypedGates(t *testing.T) {
	skipUnlessEmbeddedDolt(t)
	bin := buildBD(t)
	ctx := context.Background()

	store, err := embeddeddolt.Open(ctx, t.TempDir(), corpusDatabase, "main")
	if err != nil {
		t.Fatalf("open the served store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SetConfig(ctx, "issue_prefix", corpusPrefix); err != nil {
		t.Fatalf("set the issue prefix: %v", err)
	}
	seed := func(i *types.Issue) {
		t.Helper()
		i.Status = types.StatusOpen
		i.Title = i.ID
		if err := store.CreateIssue(ctx, i, "seed"); err != nil {
			t.Fatalf("seed %s: %v", i.ID, err)
		}
	}
	seed(&types.Issue{ID: "e2e-tgt-open", IssueType: "task"})
	seed(&types.Issue{ID: "e2e-tgt-closed", IssueType: "task"})
	if err := store.CloseIssue(ctx, "e2e-tgt-closed", "done", "seed", ""); err != nil {
		t.Fatalf("close e2e-tgt-closed: %v", err)
	}
	seed(&types.Issue{ID: "e2e-g-timer", IssueType: "gate", AwaitType: "timer", Timeout: time.Millisecond})
	seed(&types.Issue{ID: "e2e-g-timer-pending", IssueType: "gate", AwaitType: "timer", Timeout: 100 * time.Hour})
	seed(&types.Issue{ID: "e2e-g-bead-open", IssueType: "gate", AwaitType: "bead", AwaitID: "e2e-tgt-open"})
	seed(&types.Issue{ID: "e2e-g-bead-closed", IssueType: "gate", AwaitType: "bead", AwaitID: "e2e-tgt-closed"})
	seed(&types.Issue{ID: "e2e-g-pr-merged", IssueType: "gate", AwaitType: "gh:pr", AwaitID: "11"})
	seed(&types.Issue{ID: "e2e-g-pr-closed", IssueType: "gate", AwaitType: "gh:pr", AwaitID: "12"})
	seed(&types.Issue{ID: "e2e-g-run-hint", IssueType: "gate", AwaitType: "gh:run", AwaitID: "ci-wf"})

	tokenFile := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenFile, []byte(corpusToken+"\n"), 0o600); err != nil {
		t.Fatalf("write the token file: %v", err)
	}
	auth, err := httpapi.NewTokenFileAuth(tokenFile)
	if err != nil {
		t.Fatalf("load the token file: %v", err)
	}
	cfg := corpusServeConfig(t, store)
	cfg.AllowNonLoopback = true
	cfg.Auth = auth
	srv, err := httpapi.Listen(cfg)
	if err != nil {
		t.Fatalf("bind the in-process server: %v", err)
	}
	serveCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(serveCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("the in-process server did not shut down")
		}
	})

	proxy := newReferenceProxy(t, loadReferenceProfile(t), srv.Addr())
	reqs := &gateCheckRequestLog{next: proxy}
	proxyAddr := startProxy(t, reqs)

	// gh and gt are fakes on PATH: gh answers the run discovery and the two PR
	// states, gt records each escalation.
	binDir := t.TempDir()
	gh := `#!/bin/sh
case "$1 $2" in
"run list") echo '[{"databaseId":4242,"name":"ci-wf","status":"completed","conclusion":"success","createdAt":"2026-10-15T00:00:00Z","workflowName":"ci-wf"}]' ;;
"run view") echo '{"status":"completed","conclusion":"success","name":"ci-wf"}' ;;
"pr view") if [ "$3" = "11" ]; then echo '{"state":"MERGED","title":"m"}'; else echo '{"state":"CLOSED","title":"c"}'; fi ;;
*) echo "unexpected gh $*" >&2; exit 3 ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "gt"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake gt: %v", err)
	}

	dir := t.TempDir()
	env := []string{
		"BEADS_HTTP_TOKEN=" + proxyAddr + "=" + corpusToken,
		"BEADS_ACTOR=worker",
		"BD_BACKUP_ENABLED=false",
		"BD_EXPORT_AUTO=false",
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	if r := runBD(t, bin, dir, env, "", "connect", "http://"+proxyAddr, "--expect-project-id", corpusProjectID, "--json"); r.code != 0 {
		t.Fatalf("bd connect: %s", r)
	}
	reqs.take()
	time.Sleep(50 * time.Millisecond) // the 1ms timer is expired by now

	get := func(id string) *types.Issue {
		t.Helper()
		issue, err := store.GetIssue(ctx, id)
		if err != nil {
			t.Fatalf("read %s back: %v", id, err)
		}
		return issue
	}
	wantStatus := func(id string, want types.Status) {
		t.Helper()
		if got := get(id).Status; got != want {
			t.Errorf("%s status = %s, want %s", id, got, want)
		}
	}
	seenOf := func(id string) string {
		t.Helper()
		var meta map[string]json.RawMessage
		raw := get(id).Metadata
		if len(raw) == 0 {
			return ""
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			t.Fatalf("%s metadata %s: %v", id, raw, err)
		}
		var seen string
		_ = json.Unmarshal(meta["await_seen"], &seen)
		return seen
	}
	allGates := []string{"e2e-g-timer", "e2e-g-timer-pending", "e2e-g-bead-open", "e2e-g-bead-closed", "e2e-g-pr-merged", "e2e-g-pr-closed", "e2e-g-run-hint"}

	t.Run("dry-run writes nothing", func(t *testing.T) {
		r := runBD(t, bin, dir, env, "", "gate", "check", "--dry-run")
		if r.code != 0 {
			t.Fatalf("bd gate check --dry-run: %s", r)
		}
		if w := gateCheckWrites(reqs.take()); len(w) != 0 {
			t.Fatalf("--dry-run sent writes %v\n%s", w, r)
		}
		for _, id := range allGates {
			wantStatus(id, types.StatusOpen)
		}
		if seen := seenOf("e2e-g-bead-open"); seen != "" {
			t.Errorf("--dry-run recorded a sighting %q", seen)
		}
		if got := get("e2e-g-run-hint").AwaitID; got != "ci-wf" {
			t.Errorf("--dry-run persisted await_id %q", got)
		}
	})

	t.Run("timer sweep closes the expired timer", func(t *testing.T) {
		r := runBD(t, bin, dir, env, "", "gate", "check", "--type=timer", "--escalate")
		if r.code != 0 {
			t.Fatalf("bd gate check --type=timer --escalate: %s", r)
		}
		reqs.take()
		wantStatus("e2e-g-timer", types.StatusClosed)
		wantStatus("e2e-g-timer-pending", types.StatusOpen)
	})

	t.Run("bead sweep records the sighting and closes the done gate", func(t *testing.T) {
		r := runBD(t, bin, dir, env, "", "gate", "check", "--type=bead")
		if r.code != 0 {
			t.Fatalf("bd gate check --type=bead: %s", r)
		}
		writes := gateCheckWrites(reqs.take())
		sighted := false
		for _, w := range writes {
			if strings.Contains(w, "e2e-g-bead-open") {
				sighted = true
			}
		}
		if !sighted {
			t.Errorf("no write to e2e-g-bead-open reached the server; writes: %v\n%s", writes, r)
		}
		if seen := seenOf("e2e-g-bead-open"); seen != "e2e-tgt-open" {
			t.Errorf("e2e-g-bead-open await_seen = %q, want e2e-tgt-open\n%s", seen, r)
		}
		wantStatus("e2e-g-bead-open", types.StatusOpen)
		wantStatus("e2e-g-bead-closed", types.StatusClosed)
	})

	t.Run("gh:pr sweep closes the merged PR gate", func(t *testing.T) {
		r := runBD(t, bin, dir, env, "", "gate", "check", "--type=gh:pr", "--escalate")
		if r.code != 0 {
			t.Fatalf("bd gate check --type=gh:pr --escalate: %s", r)
		}
		reqs.take()
		wantStatus("e2e-g-pr-merged", types.StatusClosed)
		wantStatus("e2e-g-pr-closed", types.StatusOpen)
		if !strings.Contains(r.stdout, "e2e-g-pr-closed: ESCALATE") {
			t.Errorf("closed PR gate was not escalated:\n%s", r)
		}
	})

	t.Run("gh:run await_id persist refuses by name", func(t *testing.T) {
		r := runBD(t, bin, dir, env, "", "gate", "check", "--type=gh:run")
		if r.code == 0 {
			t.Fatalf("bd gate check --type=gh:run succeeded; the wire cannot carry await_id: %s", r)
		}
		if w := gateCheckWrites(reqs.take()); len(w) != 0 {
			t.Errorf("the refused await_id persist sent writes %v", w)
		}
		if !strings.Contains(r.stderr, "e2e-g-run-hint") || !strings.Contains(r.stderr, "await_id") {
			t.Errorf("gh:run error does not name the gate and the await_id update:\n%s", r)
		}
		for _, leak := range []string{"UpdateIssue", "Lifecycle.Update", "updateIssue"} {
			if strings.Contains(r.stderr, leak) {
				t.Errorf("gh:run error leaks %q:\n%s", leak, r)
			}
		}
		if got := get("e2e-g-run-hint").AwaitID; got != "ci-wf" {
			t.Errorf("e2e-g-run-hint await_id = %q, want ci-wf untouched", got)
		}
		wantStatus("e2e-g-run-hint", types.StatusOpen)
	})
}
