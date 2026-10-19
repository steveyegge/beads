package wire

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func dialFailingClient(t *testing.T, base string, dial func(context.Context, string, string) (net.Conn, error)) *Client {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{ResponseHeaderTimeout: testHeaderTimeout}
	if dial != nil {
		opts.HTTPClient = &http.Client{Transport: &http.Transport{DialContext: dial}, Timeout: DefaultTimeout}
	}
	c, err := New(u, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func requireDialError(t *testing.T, err error) *DialError {
	t.Helper()
	var ce *ConnectError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %T %v, want *ConnectError", err, err)
	}
	var de *DialError
	if !errors.As(err, &de) {
		t.Fatalf("err = %v, want a *DialError inside the *ConnectError", err)
	}
	var hte *ResponseHeaderTimeoutError
	if errors.As(err, &hte) {
		t.Fatalf("a request that never connected was reported as a header timeout: %v", err)
	}
	return de
}

// TestAConnectThatTimesOutNamesTheAddressThatNeverAnswered is the filtered
// port: the SYN goes unanswered until the dialer gives up. The error says no
// TCP connection was made to which address, and the read is not retried as a
// stall, because nothing was ever sent.
func TestAConnectThatTimesOutNamesTheAddressThatNeverAnswered(t *testing.T) {
	dials := 0
	addr := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 30699}
	c := dialFailingClient(t, "https://bd.example:30699", func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, &net.OpError{Op: "dial", Net: "tcp", Addr: addr, Err: os.ErrDeadlineExceeded}
	})
	before := StallRetryCount()
	err := c.Do(ctx(t), Request{Op: OpGetContext, Method: http.MethodGet, Path: PathContext}, nil)
	de := requireDialError(t, err)
	if !de.Timeout() {
		t.Errorf("DialError.Timeout() = false for a connect that timed out")
	}
	msg := err.Error()
	for _, want := range []string{"cannot reach bd serve at https://bd.example:30699", "getContext: no TCP connection to 192.0.2.10:30699", "nothing answered", "i/o timeout"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
	if strings.Contains(msg, "response headers") {
		t.Errorf("error %q blames response headers for a connect that never completed", msg)
	}
	if dials != 1 || StallRetryCount() != before {
		t.Errorf("dialed %d times with %d stall retries, want one dial and no retry", dials, StallRetryCount()-before)
	}
}

// TestARefusedConnectSaysNothingIsListening dials a real closed port.
func TestARefusedConnectSaysNothingIsListening(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a Windows refusal is WSAECONNREFUSED, which syscall.ECONNREFUSED does not match; it gets the generic connect wording")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	c := dialFailingClient(t, "http://"+addr, nil)
	err = c.Do(ctx(t), Request{Op: OpListReadyWork, Method: http.MethodGet, Path: PathReady}, nil)
	requireDialError(t, err)
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("errors.Is(err, ECONNREFUSED) = false; the transport's error must stay reachable: %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "TCP connection to "+addr+" refused: nothing is listening on that port") {
		t.Errorf("error %q does not name the refused address", msg)
	}
}

func TestAHostThatDoesNotResolveIsNamed(t *testing.T) {
	c := dialFailingClient(t, "https://nowhere.invalid", func(context.Context, string, string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "nowhere.invalid", IsNotFound: true}}
	})
	err := c.Do(ctx(t), Request{Op: OpListReadyWork, Method: http.MethodGet, Path: PathReady}, nil)
	de := requireDialError(t, err)
	if !de.Resolve {
		t.Errorf("DialError.Resolve = false for a name that did not resolve")
	}
	if msg := err.Error(); !strings.Contains(msg, "cannot resolve nowhere.invalid: no such host") {
		t.Errorf("error %q does not name the unresolved host", msg)
	}
}

// TestACanceledDialIsTheCallersOwn: the caller giving up mid-connect is not a
// connect failure to explain.
func TestACanceledDialIsTheCallersOwn(t *testing.T) {
	c, cancel := context.WithCancel(context.Background())
	client := dialFailingClient(t, "https://bd.example", func(ctx context.Context, _, _ string) (net.Conn, error) {
		cancel()
		<-ctx.Done()
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: ctx.Err()}
	})
	err := client.Do(c, Request{Op: OpListReadyWork, Method: http.MethodGet, Path: PathReady}, nil)
	var de *DialError
	if errors.As(err, &de) {
		t.Fatalf("a dial the caller canceled was reported as a connect failure: %v", err)
	}
}

// TestADialSlowerThanTheHeaderTimeoutIsStillAConnectFailure: the header timer
// starts once the request is written, so a connect that takes longer than it
// and then fails is reported as the connect failure, once, not as missing
// headers retried on another connection.
func TestADialSlowerThanTheHeaderTimeoutIsStillAConnectFailure(t *testing.T) {
	dials := 0
	addr := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 30699}
	c := dialFailingClient(t, "https://bd.example:30699", func(ctx context.Context, _, _ string) (net.Conn, error) {
		dials++
		select {
		case <-time.After(2 * testHeaderTimeout):
		case <-ctx.Done():
			return nil, &net.OpError{Op: "dial", Net: "tcp", Addr: addr, Err: ctx.Err()}
		}
		return nil, &net.OpError{Op: "dial", Net: "tcp", Addr: addr, Err: os.ErrDeadlineExceeded}
	})
	before := StallRetryCount()
	err := c.Do(ctx(t), Request{Op: OpListReadyWork, Method: http.MethodGet, Path: PathReady}, nil)
	requireDialError(t, err)
	if dials != 1 || StallRetryCount() != before {
		t.Errorf("dialed %d times with %d stall retries, want one dial and no retry", dials, StallRetryCount()-before)
	}
}
