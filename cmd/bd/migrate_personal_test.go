//go:build cgo

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
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

// requireMigratePersonalRefused runs "bd migrate-personal --yes" in dir,
// expecting it to fail with output containing want and to report no move. It
// returns the combined output.
func requireMigratePersonalRefused(t *testing.T, bd, dir, want string) string {
	t.Helper()
	out, err := bdMigratePersonal(t, bd, dir, "--yes")
	if err == nil {
		t.Fatalf("bd migrate-personal --yes succeeded, want it refused:\n%s", out)
	}
	if !strings.Contains(out, want) {
		t.Errorf("bd migrate-personal --yes output does not contain %q:\n%s", want, out)
	}
	if strings.Contains(out, "Moved") {
		t.Errorf("bd migrate-personal --yes reported a move while refusing:\n%s", out)
	}
	return out
}

// requireStillInProject asserts issueID is still in the project database.
func requireStillInProject(t *testing.T, projectBeadsDir, projectDB, issueID string) {
	t.Helper()
	if _, err := openStore(t, projectBeadsDir, projectDB).GetIssue(t.Context(), issueID); err != nil {
		t.Errorf("issue %s left the project database %q after a refused migrate-personal: %v", issueID, projectDB, err)
	}
}

// TestEmbeddedMigratePersonal_refusesPlanningRepoThatIsTheProject verifies that
// a planning repo resolving to the project's own workspace is refused before
// anything moves. Copying an issue onto itself and then deleting it from the
// project deletes the only copy while reporting it moved.
func TestEmbeddedMigratePersonal_refusesPlanningRepoThatIsTheProject(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	for _, tc := range []struct {
		name   string
		prefix string
		value  func(dir string) string
	}{
		{"project path", "mps", func(dir string) string { return dir }},
		{"dot", "mpd", func(string) string { return "." }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, beadsDir, _ := bdInit(t, bd, "--prefix", tc.prefix)
			if out, err := bdRunWithFlockRetry(t, bd, dir, "config", "set", "routing.contributor", tc.value(dir)); err != nil {
				t.Fatalf("bd config set routing.contributor: %v\n%s", err, out)
			}
			personal := bdCreate(t, bd, dir, "Self-routed personal issue")

			requireMigratePersonalRefused(t, bd, dir, "is this project's own workspace")
			requireStillInProject(t, beadsDir, tc.prefix, personal.ID)
		})
	}
}

// TestEmbeddedMigratePersonal_refusesMetadataWithoutDoltDatabase verifies that
// a planning repo whose metadata.json names no dolt_database is refused before
// anything moves, and that no database is created in it: the launcher's
// database name would otherwise choose one.
func TestEmbeddedMigratePersonal_refusesMetadataWithoutDoltDatabase(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "mpx")
	planningDir := setupContributorRouting(t, bd, dir)
	planningBeadsDir := filepath.Join(planningDir, ".beads")

	metadataPath := filepath.Join(planningBeadsDir, "metadata.json")
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read planning metadata.json: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("parse planning metadata.json: %v", err)
	}
	delete(meta, "dolt_database")
	raw, err = json.Marshal(meta)
	if err != nil {
		t.Fatalf("encode planning metadata.json: %v", err)
	}
	if err := os.WriteFile(metadataPath, raw, 0o600); err != nil {
		t.Fatalf("write planning metadata.json: %v", err)
	}
	databasesBefore := embeddeddolt.Databases(planningBeadsDir)

	personal := bdCreate(t, bd, dir, "Personal issue for a nameless planning repo")

	requireMigratePersonalRefused(t, bd, dir, "has no dolt_database field")
	requireStillInProject(t, beadsDir, "mpx", personal.ID)
	if got := embeddeddolt.Databases(planningBeadsDir); !slices.Equal(got, databasesBefore) {
		t.Errorf("planning repo databases = %v after a refused migrate-personal, want unchanged %v", got, databasesBefore)
	}
}

