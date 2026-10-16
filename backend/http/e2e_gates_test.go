//go:build cgo

package bdhttp_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// gateTokens are the two behavior tokens the gate commands depend on:
// issues.create.gates for a create carrying gate members (bd gate create),
// issues.update.gates for an await_id or waiters patch (bd gate add-waiter,
// bd gate discover, gate check's gh:run persist, bd update --await-id).
var gateTokens = []string{"issues.create.gates", "issues.update.gates"}

// withoutGateTokens rewrites the handshake into a server that predates the
// gate members: the live capability list minus the two gate tokens.
func withoutGateTokens(body map[string]any) {
	caps, _ := body["capabilities"].([]any)
	kept := make([]any, 0, len(caps))
	for _, c := range caps {
		if s, _ := c.(string); slices.Contains(gateTokens, s) {
			continue
		}
		kept = append(kept, c)
	}
	body["capabilities"] = kept
}

// gateShow is the subset of `bd show --json` the gate cases read.
type gateShow struct {
	ID        string   `json:"id"`
	Status    string   `json:"status"`
	IssueType string   `json:"issue_type"`
	AwaitType string   `json:"await_type"`
	AwaitID   string   `json:"await_id"`
	Timeout   int64    `json:"timeout"`
	Waiters   []string `json:"waiters"`
}

func showGate(t *testing.T, f *remoteRolesFixture, id string) gateShow {
	t.Helper()
	out := strings.TrimSpace(f.mustRun(nil, "show", id, "--json").stdout)
	var rows []gateShow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		var one gateShow
		if err1 := json.Unmarshal([]byte(out), &one); err1 != nil {
			t.Fatalf("parse bd show --json: %v\n%s", err, out)
		}
		return one
	}
	if len(rows) == 0 {
		t.Fatalf("bd show %s --json returned no row", id)
	}
	return rows[0]
}

func readyIDs(t *testing.T, f *remoteRolesFixture) map[string]bool {
	t.Helper()
	ids := map[string]bool{}
	for _, row := range jsonRows(t, f.mustRun(nil, "ready", "--json", "--limit", "0").stdout) {
		if id, ok := row["id"].(string); ok {
			ids[id] = true
		}
	}
	return ids
}

func writesOf(paths []string) []string {
	var writes []string
	for _, p := range paths {
		if !strings.HasPrefix(p, http.MethodGet+" ") {
			writes = append(writes, p)
		}
	}
	return writes
}

