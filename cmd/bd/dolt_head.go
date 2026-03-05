package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/storage/dolt"
)

// writeBeadsRefs writes the current Dolt branch and commit hash to the beads
// ref files, mirroring git's own .git/HEAD + .git/refs/heads/ structure:
//
//	.beads/HEAD                  ← "ref: refs/heads/<branch>"
//	.beads/refs/heads/<branch>   ← "<dolt-commit-hash>"
//
// Called after every Dolt commit (auto-commit and explicit).
func writeBeadsRefs(ctx context.Context, s *dolt.DoltStore) {
	if s == nil || s.IsClosed() {
		return
	}

	beadsDir := filepath.Dir(s.Path())

	hash, err := s.GetCurrentCommit(ctx)
	if err != nil {
		return // best effort
	}

	branch, err := s.CurrentBranch(ctx)
	if err != nil {
		branch = "main" // fallback
	}

	// Write .beads/HEAD
	headPath := filepath.Join(beadsDir, "HEAD")
	headContent := fmt.Sprintf("ref: refs/heads/%s\n", branch)
	if err := os.WriteFile(headPath, []byte(headContent), 0644); err != nil {
		return // best effort
	}

	// Write .beads/refs/heads/<branch>
	refsDir := filepath.Join(beadsDir, "refs", "heads")
	if err := os.MkdirAll(refsDir, 0755); err != nil {
		return // best effort
	}
	refPath := filepath.Join(refsDir, branch)
	if err := os.WriteFile(refPath, []byte(hash+"\n"), 0644); err != nil {
		return // best effort
	}

	// git add both files (best effort — may not be in a git repo)
	projectRoot := filepath.Dir(beadsDir)
	cmd := exec.CommandContext(ctx, "git", "add", headPath, refPath)
	cmd.Dir = projectRoot
	_ = cmd.Run()
}

// readBeadsRefs reads .beads/HEAD and the corresponding ref file to determine
// the saved Dolt branch and commit hash. Returns empty strings if files don't
// exist or can't be parsed.
func readBeadsRefs(beadsDir string) (commitHash, branch string) {
	// Read .beads/HEAD → "ref: refs/heads/<branch>"
	headPath := filepath.Join(beadsDir, "HEAD")
	headData, err := os.ReadFile(headPath)
	if err != nil {
		return "", ""
	}

	headLine := strings.TrimSpace(string(headData))
	if !strings.HasPrefix(headLine, "ref: refs/heads/") {
		return "", ""
	}
	branch = strings.TrimPrefix(headLine, "ref: refs/heads/")

	// Read .beads/refs/heads/<branch> → "<hash>"
	refPath := filepath.Join(beadsDir, "refs", "heads", branch)
	refData, err := os.ReadFile(refPath)
	if err != nil {
		return "", branch
	}

	commitHash = strings.TrimSpace(string(refData))
	return commitHash, branch
}
