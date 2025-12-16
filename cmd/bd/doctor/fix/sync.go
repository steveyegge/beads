package fix

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/steveyegge/beads/internal/deletions"
)

// DBJSONLSync fixes database-JSONL sync issues by running bd sync --import-only
func DBJSONLSync(path string) error {
	// Validate workspace
	if err := validateBeadsWorkspace(path); err != nil {
		return err
	}

	beadsDir := filepath.Join(path, ".beads")

	// Check if both database and JSONL exist
	dbPath := filepath.Join(beadsDir, "beads.db")
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	beadsJSONLPath := filepath.Join(beadsDir, "beads.jsonl")

	hasDB := false
	if _, err := os.Stat(dbPath); err == nil {
		hasDB = true
	}

	hasJSONL := false
	actualJSONLPath := ""
	if _, err := os.Stat(jsonlPath); err == nil {
		hasJSONL = true
		actualJSONLPath = jsonlPath
	} else if _, err := os.Stat(beadsJSONLPath); err == nil {
		hasJSONL = true
		actualJSONLPath = beadsJSONLPath
	}

	if !hasDB || !hasJSONL {
		// Nothing to sync
		return nil
	}

	// Get bd binary path
	bdBinary, err := getBdBinary()
	if err != nil {
		return err
	}

	// Run bd sync --import-only to import JSONL updates
	cmd := exec.Command(bdBinary, "sync", "--import-only") // #nosec G204 -- bdBinary from validated executable path
	cmd.Dir = path                                          // Set working directory without changing process dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to sync database with JSONL: %w", err)
	}

	// bd-8v5o: Clean up deletions manifest for hydrated issues
	// After sync, remove any issues from deletions.jsonl that exist in JSONL
	// This prevents perpetual "Skipping bd-xxx (in deletions manifest)" warnings
	if err := cleanupDeletionsManifest(beadsDir, actualJSONLPath); err != nil {
		// Non-fatal - just log warning
		fmt.Printf("  Warning: failed to clean up deletions manifest: %v\n", err)
	}

	return nil
}

// cleanupDeletionsManifest removes issues from deletions.jsonl that exist in JSONL.
// This is needed because when issues are hydrated from git history (e.g., via bd init
// or bd sync --import-only), they may still be in the deletions manifest from a
// previous deletion. This causes perpetual skip warnings during sync.
func cleanupDeletionsManifest(beadsDir, jsonlPath string) error {
	deletionsPath := deletions.DefaultPath(beadsDir)

	// Check if deletions manifest exists
	if _, err := os.Stat(deletionsPath); os.IsNotExist(err) {
		return nil // No deletions manifest, nothing to clean up
	}

	// Load deletions manifest
	loadResult, err := deletions.LoadDeletions(deletionsPath)
	if err != nil {
		return fmt.Errorf("failed to load deletions manifest: %w", err)
	}

	if len(loadResult.Records) == 0 {
		return nil // No deletions, nothing to clean up
	}

	// Get IDs from JSONL (excluding tombstones)
	jsonlIDs, err := getNonTombstoneJSONLIDs(jsonlPath)
	if err != nil {
		return fmt.Errorf("failed to read JSONL: %w", err)
	}

	// Find IDs that are in both deletions manifest and JSONL
	var idsToRemove []string
	for id := range loadResult.Records {
		if jsonlIDs[id] {
			idsToRemove = append(idsToRemove, id)
		}
	}

	if len(idsToRemove) == 0 {
		return nil // No conflicting entries
	}

	// Remove conflicting entries from deletions manifest
	result, err := deletions.RemoveDeletions(deletionsPath, idsToRemove)
	if err != nil {
		return fmt.Errorf("failed to remove deletions: %w", err)
	}

	if result.RemovedCount > 0 {
		fmt.Printf("  Removed %d issue(s) from deletions manifest (now hydrated in JSONL)\n", result.RemovedCount)
	}

	return nil
}

// getNonTombstoneJSONLIDs reads the JSONL file and returns a set of IDs
// that are not tombstones (status != "tombstone").
func getNonTombstoneJSONLIDs(jsonlPath string) (map[string]bool, error) {
	ids := make(map[string]bool)

	file, err := os.Open(jsonlPath) // #nosec G304 - path validated by caller
	if err != nil {
		if os.IsNotExist(err) {
			return ids, nil
		}
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var issue struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(line, &issue); err != nil {
			continue
		}
		// Only include non-tombstone issues
		if issue.ID != "" && issue.Status != "tombstone" {
			ids[issue.ID] = true
		}
	}

	return ids, scanner.Err()
}
