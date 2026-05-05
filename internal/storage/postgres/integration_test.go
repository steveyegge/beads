//go:build integration_pg

package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// pgDSNEnv lets CI hand us a pre-provisioned PG instance and skip
// testcontainers startup. Matches the architect's plan in P6.
const pgDSNEnv = "BEADS_TEST_POSTGRES_DSN"

// startPG spins up postgres:14-alpine via testcontainers-go (or returns the
// pre-configured DSN when BEADS_TEST_POSTGRES_DSN is set).
func startPG(t *testing.T) (string, func()) {
	t.Helper()
	if dsn := os.Getenv(pgDSNEnv); dsn != "" {
		return dsn, func() {}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	container, err := tcpostgres.Run(ctx, "postgres:14-alpine",
		tcpostgres.WithDatabase("bd_test"),
		tcpostgres.WithUsername("bd"),
		tcpostgres.WithPassword("bd"),
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

// TestSmokePath runs the bd command-equivalent sequence end-to-end against PG
// using internal/storage/postgres directly. Mirrors the acceptance criterion
// in bead be-6fk.3 / ADR be-l7t.3.
func TestSmokePath(t *testing.T) {
	dsn, stop := startPG(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	store, err := openStore(ctx, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	// init: set the issue prefix.
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("set issue_prefix: %v", err)
	}

	// create #1
	parent := &types.Issue{
		Title:     "Parent",
		IssueType: types.TypeTask,
		Priority:  1,
		Status:    types.StatusOpen,
	}
	if err := store.CreateIssue(ctx, parent, "tester"); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if parent.ID == "" {
		t.Fatal("parent ID was not assigned")
	}

	// create #2
	child := &types.Issue{
		Title:     "Child",
		IssueType: types.TypeTask,
		Priority:  2,
		Status:    types.StatusOpen,
	}
	if err := store.CreateIssue(ctx, child, "tester"); err != nil {
		t.Fatalf("create child: %v", err)
	}

	// dep add: parent blocks child
	dep := &types.Dependency{
		IssueID:     child.ID,
		DependsOnID: parent.ID,
		Type:        types.DepBlocks,
	}
	if err := store.AddDependency(ctx, dep, "tester"); err != nil {
		t.Fatalf("add dependency: %v", err)
	}

	// ready: parent should be ready, child should not.
	ready, err := store.GetReadyWork(ctx, types.WorkFilter{})
	if err != nil {
		t.Fatalf("get ready work: %v", err)
	}
	readyIDs := map[string]bool{}
	for _, r := range ready {
		readyIDs[r.ID] = true
	}
	if !readyIDs[parent.ID] {
		t.Errorf("expected parent %s in ready, got %v", parent.ID, readyIDs)
	}
	if readyIDs[child.ID] {
		t.Errorf("did NOT expect blocked child %s in ready", child.ID)
	}

	// update --claim
	if err := store.ClaimIssue(ctx, parent.ID, "tester"); err != nil {
		t.Fatalf("claim parent: %v", err)
	}
	got, err := store.GetIssue(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get after claim: %v", err)
	}
	if got.Status != types.StatusInProgress {
		t.Errorf("expected status in_progress after claim, got %s", got.Status)
	}
	if got.Assignee != "tester" {
		t.Errorf("expected assignee tester after claim, got %q", got.Assignee)
	}

	// claim contention: a different actor must be rejected
	if err := store.ClaimIssue(ctx, parent.ID, "interloper"); !errors.Is(err, storage.ErrAlreadyClaimed) {
		t.Errorf("expected ErrAlreadyClaimed, got %v", err)
	}

	// comment add
	c, err := store.AddIssueComment(ctx, parent.ID, "tester", "first comment")
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if c.ID == "" {
		t.Error("comment ID was not assigned")
	}
	comments, err := store.GetIssueComments(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get comments: %v", err)
	}
	if len(comments) != 1 || comments[0].Text != "first comment" {
		t.Errorf("expected one comment 'first comment', got %v", comments)
	}

	// close
	if err := store.CloseIssue(ctx, parent.ID, "done", "tester", "session-1"); err != nil {
		t.Fatalf("close parent: %v", err)
	}

	// child should now appear in ready
	ready, err = store.GetReadyWork(ctx, types.WorkFilter{})
	if err != nil {
		t.Fatalf("get ready work after close: %v", err)
	}
	readyIDs = map[string]bool{}
	for _, r := range ready {
		readyIDs[r.ID] = true
	}
	if !readyIDs[child.ID] {
		t.Errorf("expected child %s in ready after parent close, got %v", child.ID, readyIDs)
	}

	// export-equivalent reads
	if _, err := store.GetAllConfig(ctx); err != nil {
		t.Errorf("get all config: %v", err)
	}
	got, err = store.GetIssue(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get parent post-close: %v", err)
	}
	if got.Status != types.StatusClosed {
		t.Errorf("expected closed parent, got %s", got.Status)
	}
	deps, err := store.GetDependencies(ctx, child.ID)
	if err != nil {
		t.Fatalf("get child deps: %v", err)
	}
	if len(deps) != 1 || deps[0].ID != parent.ID {
		t.Errorf("expected child dep on %s, got %v", parent.ID, deps)
	}
	events, err := store.GetEvents(ctx, parent.ID, 10)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected at least one audit event for parent")
	}
}

// TestConcurrentReads opens N=20 goroutines and confirms the connection
// pool serves them without errors. The architect's NFR-2 budget owns the
// hard quantitative threshold; here we just verify nothing blows up under
// realistic concurrency.
func TestConcurrentReads(t *testing.T) {
	dsn, stop := startPG(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	store, err := openStore(ctx, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("set issue_prefix: %v", err)
	}
	seed := &types.Issue{Title: "Seed", IssueType: types.TypeTask, Priority: 2, Status: types.StatusOpen}
	if err := store.CreateIssue(ctx, seed, "tester"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const fanout = 20
	errs := make(chan error, fanout)
	for i := 0; i < fanout; i++ {
		go func() {
			if _, err := store.GetIssue(ctx, seed.ID); err != nil {
				errs <- err
				return
			}
			if _, err := store.GetReadyWork(ctx, types.WorkFilter{}); err != nil {
				errs <- err
				return
			}
			errs <- nil
		}()
	}
	for i := 0; i < fanout; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent reader %d: %v", i, err)
		}
	}
}

// TestRoundtripIdempotency ensures rerunning openStore against an existing
// migration set is a no-op (advisory lock + bd_schema_migrations bookkeeping).
func TestRoundtripIdempotency(t *testing.T) {
	dsn, stop := startPG(t)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for i := 0; i < 3; i++ {
		store, err := openStore(ctx, storage.ConnectionConfig{DSN: dsn})
		if err != nil {
			t.Fatalf("attempt %d: open: %v", i, err)
		}
		store.Close()
	}
}

// avoid unused-import errors when the test build tag is off
var _ = strings.HasPrefix
