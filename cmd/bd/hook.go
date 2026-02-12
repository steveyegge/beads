package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/git"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/types"
)

// jsonlFilePaths lists all JSONL files that should be staged/tracked.
// Includes beads.jsonl for backwards compatibility with older installations.
var jsonlFilePaths = []string{
	".beads/issues.jsonl",
	".beads/deletions.jsonl",
	".beads/interactions.jsonl",
	".beads/beads.jsonl", // Legacy filename, kept for backwards compatibility
}

// hookCmd is the main "bd hook" command that git hooks call into.
// This is distinct from "bd hooks" (plural) which manages hook installation.
var hookCmd = &cobra.Command{
	Use:   "hook <hook-name> [args...]",
	Short: "Execute a git hook (called by hook scripts)",
	Long: `Execute the logic for a git hook. This command is called by
hook scripts installed in .beads/hooks/ (or .git/hooks/).

Supported hooks:
  - pre-commit: Export database to JSONL, stage changes
  - post-merge: Import JSONL to database after pull/merge
  - post-checkout: Import JSONL after branch checkout (with guard)

The hook scripts delegate to this command so hook behavior is always
in sync with the installed bd version.

Configuration (.beads/config.yaml):
  hooks:
    chain_strategy: before  # before | after | replace
    chain_timeout_ms: 5000  # Timeout for chained hooks`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		hookName := args[0]
		hookArgs := args[1:]

		var exitCode int
		switch hookName {
		case "pre-commit":
			exitCode = hookPreCommit()
		case "post-merge":
			exitCode = hookPostMerge(hookArgs)
		case "post-checkout":
			exitCode = hookPostCheckout(hookArgs)
		default:
			fmt.Fprintf(os.Stderr, "Unknown hook: %s\n", hookName)
			os.Exit(1)
		}

		os.Exit(exitCode)
	},
}

// =============================================================================
// Export State Tracking (per-worktree)
// =============================================================================

// ExportState tracks the export state for a specific worktree.
// This prevents polecats sharing a Dolt DB from exporting uncommitted
// work from other polecats. See design doc Part 21.
//
// Key insight: We track Dolt commit hash (not git commit) because:
// - Dolt is the source of truth for issue data
// - We use dolt_diff() to detect if changes exist since last export
// - Each worktree may have exported at different Dolt commits
type ExportState struct {
	WorktreeRoot     string    `json:"worktree_root"`
	WorktreeHash     string    `json:"worktree_hash,omitempty"` // Hash of worktree path (for debugging)
	LastExportCommit string    `json:"last_export_commit"`      // Dolt commit hash when last exported
	LastExportTime   time.Time `json:"last_export_time"`
	JSONLHash        string    `json:"jsonl_hash,omitempty"` // Hash of JSONL at last export
}

// getWorktreeHash returns a hash of the worktree root for use in filenames.
func getWorktreeHash(worktreeRoot string) string {
	h := sha256.Sum256([]byte(worktreeRoot))
	return hex.EncodeToString(h[:8]) // Use first 8 bytes (16 hex chars)
}

// getExportStateDir returns the path to the export state directory.
func getExportStateDir(beadsDir string) string {
	return filepath.Join(beadsDir, "export-state")
}

// getExportStatePath returns the path to the export state file for this worktree.
func getExportStatePath(beadsDir, worktreeRoot string) string {
	return filepath.Join(getExportStateDir(beadsDir), getWorktreeHash(worktreeRoot)+".json")
}

// loadExportState loads the export state for the current worktree.
func loadExportState(beadsDir, worktreeRoot string) (*ExportState, error) {
	path := getExportStatePath(beadsDir, worktreeRoot)
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from beadsDir
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No state yet
		}
		return nil, err
	}

	var state ExportState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// saveExportState saves the export state for the current worktree.
