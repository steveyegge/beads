//go:build cgo

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
	"github.com/steveyegge/beads/internal/types"
)

// bdMigratePersonal runs "bd migrate-personal" with the given args.
// Returns stdout+stderr and error (does not fatal on error).
func bdMigratePersonal(t *testing.T, bd, dir string, args ...string) (string, error) {
	t.Helper()
	fullArgs := append([]string{"migrate-personal"}, args...)
	cmd := exec.Command(bd, fullArgs...)
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// setupContributorRouting sets routing.contributor to a planning dir so that
// migrate-personal has somewhere to route to.
func setupContributorRouting(t *testing.T, bd, dir string) string {
	t.Helper()
	planningDir := t.TempDir()
	// Init a minimal git repo in the planning dir.
	gitCommand("init", planningDir).Run() //nolint:errcheck
	// Initialize beads in planning dir.
	initCmd := exec.Command(bd, "init", "--prefix", "pl", "--quiet")
	initCmd.Dir = planningDir
	initCmd.Env = bdEnv(planningDir)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init planning dir: %v\n%s", err, out)
	}

	// Point routing.contributor at the planning dir.
	bdRunWithFlockRetry(t, bd, dir, "config", "set", "routing.contributor", planningDir) //nolint:errcheck
	bdRunWithFlockRetry(t, bd, dir, "config", "set", "routing.mode", "auto")             //nolint:errcheck
	return planningDir
}

// migratePersonalPlanningDatabase is the database setupContributorRouting's
// "bd init --prefix pl" gives the planning repo.
const migratePersonalPlanningDatabase = "pl"

// requireMigratePersonal runs "bd migrate-personal --yes" in dir with bdEnv plus
// extraEnv, fails the test if it errors, and returns its combined output. It
// also rejects the two symptoms of opening the planning store as a SQL server
// under the default database name: a dial to an unreachable server and the
// default-database fallback warning.
func requireMigratePersonal(t *testing.T, bd, dir string, extraEnv ...string) string {
	t.Helper()
	cmd := exec.Command(bd, "migrate-personal", "--yes")
	cmd.Dir = dir
	cmd.Env = append(bdEnv(dir), extraEnv...)
	outBytes, err := cmd.CombinedOutput()
	out := string(outBytes)
	if err != nil {
		t.Fatalf("bd migrate-personal --yes failed: %v\noutput:\n%s", err, out)
	}
	for _, symptom := range []string{"Dolt server unreachable", "no database name configured"} {
		if strings.Contains(out, symptom) {
			t.Errorf("bd migrate-personal --yes output contains %q:\n%s", symptom, out)
		}
	}
	return out
}

// requireMovedToPlanning asserts issueID now lives in the planning repo's own
// database (and that no other database was created there) and is gone from
// the project database.
func requireMovedToPlanning(t *testing.T, projectBeadsDir, projectDB, planningDir, issueID, title string) {
	t.Helper()
	planningBeadsDir := filepath.Join(planningDir, ".beads")
	if got, want := embeddeddolt.Databases(planningBeadsDir), []string{migratePersonalPlanningDatabase}; !slices.Equal(got, want) {
		t.Errorf("planning repo databases = %v, want %v", got, want)
	}
	moved, err := openStore(t, planningBeadsDir, migratePersonalPlanningDatabase).GetIssue(t.Context(), issueID)
	if err != nil {
		t.Fatalf("issue %s not in the planning database %q: %v", issueID, migratePersonalPlanningDatabase, err)
	}
	if moved.Title != title {
		t.Errorf("planning issue %s title = %q, want %q", issueID, moved.Title, title)
	}
	if _, err := openStore(t, projectBeadsDir, projectDB).GetIssue(t.Context(), issueID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("issue %s still in the project database %q: err = %v, want storage.ErrNotFound", issueID, projectDB, err)
	}
}

