package dsn

import (
	"fmt"
	"net"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
)

// RenderRedacted returns a human-readable "host:port/db" summary of a DSN
// with all credentials stripped. Suitable for log lines and error messages
// where the full URI (including password) must never appear (NFR-4).
//
// The input DSN may include a password; it is stripped via defense-in-depth
// (pgconn.ParseConfig → cfg.Password = "" → ConfigToConnString), so the
// function is safe to call with either a stripped or a full DSN.
//
// Examples:
//
//	"postgres://user:secret@staging.db:5433/mybd?sslmode=disable"  → "staging.db:5433/mybd"
//	"postgres://127.0.0.1:5433/beads_be?sslmode=disable"           → "127.0.0.1:5433/beads_be"
func RenderRedacted(rawDSN string) string {
	cfg, err := pgconn.ParseConfig(rawDSN)
	if err != nil {
		return "(unparseable)"
	}
	// Defense-in-depth: strip password even if the caller passed a full DSN.
	cfg.Password = ""

	host := cfg.Host
	if cfg.Port != 0 {
		host = net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port)))
	}
	if cfg.Database == "" {
		return host
	}
	return fmt.Sprintf("%s/%s", host, cfg.Database)
}

// ParseConnectionTarget extracts the host, port, and database from a DSN.
// The input DSN may include a password; it is stripped before the fields
// are read (defense-in-depth). Returns an error if the DSN cannot be parsed.
//
// Port is 0 when the DSN contains no explicit port.
// Database is "" when the DSN contains no path component.
func ParseConnectionTarget(rawDSN string) (host string, port int, db string, err error) {
	cfg, err := pgconn.ParseConfig(rawDSN)
	if err != nil {
		return "", 0, "", fmt.Errorf("parsing DSN: %w", err)
	}
	// Defense-in-depth: zero out the password so callers cannot accidentally
	// forward a parsed config containing credentials.
	cfg.Password = ""
	return cfg.Host, int(cfg.Port), cfg.Database, nil
}
