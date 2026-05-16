package dsn

import (
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
