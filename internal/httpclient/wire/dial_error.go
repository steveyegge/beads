package wire

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

// DialError is a request that never reached the server because no connection
// could be opened: the name did not resolve, or the TCP connect failed. It is
// returned inside a *ConnectError, so callers that classify an unreachable
// server keep working, and it unwraps to the transport's own error.
//
// It exists to say which step failed. The transport's text names the request
// (`Get "https://…/v0/beads/context": dial tcp …: i/o timeout`) where the
// operator needs the address that did not answer and what that usually means.
type DialError struct {
	Op string
	// Addr is the address the connect was aimed at (ip:port), or the host
	// name that did not resolve.
	Addr string
	// After is how long the attempt ran before it failed.
	After time.Duration
	// Resolve reports a name lookup failure rather than a failed connect.
	Resolve bool
	Err     error
}

func (e *DialError) Error() string {
	switch {
	case e.Resolve:
		return fmt.Sprintf("%s: cannot resolve %s: %v", e.Op, e.Addr, e.cause())
	case errors.Is(e.Err, syscall.ECONNREFUSED):
		return fmt.Sprintf("%s: TCP connection to %s refused: nothing is listening on that port", e.Op, e.Addr)
	case isTimeout(e.Err):
		return fmt.Sprintf("%s: no TCP connection to %s after %s: nothing answered, so a firewall may be dropping the connection or the host is down (%v)",
			e.Op, e.Addr, e.After.Round(100*time.Millisecond), e.cause())
	default:
		return fmt.Sprintf("%s: TCP connection to %s failed: %v", e.Op, e.Addr, e.cause())
	}
}

// cause is the innermost network error, without the request and address the
// transport's own text repeats.
func (e *DialError) cause() error {
	var dnsErr *net.DNSError
	if errors.As(e.Err, &dnsErr) {
		return errors.New(dnsErr.Err)
	}
	var opErr *net.OpError
	if errors.As(e.Err, &opErr) && opErr.Err != nil {
		return opErr.Err
	}
	return e.Err
}

func (e *DialError) Unwrap() error { return e.Err }

// Timeout reports whether the connect timed out, so net.Error-style checks
// keep classifying it.
func (e *DialError) Timeout() bool { return isTimeout(e.Err) }

// dialFailure classifies a transport error as a resolve or connect failure,
// or returns nil for anything else (a TLS handshake, a reset). host is the
// request's host:port, named when the transport's error carries no address.
func dialFailure(op, host string, err error, after time.Duration) *DialError {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return &DialError{Op: op, Addr: dnsErr.Name, After: after, Resolve: true, Err: err}
	}
	var opErr *net.OpError
	if !errors.As(err, &opErr) || opErr.Op != "dial" {
		return nil
	}
	addr := host
	if opErr.Addr != nil {
		addr = opErr.Addr.String()
	}
	return &DialError{Op: op, Addr: addr, After: after, Err: err}
}
