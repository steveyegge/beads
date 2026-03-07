package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/debug"
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
	if !config.IsBranchStrategyEnabled() {
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

	// git add ref files (best effort — may not be in a git repo)
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

// checkBeadsRefSync compares .beads/refs against the current Dolt state and
// takes action based on branch_strategy.* settings. Called from PersistentPreRun
// after store initialization.
//
// Settings (all default to false):
//   - branch_strategy.prompt: show interactive prompt at decision points
//   - branch_strategy.defaults.reset_dolt_with_git: auto-reset Dolt on mismatch
//
// Behavior matrix:
//
//	prompt=false, reset=false → silent (log to debug, no action)
//	prompt=false, reset=true  → auto-reset, message to stderr
//	prompt=true,  reset=false → prompt [y/N], default keeps current state
//	prompt=true,  reset=true  → prompt [Y/n], default resets
func checkBeadsRefSync(ctx context.Context, s *dolt.DoltStore) {
	if s == nil || s.IsClosed() {
		return
	}

	beadsDir := filepath.Dir(s.Path())

	if !config.IsBranchStrategyEnabled() {
		// Refs disabled — clean up stale ref files if they exist
		cleanupStaleBeadsRefs(beadsDir)
		return
	}

	savedHash, savedBranch := readBeadsRefs(beadsDir)
	if savedHash == "" {
		// No ref files — pre-feature commit or first use.
		// Check if current branch needs strategy registration.
		maybePromptBranchStrategy(ctx, s)
		return
	}

	// Get current Dolt state
	currentHash, err := s.GetCurrentCommit(ctx)
	if err != nil {
		return // can't check — skip silently
	}
	currentBranch, err := s.CurrentBranch(ctx)
	if err != nil {
		return
	}

	// Check if Dolt branch matches what .beads/HEAD says
	if savedBranch != "" && savedBranch != currentBranch {
		if err := s.Checkout(ctx, savedBranch); err != nil {
			debug.Logf("beads ref sync: could not switch Dolt to branch %s: %v", savedBranch, err)
			return
		}
		currentHash, err = s.GetCurrentCommit(ctx)
		if err != nil {
			return
		}
		currentBranch = savedBranch
	}

	// Compare commit hashes
	if currentHash == savedHash {
		maybePromptBranchStrategy(ctx, s)
		return // In sync
	}

	// Mismatch detected — read settings from config.yaml
	// (git-tracked, survives DOLT_RESET --hard unlike the Dolt config table)
	promptEnabled := config.GetBool("branch_strategy.prompt")
	resetDefault := config.GetBool("branch_strategy.defaults.reset_dolt_with_git")

	debug.Logf("beads ref sync: mismatch on %s (have %s, want %s) prompt=%v reset=%v",
		currentBranch, truncHash(currentHash), truncHash(savedHash), promptEnabled, resetDefault)

	if !promptEnabled && !resetDefault {
		// Silent mode (default) — detect but take no action
		return
	}

	if !promptEnabled && resetDefault {
		// Auto-reset without prompting
		if err := s.ResetToCommit(ctx, savedHash); err != nil {
			fmt.Fprintf(os.Stderr, "beads: auto-sync reset failed: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "beads: auto-synced Dolt to %s (branch %s)\n", truncHash(savedHash), currentBranch)
		}
		return
	}

	// Prompt mode — need a TTY
	if !isTerminal() {
		fmt.Fprintf(os.Stderr, "beads: warning: Dolt state mismatch (have %s, want %s). "+
			"Set branch_strategy.defaults.reset_dolt_with_git=true to auto-reset.\n",
			truncHash(currentHash), truncHash(savedHash))
		return
	}

	// Build prompt
	defaultIsReset := resetDefault // true when both prompt and reset are true
	fmt.Fprintf(os.Stderr, "\nbeads: Reset Beads to match Git history? (Dolt state at Git commit was previously %s.)\n", truncHash(savedHash))
	fmt.Fprintf(os.Stderr, "\n  Yes, DOLT_RESET --hard %s\n", truncHash(savedHash))
	fmt.Fprintf(os.Stderr, "  No, preserve Dolt state at %s\n", truncHash(currentHash))
	fmt.Fprintf(os.Stderr, "\n  Note: If you preserve Dolt state at %s, you can manually\n", truncHash(currentHash))
	fmt.Fprintf(os.Stderr, "  `DOLT_RESET --hard %s` later. If you reset now, Dolt state at\n", truncHash(savedHash))
	fmt.Fprintf(os.Stderr, "  %s is still recoverable by hash (`DOLT_RESET --hard %s`)\n", truncHash(currentHash), truncHash(currentHash))
	fmt.Fprintf(os.Stderr, "  but no branch will point to it.\n")

	if defaultIsReset {
		fmt.Fprintf(os.Stderr, "\nChoice [Y/n]: ")
	} else {
		fmt.Fprintf(os.Stderr, "\nChoice [y/N]: ")
	}

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return // EOF or error — keep current
	}
	choice := strings.TrimSpace(strings.ToLower(line))

	shouldReset := false
	if choice == "" {
		shouldReset = defaultIsReset // Enter = default
	} else if choice == "y" || choice == "yes" {
		shouldReset = true
	}

	if shouldReset {
		if err := s.ResetToCommit(ctx, savedHash); err != nil {
			fmt.Fprintf(os.Stderr, "beads: reset failed: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "beads: Dolt reset to %s\n", truncHash(savedHash))
		}
	} else {
		fmt.Fprintf(os.Stderr, "beads: keeping current Dolt state\n")
	}
}

// cleanupStaleBeadsRefs stages deletion of ref files when branch_strategy is
// disabled but ref files still exist from a previous configuration. Warns the
// user and suggests a commit message.
func cleanupStaleBeadsRefs(beadsDir string) {
	headPath := filepath.Join(beadsDir, "HEAD")
	if _, err := os.Stat(headPath); os.IsNotExist(err) {
		return // no ref files — nothing to clean up
	}

	projectRoot := filepath.Dir(beadsDir)
	refsDir := filepath.Join(beadsDir, "refs")

	// Stage deletion of ref files
	cmd := exec.CommandContext(context.Background(), "git", "rm", "--cached", "-r", "--ignore-unmatch", headPath)
	cmd.Dir = projectRoot
	_ = cmd.Run()

	cmd = exec.CommandContext(context.Background(), "git", "rm", "--cached", "-r", "--ignore-unmatch", refsDir)
	cmd.Dir = projectRoot
	_ = cmd.Run()

	// Remove from disk
	os.Remove(headPath)
	os.RemoveAll(refsDir)

	fmt.Fprintf(os.Stderr, "beads: branch_strategy disabled — removed stale ref files (.beads/HEAD, .beads/refs/)\n")
	fmt.Fprintf(os.Stderr, "beads: suggested commit: git commit -m \"chore: remove beads ref files (branch_strategy disabled)\"\n")
}

// truncHash returns the first 8 characters of a hash, or the full string if shorter.
func truncHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}
