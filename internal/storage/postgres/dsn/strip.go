// Package dsn provides Postgres DSN parsing and credential-stripping helpers
// for bd's persistence layer. The stripped form is what lands in
// metadata.json; the password is recovered at runtime from the
// BEADS_POSTGRES_PASSWORD env var (or PG-native sources like .pgpass).
package dsn

import (
	"errors"
	"net"
	"net/url"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
)

// Strip parses the DSN, removes the password component, and re-marshals it
// for persistence. Both URI form ("postgres://user:pw@host/db") and keyword
// form ("password=pw user=...") are accepted — pgconn.ParseConfig normalizes
// both.
//
// On parse error the wrapped pgconn error is intentionally dropped because
// pgconn's Error() text echoes the raw input — preserving it would surface
// credential-bearing strings in stderr / logs.
func Strip(rawDSN string) (string, error) {
	if rawDSN == "" {
		return "", errors.New("postgres: empty connection string")
	}
	cfg, err := pgconn.ParseConfig(rawDSN)
	if err != nil {
		return "", errors.New("postgres: invalid connection string")
	}
	cfg.Password = ""
	return ConfigToConnString(cfg), nil
}

// Compose returns a complete DSN by injecting password into a stripped DSN.
// If password is empty, returns the stripped form unchanged so PG can fall
// through to .pgpass / IAM / peer auth.
func Compose(strippedDSN, password string) string {
	if password == "" {
		return strippedDSN
	}
	cfg, err := pgconn.ParseConfig(strippedDSN)
	if err != nil {
		// Pass through; the postgres factory's own ParseConfig will surface
		// a clean error with redaction.
		return strippedDSN
	}
	cfg.Password = password
	return ConfigToConnString(cfg)
}

// ConfigToConnString re-marshals a *pgconn.Config back into a URI-form DSN.
// pgx/v5 does not export an inverse for ParseConfig; this helper composes
// the URI from the well-known fields. Round-trip stable for inputs that
// don't carry exotic libpq extensions (covered by Strip's test suite).
//
// The output preserves whatever password is on cfg.Password — Strip clears
// it before calling, Compose sets it before calling.
func ConfigToConnString(cfg *pgconn.Config) string {
	if cfg == nil {
		return ""
	}
	u := url.URL{Scheme: "postgres"}
	if cfg.User != "" {
		if cfg.Password != "" {
			u.User = url.UserPassword(cfg.User, cfg.Password)
		} else {
			u.User = url.User(cfg.User)
		}
	}
	host := cfg.Host
	if cfg.Port != 0 {
		u.Host = net.JoinHostPort(host, strconv.Itoa(int(cfg.Port)))
	} else {
		u.Host = host
	}
	if cfg.Database != "" {
		u.Path = "/" + cfg.Database
	}
	q := url.Values{}
	if len(cfg.RuntimeParams) > 0 {
		keys := make([]string, 0, len(cfg.RuntimeParams))
		for k := range cfg.RuntimeParams {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			q.Set(k, cfg.RuntimeParams[k])
		}
	}
	if _, ok := cfg.RuntimeParams["sslmode"]; !ok {
		// pgconn collapses sslmode into TLSConfig and removes it from
		// RuntimeParams. Reconstruct an approximation so the persisted
		// form survives the round-trip: nil TLSConfig ⇒ sslmode=disable
		// (matches pgconn semantics for sslmode=disable inputs);
		// non-nil ⇒ sslmode=require (cert paths are not recoverable from
		// *tls.Config so we cannot do better than the pgconn-default).
		if cfg.TLSConfig == nil {
			q.Set("sslmode", "disable")
		} else {
			q.Set("sslmode", "require")
		}
	}
	if encoded := q.Encode(); encoded != "" {
		u.RawQuery = encoded
	}
	return u.String()
}