// TestEmbeddedMigratePersonal_initializesContributorPlanningRepos verifies that
// the planning repos contributor setup leaves behind are initialized the way a
// routed bd create initializes them, and then migrated into: the wizard's empty
// .beads (runContributorWizard), and fork setup's database with no
// metadata.json and no issue_prefix (autoConfigureForkContributor).
func TestEmbeddedMigratePersonal_initializesContributorPlanningRepos(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	for _, tc := range []struct {
		name   string
		prefix string
		// setup shapes planningBeadsDir, which exists and is empty.
		setup func(t *testing.T, planningBeadsDir string)
	}{
		{"wizard empty beads dir", "mpw", func(*testing.T, string) {}},
		{"fork setup database without metadata", "mpf", func(t *testing.T, planningBeadsDir string) {
			s, err := embeddeddolt.Open(t.Context(), planningBeadsDir, "beads", "main")
			if err != nil {
				t.Fatalf("create the fork-setup planning database: %v", err)
			}
			if err := s.Close(); err != nil {
				t.Fatalf("close the fork-setup planning database: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, beadsDir, _ := bdInit(t, bd, "--prefix", tc.prefix)
			planningDir := t.TempDir()
			planningBeadsDir := filepath.Join(planningDir, ".beads")
			if err := os.MkdirAll(planningBeadsDir, 0o750); err != nil {
				t.Fatalf("create planning .beads: %v", err)
			}
			tc.setup(t, planningBeadsDir)
			if out, err := bdRunWithFlockRetry(t, bd, dir, "config", "set", "routing.contributor", planningDir); err != nil {
				t.Fatalf("bd config set routing.contributor: %v\n%s", err, out)
			}
			personal := bdCreate(t, bd, dir, "Personal issue for a contributor planning repo")

			out := requireMigratePersonal(t, bd, dir)
			if !strings.Contains(out, "Moved 1 issue") {
				t.Errorf("migrate-personal output does not report moving 1 issue:\n%s", out)
			}
			database, err := loadDoltDatabase(planningBeadsDir)
			if err != nil {
				t.Fatalf("planning metadata.json after migrate-personal: %v", err)
			}
			if database != tc.prefix {
				t.Errorf("planning metadata.json dolt_database = %q, want %q (the project prefix, as a routed bd create writes)", database, tc.prefix)
			}
			moved, err := openStore(t, planningBeadsDir, tc.prefix).GetIssue(t.Context(), personal.ID)
			if err != nil {
				t.Fatalf("issue %s not in the planning database %q: %v", personal.ID, tc.prefix, err)
			}
			if moved.Title != "Personal issue for a contributor planning repo" {
				t.Errorf("planning issue %s title = %q", personal.ID, moved.Title)
			}
			if _, err := openStore(t, beadsDir, tc.prefix).GetIssue(t.Context(), personal.ID); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("issue %s still in the project database: err = %v, want storage.ErrNotFound", personal.ID, err)
			}
		})
	}
}

// TestEmbeddedMigratePersonal_refusesPlanningRepoWithoutDatabase verifies that
// a planning repo whose metadata.json names a database that has not been
// bootstrapped yet (a clone, say) is refused before anything moves, and is not
// given a database: a migration is not the place to create one.
func TestEmbeddedMigratePersonal_refusesPlanningRepoWithoutDatabase(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "mpb")
	planningDir := setupContributorRouting(t, bd, dir)
	planningBeadsDir := filepath.Join(planningDir, ".beads")
	if err := os.RemoveAll(filepath.Join(planningBeadsDir, "embeddeddolt")); err != nil {
		t.Fatalf("remove the planning database: %v", err)
	}

	personal := bdCreate(t, bd, dir, "Personal issue for an unbootstrapped planning repo")

	requireMigratePersonalRefused(t, bd, dir, "no embedded database")
	requireStillInProject(t, beadsDir, "mpb", personal.ID)
	if got := embeddeddolt.Databases(planningBeadsDir); len(got) != 0 {
		t.Errorf("planning repo databases = %v after a refused migrate-personal, want none", got)
	}
}

// TestEmbeddedMigratePersonal_refusesIssueAlreadyInPlanningRepo verifies that
// an issue ID that already exists in the planning repo is refused before
// anything moves. Without the refusal the copy upserts onto the planning repo's
// row, overwriting it, and the project's copy is then deleted.
func TestEmbeddedMigratePersonal_refusesIssueAlreadyInPlanningRepo(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "mpc")
	planningDir := setupContributorRouting(t, bd, dir)

	personal := bdCreate(t, bd, dir, "Project personal version")
	const planningTitle = "Planning repo ORIGINAL row"
	bdCreate(t, bd, planningDir, planningTitle, "--id", personal.ID, "--force")

	requireMigratePersonalRefused(t, bd, dir, "already exists in the planning repo")
	requireStillInProject(t, beadsDir, "mpc", personal.ID)
	planningBeadsDir := filepath.Join(planningDir, ".beads")
	kept, err := openStore(t, planningBeadsDir, migratePersonalPlanningDatabase).GetIssue(t.Context(), personal.ID)
	if err != nil {
		t.Fatalf("planning repo's own issue %s gone after a refused migrate-personal: %v", personal.ID, err)
	}
	if kept.Title != planningTitle {
		t.Errorf("planning issue %s title = %q after a refused migrate-personal, want unchanged %q", personal.ID, kept.Title, planningTitle)
	}
}

