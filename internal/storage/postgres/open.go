package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/steveyegge/beads/internal/storage"
)

const (
	defaultMaxConns        = 10
	defaultMinConns        = 0
	defaultMaxConnLifetime = time.Hour
	defaultMaxConnIdleTime = 30 * time.Minute
	minServerVersionNum    = 140000
)

// allowedDSNParams enumerates the DSN query parameters the driver accepts.
// Anything else returns ErrUnknownDSNParam — we fail loudly on typo'd pool
// tunings rather than letting them be silently ignored.
var allowedDSNParams = map[string]bool{
	// libpq-style runtime params
	"host":             true,
	"port":             true,
	"dbname":           true,
	"user":             true,
	"password":         true,
	"sslmode":          true,
	"sslrootcert":      true,
	"sslcert":          true,
	"sslkey":           true,
	"application_name": true,
	"search_path":      true,
	"connect_timeout":  true,
	"options":          true,

	// pgxpool-specific tuning
	"pool_max_conns":                true,
	"pool_min_conns":                true,
	"pool_max_conn_lifetime":        true,
	"pool_max_conn_lifetime_jitter": true,
	"pool_max_conn_idle_time":       true,
	"pool_health_check_period":      true,
}

// openStore creates a pool, runs first-connect schema migrations under an
// advisory lock, and returns the resulting *PostgresStore.
func openStore(ctx context.Context, cfg storage.ConnectionConfig) (*PostgresStore, error) {
	if cfg.DSN == "" {
		return nil, errors.New("postgres: missing DSN (set --dsn or the appropriate metadata.json field)")
	}

	if err := validateDSNParams(cfg.DSN); err != nil {
		return nil, err
	}

	pgxCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, wrapErr("parse dsn", err)
	}

	// Apply pool defaults that ParseConfig leaves at zero values when not
	// in the DSN.
	if pgxCfg.MaxConns == 0 {
		pgxCfg.MaxConns = defaultMaxConns
	}
	if pgxCfg.MinConns < 0 {
		pgxCfg.MinConns = defaultMinConns
	}
	if pgxCfg.MaxConnLifetime == 0 {
		pgxCfg.MaxConnLifetime = defaultMaxConnLifetime
	}
	if pgxCfg.MaxConnIdleTime == 0 {
		pgxCfg.MaxConnIdleTime = defaultMaxConnIdleTime
	}

	// Force UTC on every new connection so NOW()/CURRENT_TIMESTAMP write the
	// UTC wall clock into our timezone-naive TIMESTAMP columns. Round-trip
	// parity with Dolt depends on this.
	pgxCfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET TIME ZONE 'UTC'")
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		return nil, wrapErr("new pool", err)
	}

	store := &PostgresStore{pool: pool}

	if err := store.checkVersion(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	if err := store.runMigrations(ctx, cfg.ReadOnly); err != nil {
		pool.Close()
		return nil, err
	}

	return store, nil
}

// validateDSNParams parses cfg.DSN as a URL or libpq key=value string and
// rejects any query parameter not in the allowlist.
func validateDSNParams(dsn string) error {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		// URL form: keys live in the query string.
		u, err := url.Parse(dsn)
		if err != nil {
			return wrapErr("validate dsn", err)
		}
		for k := range u.Query() {
			if !allowedDSNParams[strings.ToLower(k)] {
				return ErrUnknownDSNParam{Name: k}
			}
		}
		return nil
	}
	// libpq key=value form: scan for `key=` tokens.
	for _, kv := range strings.Fields(dsn) {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		key := strings.ToLower(kv[:eq])
		if !allowedDSNParams[key] {
			return ErrUnknownDSNParam{Name: kv[:eq]}
		}
	}
	return nil
}

// checkVersion confirms the connected server is PG14 or newer.
func (s *PostgresStore) checkVersion(ctx context.Context) error {
	var verStr string
	if err := s.pool.QueryRow(ctx, "SHOW server_version_num").Scan(&verStr); err != nil {
		return wrapErr("server version check", err)
	}
	ver, err := strconv.Atoi(strings.TrimSpace(verStr))
	if err != nil {
		return wrapErr("parse server version", err)
	}
	if ver < minServerVersionNum {
		return ErrUnsupportedVersion{ServerVersionNum: ver}
	}
	return nil
}

// hostInfo extracts a non-credential identifier for error messages.
// Currently unused but kept for follow-up use (e.g. richer error wrapping).
func hostInfo(cfg *pgxpool.Config) string {
	if cfg == nil || cfg.ConnConfig == nil {
		return ""
	}
	return fmt.Sprintf("host=%s port=%d database=%s user=%s",
		cfg.ConnConfig.Host, cfg.ConnConfig.Port,
		cfg.ConnConfig.Database, cfg.ConnConfig.User)
}
