package main

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

// TestExternalServerUnreachableMessage pins which failures get the --external
// wording. Only a failed dial may: the message claims the server could not be
// reached, so any other open failure (an identity mismatch, a handshake that
// was reset) must keep its own text instead of being reported as "externally
// managed".
func TestExternalServerUnreachableMessage(t *testing.T) {
	refused := errors.New("connect: connection refused")
	dial := &net.OpError{
		Op:   "dial",
		Net:  "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 3307},
		Err:  refused,
	}

	tests := []struct {
		name         string
		err          error
		wantEndpoint string // "" means the error is not rewritten
	}{
		{"dial error", dial, "127.0.0.1:3307"},
		{
			// The store wraps the dial error with %w and its own advice.
			"dial error wrapped like the store does",
			fmt.Errorf("Dolt server unreachable at 127.0.0.1:3307: %w\n\nThe Dolt server may not be running.", dial),
			"127.0.0.1:3307",
		},
		{
			"unix socket dial error",
			&net.OpError{Op: "dial", Net: "unix", Addr: &net.UnixAddr{Name: "/run/dolt.sock", Net: "unix"}, Err: refused},
			"/run/dolt.sock",
		},
		{
			// A name that does not resolve has no address to report.
			"dial error without an address",
			&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("no such host")},
			"the configured endpoint",
		},
		{"read error after connecting", &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}, ""},
		{"not a network error", errors.New("project identity mismatch"), ""},
	}
	// Every rewritten message says the server is declared external and that bd
	// did not start one, and never advises `bd dolt start`.
	requireDeclaredExternal := func(t *testing.T, got string, wants ...string) {
		t.Helper()
		for _, want := range append(wants, "externally managed", "did not start") {
			if !strings.Contains(got, want) {
				t.Errorf("message does not contain %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, "bd dolt start") {
			t.Errorf("message advises `bd dolt start`:\n%s", got)
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := externalServerUnreachableMessage(tt.err)
			if tt.wantEndpoint == "" {
				if got != "" {
					t.Fatalf("message = %q, want none for an error that is not a failed dial", got)
				}
				return
			}
			requireDeclaredExternal(t, got, tt.wantEndpoint)
		})
	}

	// A dial to port 0 is an init that was never told a port: the address is a
	// placeholder, not the server's endpoint, so the message must not offer it
	// as one. The first line quotes the dialled address, so it is cut out
	// before looking.
	t.Run("dial error with no port named", func(t *testing.T) {
		noPort := &net.OpError{
			Op:   "dial",
			Net:  "tcp",
			Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0},
			Err:  refused,
		}
		got := externalServerUnreachableMessage(noPort)
		if got == "" {
			t.Fatal("message = none, want the --external wording for a failed dial")
		}
		requireDeclaredExternal(t, got)
		if strings.Contains(strings.ReplaceAll(got, noPort.Error(), ""), ":0") {
			t.Errorf("message offers port 0 as the endpoint:\n%s", got)
		}
	})
}