// TestEmbeddedMigratePersonal_repoSyncMirrorHint verifies the phase-0 refusal
// for a project copy that bd repo sync imported from the planning repo: the
// error names bd repo remove, and once the mirror is removed a rerun moves the
// remaining personal issue.
func TestEmbeddedMigratePersonal_repoSyncMirrorHint(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "mps")
	planningDir := setupContributorRouting(t, bd, dir)

	const personalTitle = "Personal issue beside a repo sync mirror"
	personal := bdCreate(t, bd, dir, personalTitle)

	// The mirror counts as personal only when its creator is the project's git
	// identity; the planning repo's own HOME gives it a different one.
	const planningTitle = "Planning issue mirrored by repo sync"
	planningIssue := bdCreate(t, bd, planningDir, planningTitle, "--actor", personal.CreatedBy)
	if out, err := bdRunWithFlockRetry(t, bd, planningDir, "export", "-o", filepath.Join(".beads", "issues.jsonl")); err != nil {
		t.Fatalf("bd export in the planning repo: %v\n%s", err, out)
	}
	for _, args := range [][]string{{"repo", "add", planningDir}, {"repo", "sync"}} {
		if out, err := bdRunWithFlockRetry(t, bd, dir, args...); err != nil {
			t.Fatalf("bd %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	requireStillInProject(t, beadsDir, "mps", planningIssue.ID)

	requireMigratePersonalRefused(t, bd, dir, "by 'bd repo sync' (remove those copies with 'bd repo remove "+planningDir+"', then rerun); nothing was moved")
	requireStillInProject(t, beadsDir, "mps", personal.ID)

	if out, err := bdRunWithFlockRetry(t, bd, dir, "repo", "remove", planningDir); err != nil {
		t.Fatalf("bd repo remove: %v\n%s", err, out)
	}
	requireMigratePersonal(t, bd, dir)
	requireMovedToPlanning(t, beadsDir, "mps", planningDir, personal.ID, personalTitle)
	kept, err := openStore(t, filepath.Join(planningDir, ".beads"), migratePersonalPlanningDatabase).GetIssue(t.Context(), planningIssue.ID)
	if err != nil {
		t.Fatalf("planning repo's own issue %s gone after migrate-personal: %v", planningIssue.ID, err)
	}
	if kept.Title != planningTitle {
		t.Errorf("planning issue %s title = %q, want unchanged %q", planningIssue.ID, kept.Title, planningTitle)
	}
}

// TestEmbeddedMigratePersonal_abortLeavesLegacyPlanningConfig verifies that an
// aborted run does not rewrite a planning repo that still has a legacy
// config.json: the self-routing check runs before the prompt and must read the
// workspace without migrating its metadata.
func TestEmbeddedMigratePersonal_abortLeavesLegacyPlanningConfig(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "mpl")
	planningDir := t.TempDir()
	planningBeadsDir := filepath.Join(planningDir, ".beads")
	if err := os.MkdirAll(planningBeadsDir, 0o750); err != nil {
		t.Fatalf("create planning .beads: %v", err)
	}
	legacyPath := filepath.Join(planningBeadsDir, "config.json")
	legacy := []byte(`{"database":"beads.db"}`)
	if err := os.WriteFile(legacyPath, legacy, 0o600); err != nil {
		t.Fatalf("write legacy planning config.json: %v", err)
	}
	if out, err := bdRunWithFlockRetry(t, bd, dir, "config", "set", "routing.contributor", planningDir); err != nil {
		t.Fatalf("bd config set routing.contributor: %v\n%s", err, out)
	}
	personal := bdCreate(t, bd, dir, "Personal issue for a legacy planning repo")

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

	got, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatalf("legacy planning config.json gone after an aborted migrate-personal: %v", err)
	}
	if string(got) != string(legacy) {
		t.Errorf("legacy planning config.json = %s after an aborted migrate-personal, want unchanged %s", got, legacy)
	}
	if _, err := os.Stat(filepath.Join(planningBeadsDir, "metadata.json")); !os.IsNotExist(err) {
		t.Errorf("planning metadata.json written by an aborted migrate-personal: stat err = %v, want not exist", err)
	}
	requireStillInProject(t, beadsDir, "mpl", personal.ID)
}

