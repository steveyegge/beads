//go:build cgo && integration

package dolt

import (
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Git Remote Integration Tests
//
// These tests validate Dolt's native git remote support: push/pull/clone
// to/from standard bare git repositories. Unlike the federation tests
// (which use Dolt's remotesapi protocol over HTTP), these tests use
// file:// URLs pointing to local bare git repos — no network, CI-friendly.
//
// Architecture:
//   - All operations (source + clone) use the `dolt` CLI exclusively.
//   - The embedded Dolt driver panics on Close in multi-store processes,
//     so we avoid it entirely and verify via `dolt sql -q ... -r csv`.
//
// Prerequisites:
//   - dolt >= 1.81.8 (native git remote support)
//   - git CLI available
//
// Run:
//   go test -tags='cgo integration' -run TestGitRemote ./internal/storage/dolt/

// gitRemoteSetup holds resources for a git-remote test scenario.
type gitRemoteSetup struct {
	baseDir   string // root temp dir
	remoteDir string // bare git repo path
	remoteURL string // file:// URL for the bare repo
	sourceDir string // dolt source repo directory
}

// setupGitRemote creates a bare git repo (seeded with an initial commit)
// and a Dolt source repo with the bare repo configured as "origin".
// Schema and config are initialized; ready for data writes and push.
func setupGitRemote(t *testing.T) *gitRemoteSetup {
	t.Helper()
	skipIfNoDolt(t)
	skipIfNoGit(t)

	baseDir, err := os.MkdirTemp("", "git-remote-test-*")
	if err != nil {
		t.Fatalf("failed to create base dir: %v", err)
	}

	// Create bare git repo
	remoteDir := filepath.Join(baseDir, "remote.git")
	runCmd(t, baseDir, "git", "init", "--bare", "-b", "main", remoteDir)

	// Seed with an initial commit (Dolt requires at least one branch)
	seedDir := filepath.Join(baseDir, "seed")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		os.RemoveAll(baseDir)
		t.Fatalf("failed to create seed dir: %v", err)
	}
	runCmd(t, seedDir, "git", "init", "-b", "main")
	runCmd(t, seedDir, "git", "commit", "--allow-empty", "-m", "init")
	runCmd(t, seedDir, "git", "remote", "add", "origin", remoteDir)
	runCmd(t, seedDir, "git", "push", "-u", "origin", "main")

	remoteURL := "file://" + remoteDir

	// Initialize dolt repo, configure remote, create schema
	sourceDir := filepath.Join(baseDir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		os.RemoveAll(baseDir)
		t.Fatalf("failed to create source dir: %v", err)
	}
	runCmd(t, sourceDir, "dolt", "init")
	runCmd(t, sourceDir, "dolt", "remote", "add", "origin", remoteURL)

	// Initialize beads schema via CLI (mirrors what New() does).
	// dolt sql in the repo dir already defaults to the repo's database.
	initSchemaSQL := fmt.Sprintf(`%s
%s
%s
%s
CALL DOLT_ADD('.');
CALL DOLT_COMMIT('-Am', 'Genesis: schema and config');`, schema, defaultConfig, readyIssuesView, blockedIssuesView)
	runDoltSQL(t, sourceDir, initSchemaSQL)

	return &gitRemoteSetup{
		baseDir:   baseDir,
		remoteDir: remoteDir,
		remoteURL: remoteURL,
		sourceDir: sourceDir,
	}
}

// cleanup removes all temp dirs.
func (s *gitRemoteSetup) cleanup() {
	os.RemoveAll(s.baseDir)
}

// --- CLI helpers ---

// doltPush pushes to "origin" via CLI.
func doltPush(t *testing.T, dir string) {
	t.Helper()
	runCmd(t, dir, "dolt", "push", "origin", "main")
}

// doltPull pulls from "origin" via CLI.
func doltPull(t *testing.T, dir string) {
	t.Helper()
	runCmd(t, dir, "dolt", "pull", "origin")
}

// doltClone clones from remoteURL into cloneDir via CLI.
func doltClone(t *testing.T, remoteURL, cloneDir string) {
	t.Helper()
	cmd := exec.Command("dolt", "clone", remoteURL, cloneDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dolt clone failed: %v\nOutput: %s", err, output)
	}
}

// runCmd executes a command in the given directory.
func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed in %s: %v\nOutput: %s", name, args, dir, err, output)
	}
}

// runDoltSQL executes SQL via `dolt sql` CLI in the given directory.
func runDoltSQL(t *testing.T, dir, query string) {
	t.Helper()
	cmd := exec.Command("dolt", "sql", "-q", query)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dolt sql failed in %s: %v\nQuery: %.200s...\nOutput: %s", dir, err, query, output)
	}
}

