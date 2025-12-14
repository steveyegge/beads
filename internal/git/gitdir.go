package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GetGitDir returns the actual .git directory path for the current repository.
// In a normal repo, this is ".git". In a worktree, .git is a file
// containing "gitdir: /path/to/actual/git/dir", so we use git rev-parse.
//
// This function uses Git's native worktree-aware APIs and should be used
// instead of direct filepath.Join(path, ".git") throughout the codebase.
func GetGitDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// GetGitHooksDir returns the path to the Git hooks directory.
// This function is worktree-aware and handles both regular repos and worktrees.
func GetGitHooksDir() (string, error) {
	gitDir, err := GetGitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(gitDir, "hooks"), nil
}

// GetGitRefsDir returns the path to the Git refs directory.
// This function is worktree-aware and handles both regular repos and worktrees.
func GetGitRefsDir() (string, error) {
	gitDir, err := GetGitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(gitDir, "refs"), nil
}

// GetGitHeadPath returns the path to the Git HEAD file.
// This function is worktree-aware and handles both regular repos and worktrees.
func GetGitHeadPath() (string, error) {
	gitDir, err := GetGitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(gitDir, "HEAD"), nil
}

// IsWorktree returns true if the current directory is in a Git worktree.
// This is determined by comparing --git-dir and --git-common-dir.
func IsWorktree() bool {
	gitDir := getGitDirNoError("--git-dir")
	if gitDir == "" {
		return false
	}

	commonDir := getGitDirNoError("--git-common-dir")
	if commonDir == "" {
		return false
	}

	absGit, err1 := filepath.Abs(gitDir)
	absCommon, err2 := filepath.Abs(commonDir)
	if err1 != nil || err2 != nil {
		return false
	}

	return absGit != absCommon
}

// GetMainRepoRoot returns the main repository root directory.
// When in a worktree, this returns the main repository root.
// Otherwise, it returns the regular repository root.
func GetMainRepoRoot() (string, error) {
	if IsWorktree() {
		// In worktree: read .git file to find main repo
		gitFileContent := getGitDirNoError("--git-dir")
		if gitFileContent == "" {
			return "", fmt.Errorf("not a git repository")
		}

		// If gitFileContent contains "worktrees", it's a worktree path
		// Read the .git file to get the main git dir
		if strings.Contains(gitFileContent, "worktrees") {
			content, err := exec.Command("cat", ".git").Output()
			if err == nil {
				line := strings.TrimSpace(string(content))
				if strings.HasPrefix(line, "gitdir: ") {
					gitDir := strings.TrimPrefix(line, "gitdir: ")
					// Remove /worktrees/* part
					if idx := strings.Index(gitDir, "/worktrees/"); idx > 0 {
						gitDir = gitDir[:idx]
					}
					return filepath.Dir(gitDir), nil
				}
			}
		}

		// Fallback: use --git-common-dir with validation
		commonDir := getGitDirNoError("--git-common-dir")
		if commonDir != "" {
			// Validate that commonDir exists
			if info, err := os.Stat(commonDir); err == nil && info.IsDir() {
				return filepath.Dir(commonDir), nil
			}
		}

		return "", fmt.Errorf("unable to determine main repository root")
	} else {
		gitDir, err := GetGitDir()
		if err != nil {
			return "", err
		}
		return filepath.Dir(gitDir), nil
	}
}

// getGitDirNoError is a helper that returns empty string on error
// to avoid cluttering code with error handling for simple checks.
func getGitDirNoError(flag string) string {
	cmd := exec.Command("git", "rev-parse", flag)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
