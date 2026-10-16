//go:build cgo

package bdhttp_test

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
)

// recordedRequest is one request the query-recording proxy forwarded.
type recordedRequest struct {
	method, path string
	query        url.Values
}

func (r recordedRequest) String() string {
	if len(r.query) == 0 {
		return r.method + " " + r.path
	}
	return r.method + " " + r.path + "?" + r.query.Encode()
}

// queryRecorder keeps each request's decoded query beside its method and path,
// so a case can assert WHICH ids a read named rather than only how many reads a
// command made. requestCounter records paths alone, which cannot tell a read of
// the listed ids from a read of every id in the workspace.
type queryRecorder struct {
	mu   sync.Mutex
	reqs []recordedRequest
}

func (q *queryRecorder) reset() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.reqs = nil
}

func (q *queryRecorder) snapshot() []recordedRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]recordedRequest(nil), q.reqs...)
}

// startQueryRecordingProxy is startCountingProxy that also records each
// request's query.
func startQueryRecordingProxy(t *testing.T, upstream string) (string, *queryRecorder) {
	t.Helper()
	target, err := url.Parse("http://" + upstream)
	if err != nil {
		t.Fatal(err)
	}
	rec := &queryRecorder{}
	proxy := httputil.NewSingleHostReverseProxy(target)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.Query()})
		rec.mu.Unlock()
		proxy.ServeHTTP(w, r)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String(), rec
}

// TestE2E_ListTextRenderingsReadOnlyTheListedIssuesEdges pins the dependency
// read behind `bd list`'s text renderings over http — the default tree, --deps,
// a page cut by --limit, the --parent tree and --format — to ONE anchored
// listDependencies request naming exactly the listed issues.
//
// It used to be GetAllDependencyRecords, which over http enumerates every issue
// id in the workspace (all=true, 200 per page) and then reads every id's edges
// in chunks of 100: O(workspace/100) sequential requests for a 20-row page,
// 62 requests and ~6 s against a ~3.8k-issue workspace at ~50 ms a round trip.
// Every rendering reads only edges OWNED by a listed issue, so the listed ids
// are the whole question. The fixture makes the difference visible on a small
// workspace: a closed issue the default listing hides owns a parent-child edge
// onto the listed parent and a blocks edge onto a listed issue, and a read of
// the whole workspace would name it as an anchor.
//
// The ids are not dotted, so the nesting the tree prints can only have come
// from the parent-child edges this read returned: if the read failed, the tree
// would fall back to dotted-id nesting and print every issue as a root.
func TestE2E_ListTextRenderingsReadOnlyTheListedIssuesEdges(t *testing.T) {
	skipUnlessEmbeddedDolt(t)
	bin := buildBD(t)
	proxyAddr, rec := startQueryRecordingProxy(t, startE2EServer(t))
	f := newHTTPFixture(t, bin, proxyAddr)

	// Each issue is created with its outgoing edges (--deps), one subprocess
	// apiece: a bare id is a blocks edge onto it.
	parent := f.create("tree parent", "-t", "epic", "-p", "0")
	childA := f.create("tree child a", "-t", "task", "-p", "1", "--deps", "parent-child:"+parent)
	childB := f.create("tree child b", "-t", "task", "-p", "2", "--deps", "parent-child:"+parent+","+childA)
	loner := f.create("tree loner", "-t", "task", "-p", "3", "--deps", "related:"+childA)
	hidden := f.create("tree hidden", "-t", "task", "-p", "1", "--deps", "parent-child:"+parent+","+loner)
	f.mustRun(nil, "close", hidden, "--reason", "done", "--force")

	listed := []string{parent, childA, childB, loner}
	for _, tc := range []struct {
		name    string
		args    []string
		anchors []string
		// jsonArgs is the --json listing of the same page, whose request count
		// is the budget's base: a text rendering costs the page plus its ONE
		// edge read. Empty for --parent, whose tree walk is not a page.
		jsonArgs []string
		want     []string
	}{
		{
			name: "tree", args: []string{"list"}, anchors: listed,
			jsonArgs: []string{"list", "--json"},
			want:     []string{"├── ○ " + childA, "└── ○ " + childB},
		},
		{
			name: "deps all", args: []string{"list", "--deps=all"}, anchors: listed,
			jsonArgs: []string{"list", "--json"},
			want:     []string{"[depends-on]", "[related]"},
		},
		{
			name: "truncated page", args: []string{"list", "--limit", "2"}, anchors: []string{parent, childA},
			jsonArgs: []string{"list", "--json", "--limit", "2"},
			want:     []string{"└── ○ " + childA},
		},
		{
			name: "parent tree", args: []string{"list", "--parent", parent}, anchors: []string{parent, childA, childB},
			want: []string{"├── ○ " + childA, "└── ○ " + childB},
		},
		{
			name: "format digraph", args: []string{"list", "--format", "digraph"}, anchors: listed,
			jsonArgs: []string{"list", "--json"},
			want:     []string{childA + " " + parent, childB + " " + parent, childB + " " + childA, loner + " " + childA},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Through the subtest's own t: f.mustRun fails the PARENT test,
			// which a subtest may not do.
			run := func(args ...string) string {
				t.Helper()
				r := runBD(t, f.bin, f.dir, nil, args...)
				if r.code != 0 {
					t.Fatalf("bd %s failed (exit %d): stdout=%s stderr=%s", strings.Join(args, " "), r.code, r.stdout, r.stderr)
				}
				return r.stdout
			}
			rec.reset()
			out := run(tc.args...)
			reqs := rec.snapshot()
			t.Logf("bd %s: %d requests: %v", strings.Join(tc.args, " "), len(reqs), reqs)

			var edgeReads []recordedRequest
			for _, r := range reqs {
				if r.path == "/v0/beads/dependencies" {
					edgeReads = append(edgeReads, r)
				}
			}
			if len(edgeReads) != 1 {
				t.Fatalf("bd %s made %d listDependencies requests, want exactly 1 (the listed issues' edges): %v",
					strings.Join(tc.args, " "), len(edgeReads), edgeReads)
			}
			got := slices.Sorted(slices.Values(edgeReads[0].query["issue_id"]))
			want := slices.Sorted(slices.Values(tc.anchors))
			if !slices.Equal(got, want) {
				t.Errorf("bd %s read the edges of %v, want exactly the listed issues %v",
					strings.Join(tc.args, " "), got, want)
			}
			if tc.jsonArgs != nil {
				rec.reset()
				run(tc.jsonArgs...)
				base := len(rec.snapshot())
				if len(reqs) > base+1 {
					t.Errorf("bd %s made %d requests; bd %s makes %d for the same page, and the text rendering may add only its one edge read",
						strings.Join(tc.args, " "), len(reqs), strings.Join(tc.jsonArgs, " "), base)
				}
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("bd %s output lacks %q:\n%s", strings.Join(tc.args, " "), w, out)
				}
			}
			if strings.Contains(out, hidden) {
				t.Errorf("bd %s printed the closed issue %s, which the default listing hides:\n%s",
					strings.Join(tc.args, " "), hidden, out)
			}
		})
	}
}
