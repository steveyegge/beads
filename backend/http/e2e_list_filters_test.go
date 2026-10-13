//go:build cgo

package bdhttp_test

import (
	"slices"
	"strings"
	"testing"
)

// listScopeShapes are the four maintainer-city `bd list` shapes S15b serves,
// one per corpus shape (backend/http/corpus/testdata/corpus.yaml), with the
// token each needs and the titles each must answer on the scope seed:
//   - gc-list-on-boot: gc's default on_boot hook, run on every agent boot;
//   - gc-list-on-boot-workflow: the same recovery, workflow lane;
//   - mc-list-title: the PR-intake formula's `--title "$SUBJECT"`;
//   - mc-list-id: the pr-review skill's `--id <ids>`.
var listScopeShapes = []struct {
	corpusID string
	args     []string
	token    string
	want     []string
}{
	{"gc-list-on-boot",
		[]string{"list", "--metadata-field", "gc.routed_to=pool-a", "--status=in_progress", "--no-assignee", "--json"},
		"issues.list.noAssignee", []string{"orphan-ip"}},
	{"gc-list-on-boot-workflow",
		[]string{"list", "--metadata-field", "gc.run_target=pool-a", "--metadata-field", "gc.kind=workflow", "--status=in_progress", "--no-assignee", "--json"},
		"issues.list.noAssignee", []string{"orphan-wf"}},
	// Case-insensitive substring, every status.
	{"mc-list-title",
		[]string{"list", "--all", "--include-infra", "--json", "--limit", "10", "--title", "CHILD-A"},
		"issues.list.title", []string{"child-a"}},
	// A closed id in the set is listed under --all; ids outside it are not.
	{"mc-list-id",
		[]string{"list", "--all", "--id", "{child-c},{loner}", "--json", "--limit", "0"},
		"issues.list.ids", []string{"child-c", "loner"}},
}

// seedListScope adds the on_boot recovery rows to the shared seed: one
// unassigned in-progress row per lane, and an ASSIGNED in-progress row on the
// routed lane, which --no-assignee must leave out.
func (f *remoteRolesFixture) seedListScope() {
	f.t.Helper()
	ip := f.create("orphan-ip", "-t", "task")
	f.mustRun(nil, "update", ip, "-s", "in_progress", "--set-metadata", "gc.routed_to=pool-a")
	wf := f.create("orphan-wf", "-t", "task")
	f.mustRun(nil, "update", wf, "-s", "in_progress", "--set-metadata", "gc.run_target=pool-a", "--set-metadata", "gc.kind=workflow")
	owned := f.create("owned-ip", "-t", "task", "-a", "worker")
	f.mustRun(nil, "update", owned, "-s", "in_progress", "--set-metadata", "gc.routed_to=pool-a", "--set-metadata", "gc.run_target=pool-a", "--set-metadata", "gc.kind=workflow")
}

// TestE2E_ListScopeFiltersOverHTTP is S15b. `bd list --no-assignee`, `--title`
// and `--id` refused over http (the list encoder had no parameter for them);
// they now ride listIssues' `no_assignee`, `title` and `id`, each behind its
// own behavior token, and the server applies them through the same Reader
// role a local `bd list` reads through. Each corpus shape must answer the
// rows an embedded workspace answers, and exactly the rows it names.
//
// Against a server that lacks a token (the MC production server's handshake
// shape, which predates all three) each shape refuses BEFORE dialing
// listIssues, naming the token, rather than answering a wider listing.
func TestE2E_ListScopeFiltersOverHTTP(t *testing.T) {
	skipUnlessEmbeddedDolt(t)
	bin := buildBD(t)
	addr := startE2EServer(t)
	remote := newHTTPFixture(t, bin, addr)
	local := newEmbeddedFixture(t, bin)
	for _, f := range []*remoteRolesFixture{remote, local} {
		f.seed()
		f.seedListScope()
	}

	for _, shape := range listScopeShapes {
		t.Run(shape.corpusID, func(t *testing.T) {
			r := remote.run(nil, remote.expand(shape.args)...)
			if r.code != 0 {
				t.Fatalf("http: bd %s failed (exit %d): stderr=%s", strings.Join(shape.args, " "), r.code, r.stderr)
			}
			l := local.mustRun(nil, local.expand(shape.args)...)
			got, want := remote.titlesOf(r.stdout), local.titlesOf(l.stdout)
			if !slices.Equal(got, want) {
				t.Errorf("http rows %v, embedded rows %v", got, want)
			}
			if !slices.Equal(got, shape.want) {
				t.Errorf("http rows %v, want exactly %v", got, shape.want)
			}
		})
	}

	t.Run("MC server mask refuses before dialing", func(t *testing.T) {
		proxyAddr, counter := startMaskingProxy(t, addr, mcContextMask)
		f := newHTTPFixture(t, bin, proxyAddr)
		f.ids = remote.ids // same workspace behind the proxy
		for _, shape := range listScopeShapes {
			counter.reset()
			r := f.run(nil, f.expand(shape.args)...)
			if r.code == 0 {
				t.Errorf("%s: bd %s exited 0 against a server lacking %s: %s", shape.corpusID, strings.Join(shape.args, " "), shape.token, r.stdout)
				continue
			}
			if !strings.Contains(r.stderr, shape.token) {
				t.Errorf("%s: the refusal does not name %s: stderr=%s", shape.corpusID, shape.token, r.stderr)
			}
			for _, p := range counter.snapshot() {
				if p == "GET /v0/beads/issues" {
					t.Errorf("%s: dialed listIssues against a server lacking %s: %v", shape.corpusID, shape.token, counter.snapshot())
					break
				}
			}
		}
	})
}
