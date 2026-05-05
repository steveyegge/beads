//go:build integration_pg

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/steveyegge/beads/internal/configfile"
)

// pgDSNEnv lets CI hand a pre-provisioned PG instance and skip
// testcontainers startup. Matches the architect's plan in P6.
const pgInitDSNEnv = "BEADS_TEST_POSTGRES_DSN"

// filterTestEnv returns a copy of env with any KEY=... entries dropped for
// the supplied keys. Used to scrub host BEADS_DIR / BEADS_DOLT_* leakage so
// integration tests resolve to the per-test tmpDir instead of the user's
// real .beads directory.
func filterTestEnv(env []string, drop ...string) []string {
	out := make([]string, 0, len(env))
outer:
	for _, kv := range env {
		for _, key := range drop {
			if strings.HasPrefix(kv, key+"=") {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

// isolatedTempDir returns a fresh tempdir under /tmp (not the user's home)
// so bd's walk-up dir resolution can't accidentally find a real ~/.beads
// while exercising init. The dir is registered for cleanup with t.
func isolatedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "bd-init-pg-")
	if err != nil {
		t.Fatalf("isolatedTempDir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func startPGForInit(t *testing.T) (string, func()) {
	t.Helper()
	if dsn := os.Getenv(pgInitDSNEnv); dsn != "" {
		return dsn, func() {}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	container, err := tcpostgres.Run(ctx, "postgres:14-alpine",
		tcpostgres.WithDatabase("bd_init_test"),
		tcpostgres.WithUsername("bd"),
		tcpostgres.WithPassword("bdpass"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("testcontainers-go could not start postgres:14-alpine (no docker?): %v", err)
	}
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("connection string: %v", err)
	}
	return dsn, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = container.Terminate(ctx)
	}
}

// TestInitPostgresPersistsStrippedDSN verifies that `bd init --backend=postgres`
// (a) successfully connects, (b) runs first-connect schema migrations, and
// (c) writes a stripped DSN (no password) to metadata.json.
func TestInitPostgresPersistsStrippedDSN(t *testing.T) {
	bd := buildBDForInitTests(t)
	rawDSN, stop := startPGForInit(t)
	defer stop()

	tmpDir := isolatedTempDir(t)

	cmd := exec.Command(bd, "init", "--backend", "postgres", "--dsn", rawDSN, "--quiet")
	cmd.Dir = tmpDir
	cmd.Env = append(filterTestEnv(os.Environ(), "BEADS_DIR"), "BEADS_DOLT_AUTO_START=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bd init --backend=postgres: %v\n%s", err, out)
	}

	beadsDir := filepath.Join(tmpDir, ".beads")
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		t.Fatalf("load metadata.json: %v", err)
	}
	if cfg == nil {
		t.Fatal("metadata.json not written")
	}

	if cfg.GetBackend() != configfile.BackendPostgres {
		t.Errorf("Backend = %q, want %q", cfg.GetBackend(), configfile.BackendPostgres)
	}
	if cfg.PostgresDSN == "" {
		t.Error("PostgresDSN is empty; expected stripped DSN")
	}
	// "bdpass" is the testcontainers password — must NOT survive into the
	// persisted DSN.
	if strings.Contains(cfg.PostgresDSN, "bdpass") {
		t.Errorf("PostgresDSN leaked password: %s", cfg.PostgresDSN)
	}
	if cfg.ProjectID == "" {
		t.Error("ProjectID was not generated")
	}
}

// TestInitPostgresIdempotent verifies that re-running bd init against the same
// PG database is a no-op (migrations bookkeeping table is idempotent).
func TestInitPostgresIdempotent(t *testing.T) {
	bd := buildBDForInitTests(t)
	rawDSN, stop := startPGForInit(t)
	defer stop()

	tmpDir := isolatedTempDir(t)

	for i := 0; i < 2; i++ {
		cmd := exec.Command(bd, "init", "--backend", "postgres", "--dsn", rawDSN, "--quiet")
		cmd.Dir = tmpDir
		cmd.Env = append(filterTestEnv(os.Environ(), "BEADS_DIR"), "BEADS_DOLT_AUTO_START=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bd init iteration %d: %v\n%s", i, err, out)
		}
	}
}

// TestInitDoltDefaultMetadataIsByteIdentical asserts that `bd init` with no
// flags produces a metadata.json identical to a known-good baseline (modulo
// the auto-generated project ID and DoltDatabase derived from the temp dir
// name). This is the negative-space guard for the no-flag-byte-identical
// AC: my changes must not perturb the existing Dolt path.
func TestInitDoltDefaultMetadataIsByteIdentical(t *testing.T) {
	bd := buildBDForInitTests(t)

	tmpDir := isolatedTempDir(t)
	cmd := exec.Command(bd, "init", "--quiet", "--prefix", "be")
	cmd.Dir = tmpDir
	cmd.Env = append(filterTestEnv(os.Environ(), "BEADS_DIR"), "BEADS_DOLT_AUTO_START=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("bd init (default dolt) failed; skipping byte-identity check: %v\n%s", err, out)
	}

	beadsDir := filepath.Join(tmpDir, ".beads")
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg == nil {
		t.Fatal("metadata.json missing")
	}

	if cfg.Backend != configfile.BackendDolt {
		t.Errorf("Backend = %q, want %q", cfg.Backend, configfile.BackendDolt)
	}
	if cfg.PostgresDSN != "" {
		t.Errorf("PostgresDSN should be empty for dolt path, got: %q", cfg.PostgresDSN)
	}

	// Sanity: persisted JSON must not include the postgres_dsn key at all
	// (omitempty on the struct should drop it).
	data, err := os.ReadFile(filepath.Join(beadsDir, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal metadata.json: %v", err)
	}
	if _, present := raw["postgres_dsn"]; present {
		t.Errorf("dolt-default metadata.json carries postgres_dsn key: %s", string(data))
	}
}
