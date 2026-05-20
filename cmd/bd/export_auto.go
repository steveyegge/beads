package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/beads/internal/atomicfile"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/debug"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
)

// exportAutoState tracks auto-export state to avoid redundant work.
type exportAutoState struct {
	LastDoltCommit string    `json:"last_dolt_commit"`
	Timestamp      time.Time `json:"timestamp"`
	Issues         int       `json:"issues"`
	Memories       int       `json:"memories"`
}

const exportAutoStateFile = "export-state.json"

// maybeAutoExport writes a git-tracked JSONL file if enabled and due.
// Called from PersistentPostRun after auto-backup.
func maybeAutoExport(ctx context.Context) {
	// Skip when running as a git hook to avoid re-export during pre-commit.
	if os.Getenv("BD_GIT_HOOK") == "1" {
		debug.Logf("auto-export: skipping — running as git hook\n")
		return
	}

	if !config.GetBool("export.auto") {
		return
	}
	if store == nil {
		return
	}
	if lm, ok := storage.UnwrapStore(store).(storage.LifecycleManager); ok && lm.IsClosed() {
		return
	}

	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return
	}

	// Resolve the export path early — the throttle decision below consults
	// it (GH#3848: when the path is git-ignored we bypass throttle to keep
	// the JSONL in sync with Dolt).
	exportPath := config.GetString("export.path")
	if exportPath == "" {
		if globalFlag {
			exportPath = "global-issues.jsonl"
		} else {
			exportPath = "issues.jsonl"
		}
	}
	fullPath := filepath.Join(beadsDir, exportPath)

	// Load state + interval.
	state := loadExportAutoState(beadsDir)
	interval := config.GetDuration("export.interval")
	if interval == 0 {
		interval = 60 * time.Second
	}

	// Change detection via Dolt commit hash. Cheap (single hash compare
	// against stored state), so do it before the throttle — when there's no
	// change, there's nothing to throttle. This also lets the throttle-bypass
	// below act only when there's an actual pending write.
	currentCommit, err := store.GetCurrentCommit(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-export skipped: failed to get current commit: %v\n", err)
		return
	}
	if currentCommit == state.LastDoltCommit && state.LastDoltCommit != "" {
		debug.Logf("auto-export: no changes since last export\n")
		return
	}

	// Throttle decision is factored into a pure function so the branching
	// logic is unit-testable without a store or filesystem. See
	// shouldBypassThrottle for the policy.
	if !shouldExport(state, interval) {
		if shouldBypassThrottle(state, interval, isExportPathGitignored(ctx, beadsDir, fullPath)) {
			debug.Logf("auto-export: bypassing throttle — export path is gitignored (GH#3848)\n")
		} else {
			debug.Logf("auto-export: throttled (last export %s ago, interval %s)\n",
				time.Since(state.Timestamp).Round(time.Second), interval)
			return
		}
	}

	// Run the export — memories are excluded from auto-export because they
	// contain private agent context that must not reach git history (GH#3650).
	issueCount, memoryCount, err := exportToFile(ctx, fullPath, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-export failed: %v\n", err)
		return
	}

	debug.Logf("auto-export: wrote %d issues and %d memories to %s\n",
		issueCount, memoryCount, fullPath)

	// Don't prime the throttle on an empty export (e.g. immediately after
	// `bd init`). Saving state here would block the first real `bd create`
	// from exporting for up to export.interval seconds even though the data
	// has changed. Remove the empty file too so users don't see a stale 0-byte
	// issues.jsonl before any issues exist.
	if issueCount == 0 && memoryCount == 0 {
		_ = os.Remove(fullPath)
		return
	}
	warnJSONLWithoutDoltRemote("auto-export")

	// Optional git add — skip when no-git-ops is set (GH#3314), when not in a
	// git repo (standalone BEADS_DIR flow), or when export.git-add is false.
	if config.GetBool("export.git-add") && !config.GetBool("no-git-ops") && isGitRepo() {
		if err := gitAddFile(fullPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: auto-export: git add failed: %v\n", err)
		}
	}

	// Save state
	newState := exportAutoState{
		LastDoltCommit: currentCommit,
		Timestamp:      time.Now(),
		Issues:         issueCount,
		Memories:       memoryCount,
	}
	saveExportAutoState(beadsDir, &newState)
}

