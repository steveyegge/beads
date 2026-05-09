package doltutil

import (
	"strings"
	"testing"
)

func TestServerDSN_TLSExplicitlyDisabledByDefault(t *testing.T) {
	dsn := ServerDSN{
		Host: "dolt.example.com",
		Port: 3307,
		User: "root",
	}.String()

	// go-sql-driver/mysql v1.8+ defaults to tls=preferred when TLSConfig
	// is empty. Dolt servers without TLS reject this, so we must explicitly
	// disable TLS when not requested. The formatted DSN should contain
	// tls=false (or the equivalent).
	if !strings.Contains(dsn, "tls=false") {
		t.Errorf("DSN should contain tls=false when TLS is not enabled; got %q", dsn)
	}
}

func TestServerDSN_UnixSocket(t *testing.T) {
	dsn := ServerDSN{
		Socket: "/tmp/dolt.sock",
		Host:   "should-be-ignored",
		Port:   9999,
		User:   "root",
	}.String()

	if !strings.Contains(dsn, "unix") {
		t.Errorf("DSN should use unix network; got %q", dsn)
	}
	if !strings.Contains(dsn, "/tmp/dolt.sock") {
		t.Errorf("DSN should contain socket path; got %q", dsn)
	}
	// Host:Port should not appear in the DSN address
	if strings.Contains(dsn, "should-be-ignored") || strings.Contains(dsn, "9999") {
		t.Errorf("DSN should ignore Host/Port when Socket is set; got %q", dsn)
	}
}

func TestServerDSN_UnixSocketHonorsTLS(t *testing.T) {
	// TLS over unix sockets is valid (defense-in-depth, client certs).
	// The DSN should respect the TLS setting regardless of transport.
	dsn := ServerDSN{
		Socket: "/tmp/dolt.sock",
		User:   "root",
		TLS:    true,
	}.String()

	if !strings.Contains(dsn, "tls=true") {
		t.Errorf("DSN should honor TLS=true even for unix sockets; got %q", dsn)
	}
}

func TestServerDSN_UnixSocketDefaultTLSOff(t *testing.T) {
	dsn := ServerDSN{
		Socket: "/tmp/dolt.sock",
		User:   "root",
	}.String()

	if !strings.Contains(dsn, "tls=false") {
		t.Errorf("DSN should default to tls=false for unix sockets; got %q", dsn)
	}
}

func TestServerDSN_TCPFallbackWithoutSocket(t *testing.T) {
	dsn := ServerDSN{
		// gt-tzm0t: use a remote host so the local-socket fallback doesn't
		// kick in on dev machines that happen to have /tmp/mysql.sock present.
		Host: "remote.example.com",
		Port: 3307,
		User: "root",
	}.String()

	if strings.Contains(dsn, "unix") {
		t.Errorf("DSN should use tcp when Socket is empty; got %q", dsn)
	}
	if !strings.Contains(dsn, "tcp") {
		t.Errorf("DSN should contain tcp network; got %q", dsn)
	}
}

func TestServerDSN_TLSEnabledWhenRequested(t *testing.T) {
	dsn := ServerDSN{
		Host: "hosted.doltdb.com",
		Port: 3307,
		User: "myuser",
		TLS:  true,
	}.String()

	if !strings.Contains(dsn, "tls=true") {
		t.Errorf("DSN should contain tls=true when TLS is enabled; got %q", dsn)
	}
	if strings.Contains(dsn, "tls=false") {
		t.Errorf("DSN should not contain tls=false when TLS is enabled; got %q", dsn)
	}
}

// gt-tzm0t: ServerDSN.String falls back to /tmp/mysql.sock when Socket is
// empty AND host is loopback AND a unix socket is listening at the default
// path. Without this, bd consumers reading metadata.json without an explicit
// dolt_server_socket entry built TCP DSNs against a local Dolt and produced
// TIME_WAIT churn (see internal/storage/doltutil/dsn.go::resolveLocalSocket).

// withMockSocket replaces localSocketPath for the duration of the test.
func withMockSocket(t *testing.T, sockPath string) {
	t.Helper()
	orig := localSocketPath
	localSocketPath = func() string { return sockPath }
	t.Cleanup(func() { localSocketPath = orig })
}

func withNoSocket(t *testing.T) { t.Helper(); withMockSocket(t, "") }

func TestServerDSN_FallsBackToLocalSocket_OnLoopbackHost(t *testing.T) {
	withMockSocket(t, "/tmp/mysql.sock")
	for _, host := range []string{"", "127.0.0.1", "localhost", "::1"} {
		t.Run("host="+host, func(t *testing.T) {
			dsn := ServerDSN{Host: host, Port: 3307, User: "root", Database: "hq"}.String()
			if !strings.Contains(dsn, "@unix(/tmp/mysql.sock)") {
				t.Errorf("expected unix DSN for loopback host %q, got %q", host, dsn)
			}
		})
	}
}

func TestServerDSN_PrefersExplicitSocket(t *testing.T) {
	withMockSocket(t, "/tmp/mysql.sock")
	dsn := ServerDSN{Socket: "/tmp/custom.sock", Host: "127.0.0.1", Port: 3307, User: "root"}.String()
	if !strings.Contains(dsn, "@unix(/tmp/custom.sock)") {
		t.Errorf("expected explicit socket DSN, got %q", dsn)
	}
}

func TestServerDSN_KeepsTCPForNonLoopbackHost(t *testing.T) {
	// Even when a local socket exists, a remote host must dial over TCP.
	withMockSocket(t, "/tmp/mysql.sock")
	dsn := ServerDSN{Host: "10.0.0.5", Port: 3307, User: "root"}.String()
	if !strings.Contains(dsn, "@tcp(10.0.0.5:3307)") {
		t.Errorf("expected TCP DSN for non-loopback host, got %q", dsn)
	}
}

func TestServerDSN_NoSocketAvailable_KeepsTCP(t *testing.T) {
	withNoSocket(t)
	dsn := ServerDSN{Host: "127.0.0.1", Port: 3307, User: "root"}.String()
	if !strings.Contains(dsn, "@tcp(127.0.0.1:3307)") {
		t.Errorf("expected TCP DSN when no socket, got %q", dsn)
	}
}

func TestResolveLocalSocket(t *testing.T) {
	withMockSocket(t, "/tmp/mysql.sock")
	cases := []struct {
		name   string
		socket string
		host   string
		want   string
	}{
		{"explicit socket wins", "/tmp/custom.sock", "127.0.0.1", "/tmp/custom.sock"},
		{"loopback empty host falls back", "", "", "/tmp/mysql.sock"},
		{"loopback 127.0.0.1 falls back", "", "127.0.0.1", "/tmp/mysql.sock"},
		{"loopback localhost falls back", "", "localhost", "/tmp/mysql.sock"},
		{"loopback ::1 falls back", "", "::1", "/tmp/mysql.sock"},
		{"non-loopback skips", "", "10.0.0.5", ""},
		{"remote-looking-loopback skips", "", "192.168.1.1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveLocalSocket(tc.socket, tc.host); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveLocalSocket_NoSocketAvailable(t *testing.T) {
	withNoSocket(t)
	if got := resolveLocalSocket("", "127.0.0.1"); got != "" {
		t.Errorf("expected empty when no socket exists, got %q", got)
	}
}