func saveExportState(beadsDir, worktreeRoot string, state *ExportState) error {
	dir := getExportStateDir(beadsDir)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("creating export-state directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	path := getExportStatePath(beadsDir, worktreeRoot)
	// #nosec G306 -- state file in .beads directory
	return os.WriteFile(path, data, 0644)
}

// computeJSONLHashForHook computes a hash of the JSONL file contents for hook state tracking.
// This is a wrapper around computeJSONLHash that handles missing files gracefully.
func computeJSONLHashForHook(jsonlPath string) (string, error) {
	hash, err := computeJSONLHash(jsonlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return hash, nil
}

// getCurrentGitCommit returns the current git HEAD commit hash.
func getCurrentGitCommit() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// getWorktreeRoot returns the root of the current worktree.
func getWorktreeRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// =============================================================================
// Hook Chaining Configuration
// =============================================================================

// HookChainStrategy defines how to chain with existing hooks.
type HookChainStrategy string

const (
	ChainBefore  HookChainStrategy = "before"  // Run existing hook before bd hook
	ChainAfter   HookChainStrategy = "after"   // Run existing hook after bd hook
	ChainReplace HookChainStrategy = "replace" // Replace existing hook entirely
)

// HookConfig holds hook-related configuration from config.yaml.
type HookConfig struct {
	ChainStrategy  HookChainStrategy `yaml:"chain_strategy" json:"chain_strategy"`
	ChainTimeoutMs int               `yaml:"chain_timeout_ms" json:"chain_timeout_ms"`
}

// DefaultHookConfig returns the default hook configuration.
func DefaultHookConfig() *HookConfig {
	return &HookConfig{
		ChainStrategy:  ChainBefore,
		ChainTimeoutMs: 5000,
	}
}

// loadHookConfig loads hook configuration from config.yaml.
func loadHookConfig(beadsDir string) *HookConfig {
	cfg := DefaultHookConfig()

	configPath := filepath.Join(beadsDir, "config.yaml")
	data, err := os.ReadFile(configPath) // #nosec G304 -- config path is trusted
	if err != nil {
		return cfg
	}

	// Simple YAML parsing for hooks config
	lines := strings.Split(string(data), "\n")
	inHooks := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "hooks:" {
			inHooks = true
			continue
		}
		if inHooks {
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && trimmed != "" {
				inHooks = false
				continue
			}
			if strings.HasPrefix(trimmed, "chain_strategy:") {
				value := strings.TrimPrefix(trimmed, "chain_strategy:")
				value = strings.TrimSpace(value)
				value = strings.Trim(value, `"'`)
				switch value {
				case "before":
					cfg.ChainStrategy = ChainBefore
				case "after":
					cfg.ChainStrategy = ChainAfter
				case "replace":
					cfg.ChainStrategy = ChainReplace
				}
			}
			if strings.HasPrefix(trimmed, "chain_timeout_ms:") {
				value := strings.TrimPrefix(trimmed, "chain_timeout_ms:")
				value = strings.TrimSpace(value)
				var timeout int
				if _, err := fmt.Sscanf(value, "%d", &timeout); err == nil && timeout > 0 {
					cfg.ChainTimeoutMs = timeout
				}
			}
		}
	}

	return cfg
}

// runChainedHookWithConfig runs a chained hook with timeout from config.
func runChainedHookWithConfig(hookName string, args []string, cfg *HookConfig) int {
	if cfg.ChainStrategy == ChainReplace {
		return 0 // Skip chained hook
	}

	// Get the hooks directory
	hooksDir, err := getHooksDir()
	if err != nil {
		return 0
	}

	oldHookPath := filepath.Join(hooksDir, hookName+".old")

	// Check if the .old hook exists and is executable
	info, err := os.Stat(oldHookPath)
	if err != nil {
		return 0 // No chained hook
	}
	if info.Mode().Perm()&0111 == 0 {
		return 0 // Not executable
	}

	// Check if .old is itself a bd shim - skip to prevent infinite recursion
	versionInfo, err := getHookVersion(oldHookPath)
	if err == nil && versionInfo.IsShim {
		return 0
	}

	// Create context with timeout
	timeout := time.Duration(cfg.ChainTimeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Run the chained hook
	// #nosec G204 -- hookName is from controlled list, path is from hooks directory
	cmd := exec.CommandContext(ctx, oldHookPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Fprintf(os.Stderr, "Warning: chained hook %s timed out after %dms\n", hookName, cfg.ChainTimeoutMs)
			return 0 // Don't block on timeout, just warn
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "Warning: chained hook %s failed: %v\n", hookName, err)
		return 1
	}

	return 0
}

// getHooksDir returns the hooks directory (.beads/hooks/ or .git/hooks/).
func getHooksDir() (string, error) {
	// First check for .beads/hooks/ (preferred location)
	beadsDir := beads.FindBeadsDir()
	if beadsDir != "" {
		beadsHooksDir := filepath.Join(beadsDir, "hooks")
		if _, err := os.Stat(beadsHooksDir); err == nil {
			return beadsHooksDir, nil
		}
	}

	// Fall back to git hooks directory
	return git.GetGitHooksDir()
}

// =============================================================================
// Hook Implementations
// =============================================================================

// hookPreCommit implements the pre-commit hook: Export database to JSONL.
func hookPreCommit() int {
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return 0 // Not a beads workspace
	}

	cfg := loadHookConfig(beadsDir)

	// Run chained hook based on strategy
	if cfg.ChainStrategy == ChainBefore {
		if exitCode := runChainedHookWithConfig("pre-commit", nil, cfg); exitCode != 0 {
			return exitCode
		}
	}

	// Check if sync-branch is configured (changes go to separate branch)
	if hookGetSyncBranch() != "" {
		if cfg.ChainStrategy == ChainAfter {
			return runChainedHookWithConfig("pre-commit", nil, cfg)
		}
		return 0
	}

	// Get worktree root for per-worktree state tracking
	worktreeRoot, err := getWorktreeRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not determine worktree root: %v\n", err)
		worktreeRoot = beadsDir // Fallback
	}

	// Check if we're using Dolt backend - use branch-then-merge pattern
	backend := factory.GetBackendFromConfig(beadsDir)
	if backend == configfile.BackendDolt {
		exitCode := hookPreCommitDolt(beadsDir, worktreeRoot)
		if cfg.ChainStrategy == ChainAfter && exitCode == 0 {
			return runChainedHookWithConfig("pre-commit", nil, cfg)
		}
		return exitCode
	}

	// SQLite backend: Use existing sync --flush-only
	cmd := exec.Command("bd", "sync", "--flush-only", "--no-daemon")
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Warning: Failed to flush bd changes to JSONL")
		fmt.Fprintln(os.Stderr, "Run 'bd sync --flush-only' manually to diagnose")
	}

	// Stage JSONL files
	if os.Getenv("BEADS_NO_AUTO_STAGE") == "" {
		rc, rcErr := beads.GetRepoContext()
		ctx := context.Background()
		for _, f := range jsonlFilePaths {
			if _, err := os.Stat(f); err == nil {
				var gitAdd *exec.Cmd
				if rcErr == nil {
					gitAdd = rc.GitCmdCWD(ctx, "add", f)
				} else {
					// #nosec G204 -- f comes from jsonlFilePaths (controlled, hardcoded paths)
					gitAdd = exec.Command("git", "add", f)
				}
				_ = gitAdd.Run()
			}
		}
	}

	// Update export state
	currentCommit, _ := getCurrentGitCommit()
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	jsonlHash, _ := computeJSONLHashForHook(jsonlPath)

	state := &ExportState{
		WorktreeRoot:     worktreeRoot,
		LastExportCommit: currentCommit,
		LastExportTime:   time.Now(),
		JSONLHash:        jsonlHash,
	}
	if err := saveExportState(beadsDir, worktreeRoot, state); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save export state: %v\n", err)
	}

	if cfg.ChainStrategy == ChainAfter {
		return runChainedHookWithConfig("pre-commit", nil, cfg)
	}
	return 0
}