// exportToFile atomically exports issues + memories to the given file path.
// Writes to a temp file first, then renames into place so readers never see
// a partial or truncated export. Used by both `bd export -o` and auto-export.
func exportToFile(ctx context.Context, path string, includeMemories bool) (issueCount, memoryCount int, err error) {
	w, err := atomicfile.Create(path, 0o644)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create export file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = w.Abort()
		}
	}()

	// Build filter: exclude infra types and templates
	filter := types.IssueFilter{Limit: 0}
	var infraTypes []string
	if store != nil {
		infraSet := store.GetInfraTypes(ctx)
		if len(infraSet) > 0 {
			for t := range infraSet {
				infraTypes = append(infraTypes, t)
			}
		}
	}
	if len(infraTypes) == 0 {
		infraTypes = dolt.DefaultInfraTypes()
	}
	for _, t := range infraTypes {
		filter.ExcludeTypes = append(filter.ExcludeTypes, types.IssueType(t))
	}
	isTemplate := false
	filter.IsTemplate = &isTemplate

	// Exclude ephemeral wisps — they are private/transient and must not
	// reach git history or external integrations (GH#3649).
	persistentOnly := false
	filter.Ephemeral = &persistentOnly

	issues, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to search issues: %w", err)
	}

	// Bulk-load relational data
	if len(issues) > 0 {
		issueIDs := make([]string, len(issues))
		for i, issue := range issues {
			issueIDs[i] = issue.ID
		}
		labelsMap, _ := store.GetLabelsForIssues(ctx, issueIDs)
		allDeps, _ := store.GetDependencyRecordsForIssues(ctx, issueIDs)
		commentsMap, _ := store.GetCommentsForIssues(ctx, issueIDs)
		commentCounts, _ := store.GetCommentCounts(ctx, issueIDs)
		depCounts, _ := store.GetDependencyCounts(ctx, issueIDs)

		for _, issue := range issues {
			issue.Labels = labelsMap[issue.ID]
			issue.Dependencies = allDeps[issue.ID]
			issue.Comments = commentsMap[issue.ID]
		}

		// Write issues
		enc := json.NewEncoder(w)
		for _, issue := range issues {
			counts := depCounts[issue.ID]
			if counts == nil {
				counts = &types.DependencyCounts{}
			}
			sanitizeZeroTime(issue)
			record := &exportIssueRecord{
				RecordType: "issue",
				IssueWithCounts: &types.IssueWithCounts{
					Issue:           issue,
					DependencyCount: counts.DependencyCount,
					DependentCount:  counts.DependentCount,
					CommentCount:    commentCounts[issue.ID],
				},
			}
			if err := enc.Encode(record); err != nil {
				return 0, 0, fmt.Errorf("failed to write issue %s: %w", issue.ID, err)
			}
			issueCount++
		}
	}

	// Write memories
	if includeMemories {
		allConfig, err := store.GetAllConfig(ctx)
		if err == nil {
			fullPrefix := kvPrefix + memoryPrefix
			// Sort keys for deterministic output order (GH#3474).
			var memKeys []string
			for k := range allConfig {
				if strings.HasPrefix(k, fullPrefix) {
					memKeys = append(memKeys, k)
				}
			}
			sort.Strings(memKeys)
			for _, k := range memKeys {
				v := allConfig[k]
				userKey := strings.TrimPrefix(k, fullPrefix)
				record := map[string]string{
					"_type": "memory",
					"key":   userKey,
					"value": v,
				}
				data, err := json.Marshal(record)
				if err != nil {
					return issueCount, memoryCount, fmt.Errorf("failed to marshal memory %s: %w", userKey, err)
				}
				if _, err := w.Write(data); err != nil {
					return issueCount, memoryCount, fmt.Errorf("failed to write memory: %w", err)
				}
				if _, err := w.Write([]byte{'\n'}); err != nil {
					return issueCount, memoryCount, fmt.Errorf("failed to write newline: %w", err)
				}
				memoryCount++
			}
		}
	}

	if err := w.Close(); err != nil {
		return issueCount, memoryCount, fmt.Errorf("failed to finalize export: %w", err)
	}

	return issueCount, memoryCount, nil
}

