package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"
)

// memoServer answers getIssue for bd-1 (a fresh title per request, so a hit is
// told apart from a re-read), a 404 for anything else, and 200 to any write.
func memoServer(t *testing.T) (*Client, *recorder) {
	t.Helper()
	served := 0
	return newTestClient(t, Options{}, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.URL.Path != "/v0/beads/issues/bd-1" {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Not Found","status":404,"code":"not_found","detail":"no such issue"}`))
			return
		}
		served++
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "bd-1", "title": "read " + string(rune('0'+served))})
	})
}

func memoGetIssue(t *testing.T, c *Client, id string, q url.Values) (string, error) {
	t.Helper()
	path, err := IssuePath(id)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Title string `json:"title"`
	}
	err = c.Do(context.Background(), Request{Op: OpGetIssue, Method: http.MethodGet, Path: path, Query: q, IssueID: id}, &out)
	return out.Title, err
}

// TestReadMemoCollapsesRepeatedRowReads is bts-xpw.3.14.6's per-command memo:
// once enabled, an identical getIssue is answered from the response already
// received, a different query is a different read, and every answer is decoded
// afresh (the second caller gets its own value, equal to the first).
func TestReadMemoCollapsesRepeatedRowReads(t *testing.T) {
	c, rec := memoServer(t)
	c.EnableReadMemo()

	first, err := memoGetIssue(t, c, "bd-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := memoGetIssue(t, c, "bd-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec.count() != 1 || first != second {
		t.Fatalf("two identical reads made %d requests and answered %q then %q; want one request, one answer", rec.count(), first, second)
	}
	withComments := url.Values{"include_comments": {"true"}}
	if _, err := memoGetIssue(t, c, "bd-1", withComments); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 2 {
		t.Fatalf("a read with a different query was answered from the memo (%d requests)", rec.count())
	}
}

// TestReadMemoIsDroppedByAnyWrite: a write through the client may change what a
// memoized read said, so the next read goes back to the server.
func TestReadMemoIsDroppedByAnyWrite(t *testing.T) {
	c, rec := memoServer(t)
	c.EnableReadMemo()

	before, _ := memoGetIssue(t, c, "bd-1", nil)
	path, _ := IssuePath("bd-1")
	if err := c.Do(context.Background(), Request{Op: OpUpdateIssue, Method: http.MethodPatch, Path: path, Body: map[string]any{}}, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := memoGetIssue(t, c, "bd-1", nil)
	if rec.count() != 3 || before == after {
		t.Fatalf("read after a write: %d requests, %q then %q; want the row re-read", rec.count(), before, after)
	}
}

// TestReadMemoKeepsOnlySuccessesAndExpires: a miss is re-asked (it may have been
// created since), and an entry older than ReadMemoTTL is re-read.
func TestReadMemoKeepsOnlySuccessesAndExpires(t *testing.T) {
	c, rec := memoServer(t)
	c.EnableReadMemo()
	now := time.Unix(1_000_000, 0)
	c.memo.now = func() time.Time { return now }

	for range 2 {
		if _, err := memoGetIssue(t, c, "bd-404", nil); err == nil {
			t.Fatal("a missing row read without an error")
		}
	}
	if rec.count() != 2 {
		t.Fatalf("a 404 was memoized: %d requests for two reads", rec.count())
	}
	_, _ = memoGetIssue(t, c, "bd-1", nil)
	now = now.Add(ReadMemoTTL + time.Second)
	_, _ = memoGetIssue(t, c, "bd-1", nil)
	if rec.count() != 4 {
		t.Fatalf("an expired entry answered: %d requests, want 4", rec.count())
	}
}

// TestReadMemoIsOffByDefault: a client nobody opted in re-reads every time —
// the memo is for one command's transport, never a long-lived one's.
func TestReadMemoIsOffByDefault(t *testing.T) {
	c, rec := memoServer(t)
	_, _ = memoGetIssue(t, c, "bd-1", nil)
	_, _ = memoGetIssue(t, c, "bd-1", nil)
	if rec.count() != 2 || c.ReadMemoEnabled() {
		t.Fatalf("an un-enabled client memoized (%d requests)", rec.count())
	}
}

// TestReadMemoAgesFromTheServersAnswerNotTheLastHit: an entry's age is the age
// of the answer the server gave, and a hit does not renew it. A row read more
// often than ReadMemoTTL (bd show --watch polls every 2 s) is still re-read
// once its answer is a TTL old; were each hit to re-stamp the entry, it would
// answer from the first read for as long as the polling kept up.
func TestReadMemoAgesFromTheServersAnswerNotTheLastHit(t *testing.T) {
	c, rec := memoServer(t)
	c.EnableReadMemo()
	now := time.Unix(1_000_000, 0)
	c.memo.now = func() time.Time { return now }

	var titles []string
	for range 4 { // t = 0 s (miss), 2 s (hit), 4 s (hit), 6 s (a TTL old: re-read)
		title, err := memoGetIssue(t, c, "bd-1", nil)
		if err != nil {
			t.Fatal(err)
		}
		titles = append(titles, title)
		now = now.Add(2 * time.Second)
	}
	if rec.count() != 2 || titles[3] == titles[0] {
		t.Fatalf("a row polled every 2 s made %d requests and answered %q; want it re-read at 6 s (2 requests)", rec.count(), titles)
	}
}

// TestReadMemoDropsAnAnswerThatOverlappedAWrite: a read whose request was in
// flight while a write went through the client may carry the pre-write row,
// even though it returned after the write did. Storing it would answer every
// later read with that row for a whole TTL — the command's own write unseen.
func TestReadMemoDropsAnAnswerThatOverlappedAWrite(t *testing.T) {
	arrived, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	served := 0
	c, rec := newTestClient(t, Options{ResponseHeaderTimeout: -1}, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		mu.Lock()
		served++
		n := served
		mu.Unlock()
		if n == 1 {
			close(arrived)
			<-release
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "bd-1", "title": fmt.Sprintf("read %d", n)})
	})
	c.EnableReadMemo()
	path, err := IssuePath("bd-1")
	if err != nil {
		t.Fatal(err)
	}

	type answer struct {
		title string
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		var out struct {
			Title string `json:"title"`
		}
		err := c.Do(context.Background(), Request{Op: OpGetIssue, Method: http.MethodGet, Path: path, IssueID: "bd-1"}, &out)
		done <- answer{out.Title, err}
	}()
	<-arrived // the read is on the wire; the write starts and finishes while it waits
	if err := c.Do(context.Background(), Request{Op: OpUpdateIssue, Method: http.MethodPatch, Path: path, Body: map[string]any{}}, nil); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	overlapped := <-done
	if overlapped.err != nil {
		t.Fatal(overlapped.err)
	}

	after, err := memoGetIssue(t, c, "bd-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec.count() != 3 || after == overlapped.title {
		t.Fatalf("the read after the write answered %q (the overlapped read's %q) in %d requests; want it re-read (3 requests)", after, overlapped.title, rec.count())
	}
}

// TestReadMemoHoldsAtMostOneTTLOfAnswers: expired entries are swept when a new
// answer is stored, so a client that lives on (and keeps reading new rows)
// does not keep every answer it ever received.
func TestReadMemoHoldsAtMostOneTTLOfAnswers(t *testing.T) {
	c, _ := memoServer(t)
	c.EnableReadMemo()
	now := time.Unix(1_000_000, 0)
	c.memo.now = func() time.Time { return now }

	if _, err := memoGetIssue(t, c, "bd-1", nil); err != nil {
		t.Fatal(err)
	}
	now = now.Add(ReadMemoTTL + time.Second)
	if _, err := memoGetIssue(t, c, "bd-1", url.Values{"include_comments": {"true"}}); err != nil {
		t.Fatal(err)
	}
	c.memo.mu.Lock()
	held := len(c.memo.entries)
	c.memo.mu.Unlock()
	if held != 1 {
		t.Fatalf("the memo holds %d answers after one expired and one was stored; want 1", held)
	}
}
