package wire

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// stallServer answers every request normally except those that arrive on the
// FIRST connection it ever sees: those get no response headers at all, the
// shape of the shadow run's stuck HTTP/1.1 connection (a silent connection
// while other connections to the same serve kept answering). The hung handler
// returns when the client gives up on the connection.
type stallServer struct {
	srv *httptest.Server

	mu      sync.Mutex
	first   string   // RemoteAddr of the first connection
	remotes []string // RemoteAddr of every request, in order
	methods []string
	hang    func(w http.ResponseWriter, r *http.Request)
}

func newStallServer(t *testing.T, answer http.HandlerFunc) *stallServer {
	t.Helper()
	s := &stallServer{}
	s.hang = func(_ http.ResponseWriter, r *http.Request) {
		// Drain the body first: net/http only notices the client closing the
		// connection (and cancels r.Context) once the body has been read.
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		if s.first == "" {
			s.first = r.RemoteAddr
		}
		stuck := r.RemoteAddr == s.first
		s.remotes = append(s.remotes, r.RemoteAddr)
		s.methods = append(s.methods, r.Method)
		hang := s.hang
		s.mu.Unlock()
		if stuck {
			hang(w, r)
			return
		}
		answer(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stallServer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.remotes...)
}

func (s *stallServer) client(t *testing.T, opts Options) *Client {
	t.Helper()
	base, err := url.Parse(s.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if opts.HTTPClient == nil {
		// The production shape: a pooled transport with the package's own 60s
		// overall ceiling, which the header timeout must undercut.
		opts.HTTPClient = &http.Client{Transport: s.srv.Client().Transport, Timeout: DefaultTimeout}
	}
	c, err := New(base, nil, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

const testHeaderTimeout = 300 * time.Millisecond

func TestAStalledIdempotentReadRetriesOnceOnANewConnection(t *testing.T) {
	s := newStallServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	var events []StallRetry
	c := s.client(t, Options{
		ResponseHeaderTimeout: testHeaderTimeout,
		OnStallRetry:          func(e StallRetry) { events = append(events, e) },
	})
	before := StallRetryCount()

	start := time.Now()
	var out map[string]any
	if err := c.Do(ctx(t), Request{Op: OpGetContext, Method: http.MethodGet, Path: PathContext}, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	elapsed := time.Since(start)

	if out["ok"] != true {
		t.Fatalf("body = %v, want the second connection's answer", out)
	}
	// One header timeout, then one prompt retry: well under two timeouts, and
	// nowhere near the 60s overall ceiling the shadow run hit.
	if elapsed < testHeaderTimeout || elapsed > 2*testHeaderTimeout+time.Second {
		t.Errorf("elapsed %v, want about one header timeout (%v)", elapsed, testHeaderTimeout)
	}
	seen := s.seen()
	if len(seen) != 2 {
		t.Fatalf("server saw %d requests, want 2 (the stalled one and one retry): %v", len(seen), seen)
	}
	if seen[0] == seen[1] {
		t.Errorf("the retry rode the stuck connection %s; it must use a new one", seen[0])
	}
	if len(events) != 1 {
		t.Fatalf("OnStallRetry fired %d times, want 1: %+v", len(events), events)
	}
	e := events[0]
	if e.Op != OpGetContext || e.Method != http.MethodGet || e.Retry != 1 || e.Reason != StallHeaderTimeout {
		t.Errorf("event = %+v, want op %s, GET, retry 1, reason %q", e, OpGetContext, StallHeaderTimeout)
	}
	if !strings.Contains(e.String(), "retry 1") || !strings.Contains(e.String(), e.Host) {
		t.Errorf("event line %q should carry the retry count and the host", e.String())
	}
	if got := StallRetryCount() - before; got != 1 {
		t.Errorf("StallRetryCount advanced by %d, want 1", got)
	}
}

func TestAnExplicitlyIdempotentPostReadRetriesToo(t *testing.T) {
	s := newStallServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	c := s.client(t, Options{ResponseHeaderTimeout: testHeaderTimeout})
	err := c.Do(ctx(t), Request{Op: OpBatchGetIssues, Method: http.MethodPost, Path: PathIssuesBatchGet,
		Body: map[string]any{"ids": []string{"x"}}, Idempotent: true}, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if n := len(s.seen()); n != 2 {
		t.Fatalf("server saw %d requests, want 2", n)
	}
}

func TestAStalledWriteIsNeverRetriedAndSurfacesATypedTimeout(t *testing.T) {
	s := newStallServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	fired := 0
	// A write is bounded by the client's overall timeout, not the header
	// timer; shrink the overall one so the stall surfaces quickly.
	c := s.client(t, Options{
		HTTPClient:            &http.Client{Transport: s.srv.Client().Transport, Timeout: 2 * testHeaderTimeout},
		ResponseHeaderTimeout: testHeaderTimeout,
		OnStallRetry:          func(StallRetry) { fired++ },
	})

	start := time.Now()
	err := c.Do(ctx(t), Request{Op: OpCreateIssue, Method: http.MethodPost, Path: PathIssues,
		Body: map[string]any{"title": "t"}}, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Do succeeded; want the header timeout")
	}
	var hte *ResponseHeaderTimeoutError
	if !errors.As(err, &hte) {
		t.Fatalf("err = %T %v, want *ResponseHeaderTimeoutError", err, err)
	}
	if !errors.Is(err, ErrResponseHeaderTimeout) {
		t.Errorf("err does not unwrap to ErrResponseHeaderTimeout: %v", err)
	}
	var ce *ConnectError
	if !errors.As(err, &ce) {
		t.Errorf("err should still be a *ConnectError for callers that classify unreachable servers: %v", err)
	}
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Errorf("err should report Timeout() = true: %v", err)
	}
	if hte.Retried || hte.Attempts != 1 || hte.Method != http.MethodPost {
		t.Errorf("hte = %+v, want one unretried POST attempt", hte)
	}
	host := s.srv.Listener.Addr().String()
	msg := err.Error()
	for _, want := range []string{host, "not retried", "check the server"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should contain %q", msg, want)
		}
	}
	if n := len(s.seen()); n != 1 {
		t.Fatalf("server saw %d requests, want exactly 1 (a write is never replayed)", n)
	}
	if fired != 0 {
		t.Errorf("OnStallRetry fired %d times for a write", fired)
	}
	if elapsed < 2*testHeaderTimeout || elapsed > 2*testHeaderTimeout+time.Second {
		t.Errorf("elapsed %v; the write should fail at the overall timeout (%v)", elapsed, 2*testHeaderTimeout)
	}
}

// TestASlowWriteIsNotCutOffByTheHeaderTimeout: bd serve answers a write only
// after it commits, so a write whose headers take longer than the header
// timeout is a slow but healthy write, and must complete.
func TestASlowWriteIsNotCutOffByTheHeaderTimeout(t *testing.T) {
	c, rec := newTestClient(t, Options{ResponseHeaderTimeout: testHeaderTimeout}, nil, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		time.Sleep(3 * testHeaderTimeout)
		_, _ = w.Write([]byte(`{}`))
	})
	err := c.Do(ctx(t), Request{Op: OpCreateIssue, Method: http.MethodPost, Path: PathIssues,
		Body: map[string]any{"title": "t"}}, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if n := rec.count(); n != 1 {
		t.Errorf("server saw %d requests, want 1", n)
	}
}

func TestAReadThatStallsTwiceGivesUpAfterOneRetry(t *testing.T) {
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)
	c, err := New(base, nil, Options{HTTPClient: srv.Client(), ResponseHeaderTimeout: testHeaderTimeout})
	if err != nil {
		t.Fatal(err)
	}
	err = c.Do(ctx(t), Request{Op: OpGetContext, Method: http.MethodGet, Path: PathContext}, nil)
	var hte *ResponseHeaderTimeoutError
	if !errors.As(err, &hte) {
		t.Fatalf("err = %v, want *ResponseHeaderTimeoutError", err)
	}
	if !hte.Retried || hte.Attempts != 2 {
		t.Errorf("hte = %+v, want 2 attempts with the retry recorded", hte)
	}
	mu.Lock()
	defer mu.Unlock()
	if n != 2 {
		t.Errorf("server saw %d requests, want 2", n)
	}
}

func TestAResetConnectionOnAReadRetriesOnANewConnection(t *testing.T) {
	s := newStallServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	// The first connection is reset (RST, not FIN) instead of hanging.
	s.hang = func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetLinger(0)
		}
		_ = conn.Close()
	}
	var events []StallRetry
	c := s.client(t, Options{ResponseHeaderTimeout: testHeaderTimeout, OnStallRetry: func(e StallRetry) { events = append(events, e) }})
	var out map[string]any
	if err := c.Do(ctx(t), Request{Op: OpGetContext, Method: http.MethodGet, Path: PathContext}, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if out["ok"] != true {
		t.Fatalf("body = %v", out)
	}
	if len(events) != 1 || events[0].Reason != StallConnectionReset {
		t.Fatalf("events = %+v, want one %q retry", events, StallConnectionReset)
	}
}

func TestASlowStreamingBodyIsNotCutOffByTheHeaderTimeout(t *testing.T) {
	c, rec := newTestClient(t, Options{ResponseHeaderTimeout: testHeaderTimeout}, nil, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true`)
		w.(http.Flusher).Flush()
		// Keep streaming for well past the header timeout.
		for range 8 {
			time.Sleep(testHeaderTimeout / 2)
			_, _ = io.WriteString(w, " ")
			w.(http.Flusher).Flush()
		}
		_, _ = io.WriteString(w, `}`)
	})
	start := time.Now()
	var out map[string]any
	if err := c.Do(ctx(t), Request{Op: OpGetContext, Method: http.MethodGet, Path: PathContext}, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if out["ok"] != true {
		t.Fatalf("body = %v", out)
	}
	if elapsed := time.Since(start); elapsed < 3*testHeaderTimeout {
		t.Errorf("elapsed %v: the body should have streamed past the header timeout", elapsed)
	}
	if n := rec.count(); n != 1 {
		t.Errorf("server saw %d requests, want 1 (no retry for a slow body)", n)
	}
}

func TestTheDefaultHeaderTimeoutUndercutsTheOverallTimeout(t *testing.T) {
	if DefaultResponseHeaderTimeout <= 0 || DefaultResponseHeaderTimeout >= DefaultTimeout {
		t.Fatalf("DefaultResponseHeaderTimeout = %v, want it positive and below DefaultTimeout (%v)",
			DefaultResponseHeaderTimeout, DefaultTimeout)
	}
}

func TestANegativeHeaderTimeoutDisablesIt(t *testing.T) {
	c, _ := newTestClient(t, Options{ResponseHeaderTimeout: -1}, nil, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * testHeaderTimeout)
		_, _ = w.Write([]byte(`{}`))
	})
	if c.headerTimeout != 0 {
		t.Fatalf("headerTimeout = %v, want disabled", c.headerTimeout)
	}
	if err := c.Do(ctx(t), Request{Op: OpGetContext, Method: http.MethodGet, Path: PathContext}, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
}