// skipIfNoGit skips if git is not available.
func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed, skipping test")
	}
}

// sourceInsertIssue inserts an issue into the source via CLI SQL.
func sourceInsertIssue(t *testing.T, dir, id, title string) {
	t.Helper()
	q := fmt.Sprintf(
		`INSERT INTO issues (id, title, description, design, acceptance_criteria, notes, status, priority, issue_type, created_at, updated_at) `+
			`VALUES ('%s', '%s', '', '', '', '', 'open', 2, 'task', NOW(), NOW())`,
		escapeSQL(id), escapeSQL(title))
	runDoltSQL(t, dir, q)
}

// sourceInsertIssueDesc inserts an issue with a description via CLI SQL.
func sourceInsertIssueDesc(t *testing.T, dir, id, title, desc string) {
	t.Helper()
	q := fmt.Sprintf(
		`INSERT INTO issues (id, title, description, design, acceptance_criteria, notes, status, priority, issue_type, created_at, updated_at) `+
			`VALUES ('%s', '%s', '%s', '', '', '', 'open', 2, 'task', NOW(), NOW())`,
		escapeSQL(id), escapeSQL(title), escapeSQL(desc))
	runDoltSQL(t, dir, q)
}

// escapeSQL escapes single quotes for SQL string literals.
func escapeSQL(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			result = append(result, '\'', '\'')
		} else if s[i] == '\\' {
			result = append(result, '\\', '\\')
		} else {
			result = append(result, s[i])
		}
	}
	return string(result)
}

// sourceCommitAndPush commits all changes and pushes to origin.
func sourceCommitAndPush(t *testing.T, dir, msg string) {
	t.Helper()
	runDoltSQL(t, dir, fmt.Sprintf("CALL DOLT_ADD('.'); CALL DOLT_COMMIT('-Am', '%s')", escapeSQL(msg)))
	doltPush(t, dir)
}

// --- Clone verification helpers (all CLI-based) ---