func loadExportAutoState(beadsDir string) *exportAutoState {
	path := filepath.Join(beadsDir, exportAutoStateFile)
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return &exportAutoState{}
	}
	var state exportAutoState
	if err := json.Unmarshal(data, &state); err != nil {
		return &exportAutoState{}
	}
	return &state
}

func saveExportAutoState(beadsDir string, state *exportAutoState) {
	path := filepath.Join(beadsDir, exportAutoStateFile)
	data, err := json.Marshal(state)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-export: failed to marshal state: %v\n", err)
		return
	}
	if err := atomicfile.WriteFile(path, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-export: failed to save state: %v\n", err)
	}
}

// gitAddFile stages a file in the enclosing git repo. When called from
// inside a git hook, it scrubs inherited GIT_* env vars (so git
// rediscovers the repo from cwd rather than treating cmd.Dir as the
// worktree root) and skips staging when the target is outside the hook's
// worktree (the .beads/redirect case, where staging would pollute the
// main repo's index). See GH#3311, scrubGitHookEnv, hookWorkTreeRoot.
func gitAddFile(path string) error {
	if wt := hookWorkTreeRoot(); wt != "" && !pathInsideDir(path, wt) {
		// Running inside a hook AND target is outside the hook's worktree.
		// Staging here would pollute a different repo's index; skip.
		return nil
	}
	cmd := exec.Command("git", "add", path)
	cmd.Dir = filepath.Dir(path)
	cmd.Env = scrubGitHookEnv(os.Environ())
	// Capture combined output so the caller's warning surfaces git's stderr
	// (e.g. "paths are ignored", "Unable to create index.lock") instead of
	// just the exit-status text.
	out, err := cmd.CombinedOutput()
	if err != nil {
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			return fmt.Errorf("%w: %s", err, trimmed)
		}
		return err
	}
	return nil
}

// shouldExport reports whether the throttle window has elapsed (or never
// existed). Returns true on the first export (no recorded timestamp) and
// any subsequent export after the configured interval. Returns false only
// when a recent export exists AND the interval has not yet elapsed.
//
// Pure function — no I/O, no clock, no globals. The only inputs are the
// recorded state and the configured interval. Caller passes in
// time.Now() implicitly via time.Since.
func shouldExport(state *exportAutoState, interval time.Duration) bool {
	if state.Timestamp.IsZero() {
		return true // first run — never throttle the initial export
	}
	return time.Since(state.Timestamp) >= interval
}

// shouldBypassThrottle reports whether the auto-export should fire even
// though shouldExport said the throttle window is still active.
//
// Bypass policy: when the export path is git-ignored, the JSONL on disk
// is the only cross-machine sync substrate (no git push will carry it,
// no pre-commit hook will refresh it). Throttling a pending Dolt write
// in that case means the write is silently lost when the embedded Dolt
// state is rebuilt (fresh clone, container restart, host migration).
// Bypass the throttle to keep the JSONL in sync with Dolt. See GH#3848.
//
// Pure function — pulled out so the branching logic is unit-testable
// without a store, subprocess, or filesystem. Codecov needs at least
// one non-CGO, non-`-short` test to attribute coverage; this gives it
// one.
//
// Caller is responsible for the timing check (shouldExport returns false)
// before consulting this. We don't re-check here so the policy is
// expressible as a single boolean.
func shouldBypassThrottle(state *exportAutoState, interval time.Duration, gitignored bool) bool {
	if state.Timestamp.IsZero() {
		return false // first run — nothing to bypass
	}
	if time.Since(state.Timestamp) >= interval {
		return false // throttle already elapsed; normal path handles it
	}
	return gitignored
}

