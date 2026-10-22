package fix

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/configfile"
)

// ClassicArtifacts removes beads classic artifacts found by scanning the path.
// Only removes artifacts that are safe to delete:
// - JSONL export artifacts (not issues.jsonl itself)
// - SQLite WAL/SHM files and backup databases
// - Extra files in redirect-only .beads directories
//
// It never migrates a .beads directory's metadata, and leaves a .beads
// directory in a format this build cannot open untouched (see
// cleanBeadsDirArtifacts).
func ClassicArtifacts(path string) error {
	var removed, skipped, errCount int

	// Walk the directory tree looking for .beads/ directories
	err := filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip directories we can't read
		}

		// Skip heavy directories
		base := filepath.Base(walkPath)
		if info.IsDir() && (base == "node_modules" || base == "vendor" || base == "__pycache__") {
			return filepath.SkipDir
		}

		// We only care about directories named ".beads"
		if !info.IsDir() || base != ".beads" {
			return nil
		}

		r, s, e := cleanBeadsDirArtifacts(walkPath)
		removed += r
		skipped += s
		errCount += e

		return filepath.SkipDir
	})
	if err != nil {
		return fmt.Errorf("failed to walk directory tree: %w", err)
	}

	// Report summary
	fmt.Printf("  Artifact cleanup: %d removed, %d skipped, %d errors\n", removed, skipped, errCount)

	if skipped > 0 {
		fmt.Println("  Skipped items may need manual review (e.g., issues.jsonl in dolt dirs, beads.db files)")
	}

	if errCount > 0 {
		return fmt.Errorf("%d artifact(s) could not be removed", errCount)
	}

	return nil
}

// cleanBeadsDirArtifacts cleans artifacts from a single .beads directory.
// Returns counts of removed, skipped, and errored items.
//
// The walk reaches the .beads directories of nested repositories, sub-projects
// and worktrees, and command admission checked only the workspace doctor runs
// in. So beadsDir's metadata is read as discovery reads it, without migrating
// a legacy config.json into metadata.json, and a workspace in a format this
// build cannot open is left byte-identical: configfile.Load would rewrite that
// config.json, dropping keys it does not know, and the cleanup below would
// remove files, the format marker among them, whose meaning this build does
// not know.
func cleanBeadsDirArtifacts(beadsDir string) (removed, skipped, errCount int) {
	cfg, err := configfile.LoadAndCheckWorkspaceFormat(beadsDir)
	var formatErr *configfile.UnsupportedWorkspaceFormatError
	if errors.As(err, &formatErr) {
		return
	}

	hasDolt := hasDoltDir(beadsDir, cfg)
	isRedirectExpected := isRedirectExpectedLocation(beadsDir)

	// 1. Clean JSONL artifacts in dolt-native directories
	if hasDolt {
		r, s, e := cleanJSONLArtifacts(beadsDir)
		removed += r
		skipped += s
		errCount += e
	}

	// 2. Clean SQLite artifacts
	r, s, e := cleanSQLiteArtifacts(beadsDir)
	removed += r
	skipped += s
	errCount += e

	// 3. Clean cruft .beads directories (if redirect is expected)
	// Clean even when the redirect file is missing — stale cruft files
	// (config.yaml, metadata.json, README.md, issues.jsonl, etc.) prevent
	// the redirect from being created and should be removed regardless.
	if isRedirectExpected {
		r, e := cleanCruftBeadsDirFiles(beadsDir)
		removed += r
		errCount += e
	}

	return
}

// hasDoltDir returns true if the Dolt database directory of the .beads
// directory exists: the one cfg, beadsDir's metadata, resolves (respecting
// dolt_data_dir), or beadsDir/dolt when there is no metadata or it does not
// load (cfg nil).
func hasDoltDir(beadsDir string, cfg *configfile.Config) bool {
	doltPath := filepath.Join(beadsDir, "dolt")
	if cfg != nil {
		doltPath = cfg.DatabasePath(beadsDir)
	}
	info, err := os.Stat(doltPath)
	return err == nil && info.IsDir()
}

// isRedirectExpectedLocation returns true if this .beads directory should contain
// only a redirect file.
func isRedirectExpectedLocation(beadsDir string) bool {
	parent := filepath.Dir(beadsDir)
	grandparent := filepath.Dir(parent)
	grandparentName := filepath.Base(grandparent)
	parentName := filepath.Base(parent)

	// Pattern: */polecats/*/.beads/ (orchestrator worker — backwards compat)
	if grandparentName == "polecats" {
		return true
	}
	// Pattern: */crew/*/.beads/ (orchestrator assistant — backwards compat)
	if grandparentName == "crew" {
		return true
	}
	// Pattern: */refinery/rig/.beads/ (orchestrator processor — backwards compat)
	if parentName == "rig" && grandparentName == "refinery" {
		return true
	}
	// Pattern: .git/beads-worktrees/*/.beads/
	if grandparentName == "beads-worktrees" {
		return true
	}
	// Rig-root .beads/ with a mayor/rig/.beads/ canonical location
	canonicalDir := filepath.Join(parent, "mayor", "rig", ".beads")
	if _, err := os.Stat(canonicalDir); err == nil {
		// Also check it has polecats/ or mayor/ siblings
		for _, sibling := range []string{"mayor", "polecats"} {
			if info, err := os.Stat(filepath.Join(parent, sibling)); err == nil && info.IsDir() {
				return true
			}
		}
	}

	return false
}

