//go:build cgo

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/steveyegge/beads/internal/storage/dbproxy/pidfile"
	"github.com/steveyegge/beads/internal/storage/dbproxy/proxy"
	"github.com/steveyegge/beads/internal/testutil"
	"github.com/steveyegge/beads/internal/types"
)

// requireProxiedServerEnv is the per-test gate for the proxied-server
// integration suite. Skips unless BEADS_TEST_PROXIED_SERVER=1 is set AND
// the dolt binary is on PATH. CI sets the env var and installs dolt; local
// developers opt in explicitly.
func requireProxiedServerEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("BEADS_TEST_PROXIED_SERVER") != "1" {
		t.Skip("set BEADS_TEST_PROXIED_SERVER=1 to run proxied-server integration tests")
	}
	testutil.RequireDoltBinary(t)
}

// bdProxiedEnv returns the env for a `bd` subprocess running against a
// proxied-server project. Compared to bdEnv: BEADS_DOLT_AUTO_START is left
// enabled (the proxied path needs to spawn its dolt server), BEADS_NO_DAEMON
// stays set so the bd process doesn't fork an unrelated daemon, and HOME is
// scoped to the test tempdir to keep .git, .config, etc. isolated.
//
// Like bdEnv we strip pre-existing BEADS_* vars so test runs don't inherit
// the developer's environment.
func bdProxiedEnv(dir string) []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "BEADS_") {
			continue
		}
		env = append(env, e)
	}
	return append(env,
		"HOME="+dir,
		"BEADS_DOLT_PROXIED_SERVER=1",
		"BEADS_NO_DAEMON=1",
	)
}

// bdProxiedRun runs `bd <args>` against a proxied project. No retry — the
// proxy serializes writes through a single dolt sql-server, so we expect
// no contention. Returns stdout on success, combined stdout/stderr on
// failure (so test assertions can match against either stream).
func bdProxiedRun(t *testing.T, bd, dir string, args ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(bd, args...)
	cmd.Dir = dir
	cmd.Env = bdProxiedEnv(dir)
	stdout, stderr, err := runCommandBuffers(t, cmd)
	if err != nil {
		return append(stdout.Bytes(), stderr.Bytes()...), err
	}
	return stdout.Bytes(), nil
}