// queryCSV runs a SQL query via dolt CLI and returns parsed rows as maps.
func queryCSV(t *testing.T, dir, query string) []map[string]string {
	t.Helper()
	cmd := exec.Command("dolt", "sql", "-q", query, "-r", "csv")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dolt sql query failed: %v\nQuery: %s\nOutput: %s", err, query, output)
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil
	}
	reader := csv.NewReader(strings.NewReader(trimmed))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("csv parse failed: %v\nRaw: %s", err, output)
	}
	if len(records) < 2 {
		return nil // header only, no data rows
	}
	headers := records[0]
	var rows []map[string]string
	for _, rec := range records[1:] {
		row := make(map[string]string)
		for i, h := range headers {
			if i < len(rec) {
				row[h] = rec[i]
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// queryScalar runs a query expected to return a single value.
func queryScalar(t *testing.T, dir, query string) string {
	t.Helper()
	rows := queryCSV(t, dir, query)
	if len(rows) == 0 {
		return ""
	}
	for _, v := range rows[0] {
		return v
	}
	return ""
}

// queryCount runs a COUNT(*) query and returns the integer result.
func queryCount(t *testing.T, dir, query string) int {
	t.Helper()
	s := queryScalar(t, dir, query)
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("expected integer from query, got %q: %v", s, err)
	}
	return n
}

// --- Tests ---

func TestGitRemoteAdd(t *testing.T) {
	setup := setupGitRemote(t)
	defer setup.cleanup()

	// Verify remote via CLI
	cmd := exec.Command("dolt", "remote", "-v")
	cmd.Dir = setup.sourceDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dolt remote -v: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "origin") {
		t.Fatalf("expected origin remote, got:\n%s", output)
	}
	t.Logf("Remotes:\n%s", output)
}

func TestGitRemotePushEmptyDB(t *testing.T) {
	setup := setupGitRemote(t)
	defer setup.cleanup()

	// Push schema-only database
	doltPush(t, setup.sourceDir)

	// Clone and verify schema via CLI
	cloneDir := filepath.Join(setup.baseDir, "clone-empty")
	doltClone(t, setup.remoteURL, cloneDir)

	val := queryScalar(t, cloneDir, "SELECT value FROM config WHERE `key` = 'compaction_enabled'")
	if val != "false" {
		t.Errorf("clone: compaction_enabled = %q, want %q", val, "false")
	}
}

func TestGitRemotePushWithData(t *testing.T) {
	setup := setupGitRemote(t)
	defer setup.cleanup()

	sourceInsertIssue(t, setup.sourceDir, "git-001", "First git remote issue")
	sourceCommitAndPush(t, setup.sourceDir, "Add git-001")

	// Clone and verify
	cloneDir := filepath.Join(setup.baseDir, "clone-data")
	doltClone(t, setup.remoteURL, cloneDir)

	rows := queryCSV(t, cloneDir, "SELECT id, title FROM issues WHERE id = 'git-001'")
	if len(rows) == 0 {
		t.Fatal("clone: expected git-001 to exist")
	}
	if rows[0]["title"] != "First git remote issue" {
		t.Errorf("clone: title = %q, want %q", rows[0]["title"], "First git remote issue")
	}
}

func TestGitRemotePushIdempotent(t *testing.T) {
	setup := setupGitRemote(t)
	defer setup.cleanup()

	sourceInsertIssue(t, setup.sourceDir, "git-idem-1", "Idempotent test")
	sourceCommitAndPush(t, setup.sourceDir, "Add data")

	// Second push with no new changes — should not error
	doltPush(t, setup.sourceDir)
}

func TestGitRemotePushIncremental(t *testing.T) {
	setup := setupGitRemote(t)
	defer setup.cleanup()

	// First batch
	sourceInsertIssue(t, setup.sourceDir, "git-inc-1", "Incremental 1")
	sourceCommitAndPush(t, setup.sourceDir, "First batch")

	// Second batch
	sourceInsertIssue(t, setup.sourceDir, "git-inc-2", "Incremental 2")
	sourceInsertIssue(t, setup.sourceDir, "git-inc-3", "Incremental 3")
	sourceCommitAndPush(t, setup.sourceDir, "Second batch")

	// Clone and verify all three
	cloneDir := filepath.Join(setup.baseDir, "clone-inc")
	doltClone(t, setup.remoteURL, cloneDir)

	for _, id := range []string{"git-inc-1", "git-inc-2", "git-inc-3"} {
		count := queryCount(t, cloneDir, fmt.Sprintf("SELECT COUNT(*) FROM issues WHERE id = '%s'", id))
		if count != 1 {
			t.Errorf("clone: expected %s to exist", id)
		}
	}

	commitCount := queryCount(t, cloneDir, "SELECT COUNT(*) FROM dolt_log")
	if commitCount < 3 {
		t.Errorf("clone: expected at least 3 commits (genesis + 2 batches), got %d", commitCount)
	}
	t.Logf("Clone has %d commits", commitCount)
}

func TestGitRemoteClone(t *testing.T) {
	setup := setupGitRemote(t)
	defer setup.cleanup()

	for i := 1; i <= 5; i++ {
		sourceInsertIssue(t, setup.sourceDir, fmt.Sprintf("clone-%03d", i), fmt.Sprintf("Clone test issue %d", i))
	}
	sourceCommitAndPush(t, setup.sourceDir, "Batch for clone test")

	cloneDir := filepath.Join(setup.baseDir, "full-clone")
	doltClone(t, setup.remoteURL, cloneDir)

	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("clone-%03d", i)
		rows := queryCSV(t, cloneDir, fmt.Sprintf("SELECT title FROM issues WHERE id = '%s'", id))
		if len(rows) == 0 {
			t.Errorf("clone: expected %s to exist", id)
			continue
		}
		expected := fmt.Sprintf("Clone test issue %d", i)
		if rows[0]["title"] != expected {
			t.Errorf("clone: %s title = %q, want %q", id, rows[0]["title"], expected)
		}
	}

	// Verify origin remote on clone
	remoteCount := queryCount(t, cloneDir, "SELECT COUNT(*) FROM dolt_remotes WHERE name = 'origin'")
	if remoteCount != 1 {
		t.Error("clone: expected 'origin' remote")
	}
}

func TestGitRemotePull(t *testing.T) {
	setup := setupGitRemote(t)
	defer setup.cleanup()

	// Push initial data
	sourceInsertIssue(t, setup.sourceDir, "pull-001", "Before pull")
	sourceCommitAndPush(t, setup.sourceDir, "Initial data")

	// Clone
	cloneDir := filepath.Join(setup.baseDir, "pull-clone")
	doltClone(t, setup.remoteURL, cloneDir)

	// Push new data from source
	sourceInsertIssue(t, setup.sourceDir, "pull-002", "After initial clone")
	sourceCommitAndPush(t, setup.sourceDir, "New data")

	// Pull into clone
	doltPull(t, cloneDir)

	// Verify new issue appeared
	rows := queryCSV(t, cloneDir, "SELECT title FROM issues WHERE id = 'pull-002'")
	if len(rows) == 0 {
		t.Fatal("clone: expected pull-002 to exist after pull")
	}
	if rows[0]["title"] != "After initial clone" {
		t.Errorf("clone: pull-002 title = %q, want %q", rows[0]["title"], "After initial clone")
	}
}