// hookPreCommitDolt implements pre-commit for Dolt backend.
// Export Dolt → JSONL with per-worktree state tracking.
//
// Per design doc Part 21, this function:
// 1. Loads export state for the current worktree
// 2. Gets the current Dolt commit hash (not git commit)
// 3. Checks if export is needed (skip if already exported this commit)
// 4. Exports to JSONL and saves new state
//
// bd-ma0s.6: Routes GetCurrentCommit and Diff through daemon RPC when available.
func hookPreCommitDolt(beadsDir, worktreeRoot string) int {
	// Load previous export state for this worktree
	prevState, _ := loadExportState(beadsDir, worktreeRoot)

	// Use daemon RPC for versioned operations (bd-ma0s.6)
	return hookPreCommitDoltViaDaemon(beadsDir, worktreeRoot, prevState)
}

// hookPreCommitDoltViaDaemon implements pre-commit for Dolt backend using daemon RPC.
// bd-ma0s.6: Added for daemon RPC routing of GetCurrentCommit and Diff.
func hookPreCommitDoltViaDaemon(beadsDir, worktreeRoot string, prevState *ExportState) int {
	ctx := context.Background()

	// Get current Dolt commit hash via RPC
	commitResult, err := daemonClient.VcsCurrentCommit()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: VcsCurrentCommit RPC failed: %v\n", err)
		doExportAndSaveState(ctx, beadsDir, worktreeRoot, "")
		return 0
	}
	currentDoltCommit := commitResult.Hash

	// Check if we've already exported for this Dolt commit (idempotency)
	if prevState != nil && prevState.LastExportCommit == currentDoltCommit {
		return 0
	}

	// Check if there are actual changes to export (optimization) via RPC
	if prevState != nil && prevState.LastExportCommit != "" {
		diffResult, err := daemonClient.VersionedDiff(&rpc.VersionedDiffArgs{
			FromRef: prevState.LastExportCommit,
			ToRef:   currentDoltCommit,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: VersionedDiff RPC failed: %v\n", err)
			// Continue with export to be safe
		} else if len(diffResult.Entries) == 0 {
			// No changes, but update state to track new commit
			updateExportStateCommit(beadsDir, worktreeRoot, currentDoltCommit)
			return 0
		}
	}

	doExportAndSaveState(ctx, beadsDir, worktreeRoot, currentDoltCommit)
	return 0
}

