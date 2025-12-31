package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitignoreTemplate is the canonical .beads/.gitignore content
const GitignoreTemplate = `# SQLite databases
*.db
*.db?*
*.db-journal
*.db-wal
*.db-shm

# Daemon runtime files
daemon.lock
daemon.log
daemon.pid
bd.sock
sync-state.json
last-touched

# Local version tracking (prevents upgrade notification spam after git ops)
.local_version

# Worktree redirect file (created by bd worktree, points to main repo's .beads)
redirect

# Legacy database files
db.sqlite
bd.db

# Merge artifacts (temporary files from 3-way merge)
beads.base.jsonl
beads.base.meta.json
beads.left.jsonl
beads.left.meta.json
beads.right.jsonl
beads.right.meta.json

# NOTE: Do NOT add negation patterns (e.g., !issues.jsonl) here.
# They would override fork protection in .git/info/exclude, allowing
# contributors to accidentally commit upstream issue databases.
# The JSONL files (issues.jsonl, interactions.jsonl) and config files
# are tracked by git by default since no pattern above ignores them.
`

// requiredPatterns are patterns that MUST be in .beads/.gitignore
var requiredPatterns = []string{
	"beads.base.jsonl",
	"beads.left.jsonl",
	"beads.right.jsonl",
	"beads.base.meta.json",
	"beads.left.meta.json",
	"beads.right.meta.json",
	"*.db?*",
	"redirect", // worktree redirect files should never be committed
}

// CheckGitignore checks if .beads/.gitignore is up to date
func CheckGitignore() DoctorCheck {
	gitignorePath := filepath.Join(".beads", ".gitignore")
	
	// Check if file exists
	content, err := os.ReadFile(gitignorePath) // #nosec G304 -- path is hardcoded
	if err != nil {
		return DoctorCheck{
			Name:    "Gitignore",
			Status:  "warning",
			Message: ".beads/.gitignore not found",
			Fix:     "Run: bd init (safe to re-run) or bd doctor --fix",
		}
	}

	// Check for required patterns
	contentStr := string(content)
	var missing []string
	for _, pattern := range requiredPatterns {
		if !strings.Contains(contentStr, pattern) {
			missing = append(missing, pattern)
		}
	}

	if len(missing) > 0 {
		return DoctorCheck{
			Name:    "Gitignore",
			Status:  "warning",
			Message: "Outdated .beads/.gitignore (missing merge artifact patterns)",
			Detail:  "Missing: " + strings.Join(missing, ", "),
			Fix:     "Run: bd doctor --fix or bd init (safe to re-run)",
		}
	}

	return DoctorCheck{
		Name:    "Gitignore",
		Status:  "ok",
		Message: "Up to date",
	}
}

// FixGitignore updates .beads/.gitignore to the current template
func FixGitignore() error {
	gitignorePath := filepath.Join(".beads", ".gitignore")

	// If file exists and is read-only, fix permissions first
	if info, err := os.Stat(gitignorePath); err == nil {
		if info.Mode().Perm()&0200 == 0 { // No write permission for owner
			if err := os.Chmod(gitignorePath, 0600); err != nil {
				return err
			}
		}
	}

	// Write canonical template with secure file permissions
	if err := os.WriteFile(gitignorePath, []byte(GitignoreTemplate), 0600); err != nil {
		return err
	}

	// Ensure permissions are set correctly (some systems respect umask)
	if err := os.Chmod(gitignorePath, 0600); err != nil {
		return err
	}

	return nil
}

// CheckRedirectNotTracked verifies that .beads/redirect is NOT tracked by git.
// Redirect files are created by bd worktree and should never be committed.
// If accidentally committed (e.g., via git add .), they cause "redirect target does not exist"
// warnings in other clones.
func CheckRedirectNotTracked() DoctorCheck {
	redirectPath := filepath.Join(".beads", "redirect")

	// Check if file exists
	if _, err := os.Stat(redirectPath); os.IsNotExist(err) {
		// File doesn't exist - nothing to check
		return DoctorCheck{
			Name:    "Redirect Not Tracked",
			Status:  StatusOK,
			Message: "No redirect file (not a worktree)",
		}
	}

	// Check if git tracks this file
	// git ls-files exits 0 and outputs filename if tracked, empty if not
	cmd := exec.Command("git", "ls-files", redirectPath)
	output, err := cmd.Output()
	if err != nil {
		// Not in a git repo or other error - skip
		return DoctorCheck{
			Name:    "Redirect Not Tracked",
			Status:  StatusOK,
			Message: "N/A (not a git repository)",
		}
	}

	if strings.TrimSpace(string(output)) != "" {
		// File is tracked - this is bad
		return DoctorCheck{
			Name:    "Redirect Not Tracked",
			Status:  StatusWarning,
			Message: "Redirect file is tracked by git",
			Detail:  "The .beads/redirect file was accidentally committed. This causes 'redirect target does not exist' warnings in other clones.",
			Fix:     "Run 'bd doctor --fix' to untrack, or manually: git rm --cached .beads/redirect",
		}
	}

	return DoctorCheck{
		Name:    "Redirect Not Tracked",
		Status:  StatusOK,
		Message: "Redirect file not tracked (correct)",
	}
}

// FixRedirectTracking untracks .beads/redirect if it was accidentally committed.
func FixRedirectTracking() error {
	redirectPath := filepath.Join(".beads", "redirect")

	// Check if file exists
	if _, err := os.Stat(redirectPath); os.IsNotExist(err) {
		return nil // Nothing to fix
	}

	// Check if tracked
	cmd := exec.Command("git", "ls-files", redirectPath)
	output, err := cmd.Output()
	if err != nil {
		return nil // Not in a git repo
	}

	if strings.TrimSpace(string(output)) == "" {
		return nil // Not tracked, nothing to fix
	}

	// Untrack the file (keep it on disk)
	cmd = exec.Command("git", "rm", "--cached", redirectPath)
	return cmd.Run()
}

// CheckIssuesTracking verifies that issues.jsonl is tracked by git.
// This catches cases where global gitignore patterns (e.g., *.jsonl) would
// cause issues.jsonl to be ignored, breaking bd sync.
func CheckIssuesTracking() DoctorCheck {
	issuesPath := filepath.Join(".beads", "issues.jsonl")

	// First check if the file exists
	if _, err := os.Stat(issuesPath); os.IsNotExist(err) {
		// File doesn't exist yet - not an error, bd init may not have been run
		return DoctorCheck{
			Name:   "Issues Tracking",
			Status: "ok",
			Message: "No issues.jsonl yet (will be created on first issue)",
		}
	}

	// Check if git considers this file ignored
	// git check-ignore exits 0 if ignored, 1 if not ignored, 128 if error
	cmd := exec.Command("git", "check-ignore", "-q", issuesPath)
	err := cmd.Run()

	if err == nil {
		// Exit code 0 means the file IS ignored - this is bad
		// Get details about what's ignoring it
		detailCmd := exec.Command("git", "check-ignore", "-v", issuesPath)
		output, _ := detailCmd.Output()
		detail := strings.TrimSpace(string(output))

		return DoctorCheck{
			Name:    "Issues Tracking",
			Status:  "warning",
			Message: "issues.jsonl is ignored by git (bd sync will fail)",
			Detail:  detail,
			Fix:     "Check global gitignore: git config --global core.excludesfile",
		}
	}

	// Exit code 1 means not ignored (good), any other error we ignore
	return DoctorCheck{
		Name:    "Issues Tracking",
		Status:  "ok",
		Message: "issues.jsonl is tracked by git",
	}
}