func TestGitRemotePullWithLocalChanges(t *testing.T) {
	setup := setupGitRemote(t)
	defer setup.cleanup()

	// Push initial data
	sourceInsertIssue(t, setup.sourceDir, "local-001", "Shared issue")
	sourceCommitAndPush(t, setup.sourceDir, "Initial")

	// Clone
	cloneDir := filepath.Join(setup.baseDir, "local-clone")
	doltClone(t, setup.remoteURL, cloneDir)

	// Make local changes in clone (different issue, no conflict)
	runDoltSQL(t, cloneDir,
		`INSERT INTO issues (id, title, description, design, acceptance_criteria, notes, status, priority, issue_type, created_at, updated_at) `+
			`VALUES ('local-clone-001', 'Clone-only issue', '', '', '', '', 'open', 2, 'task', NOW(), NOW()); `+
			`CALL DOLT_ADD('.'); CALL DOLT_COMMIT('-Am', 'Local change')`)

	// Push new data from source (different issue, no conflict)
	sourceInsertIssue(t, setup.sourceDir, "local-002", "Source-only issue")
	sourceCommitAndPush(t, setup.sourceDir, "Source change")

	// Pull into clone (should merge cleanly)
	doltPull(t, cloneDir)

	// Verify all three issues
	for _, id := range []string{"local-001", "local-002", "local-clone-001"} {
		count := queryCount(t, cloneDir, fmt.Sprintf("SELECT COUNT(*) FROM issues WHERE id = '%s'", id))
		if count != 1 {
			t.Errorf("clone: expected %s to exist after pull", id)
		}
	}
}