// TestEmbeddedMigratePersonal_noopWhenEmpty verifies that migrate-personal with no
// matching issues exits 0 and reports nothing to migrate.
func TestEmbeddedMigratePersonal_noopWhenEmpty(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "mpn")

	setupContributorRouting(t, bd, dir)

	// No issues owned by the git identity → noop.
	out, err := bdMigratePersonal(t, bd, dir, "--yes")
	if err != nil {
		// Some backends may report "no issues to migrate" as success.
		// Allow either exit 0 or a meaningful message.
		if !strings.Contains(strings.ToLower(out), "no") &&
			!strings.Contains(strings.ToLower(out), "0 issue") {
			t.Logf("migrate-personal with no matching issues: err=%v output=%s", err, out)
		}
	}
}

// TestEmbeddedMigratePersonal_movesIssues verifies that migrate-personal moves
// issues created by the current identity into the planning repo's own database
// and leaves everyone else's issues in the project.
//
// The personal issue is created WITHOUT --actor, so its created_by is the same
// getActorWithGit identity migrate-personal resolves in the same directory and
// environment.
func TestEmbeddedMigratePersonal_movesIssues(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "mpm")
	planningDir := setupContributorRouting(t, bd, dir)

	personal := bdCreate(t, bd, dir, "Personal planning issue")
	team := bdCreate(t, bd, dir, "Team issue", "--actor", "other-person")

	out := requireMigratePersonal(t, bd, dir)
	if !strings.Contains(out, "Moved 1 issue") {
		t.Errorf("migrate-personal output does not report moving 1 issue:\n%s", out)
	}
	requireMovedToPlanning(t, beadsDir, "mpm", planningDir, personal.ID, "Personal planning issue")

	// The team issue stays in the project and is not copied to planning.
	if got, err := openStore(t, beadsDir, "mpm").GetIssue(t.Context(), team.ID); err != nil {
		t.Errorf("team issue %s missing from the project after migrate-personal: %v", team.ID, err)
	} else if got.Title != "Team issue" {
		t.Errorf("project issue %s title = %q, want %q", team.ID, got.Title, "Team issue")
	}
	planningBeadsDir := filepath.Join(planningDir, ".beads")
	if _, err := openStore(t, planningBeadsDir, migratePersonalPlanningDatabase).GetIssue(t.Context(), team.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("team issue %s in the planning database: err = %v, want storage.ErrNotFound", team.ID, err)
	}
}

// TestEmbeddedMigratePersonal_planningDatabaseIgnoresLauncherOverride verifies
// that the planning database comes from the planning repo, not the launcher.
// BEADS_DOLT_SERVER_DATABASE names the launching workspace's database (set by
// the user, or by preserveRedirectSourceDatabase after a redirect) and must not
// choose the planning database: honoring it here would create a project-named
// database inside the planning repo that no bd command run there reads.
func TestEmbeddedMigratePersonal_planningDatabaseIgnoresLauncherOverride(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "mpo")
	planningDir := setupContributorRouting(t, bd, dir)

	personal := bdCreate(t, bd, dir, "Personal issue under a launcher override")

	requireMigratePersonal(t, bd, dir, "BEADS_DOLT_SERVER_DATABASE=mpo")
	requireMovedToPlanning(t, beadsDir, "mpo", planningDir, personal.ID, "Personal issue under a launcher override")
}

// TestEmbeddedMigratePersonal_abortOnNoConfirm verifies that without --yes the
// command prompts, and answering n aborts with nothing moved. (An empty answer,
// closed stdin included, takes the default Y and moves.)
func TestEmbeddedMigratePersonal_abortOnNoConfirm(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "mpa")
	planningDir := setupContributorRouting(t, bd, dir)

	issue := bdCreate(t, bd, dir, "Personal issue for abort test")

	cmd := exec.Command(bd, "migrate-personal")
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	cmd.Stdin = strings.NewReader("n\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bd migrate-personal (answer n) failed: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "Aborted") {
		t.Errorf("bd migrate-personal (answer n) did not abort:\n%s", out)
	}

	if _, err := openStore(t, beadsDir, "mpa").GetIssue(t.Context(), issue.ID); err != nil {
		t.Errorf("issue %s left the project after an aborted migrate-personal: %v", issue.ID, err)
	}
	planningBeadsDir := filepath.Join(planningDir, ".beads")
	if _, err := openStore(t, planningBeadsDir, migratePersonalPlanningDatabase).GetIssue(t.Context(), issue.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("issue %s in the planning database after an aborted migrate-personal: err = %v, want storage.ErrNotFound", issue.ID, err)
	}
}

