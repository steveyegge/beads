package util

import (
	"strings"
	"testing"
)

func withMockSocket(t *testing.T, sockPath string) {
	t.Helper()
	orig := localSocketPath
	localSocketPath = func() string { return sockPath }
	t.Cleanup(func() { localSocketPath = orig })
}

func TestDoltServerDSN_FallsBackToLocalSocket(t *testing.T) {
	withMockSocket(t, "/tmp/mysql.sock")
	d := DoltServerDSN{Host: "127.0.0.1", Port: 3307, User: "root", Database: "hq"}
	got := d.String()
	if !strings.Contains(got, "@unix(/tmp/mysql.sock)") {
		t.Errorf("expected unix DSN for loopback host with socket present, got %q", got)
	}
}

func TestDoltServerDSN_TCPWhenNoSocket(t *testing.T) {
	withMockSocket(t, "")
	d := DoltServerDSN{Host: "127.0.0.1", Port: 3307, User: "root", Database: "hq"}
	got := d.String()
	if !strings.Contains(got, "@tcp(127.0.0.1:3307)") {
		t.Errorf("expected TCP DSN when no socket, got %q", got)
	}
}

func TestDoltServerDSN_NonLoopbackKeepsTCP(t *testing.T) {
	withMockSocket(t, "/tmp/mysql.sock")
	d := DoltServerDSN{Host: "10.0.0.5", Port: 3307, User: "root", Database: "hq"}
	got := d.String()
	if !strings.Contains(got, "@tcp(10.0.0.5:3307)") {
		t.Errorf("expected TCP for non-loopback host, got %q", got)
	}
}