func TestGitRemoteRoundTripAllTables(t *testing.T) {
	setup := setupGitRemote(t)
	defer setup.cleanup()

	// Insert parent epic
	runDoltSQL(t, setup.sourceDir,
		`INSERT INTO issues (id, title, description, design, acceptance_criteria, notes, status, priority, issue_type, created_at, updated_at) `+
			`VALUES ('rt-parent', 'Parent Epic', 'Round-trip parent', '', '', '', 'open', 1, 'epic', NOW(), NOW())`)

	// Insert child task
	runDoltSQL(t, setup.sourceDir,
		`INSERT INTO issues (id, title, description, design, acceptance_criteria, notes, status, priority, issue_type, assignee, created_at, updated_at) `+
			`VALUES ('rt-child', 'Child Task', 'Round-trip child with details', '', '', '', 'in_progress', 2, 'task', 'alice', NOW(), NOW())`)

	// Labels
	runDoltSQL(t, setup.sourceDir,
		`INSERT INTO labels (issue_id, label) VALUES ('rt-child', 'urgent'), ('rt-child', 'backend')`)

	// Comments
	runDoltSQL(t, setup.sourceDir,
		`INSERT INTO comments (issue_id, author, text, created_at) VALUES `+
			`('rt-child', 'alice', 'Working on this', NOW()), `+
			`('rt-child', 'bob', 'Looks good', NOW())`)

	// Dependency
	runDoltSQL(t, setup.sourceDir,
		`INSERT INTO dependencies (issue_id, depends_on_id, type, created_at, created_by) `+
			`VALUES ('rt-child', 'rt-parent', 'blocks', NOW(), 'test')`)

	// Config
	runDoltSQL(t, setup.sourceDir,
		"INSERT INTO config (`key`, value) VALUES ('issue_prefix', 'test') ON DUPLICATE KEY UPDATE value='test'")

	sourceCommitAndPush(t, setup.sourceDir, "Rich data for round-trip")

	// Clone and verify via CLI SQL
	cloneDir := filepath.Join(setup.baseDir, "clone-rt")
	doltClone(t, setup.remoteURL, cloneDir)

	// Verify parent epic
	rows := queryCSV(t, cloneDir, "SELECT title, issue_type FROM issues WHERE id = 'rt-parent'")
	if len(rows) == 0 {
		t.Fatal("clone: rt-parent not found")
	}
	if rows[0]["title"] != "Parent Epic" {
		t.Errorf("clone: parent title = %q, want %q", rows[0]["title"], "Parent Epic")
	}
	if rows[0]["issue_type"] != "epic" {
		t.Errorf("clone: parent type = %q, want %q", rows[0]["issue_type"], "epic")
	}

	// Verify child task
	rows = queryCSV(t, cloneDir, "SELECT title, status, assignee FROM issues WHERE id = 'rt-child'")
	if len(rows) == 0 {
		t.Fatal("clone: rt-child not found")
	}
	if rows[0]["title"] != "Child Task" {
		t.Errorf("clone: child title = %q, want %q", rows[0]["title"], "Child Task")
	}
	if rows[0]["status"] != "in_progress" {
		t.Errorf("clone: child status = %q, want %q", rows[0]["status"], "in_progress")
	}
	if rows[0]["assignee"] != "alice" {
		t.Errorf("clone: child assignee = %q, want %q", rows[0]["assignee"], "alice")
	}

	// Verify labels
	labelCount := queryCount(t, cloneDir, "SELECT COUNT(*) FROM labels WHERE issue_id = 'rt-child'")
	if labelCount != 2 {
		t.Errorf("clone: expected 2 labels, got %d", labelCount)
	}
	labelRows := queryCSV(t, cloneDir, "SELECT label FROM labels WHERE issue_id = 'rt-child' ORDER BY label")
	labelSet := map[string]bool{}
	for _, r := range labelRows {
		labelSet[r["label"]] = true
	}
	if !labelSet["urgent"] || !labelSet["backend"] {
		t.Errorf("clone: labels = %v, want {urgent, backend}", labelSet)
	}

	// Verify comments
	commentCount := queryCount(t, cloneDir, "SELECT COUNT(*) FROM comments WHERE issue_id = 'rt-child'")
	if commentCount != 2 {
		t.Errorf("clone: expected 2 comments, got %d", commentCount)
	}

	// Verify dependency
	depRows := queryCSV(t, cloneDir, "SELECT depends_on_id FROM dependencies WHERE issue_id = 'rt-child'")
	if len(depRows) != 1 {
		t.Errorf("clone: expected 1 dependency, got %d", len(depRows))
	} else if depRows[0]["depends_on_id"] != "rt-parent" {
		t.Errorf("clone: dependency target = %q, want %q", depRows[0]["depends_on_id"], "rt-parent")
	}

	// Verify blocked status (rt-child depends on open rt-parent)
	blockerCount := queryCount(t, cloneDir,
		`SELECT COUNT(*) FROM dependencies d JOIN issues i ON d.depends_on_id = i.id `+
			`WHERE d.issue_id = 'rt-child' AND i.status IN ('open', 'in_progress')`)
	if blockerCount != 1 {
		t.Errorf("clone: expected rt-child to be blocked by 1 issue, got %d", blockerCount)
	}

	// Verify config
	prefix := queryScalar(t, cloneDir, "SELECT value FROM config WHERE `key` = 'issue_prefix'")
	if prefix != "test" {
		t.Errorf("clone: issue_prefix = %q, want %q", prefix, "test")
	}
}

func TestGitRemoteSpecialCharacters(t *testing.T) {
	setup := setupGitRemote(t)
	defer setup.cleanup()

	specials := []struct {
		id    string
		title string
		desc  string
	}{
		{"spec-unicode", "日本語テスト: Dolt リモート", "Unicode: 你好世界"},
		{"spec-quotes", `Title with "double quotes"`, "Description with `backticks`"},
		{"spec-html", "Title <b>bold</b> & entities", "<script>alert(1)</script>"},
		{"spec-long", "A very long title that exceeds typical display widths and contains lots of words to test truncation behavior across the git remote boundary", "Short desc"},
		{"spec-empty-desc", "No description issue", ""},
	}

	for _, s := range specials {
		sourceInsertIssueDesc(t, setup.sourceDir, s.id, s.title, s.desc)
	}
	sourceCommitAndPush(t, setup.sourceDir, "Special characters batch")

	// Clone and verify
	cloneDir := filepath.Join(setup.baseDir, "clone-special")
	doltClone(t, setup.remoteURL, cloneDir)

	for _, s := range specials {
		rows := queryCSV(t, cloneDir, fmt.Sprintf(
			"SELECT title, description FROM issues WHERE id = '%s'", escapeSQL(s.id)))
		if len(rows) == 0 {
			t.Errorf("clone: expected %s to exist", s.id)
			continue
		}
		if rows[0]["title"] != s.title {
			t.Errorf("clone: %s title mismatch:\n  got:  %q\n  want: %q", s.id, rows[0]["title"], s.title)
		}
		if rows[0]["description"] != s.desc {
			t.Errorf("clone: %s desc mismatch:\n  got:  %q\n  want: %q", s.id, rows[0]["description"], s.desc)
		}
	}
}
