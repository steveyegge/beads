package util

import (
	"fmt"
	"os"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

// defaultDoltSocketPath is Dolt's source default unix socket path. Mirrored
// from internal/storage/doltutil/dsn.go (the two packages are siblings and
// would form a cycle if either imported the other).
const defaultDoltSocketPath = "/tmp/mysql.sock"

// localSocketPath is a var (not const) so unit tests can swap it for a temp-dir
// socket without depending on a real Dolt server.
var localSocketPath = func() string {
	info, err := os.Stat(defaultDoltSocketPath)
	if err != nil {
		return ""
	}
	if info.Mode()&os.ModeSocket == 0 {
		return ""
	}
	return defaultDoltSocketPath
}

func isLocalLoopback(host string) bool {
	return host == "" || host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// resolveLocalSocket returns the local Dolt unix socket path when the DSN
// would otherwise dial loopback TCP and a unix socket exists. See
// internal/storage/doltutil/dsn.go for the rationale (gt-tzm0t).
func resolveLocalSocket(socket, host string) string {
	if socket != "" {
		return socket
	}
	if !isLocalLoopback(host) {
		return ""
	}
	return localSocketPath()
}

type DoltServerDSN struct {
	Socket      string
	Host        string
	Port        int
	User        string
	Password    string //nolint:gosec // G117: MySQL DSN password field; required by the connection-string builder, not serialized as JSON
	Database    string
	Timeout     time.Duration
	TLSRequired bool
	TLSCert     string
	TLSKey      string
}

func (d DoltServerDSN) String() string {
	timeout := d.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	net := "tcp"
	addr := fmt.Sprintf("%s:%d", d.Host, d.Port)
	// gt-tzm0t: when Socket is unset and host is loopback, prefer the local
	// Dolt unix socket if one is listening. Avoids TIME_WAIT churn.
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
	if d.TLSRequired {
		cfg.TLSConfig = "true"
	} else {
		cfg.TLSConfig = "false"
	}

	return cfg.FormatDSN()
}