// TestE2E_GatesOverHTTP runs the gate commands through the real `bd` binary
// against an in-process `bd serve`.
//
// Against a server that advertises the gate tokens:
//   - `bd gate create` is ONE createIssue carrying the gate members and the
//     blocks edge (timer, gh:run and bead gates), and the blocked issue
//     leaves the ready set;
//   - `bd gate check --type=timer` closes an expired timer gate and the
//     blocked issue becomes ready;
//   - `bd gate add-waiter` registers a waiter once (the repeat writes nothing);
//   - `bd update --await-id` sets await_id.
//
// Against a server that does not advertise them, each command is refused
// before any write, naming the token it needs, and nothing changes.
func TestE2E_GatesOverHTTP(t *testing.T) {
	skipUnlessEmbeddedDolt(t)
	bin := buildBD(t)
	addr := startE2EServer(t)

	proxyAddr, counter := startCountingProxy(t, addr)
	f := newHTTPFixture(t, bin, proxyAddr)

	var waiterGate string

	t.Run("advertised", func(t *testing.T) {
		timerTarget := f.create("timer target")
		counter.reset()
		created := f.mustRun(nil, "gate", "create", "--type=timer", "--timeout=1ns", "--blocks", timerTarget, "--json")
		if w := writesOf(counter.snapshot()); !slices.Equal(w, []string{"POST /v0/beads/issues"}) {
			t.Errorf("gate create wrote %v, want exactly one createIssue", w)
		}
		timerGate := firstJSONField(t, created.stdout, "id")
		if g := showGate(t, f, timerGate); g.IssueType != "gate" || g.AwaitType != "timer" || g.Timeout != 1 || g.Status != "open" {
			t.Errorf("timer gate reads back %+v, want an open timer gate with a 1ns timeout", g)
		}
		if readyIDs(t, f)[timerTarget] {
			t.Errorf("%s is ready while its gate is open", timerTarget)
		}

		time.Sleep(10 * time.Millisecond)
		r := f.mustRun(nil, "gate", "check", "--type=timer", "--escalate")
		if !strings.Contains(r.stdout, "1 resolved") {
			t.Errorf("gate check --type=timer = %q, want one resolved gate", r.stdout)
		}
		if g := showGate(t, f, timerGate); g.Status != "closed" {
			t.Errorf("expired timer gate %s is %s after gate check, want closed", timerGate, g.Status)
		}
		if !readyIDs(t, f)[timerTarget] {
			t.Errorf("%s is not ready after its timer gate resolved", timerTarget)
		}

		runTarget := f.create("run target")
		runGate := firstJSONField(t, f.mustRun(nil, "gate", "create", "--type=gh:run", "--await-id=ci-wf", "--repo=owner/repo", "--blocks", runTarget, "--json").stdout, "id")
		if g := showGate(t, f, runGate); g.AwaitType != "gh:run" || g.AwaitID != "ci-wf" {
			t.Errorf("gh:run gate reads back %+v, want await gh:run ci-wf", g)
		}

		awaited := f.create("awaited bead")
		beadTarget := f.create("bead target")
		beadGate := firstJSONField(t, f.mustRun(nil, "gate", "create", "--type=bead", "--await-id", awaited, "--blocks", beadTarget, "--json").stdout, "id")
		if g := showGate(t, f, beadGate); g.AwaitType != "bead" || g.AwaitID != awaited {
			t.Errorf("bead gate reads back %+v, want await bead %s", g, awaited)
		}
		waiterGate = beadGate

		counter.reset()
		r = f.mustRun(nil, "gate", "add-waiter", beadGate, "rig/workers/a")
		if !strings.Contains(r.stdout, "Added waiter to gate "+beadGate+": rig/workers/a") {
			t.Errorf("add-waiter output = %q", r.stdout)
		}
		if w := writesOf(counter.snapshot()); len(w) != 1 || !strings.HasPrefix(w[0], "PATCH /v0/beads/issues/") {
			t.Errorf("add-waiter wrote %v, want one updateIssue", w)
		}
		counter.reset()
		r = f.mustRun(nil, "gate", "add-waiter", beadGate, "rig/workers/a")
		if !strings.Contains(r.stdout, "Waiter already registered on gate "+beadGate) {
			t.Errorf("repeat add-waiter output = %q", r.stdout)
		}
		if w := writesOf(counter.snapshot()); len(w) != 0 {
			t.Errorf("a repeat add-waiter wrote %v", w)
		}
		f.mustRun(nil, "gate", "add-waiter", beadGate, "rig/workers/b")
		if g := showGate(t, f, beadGate); !slices.Equal(g.Waiters, []string{"rig/workers/a", "rig/workers/b"}) {
			t.Errorf("waiters = %v, want [rig/workers/a rig/workers/b]", g.Waiters)
		}

		f.mustRun(nil, "update", runGate, "--await-id", "12345", "--json")
		if g := showGate(t, f, runGate); g.AwaitID != "12345" {
			t.Errorf("await_id after update --await-id = %q, want 12345", g.AwaitID)
		}
	})

	t.Run("refused without the tokens", func(t *testing.T) {
		if waiterGate == "" {
			t.Skip("the advertised case created no gate")
		}
		maskedAddr, maskedCounter := startMaskingProxy(t, addr, withoutGateTokens)
		m := newHTTPFixture(t, bin, maskedAddr)
		target := m.create("masked target")
		before := showGate(t, f, waiterGate)

		cases := []struct {
			name  string
			args  []string
			token string
		}{
			{"gate create", []string{"gate", "create", "--type=timer", "--timeout=1h", "--blocks", target}, "issues.create.gates"},
			{"gate create human", []string{"gate", "create", "--blocks", target}, "issues.create.gates"},
			{"gate add-waiter", []string{"gate", "add-waiter", waiterGate, "rig/workers/c"}, "issues.update.gates"},
			{"update --await-id", []string{"update", waiterGate, "--await-id", "999"}, "issues.update.gates"},
		}
		for _, tc := range cases {
			maskedCounter.reset()
			r := m.run(nil, tc.args...)
			if r.code == 0 {
				t.Errorf("%s against a server without %s exited 0: %s", tc.name, tc.token, r.stdout)
			}
			if !strings.Contains(r.stderr, tc.token) {
				t.Errorf("%s refusal does not name %s: %s", tc.name, tc.token, r.stderr)
			}
			if w := writesOf(maskedCounter.snapshot()); len(w) != 0 {
				t.Errorf("a refused %s wrote %v; the refusal must come before any write", tc.name, w)
			}
		}

		if !readyIDs(t, m)[target] {
			t.Errorf("%s left the ready set though every gate create was refused", target)
		}
		after := showGate(t, f, waiterGate)
		if after.AwaitID != before.AwaitID || !slices.Equal(after.Waiters, before.Waiters) {
			t.Errorf("refused writes changed the gate: before %+v after %+v", before, after)
		}
	})
}