// cleanJSONLArtifacts removes stale JSONL files from a dolt-native .beads directory.
func cleanJSONLArtifacts(beadsDir string) (removed, skipped, errCount int) {
	// Safe to delete (not the primary data source)
	safeFiles := []string{
		"issues.jsonl.new",
	}

	for _, name := range safeFiles {
		path := filepath.Join(beadsDir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := os.Remove(path); err != nil {
			fmt.Printf("  Error removing %s: %v\n", path, err)
			errCount++
			continue
		}
		fmt.Printf("  Removed: %s (JSONL artifact)\n", path)
		removed++
	}

	// interactions.jsonl - only remove if empty
	interPath := filepath.Join(beadsDir, "interactions.jsonl")
	if info, err := os.Stat(interPath); err == nil {
		if info.Size() == 0 {
			if err := os.Remove(interPath); err != nil {
				fmt.Printf("  Error removing %s: %v\n", interPath, err)
				errCount++
			} else {
				fmt.Printf("  Removed: %s (empty interactions log)\n", interPath)
				removed++
			}
		} else {
			fmt.Printf("  Skip (not empty): %s\n", interPath)
			skipped++
		}
	}

	// issues.jsonl in dolt-native directory - skip (needs manual review)
	issuesPath := filepath.Join(beadsDir, "issues.jsonl")
	if _, err := os.Stat(issuesPath); err == nil {
		fmt.Printf("  Skip (needs review): %s (issues.jsonl in dolt-native dir)\n", issuesPath)
		skipped++
	}

	return
}

// cleanSQLiteArtifacts removes leftover SQLite database files.
func cleanSQLiteArtifacts(beadsDir string) (removed, skipped, errCount int) {
	entries, err := os.ReadDir(beadsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		// WAL and SHM files are always safe to delete
		if name == "beads.db-shm" || name == "beads.db-wal" {
			path := filepath.Join(beadsDir, name)
			if err := os.Remove(path); err != nil {
				fmt.Printf("  Error removing %s: %v\n", path, err)
				errCount++
				continue
			}
			fmt.Printf("  Removed: %s (SQLite WAL/SHM)\n", path)
			removed++
			continue
		}

		// beads.db - skip (needs manual review, could be active)
		if name == "beads.db" {
			path := filepath.Join(beadsDir, name)
			fmt.Printf("  Skip (needs review): %s\n", path)
			skipped++
			continue
		}

		// Backup databases are safe to delete
		if strings.HasPrefix(name, "beads.backup-") && strings.HasSuffix(name, ".db") {
			path := filepath.Join(beadsDir, name)
			if err := os.Remove(path); err != nil {
				fmt.Printf("  Error removing %s: %v\n", path, err)
				errCount++
				continue
			}
			fmt.Printf("  Removed: %s (pre-migration backup)\n", path)
			removed++
		}
	}

	return
}

// cleanCruftBeadsDirFiles removes everything from a .beads directory except
// the redirect file, .gitkeep, and (when a redirect file is present) the
// directory's own metadata: metadata.json, or a legacy config.json, which
// this walk does not migrate. The Classic Artifacts scan
// (doctor.scanCruftBeadsDir) reports exactly the entries this removes.
func cleanCruftBeadsDirFiles(beadsDir string) (removed, errCount int) {
	entries, err := os.ReadDir(beadsDir)
	if err != nil {
		return 0, 1
	}

	// gastownhall/beads#4692: a co-located redirect and metadata.json is a
	// documented, supported topology (see fb51196f7 / docs/reference/
	// advanced.md "Database Redirects" -- a server-mode source rig's own
	// metadata.json supplies its dolt_database while the redirect points at
	// the shared Gas Town root). Deleting metadata.json here would corrupt
	// that source rig's identity while leaving the redirect itself intact,
	// so never delete it when a redirect is present in the same directory.
	// A legacy config.json there is kept too, unmigrated, but it is inert:
	// ResolveRedirect reads the source's dolt_database only from
	// metadata.json, and this walk no longer migrates (see
	// cleanBeadsDirArtifacts). bd wrote config.json only before its rename
	// to metadata.json, which predates dolt_database, so one bd wrote names
	// no database to route to. It is kept because the cleanup cannot vouch
	// for what it holds, not for routing.
	hasRedirect := false
	for _, entry := range entries {
		if entry.Name() == "redirect" {
			hasRedirect = true
			break
		}
	}

	for _, entry := range entries {
		name := entry.Name()
		// Keep redirect and .gitkeep
		if name == "redirect" || name == ".gitkeep" {
			continue
		}
		if (name == "metadata.json" || name == "config.json") && hasRedirect {
			continue
		}

		entryPath := filepath.Join(beadsDir, name)

		// Validate path doesn't escape
		rel, err := filepath.Rel(beadsDir, entryPath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			continue
		}

		if entry.IsDir() {
			if err := os.RemoveAll(entryPath); err != nil {
				fmt.Printf("  Error removing %s: %v\n", entryPath, err)
				errCount++
				continue
			}
		} else {
			if err := os.Remove(entryPath); err != nil {
				fmt.Printf("  Error removing %s: %v\n", entryPath, err)
				errCount++
				continue
			}
		}
		fmt.Printf("  Removed: %s (cruft in redirect-only dir)\n", entryPath)
		removed++
	}

	return
}
