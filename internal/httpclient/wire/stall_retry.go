package wire

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"syscall"
	"time"
)

// DefaultResponseHeaderTimeout bounds how long one attempt waits for a
// server's response HEADERS, separately from DefaultTimeout's bound on the
// whole exchange.
//
// It exists for the stuck connection the maintainer-city shadow run found: a
// single HTTP/1.1 connection that went silent while other connections to the
// same serve kept answering, so each request on it sat out the full 60s
// overall timeout. A healthy serve sends headers in milliseconds; 15s is far
// past any honest server's time-to-headers and far short of 60s. Only the
// headers are bounded: once they arrive the timer is stopped, and a slow body
// that keeps streaming is governed by DefaultTimeout (and the caller's
// context) alone.
//
// The timer applies to replayable requests (see idempotent). A write is
// bounded by the client's overall timeout instead, because bd serve answers a
// write only after it commits; see roundTripOnce.
const DefaultResponseHeaderTimeout = 15 * time.Second

// ErrResponseHeaderTimeout is the sentinel a *ResponseHeaderTimeoutError
// unwraps to.
var ErrResponseHeaderTimeout = errors.New("bd serve sent no response headers in time")

// ResponseHeaderTimeoutError is a request whose response headers did not
// arrive within the header timeout. It is returned inside a *ConnectError
// (the server said nothing), so callers that classify an unreachable server
// keep working; errors.As reaches this type for the detail.
type ResponseHeaderTimeoutError struct {
	Op     string
	Method string
	// Host is the server's host[:port], named so the operator knows which
	// server to look at.
	Host  string
	After time.Duration
	// Attempts is how many requests went out: 1 for a write (never
	// replayed), 2 for a read whose retry on a new connection stalled too.
	Attempts int
	// Retried reports whether a retry on a new connection was made.
	Retried bool
	// Replayable reports whether the request was idempotent (so only the
	// caller's own context, not policy, can have stopped a retry).
	Replayable bool
	// Err is the transport's own error, when the client's overall timeout
	// (not the header timer) is what fired.
	Err error
}

func (e *ResponseHeaderTimeoutError) Error() string {
	if !e.Retried && e.Replayable {
		return fmt.Sprintf("%s: no response headers from %s within %s; check the server at %s (is it at its connection limit?)",
			e.Op, e.Host, e.After, e.Host)
	}
	if !e.Retried {
		return fmt.Sprintf("%s: no response headers from %s within %s; the %s was not retried because it is not idempotent (it may or may not have been applied); check the server at %s (is it at its connection limit?)",
			e.Op, e.Host, e.After, e.Method, e.Host)
	}
	return fmt.Sprintf("%s: no response headers from %s within %s on %d attempts (the retry used a new connection); check the server at %s (is it at its connection limit?)",
		e.Op, e.Host, e.After, e.Attempts, e.Host)
}

func (e *ResponseHeaderTimeoutError) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrResponseHeaderTimeout, e.Err}
	}
	return []error{ErrResponseHeaderTimeout}
}

// Timeout reports true, so net.Error-style timeout checks classify it.
func (e *ResponseHeaderTimeoutError) Timeout() bool { return true }

// isTimeout reports whether err is a timeout (net.Error, or the client's own
// Timeout surfacing through *url.Error).
func isTimeout(err error) bool {
	var te interface{ Timeout() bool }
	return errors.As(err, &te) && te.Timeout()
}

// Temporary is part of the net.Error interface.
func (e *ResponseHeaderTimeoutError) Temporary() bool { return true }

// Reasons a StallRetry names.
const (
	StallHeaderTimeout   = "response_header_timeout"
	StallConnectionReset = "connection_reset"
)

// StallRetry describes one retry of an idempotent request on a new connection
// after the first attempt stalled (no headers within the header timeout) or
// had its connection reset. It is reported, never swallowed: a retry that
// hides a sick server is a stall nobody sees.
type StallRetry struct {
	Op     string
	Method string
	Host   string
	Reason string
	// Retry is the 1-based retry number for this request (always 1 today:
	// one retry per request).
	Retry int
	// Total is the process-wide count of stall retries, this one included.
	Total uint64
	// Err is the first attempt's failure.
	Err error
}

func (e StallRetry) String() string {
	return fmt.Sprintf("bd: http %s %s to %s stalled (%s); retry %d on a new connection (%d stall retries in this process): %v",
		e.Op, e.Method, e.Host, e.Reason, e.Retry, e.Total, e.Err)
}

var stallRetries atomic.Uint64

// StallRetryCount is the number of stall retries this process has made.
func StallRetryCount() uint64 { return stallRetries.Load() }

// idempotent reports whether r may be replayed on a new connection: the safe
// methods, or a read carried on POST that says so (Request.Idempotent).
func idempotent(r Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return r.Idempotent
}

// stallReason classifies a failed attempt as retryable on a new connection,
// returning "" when it is not.
func stallReason(err error) string {
	var ce *ConnectError
	if !errors.As(err, &ce) {
		// Only a failure to get an answer is a stall; a credential source's
		// own error that happens to wrap io.EOF is not.
		return ""
	}
	switch {
	case errors.Is(err, ErrResponseHeaderTimeout):
		return StallHeaderTimeout
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return StallConnectionReset
	}
	return ""
}

// headerTimer bounds the wait for response headers on one attempt. It cancels
// the attempt's context when it fires, which makes net/http close that
// connection rather than return it to the idle pool, so a retry can never
// ride it. arrived must be called as soon as Do returns; it reports whether
// the headers won the race (false: the timer fired first).
type headerTimer struct {
	state  atomic.Int32 // 0 waiting, 1 headers arrived, 2 timed out
	timer  *time.Timer
	cancel context.CancelCauseFunc
}

func startHeaderTimer(ctx context.Context, d time.Duration) (context.Context, *headerTimer) {
	attemptCtx, cancel := context.WithCancelCause(ctx)
	h := &headerTimer{cancel: cancel}
	if d > 0 {
		h.timer = time.AfterFunc(d, func() {
			if h.state.CompareAndSwap(0, 2) {
				cancel(ErrResponseHeaderTimeout)
			}
		})
	}
	return attemptCtx, h
}

func (h *headerTimer) arrived() bool {
	if h.timer != nil {
		h.timer.Stop()
	}
	return h.state.CompareAndSwap(0, 1) || h.state.Load() == 1
}

// timedOut reports whether the timer fired.
func (h *headerTimer) timedOut() bool { return h.state.Load() == 2 }

// done releases the attempt's context. Call it once the body has been read.
func (h *headerTimer) done() { h.cancel(nil) }