// bdProxiedCreate runs `bd create --json <args>` against a proxied project
// and returns the parsed issue. Mirrors bdCreate's shape but uses the
// proxied env.
func bdProxiedCreate(t *testing.T, bd, dir string, args ...string) *types.Issue {
	t.Helper()
	fullArgs := append([]string{"create", "--json"}, args...)
	out, err := bdProxiedRun(t, bd, dir, fullArgs...)
	if err != nil {
		t.Fatalf("bd create %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return parseIssueJSON(t, out)
}

// bdProxiedCreateSilent runs `bd create --silent` and returns the ID.
func bdProxiedCreateSilent(t *testing.T, bd, dir string, args ...string) string {
	t.Helper()
	fullArgs := append([]string{"create", "--silent"}, args...)
	out, err := bdProxiedRun(t, bd, dir, fullArgs...)
	if err != nil {
		t.Fatalf("bd create --silent %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// bdProxiedCreateFail runs `bd create` expecting failure. Returns combined
// stdout/stderr for assertion against the error message.
func bdProxiedCreateFail(t *testing.T, bd, dir string, args ...string) string {
	t.Helper()
	fullArgs := append([]string{"create"}, args...)
	out, err := bdProxiedRun(t, bd, dir, fullArgs...)
	if err == nil {
		t.Fatalf("bd create %s should have failed; got:\n%s", strings.Join(args, " "), out)
	}
	return string(out)
}

// bdProxiedShow runs `bd show <id> --json` against a proxied project.
func bdProxiedShow(t *testing.T, bd, dir, id string) *types.Issue {
	t.Helper()
	out, err := bdProxiedRun(t, bd, dir, "show", id, "--json")
	if err != nil {
		t.Fatalf("bd show %s --json failed: %v\n%s", id, err, out)
	}
	return parseIssueJSON(t, out)
}

// proxiedProject carries everything a sub-test needs after init: the
// project directory, the .beads dir, the proxy root (where pidfiles and
// the server config live), and the dolt database name.
type proxiedProject struct {
	dir       string
	beadsDir  string
	proxyRoot string
	database  string
	prefix    string
}

// bdProxiedInit bootstraps a fresh proxied-server project under a tempdir
// and registers cleanup that tears down the spawned proxy + dolt sql-server
// via proxy.Shutdown. Returns the project descriptor. Fatals on failure.
//
// Each call gets its own tempdir and its own proxy root, so sub-tests do
// not share a Dolt server. Spawn cost is ~1-2s per sub-test.
func bdProxiedInit(t *testing.T, bd, prefix string, extraInitArgs ...string) proxiedProject {
	t.Helper()

	dir := t.TempDir()
	initGitRepoAt(t, dir)
	beadsDir := filepath.Join(dir, ".beads")

	// Scope the proxy root to the project tempdir. bd init --proxied-server
	// will create proxyRoot/server_config.yaml on its own (picking a free
	// port via proxy.PickFreePort) and spawn the dolt sql-server inside it.
	proxyRoot := filepath.Join(beadsDir, "proxieddb")

	// Register cleanup BEFORE running init so a failure during init still
	// terminates anything that managed to start.
	t.Cleanup(func() {
		if err := proxy.Shutdown(proxyRoot); err != nil {
			t.Logf("proxy.Shutdown(%s): %v", proxyRoot, err)
		}
	})
	shutdownProxyOnInterrupt(t, proxyRoot)

	args := append([]string{
		"init",
		"--proxied-server",
		"--quiet",
		"--prefix", prefix,
		"--non-interactive",
		"--skip-hooks",
		"--skip-agents",
	}, extraInitArgs...)

	cmd := exec.Command(bd, args...)
	cmd.Dir = dir
	cmd.Env = bdProxiedEnv(dir)
	stdout, stderr, err := runCommandBuffers(t, cmd)
	if err != nil {
		t.Fatalf("bd init --proxied-server failed: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}

	// dolt database name is the prefix with non-identifier chars replaced
	// (matches resolveInitPrefix in init_proxied_server.go).
	database := sanitizePrefixForDB(prefix)

	return proxiedProject{
		dir:       dir,
		beadsDir:  beadsDir,
		proxyRoot: proxyRoot,
		database:  database,
		prefix:    prefix,
	}
}

// sanitizePrefixForDB mirrors resolveInitPrefix's transform: strip leading
// dots, trailing hyphens, swap `.` for `_`, prefix with `bd_` if the result
// doesn't start with a letter or underscore.
func sanitizePrefixForDB(p string) string {
	p = strings.TrimLeft(p, ".")
	p = strings.TrimRight(p, "-")
	p = strings.ReplaceAll(p, ".", "_")
	if len(p) == 0 {
		return "bd"
	}
	c := p[0]
	if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_') {
		p = "bd_" + p
	}
	return p
}

// shutdownProxyOnInterrupt registers a SIGINT/SIGTERM handler that tears
// down the proxy at proxyRoot before letting the test process exit. Without
// this, Ctrl+C during a long-running test leaks orphan dolt processes —
// proxy.Shutdown via t.Cleanup runs on normal exit but not on signal.
//
// Lifted from internal/storage/uow/doltserver_provider_test.go.
func shutdownProxyOnInterrupt(t *testing.T, proxyRoot string) {
	t.Helper()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			_ = proxy.Shutdown(proxyRoot)
			os.Exit(1)
		case <-done:
		}
	}()
	t.Cleanup(func() {
		signal.Stop(ch)
		close(done)
	})
}

// openProxiedDB connects to the proxy's listener as `root` (no password —
// the proxy is loopback-only). The port is read from the proxy's pidfile,
// which is written once the proxy is ready. The returned *sql.DB has a
// cleanup registered on t.
func openProxiedDB(t *testing.T, p proxiedProject) *sql.DB {
	t.Helper()
	pf, err := pidfile.Read(p.proxyRoot, proxy.PIDFileName)
	if err != nil || pf == nil {
		t.Fatalf("read proxy pidfile %s: %v (pf=%v)", p.proxyRoot, err, pf)
	}

	dsn := fmt.Sprintf("root:@tcp(127.0.0.1:%d)/%s?multiStatements=true&parseTime=true",
		pf.Port, p.database)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open mysql %s: %v", dsn, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// One ping to make sure the proxy is actually accepting connections.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping proxied db: %v", err)
	}
	return db
}

// assertProxiedDepExists verifies a dependency row exists in the
// dependencies table. Matches the assertion shape of assertDepExists
// (the embedded equivalent). depends_on_issue_id is NULL when the dep
// references a wisp or external entity, hence the COALESCE.
func assertProxiedDepExists(t *testing.T, db *sql.DB, issueID, dependsOnID string) {
	t.Helper()
	var count int
	err := db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM dependencies WHERE issue_id = ? AND COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external) = ?",
		issueID, dependsOnID).Scan(&count)
	if err != nil {
		t.Fatalf("query dep %s -> %s: %v", issueID, dependsOnID, err)
	}
	if count != 1 {
		t.Fatalf("expected one dep row %s -> %s, got %d", issueID, dependsOnID, count)
	}
}

// assertProxiedDepExistsWithType is the typed variant.
func assertProxiedDepExistsWithType(t *testing.T, db *sql.DB, issueID, dependsOnID, depType string) {
	t.Helper()
	var count int
	err := db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM dependencies WHERE issue_id = ? AND COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external) = ? AND type = ?",
		issueID, dependsOnID, depType).Scan(&count)
	if err != nil {
		t.Fatalf("query dep %s -> %s (%s): %v", issueID, dependsOnID, depType, err)
	}
	if count != 1 {
		t.Fatalf("expected one dep row %s -> %s of type %s, got %d", issueID, dependsOnID, depType, count)
	}
}

// getProxiedLabels returns all labels attached to issueID, in arbitrary
// order. Caller may sort if needed.
func getProxiedLabels(t *testing.T, db *sql.DB, issueID string) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		"SELECT label FROM labels WHERE issue_id = ?", issueID)
	if err != nil {
		t.Fatalf("query labels for %s: %v", issueID, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			t.Fatalf("scan label: %v", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("label rows iter: %v", err)
	}
	return out
}
