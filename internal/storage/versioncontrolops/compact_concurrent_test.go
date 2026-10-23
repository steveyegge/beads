package versioncontrolops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/steveyegge/beads/internal/testutil"
)

// These tests run Compact against a real `dolt sql-server` with a second
// session writing to main, because the defect they cover is a race between
// sessions that sqlmock cannot reproduce: a commit that reached main after
// the compaction plan was read was deleted by the final reset of main, after
// the writer had already been told it succeeded. Compact now refuses when
// main moved, soft-resets main, and commits the rows the reset kept.

// startCompactTestServer starts a dolt sql-server on a free local port with
// one database holding a table and a few commits, and returns a pool on it.
func startCompactTestServer(t *testing.T) *sql.DB {
	t.Helper()
	testutil.RequireDoltCLIOnly(t)

	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "DOLT_ROOT_PATH="+root, "DOLT_DISABLE_EVENT_FLUSH=true")
	for _, kv := range [][2]string{{"user.name", "compact-test"}, {"user.email", "compact-test@example.com"}} {
		cmd := exec.Command("dolt", "config", "--global", "--add", kv[0], kv[1])
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("dolt config %s: %v\n%s", kv[0], err, out)
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	srv := exec.Command("dolt", "sql-server", "--host", "127.0.0.1", "--port", fmt.Sprint(port), "--data-dir", dataDir, "--socket", filepath.Join(root, "s.sock"))
	srv.Env = env
	logFile, err := os.Create(filepath.Join(root, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	srv.Stdout, srv.Stderr = logFile, logFile
	if err := srv.Start(); err != nil {
		t.Fatalf("start dolt sql-server: %v", err)
	}
	t.Cleanup(func() {
		_ = srv.Process.Kill()
		_ = srv.Wait()
		_ = logFile.Close()
	})

	admin, err := sql.Open("mysql", fmt.Sprintf("root@tcp(127.0.0.1:%d)/", port))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for admin.Ping() != nil {
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(filepath.Join(root, "server.log"))
			t.Fatalf("dolt sql-server did not answer within 30s:\n%s", log)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := admin.Exec("CREATE DATABASE compacttest"); err != nil {
		t.Fatal(err)
	}
	_ = admin.Close()

	db, err := sql.Open("mysql", fmt.Sprintf("root@tcp(127.0.0.1:%d)/compacttest?parseTime=true", port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mustExec(t, db, "CREATE TABLE items (id INT PRIMARY KEY, v VARCHAR(40))")
	mustExec(t, db, "CALL DOLT_COMMIT('-Am', 'create items')")
	for i := 1; i <= 4; i++ {
		mustExec(t, db, "INSERT INTO items VALUES (?, ?)", i, fmt.Sprintf("row %d", i))
		mustExec(t, db, "CALL DOLT_COMMIT('-Am', ?)", fmt.Sprintf("insert row %d", i))
	}
	return db
}

func mustExec(t *testing.T, db DBConn, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// writeOnMain commits one row on main through its own session, as another
// bd process would, and returns the new head of main.
func writeOnMain(t *testing.T, db *sql.DB, id int) string {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	mustExec(t, conn, "INSERT INTO items VALUES (?, 'late write')", id)
	mustExec(t, conn, "CALL DOLT_COMMIT('-Am', 'late write')")
	return mainHead(t, conn)
}

func mainHead(t *testing.T, db DBConn) string {
	t.Helper()
	var h string
	if err := db.QueryRowContext(context.Background(), "SELECT DOLT_HASHOF('main')").Scan(&h); err != nil {
		t.Fatal(err)
	}
	return h
}

// squashAllPlan is the plan `bd compact --days 0` builds: every commit is
// old, nothing is replayed, and the boundary is main's head.
func squashAllPlan(t *testing.T, db DBConn) (initial, boundary string, old int) {
	t.Helper()
	entries, err := Log(context.Background(), db, 0)
	if err != nil {
		t.Fatal(err)
	}
	return entries[len(entries)-1].Hash, entries[0].Hash, len(entries)
}

// hookConn runs before() ahead of each Exec on the compaction session.
type hookConn struct {
	*sql.Conn
	before func(query string)
}

func (c hookConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	c.before(query)
	return c.Conn.ExecContext(ctx, query, args...)
}

func assertRow(t *testing.T, db DBConn, id int, want bool) {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM items AS OF 'main' WHERE id = ?", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if (n == 1) != want {
		t.Fatalf("row %d on main: present=%v, want present=%v", id, n == 1, want)
	}
}

func assertNoTempBranch(t *testing.T, db DBConn) {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM dolt_branches WHERE name = 'compact-tmp'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("compact-tmp branch left behind after a refused compaction")
	}
}

// A write that lands on main while the squashed branch is being built must
// survive: Compact refuses the reset and leaves main where the writer
// put it. This is the loss measured on a copy of a live store (wh-p4ir7).
func TestCompactRefusesResetWhenMainMovedDuringBuild(t *testing.T) {
	db := startCompactTestServer(t)
	ctx := context.Background()
	initial, boundary, old := squashAllPlan(t, db)

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var lateHead string
	fired := false
	hc := hookConn{Conn: conn, before: func(query string) {
		if !fired && query == "CALL DOLT_CHECKOUT('main')" {
			fired = true
			lateHead = writeOnMain(t, db, 100)
		}
	}}

	err = Compact(ctx, hc, initial, boundary, old, nil)
	if !fired {
		t.Fatal("test hook never fired: Compact no longer checks out main before the reset")
	}
	if !errors.Is(err, ErrMainMoved) {
		t.Fatalf("Compact error = %v, want ErrMainMoved", err)
	}
	if !strings.Contains(err.Error(), boundary) || !strings.Contains(err.Error(), lateHead) {
		t.Fatalf("refusal does not name both heads: %v", err)
	}
	if got := mainHead(t, db); got != lateHead {
		t.Fatalf("main head = %s, want the late write's commit %s", got, lateHead)
	}
	assertRow(t, db, 100, true)
	assertNoTempBranch(t, db)
}

// A write that lands between the log read and the start of Compact is in no
// cherry-pick either, so Compact refuses before it creates the temp branch.
func TestCompactRefusesWhenMainMovedAfterPlan(t *testing.T) {
	db := startCompactTestServer(t)
	ctx := context.Background()
	initial, boundary, old := squashAllPlan(t, db)
	lateHead := writeOnMain(t, db, 200)

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	err = Compact(ctx, conn, initial, boundary, old, nil)
	if !errors.Is(err, ErrMainMoved) {
		t.Fatalf("Compact error = %v, want ErrMainMoved", err)
	}
	if got := mainHead(t, db); got != lateHead {
		t.Fatalf("main head = %s, want %s", got, lateHead)
	}
	assertRow(t, db, 200, true)
	assertNoTempBranch(t, db)
}

// Control: with no concurrent write, Compact squashes as before, keeps every
// row and the table's value hash, and replays the recent window.
func TestCompactQuietStillSquashes(t *testing.T) {
	db := startCompactTestServer(t)
	ctx := context.Background()

	entries, err := Log(ctx, db, 0)
	if err != nil {
		t.Fatal(err)
	}
	var hashBefore string
	if err := db.QueryRowContext(ctx, "SELECT DOLT_HASHOF_TABLE('items')").Scan(&hashBefore); err != nil {
		t.Fatal(err)
	}

	// Keep the newest commit as the recent window; squash the rest.
	initial := entries[len(entries)-1].Hash
	boundary := entries[1].Hash
	recent := []string{entries[0].Hash}

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := Compact(ctx, conn, initial, boundary, len(entries)-1, recent); err != nil {
		t.Fatalf("quiet Compact: %v", err)
	}

	after, err := Log(ctx, db, 0)
	if err != nil {
		t.Fatal(err)
	}
	// initial commit + squashed base + one replayed commit
	if len(after) != 3 {
		t.Fatalf("commits after compact = %d, want 3", len(after))
	}
	var hashAfter string
	if err := db.QueryRowContext(ctx, "SELECT DOLT_HASHOF_TABLE('items')").Scan(&hashAfter); err != nil {
		t.Fatal(err)
	}
	if hashAfter != hashBefore {
		t.Fatalf("items value hash changed: %s -> %s", hashBefore, hashAfter)
	}
	for id := 1; id <= 4; id++ {
		assertRow(t, db, id, true)
	}
	assertNoTempBranch(t, db)
	assertCleanStatus(t, db)
}

// The proxied planner reads main's head with MainHead before it reads the
// log. A write that lands between the two reads is in the plan but not at
// the planned head, so CompactAtHead refuses and names both heads.
func TestCompactAtHeadRefusesWriteBetweenHeadAndLogRead(t *testing.T) {
	db := startCompactTestServer(t)
	ctx := context.Background()

	planned := mainHead(t, db)
	lateHead := writeOnMain(t, db, 300)
	initial, boundary, old := squashAllPlan(t, db)
	if boundary != lateHead {
		t.Fatalf("plan boundary = %s, want the late write's commit %s", boundary, lateHead)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = CompactAtHead(ctx, conn, planned, initial, boundary, old, nil)
	if !errors.Is(err, ErrMainMoved) {
		t.Fatalf("CompactAtHead error = %v, want ErrMainMoved", err)
	}
	if !strings.Contains(err.Error(), planned) || !strings.Contains(err.Error(), lateHead) {
		t.Fatalf("refusal does not name both heads: %v", err)
	}
	if got := mainHead(t, db); got != lateHead {
		t.Fatalf("main head = %s, want %s", got, lateHead)
	}
	assertRow(t, db, 300, true)
	assertNoTempBranch(t, db)
}

// When main has not moved but the plan's newest commit is not main's head
// (a log whose date order disagrees with its parent order), CompactAtHead
// refuses with ErrPlanNotAtHead instead of squashing main to an older tree.
func TestCompactAtHeadRefusesPlanNotEndingAtHead(t *testing.T) {
	db := startCompactTestServer(t)
	ctx := context.Background()

	entries, err := Log(ctx, db, 0)
	if err != nil {
		t.Fatal(err)
	}
	head := mainHead(t, db)
	if entries[0].Hash != head {
		t.Fatalf("fixture: newest log entry %s is not main's head %s", entries[0].Hash, head)
	}
	// A plan whose newest commit is the head's parent, as a misordered log
	// would produce.
	initial := entries[len(entries)-1].Hash
	stale := entries[1].Hash

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = CompactAtHead(ctx, conn, head, initial, stale, len(entries)-1, nil)
	if !errors.Is(err, ErrPlanNotAtHead) {
		t.Fatalf("CompactAtHead error = %v, want ErrPlanNotAtHead", err)
	}
	if !strings.Contains(err.Error(), head) || !strings.Contains(err.Error(), stale) {
		t.Fatalf("refusal does not name main's head and the plan's newest commit: %v", err)
	}
	if got := mainHead(t, db); got != head {
		t.Fatalf("main head = %s, want it unchanged at %s", got, head)
	}
	assertRow(t, db, 4, true)
	assertNoTempBranch(t, db)
}

// Control: with the head read before the log and no concurrent write,
// CompactAtHead squashes every commit after the initial one and keeps the
// table's value hash.
func TestCompactAtHeadQuietStillSquashes(t *testing.T) {
	db := startCompactTestServer(t)
	ctx := context.Background()

	planned := mainHead(t, db)
	initial, boundary, old := squashAllPlan(t, db)
	var hashBefore string
	if err := db.QueryRowContext(ctx, "SELECT DOLT_HASHOF_TABLE('items')").Scan(&hashBefore); err != nil {
		t.Fatal(err)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	res, err := CompactAtHead(ctx, conn, planned, initial, boundary, old, nil)
	if err != nil {
		t.Fatalf("quiet CompactAtHead: %v", err)
	}
	if got := mainHead(t, db); res.SquashedHead != got || res.KeptRowsCommit != "" {
		t.Fatalf("result = %+v, want the squashed head %s and no kept-rows commit on a quiet store", res, got)
	}

	after, err := Log(ctx, db, 0)
	if err != nil {
		t.Fatal(err)
	}
	// initial commit + squashed base
	if len(after) != 2 {
		t.Fatalf("commits after compact = %d, want 2", len(after))
	}
	var hashAfter string
	if err := db.QueryRowContext(ctx, "SELECT DOLT_HASHOF_TABLE('items')").Scan(&hashAfter); err != nil {
		t.Fatal(err)
	}
	if hashAfter != hashBefore {
		t.Fatalf("items value hash changed: %s -> %s", hashBefore, hashAfter)
	}
	assertNoTempBranch(t, db)
	assertCleanStatus(t, db)
}

// assertCleanStatus fails unless main's working set equals its head: a store
// left dirty by a compaction is what the compactor's quarantine reads as drift.
func assertCleanStatus(t *testing.T, db DBConn) {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM dolt_status").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("dolt_status has %d rows after compact, want 0", n)
	}
}

// A commit that lands on main after the last head check and before the reset
// is in no cherry-pick. The reset must keep its rows, and the compaction must
// commit them, so main holds them in a commit and its working set is clean.
func TestCompactKeepsCommitLandingBeforeReset(t *testing.T) {
	db := startCompactTestServer(t)
	ctx := context.Background()
	initial, boundary, old := squashAllPlan(t, db)

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	fired := false
	hc := hookConn{Conn: conn, before: func(query string) {
		if !fired && strings.HasPrefix(query, "CALL DOLT_RESET(") && strings.Contains(query, "'compact-tmp'") {
			fired = true
			writeOnMain(t, db, 600)
		}
	}}

	res, err := CompactAtHead(ctx, hc, boundary, initial, boundary, old, nil)
	if err != nil {
		t.Fatalf("CompactAtHead: %v", err)
	}
	if !fired {
		t.Fatal("test hook never fired: Compact no longer resets main to compact-tmp")
	}
	for _, id := range []int{1, 2, 3, 4, 600} {
		assertRow(t, db, id, true)
	}
	assertCleanStatus(t, db)
	assertNoTempBranch(t, db)
	assertKeptRowsResult(t, db, boundary, res)
	after, err := Log(ctx, db, 0)
	if err != nil {
		t.Fatal(err)
	}
	// initial commit + squashed base + the commit holding the late row
	if len(after) != 3 {
		t.Fatalf("commits after compact = %d, want 3", len(after))
	}
}

// bd reports a write as done before any Dolt commit holds it, so main's
// working set can carry rows when the compaction runs. They must survive the
// reset and be committed by the compaction.
func TestCompactKeepsUncommittedRow(t *testing.T) {
	db := startCompactTestServer(t)
	ctx := context.Background()
	initial, boundary, old := squashAllPlan(t, db)
	mustExec(t, db, "INSERT INTO items VALUES (700, 'written, not committed')")

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	res, err := CompactAtHead(ctx, conn, boundary, initial, boundary, old, nil)
	if err != nil {
		t.Fatalf("CompactAtHead: %v", err)
	}
	for _, id := range []int{1, 2, 3, 4, 700} {
		assertRow(t, db, id, true)
	}
	assertCleanStatus(t, db)
	assertNoTempBranch(t, db)
	assertKeptRowsResult(t, db, boundary, res)
}

// assertKeptRowsResult checks what a compaction that kept rows reports: main's
// head is the kept-rows commit, the squashed head under it holds the same data
// as the planned head, and only the kept-rows commit changes the items table.
func assertKeptRowsResult(t *testing.T, db *sql.DB, planned string, res CompactResult) {
	t.Helper()
	ctx := context.Background()
	if res.KeptRowsCommit == "" || res.KeptRowsCommit != mainHead(t, db) {
		t.Fatalf("KeptRowsCommit = %q, want main's head %s", res.KeptRowsCommit, mainHead(t, db))
	}
	if res.SquashedHead == "" || res.SquashedHead == res.KeptRowsCommit {
		t.Fatalf("SquashedHead = %q, want the commit under the kept-rows commit %s", res.SquashedHead, res.KeptRowsCommit)
	}
	squashed, err := TablesChangedBetween(ctx, db, planned, res.SquashedHead)
	if err != nil {
		t.Fatal(err)
	}
	if len(squashed) != 0 {
		t.Fatalf("tables changed by the squash = %v, want none", squashed)
	}
	kept, err := TablesChangedBetween(ctx, db, planned, res.KeptRowsCommit)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0] != "items" {
		t.Fatalf("tables changed up to the kept-rows commit = %v, want [items]", kept)
	}
}
