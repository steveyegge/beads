package doltutil

import (
	"fmt"
	"os"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

// DefaultDoltSocketPath is Dolt's source default unix socket path. Dolt listens
// here regardless of port unless config.yaml sets `socket:` explicitly. Used as
// a fallback when ServerDSN.Socket is empty and the host is loopback — see
// resolveLocalSocket below.
const DefaultDoltSocketPath = "/tmp/mysql.sock"

// localSocketPath is a var (not const) so unit tests can swap it for a temp-dir
// socket without depending on a real Dolt server. Returns DefaultDoltSocketPath
// when it is currently a unix socket, or "" otherwise.
var localSocketPath = func() string {
	info, err := os.Stat(DefaultDoltSocketPath)
	if err != nil {
		return ""
	}
	if info.Mode()&os.ModeSocket == 0 {
		return ""
	}
	return DefaultDoltSocketPath
}

// isLocalLoopback reports whether host is empty or a loopback alias.
// Unix-socket fallback is only safe when the caller would otherwise dial back
// to localhost over TCP — remote hosts must keep TCP semantics.
func isLocalLoopback(host string) bool {
	return host == "" || host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// resolveLocalSocket returns the local Dolt unix socket path when the DSN
// would otherwise dial loopback TCP and a unix socket exists at the default
// path. Returns "" otherwise.
//
// Rationale (gt-tzm0t): bd settings populated from a metadata.json missing
// `dolt_server_socket` build TCP DSNs even when a local Dolt is listening on
// /tmp/mysql.sock. Each TCP close leaves a TIME_WAIT entry on macOS lasting
// ~30s (2*MSL with MSL=15s); on busy rigs the count climbs past port-monitor
// alert thresholds. Unix-socket transport bypasses TIME_WAIT entirely.
//
// Conservative semantics: the fallback only kicks in when (a) Socket is unset
// AND (b) the host is local loopback AND (c) the default socket path is a
// listening unix socket. Remote setups, custom socket paths, and Windows are
// unaffected.
func resolveLocalSocket(socket, host string) string {
	if socket != "" {
		return socket
	}
	if !isLocalLoopback(host) {
		return ""
	}
	return localSocketPath()
}

// ServerDSN holds connection parameters for building a MySQL DSN to a Dolt server.
// All DSNs built with this struct set parseTime=true and multiStatements=true.
type ServerDSN struct {
	Socket   string // Unix domain socket path; when set, Net="unix" and Host/Port are ignored
	Host     string
	Port     int
	User     string
	Password string        //nolint:gosec // G117: MySQL DSN password field; required by the connection-string builder, not serialized as JSON
	Database string        // optional; empty connects without selecting a database
	Timeout  time.Duration // connect timeout; 0 defaults to 5s
	TLS      bool
}

// String builds the MySQL DSN string. Always sets parseTime=true,
// multiStatements=true, allowNativePasswords=true, and a connect timeout.
func (d ServerDSN) String() string {
	timeout := d.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	net := "tcp"
	addr := fmt.Sprintf("%s:%d", d.Host, d.Port)
	// gt-tzm0t: when Socket is unset and host is loopback, prefer the local
	// Dolt unix socket if one is listening. Avoids TIME_WAIT churn from
	// short-lived bd processes against a local server.
	if sock := resolveLocalSocket(d.Socket, d.Host); sock != "" {
		net = "unix"
		addr = sock
	}

	cfg := mysql.Config{
		User:                 d.User,
		Passwd:               d.Password,
		Net:                  net,
		Addr:                 addr,
		DBName:               d.Database,
		ParseTime:            true,
		MultiStatements:      true,
		Timeout:              timeout,
		AllowNativePasswords: true,
	}
	if d.TLS {
		cfg.TLSConfig = "true"
	} else {
		// go-sql-driver/mysql v1.8+ defaults to tls=preferred when TLSConfig
		// is empty. Dolt servers without TLS reject preferred-mode negotiation
		// with "TLS requested but server does not support TLS". Explicitly
		// disable TLS so connections work against non-TLS Dolt instances.
		cfg.TLSConfig = "false"
	}

	return cfg.FormatDSN()
}