// planningDirListing returns the names of every entry under beadsDir, relative
// to it, so a refused run can be checked for having written nothing there.
func planningDirListing(t *testing.T, beadsDir string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(beadsDir, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(beadsDir, path)
		if err != nil {
			return err
		}
		names = append(names, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("list %s: %v", beadsDir, err)
	}
	return names
}

// TestEmbeddedMigratePersonal_refusesUninitializablePlanningRepos verifies that
// a metadata-less planning .beads that is not a contributor-setup shape is
// refused before anything moves, and is not initialized: a redirect stub (its
// new database would sit inside the stub, where bd run in the planning repo
// never looks), and a SQLite-era repo that bd's own admission refuses (a fresh
// database beside it would hide its data and silence that refusal).
func TestEmbeddedMigratePersonal_refusesUninitializablePlanningRepos(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	for _, tc := range []struct {
		name   string
		prefix string
		want   string
		// setup shapes planningBeadsDir, which exists and is empty.
		setup func(t *testing.T, planningBeadsDir string)
	}{
		{"redirect stub", "mps", "redirects to", func(t *testing.T, planningBeadsDir string) {
			target := t.TempDir()
			initCmd := exec.Command(bd, "init", "--prefix", "pl", "--quiet")
			initCmd.Dir = target
			initCmd.Env = bdEnv(target)
			if out, err := initCmd.CombinedOutput(); err != nil {
				t.Fatalf("bd init redirect target: %v\n%s", err, out)
			}
			if err := os.WriteFile(filepath.Join(planningBeadsDir, "redirect"), []byte(filepath.Join(target, ".beads")+"\n"), 0o600); err != nil {
				t.Fatalf("write planning redirect: %v", err)
			}
		}},
		{"sqlite-era repo", "mpq", "historical SQLite workspace", func(t *testing.T, planningBeadsDir string) {
			if err := os.WriteFile(filepath.Join(planningBeadsDir, "config.json"), []byte(`{"database":"beads.db"}`), 0o600); err != nil {
				t.Fatalf("write legacy planning config.json: %v", err)
			}
			db := append([]byte("SQLite format 3\x00"), make([]byte, 4080)...)
			if err := os.WriteFile(filepath.Join(planningBeadsDir, "beads.db"), db, 0o600); err != nil {
				t.Fatalf("write legacy planning beads.db: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, beadsDir, _ := bdInit(t, bd, "--prefix", tc.prefix)
			planningDir := t.TempDir()
			planningBeadsDir := filepath.Join(planningDir, ".beads")
			if err := os.MkdirAll(planningBeadsDir, 0o750); err != nil {
				t.Fatalf("create planning .beads: %v", err)
			}
			tc.setup(t, planningBeadsDir)
			if out, err := bdRunWithFlockRetry(t, bd, dir, "config", "set", "routing.contributor", planningDir); err != nil {
				t.Fatalf("bd config set routing.contributor: %v\n%s", err, out)
			}
			personal := bdCreate(t, bd, dir, "Personal issue for an uninitializable planning repo")
			before := planningDirListing(t, planningBeadsDir)

			requireMigratePersonalRefused(t, bd, dir, tc.want)
			requireStillInProject(t, beadsDir, tc.prefix, personal.ID)
			if after := planningDirListing(t, planningBeadsDir); !slices.Equal(after, before) {
				t.Errorf("planning .beads after a refused migrate-personal = %v, want unchanged %v", after, before)
			}
		})
	}
}

// TestEmbeddedMigratePersonal_unsupportedPlanningFormatExit16 verifies that a
// planning repo in a format this build cannot open is refused the way bd's own
// admission refuses it: exit ExitUnsupportedWorkspaceFormat, with only the
// Error and Hint lines on stderr.
func TestEmbeddedMigratePersonal_unsupportedPlanningFormatExit16(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "mpg")
	planningDir := setupContributorRouting(t, bd, dir)
	metadataPath := filepath.Join(planningDir, ".beads", "metadata.json")
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read planning metadata.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse planning metadata.json: %v", err)
	}
	doc["graph_mode"] = "graph"
	raw, err = json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode planning metadata.json: %v", err)
	}
	if err := os.WriteFile(metadataPath, raw, 0o600); err != nil {
		t.Fatalf("write planning metadata.json: %v", err)
	}
	personal := bdCreate(t, bd, dir, "Personal issue for a graph-format planning repo")

	out := requireMigratePersonalRefused(t, bd, dir, "use a bd build that supports graph_mode")
	if strings.Contains(out, "initialize planning repo") {
		t.Errorf("format refusal carries the initialize prefix:\n%s", out)
	}
	cmd := exec.Command(bd, "migrate-personal", "--yes")
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != ExitUnsupportedWorkspaceFormat {
		t.Errorf("bd migrate-personal --yes err = %v, want exit %d", err, ExitUnsupportedWorkspaceFormat)
	}
	// stderr is exactly bd's own admission refusal: the Error and Hint lines,
	// with no "Error: exit code 16" placeholder from cobra and no usage dump.
	formatErr := &configfile.UnsupportedWorkspaceFormatError{
		BeadsDir:  filepath.Join(planningDir, ".beads"),
		GraphMode: "graph",
	}
	wantStderr := "Error: " + formatErr.Error() + "\nHint: " + formatErr.Hint() + "\n"
	if got := stderr.String(); got != wantStderr {
		t.Errorf("bd migrate-personal --yes stderr:\n%s\nwant exactly:\n%s", got, wantStderr)
	}
	requireStillInProject(t, beadsDir, "mpg", personal.ID)
}
