package dsn

import (
	"fmt"
	"net"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
)

// Compose inserts password into a stripped DSN (one without a password) and
// returns a full connection string suitable for pgx.Connect or pgconn.Connect.
// The stripped DSN is parsed with pgconn.ParseConfig and the password is
// injected before re-serializing. If parsing fails the stripped DSN is
// returned unchanged.
//
// Always call ApplyEnvOverrides before Compose so that env-var overrides are
// applied to the stripped DSN before the password is added.
func Compose(strippedDSN, password string) string {
	cfg, err := pgconn.ParseConfig(strippedDSN)
	if err != nil {
		return strippedDSN
	}
	cfg.Password = password
	return ConfigToConnString(cfg)
}

// RenderRedacted returns a human-readable "host:port/db" summary of a
// stripped DSN with no credentials. Use in log lines and error messages
// where the full URI must not appear (NFR-4).
//
// Examples:
//
//	"postgres://user@staging.db:5433/mybd?sslmode=disable"  → "staging.db:5433/mybd"
//	"postgres://127.0.0.1:5433/beads_be?sslmode=disable"    → "127.0.0.1:5433/beads_be"
func RenderRedacted(strippedDSN string) string {
	cfg, err := pgconn.ParseConfig(strippedDSN)
	if err != nil {
		return "(unparseable)"
	}
	if cfg.Port != 0 {
		return fmt.Sprintf("%s/%s", net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))), cfg.Database)
	}
	return fmt.Sprintf("%s/%s", cfg.Host, cfg.Database)
}
