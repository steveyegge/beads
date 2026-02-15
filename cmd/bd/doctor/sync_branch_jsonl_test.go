package doctor

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupSyncBranchWorktreeRepo(t *testing.T) (repoPath, worktreePath string) {
	t.Helper()

	repoPath = mkTmpDirInTmp(t, "bd-doctor-sync-worktree-*")
	initRepo(t, repoPath, "main")
	commitFile(t, repoPath, "README.md", "# test\n", "initial commit")
	commitFile(t, repoPath, ".gitignore", ".beads/beads.db\n.beads/beads.db-wal\n.beads/beads.db-shm\n", "ignore sqlite db")
	commitFile(t, repoPath, ".beads/config.yaml", "sync-branch: beads-sync\n", "configure sync branch")
	commitFile(t, repoPath, ".beads/issues.jsonl", `{"id":"bd-1","title":"Issue 1","status":"open"}`+"\n", "add main jsonl")

	runGit(t, repoPath, "branch", "beads-sync")

	worktreePath = filepath.Join(repoPath, ".git", "beads-worktrees", "beads-sync")
	runGit(t, repoPath, "worktree", "add", worktreePath, "beads-sync")

	return repoPath, worktreePath
}

func writeJSONLIssues(t *testing.T, jsonlPath string, count int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(jsonlPath), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(jsonlPath), err)
	}

	var b strings.Builder
	for i := 1; i <= count; i++ {
		b.WriteString(fmt.Sprintf(`{"id":"bd-%d","title":"Issue %d","status":"open"}`, i, i))
		b.WriteString("\n")
	}
	if err := os.WriteFile(jsonlPath, []byte(b.String()), 0644); err != nil {
		t.Fatalf("write %s: %v", jsonlPath, err)
	}
}

// createSyncDoctorTestDB creates a test SQLite database with issues and optional hash metadata.
// If jsonlContentHash is non-empty, it is stored as jsonl_content_hash in the metadata table.
func createSyncDoctorTestDB(t *testing.T, dbPath string, issueCount int, jsonlContentHash string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(dbPath), err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("CREATE TABLE issues (id TEXT PRIMARY KEY, status TEXT, ephemeral INTEGER)"); err != nil {
		t.Fatalf("create issues table: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE config (key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatalf("create config table: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatalf("create metadata table: %v", err)
	}

	for i := 1; i <= issueCount; i++ {
		if _, err := db.Exec("INSERT INTO issues (id, status, ephemeral) VALUES (?, ?, ?)", fmt.Sprintf("bd-%d", i), "open", 0); err != nil {
			t.Fatalf("insert issue %d: %v", i, err)
		}
	}

	if jsonlContentHash != "" {
		if _, err := db.Exec("INSERT INTO metadata (key, value) VALUES (?, ?)", "jsonl_content_hash", jsonlContentHash); err != nil {
			t.Fatalf("insert metadata: %v", err)
		}
	}
}

func TestCheckDatabaseJSONLSync_UsesSyncWorktreeJSONL(t *testing.T) {
	repoPath, worktreePath := setupSyncBranchWorktreeRepo(t)

	mainJSONL := filepath.Join(repoPath, ".beads", "issues.jsonl")
	worktreeJSONL := filepath.Join(worktreePath, ".beads", "issues.jsonl")
	dbPath := filepath.Join(repoPath, ".beads", "beads.db")

	// Main JSONL is stale, sync worktree JSONL has current state.
	writeJSONLIssues(t, mainJSONL, 1)
	writeJSONLIssues(t, worktreeJSONL, 3)
	createSyncDoctorTestDB(t, dbPath, 3, "")

	check := CheckDatabaseJSONLSync(repoPath)
	if check.Status != StatusOK {
		t.Fatalf("status=%q want %q (msg=%q detail=%q)", check.Status, StatusOK, check.Message, check.Detail)
	}
}

// TestCheckSyncDivergence_UsesSyncWorktreeJSONLForHash verifies that the hash-based
// divergence check uses the sync worktree JSONL (not main JSONL) when sync-branch is configured.
func TestCheckSyncDivergence_UsesSyncWorktreeJSONLForHash(t *testing.T) {
	repoPath, worktreePath := setupSyncBranchWorktreeRepo(t)

	mainJSONL := filepath.Join(repoPath, ".beads", "issues.jsonl")
	worktreeJSONL := filepath.Join(worktreePath, ".beads", "issues.jsonl")
	dbPath := filepath.Join(repoPath, ".beads", "beads.db")

	// main JSONL has different content from worktree JSONL
	writeJSONLIssues(t, mainJSONL, 1)
	writeJSONLIssues(t, worktreeJSONL, 1)

	// compute hash of the worktree JSONL (the one the check should use)
	worktreeHash, err := computeFileHash(worktreeJSONL)
	if err != nil {
		t.Fatalf("compute hash: %v", err)
	}

	createSyncDoctorTestDB(t, dbPath, 1, worktreeHash)

	check := CheckSyncDivergence(repoPath)
	if check.Status != StatusOK {
		t.Fatalf("status=%q want %q (msg=%q detail=%q)", check.Status, StatusOK, check.Message, check.Detail)
	}
}
