package syncbranch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/git"
)

// CommitResult contains information about a worktree commit operation
type CommitResult struct {
	Committed  bool   // True if changes were committed
	Pushed     bool   // True if changes were pushed
	Branch     string // The sync branch name
	Message    string // Commit message used
}

// PullResult contains information about a worktree pull operation
type PullResult struct {
	Pulled     bool   // True if pull was performed
	Branch     string // The sync branch name
	JSONLPath  string // Path to the synced JSONL in main repo
}

// CommitToSyncBranch commits JSONL changes to the sync branch using a git worktree.
// This allows committing to a different branch without changing the user's working directory.
//
// Parameters:
//   - ctx: Context for cancellation
//   - repoRoot: Path to the git repository root
//   - syncBranch: Name of the sync branch (e.g., "beads-sync")
//   - jsonlPath: Absolute path to the JSONL file in the main repo
//   - push: If true, push to remote after commit
//
// Returns CommitResult with details about what was done, or error if failed.
func CommitToSyncBranch(ctx context.Context, repoRoot, syncBranch, jsonlPath string, push bool) (*CommitResult, error) {
	result := &CommitResult{
		Branch: syncBranch,
	}

	// Worktree path is under .git/beads-worktrees/<branch>
	worktreePath := filepath.Join(repoRoot, ".git", "beads-worktrees", syncBranch)

	// Initialize worktree manager
	wtMgr := git.NewWorktreeManager(repoRoot)

	// Ensure worktree exists
	if err := wtMgr.CreateBeadsWorktree(syncBranch, worktreePath); err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	// Check worktree health and repair if needed
	if err := wtMgr.CheckWorktreeHealth(worktreePath); err != nil {
		// Try to recreate worktree
		if err := wtMgr.RemoveBeadsWorktree(worktreePath); err != nil {
			// Log but continue - removal might fail but recreation might work
			_ = err
		}
		if err := wtMgr.CreateBeadsWorktree(syncBranch, worktreePath); err != nil {
			return nil, fmt.Errorf("failed to recreate worktree after health check: %w", err)
		}
	}

	// Convert absolute path to relative path from repo root
	jsonlRelPath, err := filepath.Rel(repoRoot, jsonlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get relative JSONL path: %w", err)
	}

	// Sync JSONL file to worktree
	if err := wtMgr.SyncJSONLToWorktree(worktreePath, jsonlRelPath); err != nil {
		return nil, fmt.Errorf("failed to sync JSONL to worktree: %w", err)
	}

	// Also sync other beads files (deletions.jsonl, metadata.json)
	beadsDir := filepath.Dir(jsonlPath)
	for _, filename := range []string{"deletions.jsonl", "metadata.json"} {
		srcPath := filepath.Join(beadsDir, filename)
		if _, err := os.Stat(srcPath); err == nil {
			relPath, err := filepath.Rel(repoRoot, srcPath)
			if err == nil {
				_ = wtMgr.SyncJSONLToWorktree(worktreePath, relPath) // Best effort
			}
		}
	}

	// Check for changes in worktree
	worktreeJSONLPath := filepath.Join(worktreePath, jsonlRelPath)
	hasChanges, err := hasChangesInWorktree(ctx, worktreePath, worktreeJSONLPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check for changes in worktree: %w", err)
	}

	if !hasChanges {
		return result, nil // No changes to commit
	}

	// Commit in worktree
	result.Message = fmt.Sprintf("bd sync: %s", time.Now().Format("2006-01-02 15:04:05"))
	if err := commitInWorktree(ctx, worktreePath, jsonlRelPath, result.Message); err != nil {
		return nil, fmt.Errorf("failed to commit in worktree: %w", err)
	}
	result.Committed = true

	// Push if enabled
	if push {
		if err := pushFromWorktree(ctx, worktreePath, syncBranch); err != nil {
			return nil, fmt.Errorf("failed to push from worktree: %w", err)
		}
		result.Pushed = true
	}

	return result, nil
}