// doExportAndSaveState performs the export and saves state. Shared by main path and fallback.
func doExportAndSaveState(ctx context.Context, beadsDir, worktreeRoot, doltCommit string) {
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	// Export to JSONL
	if err := runJSONLExport(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not export to JSONL: %v\n", err)
		return
	}

	// Stage JSONL files for git commit
	stageJSONLFiles(ctx)

	// Save export state
	jsonlHash, _ := computeJSONLHashForHook(jsonlPath)
	state := &ExportState{
		WorktreeRoot:     worktreeRoot,
		WorktreeHash:     getWorktreeHash(worktreeRoot),
		LastExportCommit: doltCommit, // Empty string if Dolt commit unavailable
		LastExportTime:   time.Now(),
		JSONLHash:        jsonlHash,
	}
	if err := saveExportState(beadsDir, worktreeRoot, state); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save export state: %v\n", err)
	}
}

// hasDoltChanges checks if there are any changes between two Dolt commits.
func hasDoltChanges(ctx context.Context, vs storage.VersionedStorage, fromCommit, toCommit string) (bool, error) {
	diffs, err := vs.Diff(ctx, fromCommit, toCommit)
	if err != nil {
		return false, err
	}
	return len(diffs) > 0, nil
}

// updateExportStateCommit updates just the commit hash in the export state.
// Used when we detect no changes but want to track the new commit.
func updateExportStateCommit(beadsDir, worktreeRoot, doltCommit string) {
	prevState, err := loadExportState(beadsDir, worktreeRoot)
	if err != nil || prevState == nil {
		return // Can't update what doesn't exist
	}
	prevState.LastExportCommit = doltCommit
	prevState.LastExportTime = time.Now()
	_ = saveExportState(beadsDir, worktreeRoot, prevState)
}

// runJSONLExport runs the actual JSONL export via bd sync.
func runJSONLExport() error {
	cmd := exec.Command("bd", "sync", "--flush-only", "--no-daemon")
	return cmd.Run()
}

