package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

	// Write .beads/sync_config with branch_strategy settings.
	// This file is git-tracked so it survives Dolt resets, unlike
	// the Dolt config table which gets wiped by DOLT_RESET --hard.
	syncCfgPath := filepath.Join(beadsDir, "sync_config")
	writeSyncConfig(ctx, s, syncCfgPath)

	// git add all ref and config files (best effort — may not be in a git repo)
	projectRoot := filepath.Dir(beadsDir)
	cmd := exec.CommandContext(ctx, "git", "add", headPath, refPath, syncCfgPath)
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

	// Mismatch detected — read settings from git-tracked sync_config
	// (not from Dolt, which may have been wiped by a prior reset)
	syncCfg := readSyncConfig(beadsDir)
	promptEnabled := syncConfigBool(syncCfg, "branch_strategy.prompt")
	resetDefault := syncConfigBool(syncCfg, "branch_strategy.defaults.reset_dolt_with_git")

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

// configBool reads a boolean config value from the Dolt store's config table.
// Returns false if the key is not set, empty, or on any error.
func configBool(ctx context.Context, s *dolt.DoltStore, key string) bool {
	val, err := s.GetConfig(ctx, key)
	if err != nil || val == "" {
		return false
	}
	return val == "true" || val == "1"
}

// syncConfigKeys are the branch_strategy settings written to .beads/sync_config.
var syncConfigKeys = []string{
	"branch_strategy.prompt",
	"branch_strategy.defaults.reset_dolt_with_git",
}

// writeSyncConfig writes branch_strategy settings to a git-tracked file.
// This ensures settings survive Dolt resets (the Dolt config table is wiped
// by DOLT_RESET --hard, but this file is restored by git reset --hard).
func writeSyncConfig(ctx context.Context, s *dolt.DoltStore, path string) {
	var lines []string
	for _, key := range syncConfigKeys {
		val, err := s.GetConfig(ctx, key)
		if err != nil || val == "" {
			continue
		}
		lines = append(lines, key+"="+val)
	}
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	_ = os.WriteFile(path, []byte(content), 0644)
}

// readSyncConfig reads branch_strategy settings from .beads/sync_config.
// Returns a map of key→value. Missing file or keys return empty map.
func readSyncConfig(beadsDir string) map[string]string {
	path := filepath.Join(beadsDir, "sync_config")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	result := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx > 0 {
			result[line[:idx]] = line[idx+1:]
		}
	}
	return result
}

// syncConfigBool reads a boolean from the sync_config map.
func syncConfigBool(cfg map[string]string, key string) bool {
	val := cfg[key]
	return val == "true" || val == "1"
}

// truncHash returns the first 8 characters of a hash, or the full string if shorter.
func truncHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}