// PullFromSyncBranch pulls changes from the sync branch and copies JSONL to the main repo.
// This fetches remote changes without affecting the user's working directory.
//
// Parameters:
//   - ctx: Context for cancellation
//   - repoRoot: Path to the git repository root
//   - syncBranch: Name of the sync branch (e.g., "beads-sync")
//   - jsonlPath: Absolute path to the JSONL file in the main repo
//
// Returns PullResult with details about what was done, or error if failed.
func PullFromSyncBranch(ctx context.Context, repoRoot, syncBranch, jsonlPath string) (*PullResult, error) {
	result := &PullResult{
		Branch:    syncBranch,
		JSONLPath: jsonlPath,
	}

	// Worktree path is under .git/beads-worktrees/<branch>
	worktreePath := filepath.Join(repoRoot, ".git", "beads-worktrees", syncBranch)

	// Initialize worktree manager
	wtMgr := git.NewWorktreeManager(repoRoot)

	// Ensure worktree exists
	if err := wtMgr.CreateBeadsWorktree(syncBranch, worktreePath); err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	// Get remote name
	remote := getRemoteForBranch(ctx, worktreePath, syncBranch)

	// Pull in worktree
	cmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "pull", remote, syncBranch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's just "already up to date" or similar non-error
		if strings.Contains(string(output), "Already up to date") {
			result.Pulled = true
			// Still copy JSONL in case worktree has changes we haven't synced
		} else {
			return nil, fmt.Errorf("git pull failed in worktree: %w\n%s", err, output)
		}
	} else {
		result.Pulled = true
	}

	// Convert absolute path to relative path from repo root
	jsonlRelPath, err := filepath.Rel(repoRoot, jsonlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get relative JSONL path: %w", err)
	}

	// Copy JSONL from worktree to main repo
	worktreeJSONLPath := filepath.Join(worktreePath, jsonlRelPath)

	// Check if worktree JSONL exists
	if _, err := os.Stat(worktreeJSONLPath); os.IsNotExist(err) {
		// No JSONL in worktree yet, nothing to sync
		return result, nil
	}

	// Copy JSONL from worktree to main repo
	data, err := os.ReadFile(worktreeJSONLPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read worktree JSONL: %w", err)
	}

	if err := os.WriteFile(jsonlPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write main JSONL: %w", err)
	}

	// Also sync other beads files back (deletions.jsonl, metadata.json)
	beadsDir := filepath.Dir(jsonlPath)
	for _, filename := range []string{"deletions.jsonl", "metadata.json"} {
		worktreeSrcPath := filepath.Join(worktreePath, ".beads", filename)
		if data, err := os.ReadFile(worktreeSrcPath); err == nil {
			dstPath := filepath.Join(beadsDir, filename)
			_ = os.WriteFile(dstPath, data, 0644) // Best effort
		}
	}

	return result, nil
}

// hasChangesInWorktree checks if there are uncommitted changes in the worktree
func hasChangesInWorktree(ctx context.Context, worktreePath, filePath string) (bool, error) {
	// Check the entire .beads directory for changes
	beadsDir := filepath.Dir(filePath)
	relBeadsDir, err := filepath.Rel(worktreePath, beadsDir)
	if err != nil {
		// Fallback to checking just the file
		relPath, err := filepath.Rel(worktreePath, filePath)
		if err != nil {
			return false, fmt.Errorf("failed to make path relative: %w", err)
		}
		cmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "status", "--porcelain", relPath)
		output, err := cmd.Output()
		if err != nil {
			return false, fmt.Errorf("git status failed in worktree: %w", err)
		}
		return len(strings.TrimSpace(string(output))) > 0, nil
	}

	cmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "status", "--porcelain", relBeadsDir)
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status failed in worktree: %w", err)
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}

// commitInWorktree stages and commits changes in the worktree
func commitInWorktree(ctx context.Context, worktreePath, jsonlRelPath, message string) error {
	// Stage the entire .beads directory
	beadsRelDir := filepath.Dir(jsonlRelPath)

	addCmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "add", beadsRelDir)
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add failed in worktree: %w", err)
	}

	// Commit with --no-verify to skip hooks (pre-commit hook would fail in worktree context)
	// The worktree is internal to bd sync, so we don't need to run bd's pre-commit hook
	commitCmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "commit", "--no-verify", "-m", message)
	output, err := commitCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git commit failed in worktree: %w\n%s", err, output)
	}

	return nil
}

// pushFromWorktree pushes the sync branch from the worktree
func pushFromWorktree(ctx context.Context, worktreePath, branch string) error {
	remote := getRemoteForBranch(ctx, worktreePath, branch)

	// Push with explicit remote and branch, set upstream if not set
	cmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "push", "--set-upstream", remote, branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push failed from worktree: %w\n%s", err, output)
	}

	return nil
}

// getRemoteForBranch gets the remote name for a branch, defaulting to "origin"
func getRemoteForBranch(ctx context.Context, worktreePath, branch string) string {
	remoteCmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "config", "--get", fmt.Sprintf("branch.%s.remote", branch))
	remoteOutput, err := remoteCmd.Output()
	if err != nil {
		return "origin" // Default
	}
	return strings.TrimSpace(string(remoteOutput))
}

// GetRepoRoot returns the git repository root directory
func GetRepoRoot(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git root: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// HasGitRemote checks if any git remote exists
func HasGitRemote(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "git", "remote")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(output))) > 0
}
