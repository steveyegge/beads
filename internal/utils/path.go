// Package utils provides utility functions for issue ID parsing and path handling.
package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// FindJSONLInDir finds the JSONL file in the given .beads directory.
// It prefers issues.jsonl over other .jsonl files to prevent accidentally
// reading/writing to deletions.jsonl or merge artifacts (bd-tqo fix).
// Always returns a path (defaults to issues.jsonl if nothing suitable found).
//
// Search order:
// 1. issues.jsonl (canonical name)
// 2. beads.jsonl (legacy support)
// 3. Any other .jsonl file except deletions/merge artifacts
// 4. Default to issues.jsonl
func FindJSONLInDir(dbDir string) string {
	pattern := filepath.Join(dbDir, "*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		// Default to issues.jsonl if glob fails or no matches
		return filepath.Join(dbDir, "issues.jsonl")
	}

	// Prefer issues.jsonl over other .jsonl files (bd-tqo fix)
	// This prevents accidentally using deletions.jsonl or merge artifacts
	for _, match := range matches {
		if filepath.Base(match) == "issues.jsonl" {
			return match
		}
	}

	// Fall back to beads.jsonl for legacy support
	for _, match := range matches {
		if filepath.Base(match) == "beads.jsonl" {
			return match
		}
	}

	// Last resort: use first match (but skip deletions.jsonl, interactions.jsonl, and merge artifacts)
	for _, match := range matches {
		base := filepath.Base(match)
		// Skip deletions manifest, interactions (audit trail), and merge artifacts
		if base == "deletions.jsonl" ||
			base == "interactions.jsonl" ||
			base == "beads.base.jsonl" ||
			base == "beads.left.jsonl" ||
			base == "beads.right.jsonl" {
			continue
		}
		return match
	}

	// If only deletions/merge files exist, default to issues.jsonl
	return filepath.Join(dbDir, "issues.jsonl")
}

// FindMoleculesJSONLInDir finds the molecules.jsonl file in the given .beads directory.
// Returns the path to molecules.jsonl if it exists, empty string otherwise.
// Molecules are template issues used for instantiation (beads-1ra).
func FindMoleculesJSONLInDir(dbDir string) string {
	moleculesPath := filepath.Join(dbDir, "molecules.jsonl")
	// Check if file exists - we don't fall back to any other file
	// because molecules.jsonl is optional and specific
	if _, err := os.Stat(moleculesPath); err == nil {
		return moleculesPath
	}
	return ""
}

// ResolveForWrite returns the path to write to, resolving symlinks.
// If path is a symlink, returns the resolved target path.
// If path doesn't exist, returns path unchanged (new file).
func ResolveForWrite(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, nil
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return filepath.EvalSymlinks(path)
	}
	return path, nil
}

// CanonicalizePath converts a path to its canonical form by:
// 1. Converting to absolute path
// 2. Resolving symlinks
//
// If either step fails, it falls back to the best available form:
// - If symlink resolution fails, returns absolute path
// - If absolute path conversion fails, returns original path
//
// This function is used to ensure consistent path handling across the codebase,
// particularly for BEADS_DIR environment variable processing.
func CanonicalizePath(path string) string {
	// Try to get absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		// If we can't get absolute path, return original
		return path
	}

	// Try to resolve symlinks
	canonical, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// If we can't resolve symlinks, return absolute path
		return absPath
	}

	return canonical
}

// NormalizePathForComparison returns a normalized path suitable for comparison.
// It resolves symlinks and handles case-insensitive filesystems (macOS, Windows).
//
// On case-insensitive filesystems (darwin, windows), the path is lowercased
// to ensure that /Users/foo/Desktop and /Users/foo/desktop compare as equal.
//
// This function should be used whenever comparing workspace paths, not for
// storing or displaying paths (preserve original case for those purposes).
func NormalizePathForComparison(path string) string {
	if path == "" {
		return ""
	}

	// Try to get absolute path first
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	// Try to resolve symlinks
	canonical, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// If symlink resolution fails (e.g., path doesn't exist), use absolute path
		canonical = absPath
	}

	// On case-insensitive filesystems, lowercase for comparison
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		canonical = strings.ToLower(canonical)
	}

	return canonical
}

// PathsEqual compares two paths for equality, handling case-insensitive
// filesystems and symlinks. This is the preferred way to compare workspace
// paths in the daemon registry and discovery code.
func PathsEqual(path1, path2 string) bool {
	return NormalizePathForComparison(path1) == NormalizePathForComparison(path2)
}