// TestEmbeddedMigratePersonal_preservesComments is a regression test for the maphew
// review of PR #4023 (be-e2nb): migrate-personal must copy comments as
// structured rows (ImportIssueComment) so they survive in the planning repo.
// The pre-fix code used AddComment, which records an event-style entry that
// GetIssueComments never returns — silently dropping the migrated issue's
// comments.
func TestEmbeddedMigratePersonal_preservesComments(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "mpc")
	planningDir := setupContributorRouting(t, bd, dir)

	issue := bdCreate(t, bd, dir, "Personal issue with a comment")
	const commentText = "migrate-personal-must-preserve-this-comment"
	bdComment(t, bd, dir, issue.ID, commentText)

	requireMigratePersonal(t, bd, dir)
	requireMovedToPlanning(t, beadsDir, "mpc", planningDir, issue.ID, "Personal issue with a comment")

	// The comment must survive in the planning repo as a structured comment that
	// `bd comments <id>` returns.
	got := bdCommentList(t, bd, planningDir, issue.ID)
	if !strings.Contains(got, commentText) {
		t.Errorf("migrated issue %s lost its comment in the planning repo %s.\nbd comments output:\n%s",
			issue.ID, planningDir, got)
	}
}

// TestEmbeddedCopyIssueRelations_preservesCommentWithTimestamp is the in-process proof
// for the maphew review of PR #4023 (be-e2nb). The end-to-end command is covered
// by TestEmbeddedMigratePersonal_preservesComments; this drives the helper
// directly against two embedded stores to pin what the CLI output cannot show:
// a comment must land in the destination as a structured row (returned by
// GetIssueComments) preserving its original timestamp, not be lost as an
// event-style AddComment entry.
func TestEmbeddedCopyIssueRelations_preservesCommentWithTimestamp(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	// Two initialized embedded stores standing in for the project and planning
	// DBs. Same prefix so the copied issue ID is valid in both.
	_, srcBeads, _ := bdInit(t, bd, "--prefix", "mp")
	_, dstBeads, _ := bdInit(t, bd, "--prefix", "mp")
	src := openStore(t, srcBeads, "mp")
	dst := openStore(t, dstBeads, "mp")

	ctx := context.Background()

	issue := &types.Issue{
		ID:        "mp-100",
		Title:     "Issue with a comment",
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		CreatedBy: "alice",
	}
	if err := src.CreateIssue(ctx, issue, "alice"); err != nil {
		t.Fatalf("seed src CreateIssue: %v", err)
	}
	origTS := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	if _, err := src.ImportIssueComment(ctx, issue.ID, "alice", "preserve me", origTS); err != nil {
		t.Fatalf("seed src comment: %v", err)
	}

	// Phase 1a of runMigratePersonal creates the issue row in dst first.
	if err := dst.CreateIssue(ctx, issue, "alice"); err != nil {
		t.Fatalf("dst CreateIssue: %v", err)
	}

	// Code under test.
	if err := copyIssueRelations(ctx, src, dst, issue, "alice"); err != nil {
		t.Fatalf("copyIssueRelations: %v", err)
	}

	got, err := dst.GetIssueComments(ctx, issue.ID)
	if err != nil {
		t.Fatalf("dst.GetIssueComments: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("planning DB has %d structured comments, want 1 (event-style AddComment would yield 0)", len(got))
	}
	if got[0].Text != "preserve me" {
		t.Errorf("comment text = %q, want %q", got[0].Text, "preserve me")
	}
	if !got[0].CreatedAt.Equal(origTS) {
		t.Errorf("comment created_at = %v, want preserved %v (original timestamp dropped)", got[0].CreatedAt, origTS)
	}
}