// stageJSONLFiles stages JSONL files for git commit (unless BEADS_NO_AUTO_STAGE is set).
func stageJSONLFiles(ctx context.Context) {
	if os.Getenv("BEADS_NO_AUTO_STAGE") != "" {
		return
	}

	rc, rcErr := beads.GetRepoContext()
	for _, f := range jsonlFilePaths {
		if _, err := os.Stat(f); err == nil {
			var gitAdd *exec.Cmd
			if rcErr == nil {
				gitAdd = rc.GitCmdCWD(ctx, "add", f)
			} else {
				// #nosec G204 -- f comes from jsonlFilePaths (controlled, hardcoded paths)
				gitAdd = exec.Command("git", "add", f)
			}
			_ = gitAdd.Run()
		}
	}
}

// hookPostMerge implements the post-merge hook: Import JSONL to database.
func hookPostMerge(args []string) int {
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return 0 // Not a beads workspace
	}

	cfg := loadHookConfig(beadsDir)

	// Run chained hook based on strategy
	if cfg.ChainStrategy == ChainBefore {
		if exitCode := runChainedHookWithConfig("post-merge", args, cfg); exitCode != 0 {
			return exitCode
		}
	}

	// Skip during rebase
	if isRebaseInProgress() {
		if cfg.ChainStrategy == ChainAfter {
			return runChainedHookWithConfig("post-merge", args, cfg)
		}
		return 0
	}

	// Check if any JSONL file exists
	if !hasBeadsJSONL() {
		if cfg.ChainStrategy == ChainAfter {
			return runChainedHookWithConfig("post-merge", args, cfg)
		}
		return 0
	}

	// Check if we're using Dolt backend - use branch-then-merge pattern
	backend := factory.GetBackendFromConfig(beadsDir)
	if backend == configfile.BackendDolt {
		exitCode := hookPostMergeDolt(beadsDir)
		if cfg.ChainStrategy == ChainAfter && exitCode == 0 {
			return runChainedHookWithConfig("post-merge", args, cfg)
		}
		return exitCode
	}

	// SQLite backend: Use existing sync --import-only
	cmd := exec.Command("bd", "sync", "--import-only", "--no-git-history", "--no-daemon")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Warning: Failed to sync bd changes after merge")
		fmt.Fprintln(os.Stderr, string(output))
		fmt.Fprintln(os.Stderr, "Run 'bd doctor --fix' to diagnose and repair")
	}

	// Run quick health check
	healthCmd := exec.Command("bd", "doctor", "--check-health")
	_ = healthCmd.Run()

	if cfg.ChainStrategy == ChainAfter {
		return runChainedHookWithConfig("post-merge", args, cfg)
	}
	return 0
}

// hookPostMergeDolt implements post-merge for Dolt backend.
// Import JSONL → Dolt using branch-then-merge pattern:
// 1. Create jsonl-import branch
// 2. Import JSONL data to branch
// 3. Merge branch to main (cell-level conflict resolution)
// 4. Delete branch on success
//
// bd-ma0s.6: Routes Branch/Checkout/Merge/Commit/DeleteBranch through daemon RPC when available.
func hookPostMergeDolt(beadsDir string) int {
	// Use daemon RPC for VCS operations (bd-ma0s.6)
	return hookPostMergeDoltViaDaemon(beadsDir)
}

