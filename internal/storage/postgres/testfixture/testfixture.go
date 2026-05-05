// Package testfixture provides shared Postgres test plumbing for both unit
// (internal/storage/postgres) and end-to-end (cmd/bd, internal/storage/parity)
// tests. The build tag scheme defined in ADR be-l7t.6 layers
// `integration_pg` (and `integration_parity` for the parity scenario) on top
// of the mandatory `gms_pure_go` tag.
//
// Two flavors are exposed:
//
//   - AdminDSN(t) — returns a connection string with cluster-level
//     privileges (the default `postgres` database). Only the helper itself
//     uses this; tests should prefer ForTest.
//   - ForTest(t) — returns a DSN pointed at a fresh per-test database. The
//     database is created on entry and dropped on test cleanup, giving each
//     test full isolation while sharing the underlying PG instance across
//     the test binary's lifetime.
//
// The shared instance comes from one of two sources:
//
//   - BEADS_TEST_POSTGRES_DSN env var — used by CI's service container
//     path. Tests skip on connect failure rather than starting a container.
//   - testcontainers-go starting postgres:14-alpine. Used in local dev.
//     Skips with a friendly message when no docker socket is available.
package testfixture

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// safeDBName guards the per-test database name against accidental SQL
// identifier injection if the dbName construction ever changes. Today
// dbName = "bd_test_" + hex(crypto/rand 8 bytes), which always matches.
var safeDBName = regexp.MustCompile(`^bd_test_[a-f0-9]+$`)

// EnvDSN is consulted before reaching for testcontainers. Set this to a
// cluster-admin DSN (typically pointing at the `postgres` database) so the
// helper can CREATE/DROP per-test databases. Used by CI's service-container
// path where testcontainers' Docker dependency is unavailable.
const EnvDSN = "BEADS_TEST_POSTGRES_DSN"

var (
	sharedOnce sync.Once
	sharedDSN  string
	sharedStop func()
	sharedErr  error
)

// AdminDSN returns the cluster-level admin DSN. Tests typically prefer
// ForTest, which builds on top of this. Returns t.Skipf with a descriptive
// message when neither EnvDSN nor testcontainers is available.
//
// Accepts testing.TB so both *testing.T and *testing.B (benchmark) callers
// share the same fixture. The benchmark suite under benchmark_test.go
// drives the same per-database lifecycle as the integration tests.
func AdminDSN(tb testing.TB) string {
	tb.Helper()
	sharedOnce.Do(initShared)
	if sharedErr != nil {
		tb.Skipf("PG fixture unavailable (no docker socket and no %s env): %v", EnvDSN, sharedErr)
	}
	return sharedDSN
}

// ForTest returns a DSN pointed at a fresh per-test database. The database
// is created via the admin DSN on entry, registered for DROP on test
// cleanup (using PG14's `DROP DATABASE ... WITH (FORCE)` to evict any
// lingering connections), and the returned DSN substitutes the per-test
// database name.
//
// Accepts testing.TB so the same helper drives benchmarks as well as
// tests; *testing.B implements TB, and Cleanup is invoked at end-of-bench
// so each Benchmark gets a fresh database.
func ForTest(tb testing.TB) string {
	tb.Helper()
	admin := AdminDSN(tb)
	dbName := "bd_test_" + randomSuffix(tb)

	if err := createDatabase(admin, dbName); err != nil {
		tb.Fatalf("create database %s: %v", dbName, err)
	}

	tb.Cleanup(func() {
		if err := dropDatabaseForce(admin, dbName); err != nil {
			tb.Logf("teardown: drop database %s: %v", dbName, err)
		}
	})

	return swapDatabaseInDSN(admin, dbName)
}

func initShared() {
	if dsn := os.Getenv(EnvDSN); dsn != "" {
		sharedDSN = dsn
		sharedStop = func() {}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	container, err := tcpostgres.Run(ctx, "postgres:14-alpine",
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("bd"),
		tcpostgres.WithPassword("bd"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		sharedErr = fmt.Errorf("testcontainers postgres:14-alpine: %w", err)
		return
	}
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		sharedErr = fmt.Errorf("connection string: %w", err)
		return
	}
	sharedDSN = dsn
	sharedStop = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = container.Terminate(ctx)
	}
}

// runAdminSQL connects with the cluster admin DSN and issues a single SQL
// statement. pgx requires connection configs to come from pgx.ParseConfig
// (not pgconn.ParseConfig), so the admin DSN is passed straight through
// to pgx.Connect.
func runAdminSQL(adminDSN, sql string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return fmt.Errorf("connect admin: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, sql)
	return err
}

// createDatabase issues a CREATE DATABASE statement against the cluster.
// pgx does not parameterize DDL, so the database name is interpolated; the
// safeDBName regex asserts the constrained form before the fmt happens.
func createDatabase(adminDSN, dbName string) error {
	if !safeDBName.MatchString(dbName) {
		return fmt.Errorf("refusing unsafe db name %q", dbName)
	}
	return runAdminSQL(adminDSN, fmt.Sprintf(`CREATE DATABASE "%s"`, dbName))
}

// dropDatabaseForce uses PG14's `WITH (FORCE)` clause to evict lingering
// connections. v1 of bd's PG backend requires PG14+, so the FORCE syntax
// is always available.
func dropDatabaseForce(adminDSN, dbName string) error {
	if !safeDBName.MatchString(dbName) {
		return fmt.Errorf("refusing unsafe db name %q", dbName)
	}
	return runAdminSQL(adminDSN, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s" WITH (FORCE)`, dbName))
}

// swapDatabaseInDSN re-marshals a parsed DSN with a different database
// name, preserving credentials, runtime params, and TLS configuration.
func swapDatabaseInDSN(adminDSN, dbName string) string {
	cfg, err := pgconn.ParseConfig(adminDSN)
	if err != nil {
		return adminDSN
	}
	cfg.Database = dbName
	return marshalConfig(cfg)
}

// marshalConfig is a minimal pgconn.Config → URI marshaller used by
// swapDatabaseInDSN. Mirrors the helper in internal/storage/postgres/dsn
// but kept private here to avoid an import cycle (the dsn package may
// itself want to consume testfixture in the future).
func marshalConfig(cfg *pgconn.Config) string {
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
		// pgconn parses sslmode into TLSConfig but also leaves nothing in
		// RuntimeParams when sslmode=disable; restore it so DSN consumers
		// downstream don't accidentally re-enable TLS.
		if _, ok := q["sslmode"]; !ok {
			q.Set("sslmode", "disable")
		}
	}
	if encoded := q.Encode(); encoded != "" {
		u.RawQuery = encoded
	}
	return u.String()
}

func randomSuffix(tb testing.TB) string {
	tb.Helper()
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		tb.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(buf[:])
}

// CIOnly returns a non-nil error when EnvDSN is not set. Use from CI
// guard rails that need to assert "this binary should not skip just
// because docker is unavailable" — the PG leg always provides EnvDSN, so
// a missing env there is a configuration bug worth surfacing loudly.
func CIOnly() error {
	if os.Getenv(EnvDSN) == "" {
		return errors.New("BEADS_TEST_POSTGRES_DSN not set")
	}
	return nil
}
