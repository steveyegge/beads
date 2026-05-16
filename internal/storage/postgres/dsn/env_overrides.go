package dsn

import (
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
)

// validSSLModes is the set of values accepted by pgconn for sslmode.
var validSSLModes = map[string]bool{
	"disable":     true,
	"allow":       true,
	"prefer":      true,
	"require":     true,
	"verify-ca":   true,
	"verify-full": true,
}

// ApplyEnvOverrides applies BEADS_POSTGRES_* environment variables to a
// stripped DSN and returns the modified DSN plus a sorted list of field names
// that were overridden. Password is never read or written.
//
// Recognized env vars:
//   - BEADS_POSTGRES_HOST
//   - BEADS_POSTGRES_PORT  (integer; ignored + warns on non-integer)
//   - BEADS_POSTGRES_USER
//   - BEADS_POSTGRES_DATABASE
//   - BEADS_POSTGRES_SSLMODE (one of: disable allow prefer require verify-ca verify-full)
//
// libpq-style vars (DATABASE_URL, PGHOST, etc.) are intentionally NOT read.
func ApplyEnvOverrides(strippedDSN string) (string, []string) {
	cfg, err := pgconn.ParseConfig(strippedDSN)
	if err != nil {
		return strippedDSN, nil
	}
	// Ensure password is never carried through.
	cfg.Password = ""

	var overridden []string

	if v := os.Getenv("BEADS_POSTGRES_HOST"); v != "" {
		cfg.Host = v
		overridden = append(overridden, "host")
	}
	if v := os.Getenv("BEADS_POSTGRES_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p <= 0 || p > 65535 {
			fmt.Fprintf(os.Stderr, "bd: BEADS_POSTGRES_PORT=%q is not a valid port; ignoring\n", v)
		} else {
			cfg.Port = uint16(p)
			overridden = append(overridden, "port")
		}
	}
	if v := os.Getenv("BEADS_POSTGRES_USER"); v != "" {
		cfg.User = v
		overridden = append(overridden, "user")
	}
	if v := os.Getenv("BEADS_POSTGRES_DATABASE"); v != "" {
		cfg.Database = v
		overridden = append(overridden, "database")
	}
	if v := os.Getenv("BEADS_POSTGRES_SSLMODE"); v != "" {
		if !validSSLModes[v] {
			fmt.Fprintf(os.Stderr, "bd: BEADS_POSTGRES_SSLMODE=%q is not a valid sslmode; ignoring\n", v)
		} else {
			// pgconn stores sslmode in RuntimeParams when it is not "disable".
			if cfg.RuntimeParams == nil {
				cfg.RuntimeParams = make(map[string]string)
			}
			if v == "disable" {
				delete(cfg.RuntimeParams, "sslmode")
				cfg.TLSConfig = nil
			} else {
				cfg.RuntimeParams["sslmode"] = v
			}
			overridden = append(overridden, "sslmode")
		}
	}

	sort.Strings(overridden)
	return ConfigToConnString(cfg), overridden
}