// isExportPathGitignored reports whether the resolved auto-export path
// (typically `.beads/issues.jsonl`) is excluded by a gitignore rule in the
// enclosing repo.
//
// Used by the auto-export throttle bypass (GH#3848): when the JSONL path
// is git-ignored, the on-disk JSONL is the sole cross-machine sync
// substrate (there is no other catch-up path — no git push will carry it,
// no pre-commit hook will refresh it). A pending Dolt write must therefore
// land on disk on the current invocation; throttling it would lose data
// when the embedded Dolt state is rebuilt.
//
// Probe semantics:
//   - `git check-ignore -q -- <path>`: exit 0 = ignored, exit 1 = not
//     ignored, exit 128 = not in a git repo or other startup error, any
//     other exit / error = transient or unknown.
//   - True is returned only on exit 0 (definitely ignored). Exits 1 and
//     128 return false (definitely-not-ignored / not-a-git-repo, both
//     fall back to the regular throttle).
//   - **Other failure modes (timeout, missing git binary, transient
//     ExitError with code != 0, 1, 128) are logged via debug.Logf and
//     return false.** This is "fail closed for bypass" — when in doubt,
//     throttle as before, preserving pre-fix behavior rather than newly
//     amplifying writes.
//   - GIT_* env vars are scrubbed (same pattern as gitAddFile) so stray
//     GIT_DIR / GIT_WORK_TREE values cannot redirect the probe at an
//     unrelated repo.
//   - Capped at 2 seconds via ctx WithTimeout — the probe runs on the
//     main goroutine via PersistentPostRun, so a hung subprocess on a
//     slow filesystem would block every bd command. 2 s is generous for
//     a single git plumbing call.
//   - Cached per-process via sync.Once keyed on path. The .gitignore /
//     check-ignore answer is stable for a process lifetime in practice;
//     if it changes mid-process (rare: user edits .gitignore while a bd
//     subprocess is running), the next bd invocation re-probes.
func isExportPathGitignored(ctx context.Context, beadsDir, path string) bool {
	key := beadsDir + "\x00" + path
	gitignoreProbeCacheMu.Lock()
	if v, ok := gitignoreProbeCache[key]; ok {
		gitignoreProbeCacheMu.Unlock()
		return v
	}
	gitignoreProbeCacheMu.Unlock()

	result := runGitignoreProbe(ctx, beadsDir, path)

	gitignoreProbeCacheMu.Lock()
	gitignoreProbeCache[key] = result
	gitignoreProbeCacheMu.Unlock()
	return result
}

// runGitignoreProbe is the uncached subprocess invocation. Kept separate
// so tests can exercise the network of exit-code mappings without going
// through the cache.
func runGitignoreProbe(ctx context.Context, beadsDir, path string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// `--` separator is defense-in-depth: it guarantees the path is
	// interpreted as a path even if a future caller passes something that
	// starts with `-`.
	cmd := exec.CommandContext(probeCtx, "git", "check-ignore", "-q", "--", path)
	cmd.Dir = filepath.Dir(beadsDir)
	cmd.Env = scrubGitHookEnv(os.Environ())

	err := cmd.Run()
	if err == nil {
		return true // exit 0 = ignored
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 1:
			return false // exit 1 = definitely not ignored
		case 128:
			return false // exit 128 = not in a git repo / no upstream
		default:
			debug.Logf("auto-export: check-ignore unexpected exit %d for %s (treating as not ignored)\n",
				exitErr.ExitCode(), path)
			return false
		}
	}
	// Context deadline, missing git binary, fork/exec failure, etc.
	debug.Logf("auto-export: check-ignore probe failed for %s: %v (treating as not ignored)\n", path, err)
	return false
}

// gitignoreProbeCache memoizes isExportPathGitignored results per-process.
// The cache is unbounded but the key space is bounded by the number of
// distinct (beadsDir, exportPath) pairs the process touches — typically 1.
var (
	gitignoreProbeCache   = map[string]bool{}
	gitignoreProbeCacheMu sync.Mutex
)