// hookPostMergeDoltViaDaemon implements post-merge for Dolt backend using daemon RPC.
// bd-ma0s.6: Branch-then-merge import workflow routed through VCS RPC operations.
func hookPostMergeDoltViaDaemon(beadsDir string) int {
	ctx := context.Background()

	// Get current branch via RPC
	branchResult, err := daemonClient.VcsActiveBranch()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: VcsActiveBranch RPC failed: %v\n", err)
		return 0
	}
	currentBranch := branchResult.Branch

	// Create import branch via RPC
	importBranch := "jsonl-import-" + time.Now().Format("20060102-150405")
	if _, err := daemonClient.VcsBranchCreate(&rpc.VcsBranchCreateArgs{Name: importBranch}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: VcsBranchCreate RPC failed: %v\n", err)
		return 0
	}

	// Checkout import branch via RPC
	if _, err := daemonClient.VcsCheckout(&rpc.VcsCheckoutArgs{Branch: importBranch}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: VcsCheckout RPC failed: %v\n", err)
		return 0
	}

	// Import JSONL to the import branch
	// Note: importFromJSONLToStore needs direct storage access, so we open a local store
	// for the import step while using RPC for VCS operations.
	localStore, err := factory.NewFromConfig(ctx, beadsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not open database for import: %v\n", err)
		_ = restoreBranchViaDaemon(currentBranch)
		return 0
	}

	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	if err := importFromJSONLToStore(ctx, localStore, jsonlPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not import JSONL: %v\n", err)
		_ = localStore.Close()
		_ = restoreBranchViaDaemon(currentBranch)
		return 0
	}
	_ = localStore.Close()

	// Commit changes on import branch via RPC
	commandDidExplicitDoltCommit = true
	if _, err := daemonClient.VcsCommit(&rpc.VcsCommitArgs{Message: "Import from JSONL"}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: VcsCommit RPC failed: %v\n", err)
	}

	// Checkout back to original branch via RPC
	if _, err := daemonClient.VcsCheckout(&rpc.VcsCheckoutArgs{Branch: currentBranch}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: VcsCheckout RPC failed (restore): %v\n", err)
		return 0
	}

	// Merge import branch via RPC (Dolt provides cell-level merge)
	mergeResult, err := daemonClient.VcsMerge(&rpc.VcsMergeArgs{Branch: importBranch})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: VcsMerge RPC failed: %v\n", err)
		return 0
	}
	if len(mergeResult.Conflicts) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d conflict(s) detected during Dolt merge; resolve with 'bd federation conflicts' or Dolt conflict tooling\n", len(mergeResult.Conflicts))
	}

	// Commit the merge via RPC
	commandDidExplicitDoltCommit = true
	if _, err := daemonClient.VcsCommit(&rpc.VcsCommitArgs{Message: "Merge JSONL import"}); err != nil {
		// May fail if nothing to commit (fast-forward merge) - expected
	}

	// Clean up import branch via RPC
	if _, err := daemonClient.VcsBranchDelete(&rpc.VcsBranchDeleteArgs{Name: importBranch}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: VcsBranchDelete RPC failed for %s: %v\n", importBranch, err)
	}

	return 0
}

// restoreBranchViaDaemon is a helper to restore the original branch on error during daemon RPC workflow.
func restoreBranchViaDaemon(branch string) error {
	_, err := daemonClient.VcsCheckout(&rpc.VcsCheckoutArgs{Branch: branch})
	return err
}

