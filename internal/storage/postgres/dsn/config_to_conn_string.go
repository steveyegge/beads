package dsn

import (
	"net"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
)

// ConfigToConnString marshals a pgconn.Config into a postgres:// URI.
// Password is included when cfg.Password is non-empty; stripped DSN callers
// should zero it before calling. RuntimeParams (e.g. sslmode) are encoded
// as query parameters. A nil config returns the empty string.
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
	for k, v := range cfg.RuntimeParams {
		q.Set(k, v)
	}
	if cfg.TLSConfig == nil {
		// pgconn parses sslmode=disable into a nil TLSConfig and removes it
		// from RuntimeParams. Restore it so downstream consumers don't
		// accidentally re-enable TLS.
		if _, ok := q["sslmode"]; !ok {
			q.Set("sslmode", "disable")
		}
	}
	if encoded := q.Encode(); encoded != "" {
		u.RawQuery = encoded
	}
	return u.String()
}