// scrubGitHookEnv returns env with the GIT_* variables that can poison
// git's repo/worktree auto-discovery or object-store resolution removed,
// so git falls back to auto-discovery from cwd. The scrub is
// unconditional: if a user has intentionally exported any of these vars
// for scripting purposes, they will be stripped from the git-add child
// process. That is the correct trade-off here; we never want beads'
// auto-stage to honor a GIT_DIR pointing at an unrelated repo.
//
// Covered vars:
//   - Repo/worktree discovery: GIT_DIR, GIT_WORK_TREE, GIT_COMMON_DIR,
//     GIT_PREFIX, GIT_CEILING_DIRECTORIES, GIT_DISCOVERY_ACROSS_FILESYSTEM
//   - Index routing: GIT_INDEX_FILE
//   - Object routing: GIT_OBJECT_DIRECTORY, GIT_ALTERNATE_OBJECT_DIRECTORIES
//   - Config injection (any GIT_CONFIG* — e.g. GIT_CONFIG_PARAMETERS set
//     when the parent ran `git -c core.worktree=… commit`): the whole
//     GIT_CONFIG namespace, which includes _COUNT, _KEY_n, _VALUE_n,
//     _GLOBAL, _SYSTEM, _NOSYSTEM, and the legacy GIT_CONFIG itself.
func scrubGitHookEnv(env []string) []string {
	// The GIT_CONFIG prefix (no trailing "=") is intentional: it matches
	// GIT_CONFIG=, GIT_CONFIG_COUNT=, GIT_CONFIG_KEY_n=, GIT_CONFIG_VALUE_n=,
	// GIT_CONFIG_PARAMETERS=, GIT_CONFIG_GLOBAL=, GIT_CONFIG_SYSTEM=, and
	// GIT_CONFIG_NOSYSTEM= — the whole family — in one entry. No standard
	// git env var starts with GIT_CONFIG that we want to preserve.
	prefixes := []string{
		"GIT_DIR=",
		"GIT_WORK_TREE=",
		"GIT_INDEX_FILE=",
		"GIT_COMMON_DIR=",
		"GIT_PREFIX=",
		"GIT_OBJECT_DIRECTORY=",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=",
		"GIT_CEILING_DIRECTORIES=",
		"GIT_DISCOVERY_ACROSS_FILESYSTEM=",
		"GIT_CONFIG",
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		skip := false
		for _, p := range prefixes {
			if strings.HasPrefix(e, p) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, e)
		}
	}
	return out
}

// hookWorkTreeRoot returns the root of the worktree whose git hook we
// are running inside, based on the inherited GIT_DIR env var. Returns ""
// when GIT_DIR is not set (the normal non-hook case) or cannot be
// resolved to a work-tree.
//
// Resolution rules:
//   - In a linked worktree, GIT_DIR points at main/.git/worktrees/<name>
//     and that directory contains a "gitdir" file whose contents are the
//     absolute path to the worktree's .git FILE. The worktree root is
//     the parent of that .git file.
//   - In a non-worktree, GIT_DIR is typically ".git" or "<repo>/.git";
//     the worktree root is its parent.
func hookWorkTreeRoot() string {
	gitDir := os.Getenv("GIT_DIR")
	if gitDir == "" {
		return ""
	}
	var root string
	if data, err := os.ReadFile(filepath.Join(gitDir, "gitdir")); err == nil { // #nosec G304 -- path is GIT_DIR/gitdir, a well-known git internal file
		if dotGit := strings.TrimSpace(string(data)); dotGit != "" {
			root = filepath.Dir(dotGit)
		}
	}
	if root == "" && filepath.Base(gitDir) == ".git" {
		root = filepath.Dir(gitDir)
	}
	if root == "" {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	return abs
}

// pathInsideDir reports whether path is the same as dir or a descendant
// of dir, after resolving symlinks on both sides. Returns false on any
// resolution error (conservative: when in doubt, treat as outside).
//
// Resolves the PARENT of path rather than path itself, which handles the
// common "target file does not yet exist" case: on macOS /tmp is a
// symlink to /private/tmp, so asymmetric EvalSymlinks on a nonexistent
// file vs its existing parent would otherwise produce a spurious false.
// Callers (gitAddFile) always pass a path whose parent exists (either
// beadsDir, which FindBeadsDir verified, or a directory just created by
// the export write), so this single-level resolution is sufficient.
func pathInsideDir(path, dir string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	if r, err := filepath.EvalSymlinks(filepath.Dir(absPath)); err == nil {
		absPath = filepath.Join(r, filepath.Base(absPath))
	}
	if r, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = r
	}
	sep := string(filepath.Separator)
	return absPath == absDir || strings.HasPrefix(absPath, absDir+sep)
}