// hookPostCheckout implements the post-checkout hook with guard.
// Only imports if JSONL actually changed since last import.
func hookPostCheckout(args []string) int {
	// Only run on branch checkouts (flag=1)
	if len(args) >= 3 && args[2] != "1" {
		return 0
	}

	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return 0 // Not a beads workspace
	}

	cfg := loadHookConfig(beadsDir)

	// Run chained hook based on strategy
	if cfg.ChainStrategy == ChainBefore {
		if exitCode := runChainedHookWithConfig("post-checkout", args, cfg); exitCode != 0 {
			return exitCode
		}
	}

	// Skip during rebase
	if isRebaseInProgress() {
		if cfg.ChainStrategy == ChainAfter {
			return runChainedHookWithConfig("post-checkout", args, cfg)
		}
		return 0
	}

	// Check if any JSONL file exists
	if !hasBeadsJSONL() {
		if cfg.ChainStrategy == ChainAfter {
			return runChainedHookWithConfig("post-checkout", args, cfg)
		}
		return 0
	}

	// Guard: Only import if JSONL actually changed
	worktreeRoot, err := getWorktreeRoot()
	if err != nil {
		worktreeRoot = beadsDir // Fallback
	}

	prevState, _ := loadExportState(beadsDir, worktreeRoot)
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	currentHash, _ := computeJSONLHashForHook(jsonlPath)

	if prevState != nil && prevState.JSONLHash == currentHash {
		// JSONL hasn't changed, skip redundant import
		if cfg.ChainStrategy == ChainAfter {
			return runChainedHookWithConfig("post-checkout", args, cfg)
		}
		return 0
	}

	// Detect git worktree and show warning
	if isGitWorktree() {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "╔══════════════════════════════════════════════════════════════════════════╗")
		fmt.Fprintln(os.Stderr, "║ Welcome to beads in git worktree!                                        ║")
		fmt.Fprintln(os.Stderr, "╠══════════════════════════════════════════════════════════════════════════╣")
		fmt.Fprintln(os.Stderr, "║ Note: Daemon mode is not recommended with git worktrees.                 ║")
		fmt.Fprintln(os.Stderr, "║ Worktrees share the same database, and the daemon may commit changes     ║")
		fmt.Fprintln(os.Stderr, "║ to the wrong branch.                                                     ║")
		fmt.Fprintln(os.Stderr, "║                                                                          ║")
		fmt.Fprintln(os.Stderr, "║ RECOMMENDED: Disable daemon for this session:                            ║")
		fmt.Fprintln(os.Stderr, "║   export BEADS_NO_DAEMON=1                                               ║")
		fmt.Fprintln(os.Stderr, "╚══════════════════════════════════════════════════════════════════════════╝")
		fmt.Fprintln(os.Stderr, "")
	}

	// Check if we're using Dolt backend
	backend := factory.GetBackendFromConfig(beadsDir)
	if backend == configfile.BackendDolt {
		exitCode := hookPostMergeDolt(beadsDir) // Same as post-merge for Dolt
		// Update state after import
		newHash, _ := computeJSONLHashForHook(jsonlPath)
		currentCommit, _ := getCurrentGitCommit()
		state := &ExportState{
			WorktreeRoot:     worktreeRoot,
			LastExportCommit: currentCommit,
			LastExportTime:   time.Now(),
			JSONLHash:        newHash,
		}
		_ = saveExportState(beadsDir, worktreeRoot, state)

		if cfg.ChainStrategy == ChainAfter && exitCode == 0 {
			return runChainedHookWithConfig("post-checkout", args, cfg)
		}
		return exitCode
	}

	// SQLite backend: Use existing sync --import-only
	cmd := exec.Command("bd", "sync", "--import-only", "--no-git-history", "--no-daemon")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Warning: Failed to sync bd changes after checkout")
		fmt.Fprintln(os.Stderr, string(output))
		fmt.Fprintln(os.Stderr, "Run 'bd doctor --fix' to diagnose and repair")
	}

	// Update state after import
	newHash, _ := computeJSONLHashForHook(jsonlPath)
	currentCommit, _ := getCurrentGitCommit()
	state := &ExportState{
		WorktreeRoot:     worktreeRoot,
		LastExportCommit: currentCommit,
		LastExportTime:   time.Now(),
		JSONLHash:        newHash,
	}
	_ = saveExportState(beadsDir, worktreeRoot, state)

	// Run quick health check
	healthCmd := exec.Command("bd", "doctor", "--check-health")
	_ = healthCmd.Run()

	if cfg.ChainStrategy == ChainAfter {
		return runChainedHookWithConfig("post-checkout", args, cfg)
	}
	return 0
}

// =============================================================================
// Helper Functions for Dolt Import/Export
// =============================================================================

// importFromJSONLToStore imports issues from JSONL to a store.
func importFromJSONLToStore(ctx context.Context, store storage.Storage, jsonlPath string) error {
	// Parse JSONL into issues
	// #nosec G304 - jsonlPath is derived from beadsDir (trusted workspace path)
	f, err := os.Open(jsonlPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	// 2MB buffer for large issues
	scanner.Buffer(make([]byte, 0, 1024), 2*1024*1024)

	var allIssues []*types.Issue
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var issue types.Issue
		if err := json.Unmarshal([]byte(line), &issue); err != nil {
			return err
		}
		issue.SetDefaults()
		allIssues = append(allIssues, &issue)
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// Import using shared logic (no subprocess).
	// Use store.Path() as the database path (works for both sqlite and dolt).
	opts := ImportOptions{}
	_, err = importIssuesCore(ctx, store.Path(), store, allIssues, opts)
	return err
}

func init() {
	rootCmd.AddCommand(hookCmd)
}
