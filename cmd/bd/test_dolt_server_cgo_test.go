//go:build cgo

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/testutil"
)

func init() {
	beforeTestsHook = startTestDoltServer
}

// testServer holds the shared test Dolt server instance for crash detection.
var testServer *testutil.TestDoltServer

// testSharedDB is the name of the shared database for branch-per-test isolation.
var testSharedDB string

// testSharedConn is a raw *sql.DB for branch operations in the shared database.
var testSharedConn *sql.DB

// startTestDoltServer starts a dedicated Dolt SQL server in a temp directory
// on a dynamic port using the shared testutil helper. This prevents tests
// from creating testdb_* databases on the production Dolt server.
// Returns a cleanup function that stops the server and removes the temp dir.
func startTestDoltServer() func() {
	srv, cleanup := testutil.StartTestDoltServer("beads-test-dolt-*")
	if srv != nil {
		testServer = srv
		testDoltServerPort = srv.Port
		// Set BEADS_DOLT_PORT so that code paths using applyConfigDefaults
		// (e.g., bd init) connect to the test server instead of port 1.
		os.Setenv("BEADS_DOLT_PORT", fmt.Sprintf("%d", srv.Port))

		// Set up shared database for branch-per-test isolation (bd-xmf).
		// Instead of CREATE/DROP DATABASE per test, tests branch from this
		// shared DB, eliminating ~1-2s of overhead per test.
		testSharedDB = "cmdbd_pkg_shared"
		db, err := testutil.SetupSharedTestDB(srv.Port, testSharedDB)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: shared DB setup failed: %v (falling back to per-test DBs)\n", err)
			testSharedDB = ""
		} else {
			testSharedConn = db
			if err := initCmdBDSharedSchema(srv.Port); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: shared schema init failed: %v (falling back to per-test DBs)\n", err)
				testSharedDB = ""
				db.Close()
				testSharedConn = nil
			}
		}
	}
	return func() {
		if testSharedConn != nil {
			testSharedConn.Close()
			testSharedConn = nil
		}
		testSharedDB = ""
		testServer = nil
		testDoltServerPort = 0
		os.Unsetenv("BEADS_DOLT_PORT")
		cleanup()
	}
}

// initCmdBDSharedSchema initializes the schema and config on the shared database
// and commits to main so branches get a clean snapshot.
func initCmdBDSharedSchema(port int) error {
	ctx := context.Background()
	cfg := &dolt.Config{
		Path:         "/tmp/cmdbd-shared-init",
		ServerHost:   "127.0.0.1",
		ServerPort:   port,
		Database:     testSharedDB,
		MaxOpenConns: 1,
	}
	store, err := dolt.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("New: %w", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		return fmt.Errorf("SetConfig(issue_prefix): %w", err)
	}
	if err := store.SetConfig(ctx, "types.custom", "molecule,gate,convoy,merge-request,slot,agent,role,rig,event,message"); err != nil {
		return fmt.Errorf("SetConfig(types.custom): %w", err)
	}

	// Commit schema to main so branches get a clean snapshot
	db := store.DB()
	if _, err := db.ExecContext(ctx, "CALL DOLT_ADD('-A')"); err != nil {
		return fmt.Errorf("DOLT_ADD: %w", err)
	}
	if _, err := db.ExecContext(ctx, "CALL DOLT_COMMIT('--allow-empty', '-m', 'test: init shared schema')"); err != nil {
		return fmt.Errorf("DOLT_COMMIT: %w", err)
	}

	return nil
}
