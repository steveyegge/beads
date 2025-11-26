// Package deletions handles the deletions manifest for tracking deleted issues.
// The deletions.jsonl file is an append-only log that records when issues are
// deleted, enabling propagation of deletions across repo clones via git sync.
package deletions

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DeletionRecord represents a single deletion entry in the manifest.
// Timestamps are serialized as RFC3339 and may lose sub-second precision.
type DeletionRecord struct {
	ID        string    `json:"id"`               // Issue ID that was deleted
	Timestamp time.Time `json:"ts"`               // When the deletion occurred
	Actor     string    `json:"by"`               // Who performed the deletion
	Reason    string    `json:"reason,omitempty"` // Optional reason for deletion
}

// LoadResult contains the result of loading deletions, including any warnings.
type LoadResult struct {
	Records  map[string]DeletionRecord
	Skipped  int
	Warnings []string
}

// LoadDeletions reads the deletions manifest and returns a LoadResult.
// Corrupt JSON lines are skipped rather than failing the load.
// Warnings about skipped lines are collected in LoadResult.Warnings.
func LoadDeletions(path string) (*LoadResult, error) {
	result := &LoadResult{
		Records:  make(map[string]DeletionRecord),
		Warnings: []string{},
	}

	f, err := os.Open(path) // #nosec G304 - controlled path from caller
	if err != nil {
		if os.IsNotExist(err) {
			// No deletions file yet - return empty result
			return result, nil
		}
		return nil, fmt.Errorf("failed to open deletions file: %w", err)
	}
	defer f.Close()

	lineNo := 0

	scanner := bufio.NewScanner(f)
	// Allow large lines (up to 1MB) in case of very long reasons
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)

	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if line == "" {
			continue
		}

		var record DeletionRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			warning := fmt.Sprintf("skipping corrupt line %d in deletions manifest: %v", lineNo, err)
			result.Warnings = append(result.Warnings, warning)
			result.Skipped++
			continue
		}

		// Validate required fields
		if record.ID == "" {
			warning := fmt.Sprintf("skipping line %d in deletions manifest: missing ID", lineNo)
			result.Warnings = append(result.Warnings, warning)
			result.Skipped++
			continue
		}

		// Use the most recent record for each ID (last write wins)
		result.Records[record.ID] = record
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading deletions file: %w", err)
	}

	return result, nil
}

// AppendDeletion appends a single deletion record to the manifest.
// Creates the file if it doesn't exist.
// Returns an error if the record has an empty ID.
func AppendDeletion(path string, record DeletionRecord) error {
	// Validate required fields
	if record.ID == "" {
		return fmt.Errorf("cannot append deletion record: ID is required")
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Open file for appending (create if not exists)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) // #nosec G302,G304 - controlled path, 0644 needed for git
	if err != nil {
		return fmt.Errorf("failed to open deletions file for append: %w", err)
	}
	defer f.Close()

	// Marshal record to JSON
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal deletion record: %w", err)
	}

	// Write line with newline
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write deletion record: %w", err)
	}

	// Sync to ensure durability for append-only log
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync deletions file: %w", err)
	}

	return nil
}

// WriteDeletions atomically writes the entire deletions manifest.
// Used for compaction to deduplicate and prune old entries.
// An empty slice will create an empty file (clearing all deletions).
func WriteDeletions(path string, records []DeletionRecord) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create temp file in same directory for atomic rename
	base := filepath.Base(path)
	tempFile, err := os.CreateTemp(dir, base+".tmp.*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath) // Clean up temp file on error
	}()

	// Write each record as a JSON line
	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("failed to marshal deletion record: %w", err)
		}
		if _, err := tempFile.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("failed to write deletion record: %w", err)
		}
	}

	// Close before rename
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic replace
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("failed to replace deletions file: %w", err)
	}

	return nil
}

// DefaultPath returns the default path for the deletions manifest.
// beadsDir is typically .beads/
func DefaultPath(beadsDir string) string {
	return filepath.Join(beadsDir, "deletions.jsonl")
}

// Count returns the number of lines in the deletions manifest.
// This is a fast operation that doesn't parse JSON, just counts lines.
// Returns 0 if the file doesn't exist or is empty.
func Count(path string) (int, error) {
	f, err := os.Open(path) // #nosec G304 - controlled path from caller
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to open deletions file: %w", err)
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			count++
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("error reading deletions file: %w", err)
	}

	return count, nil
}

// PruneResult contains the result of a prune operation.
type PruneResult struct {
	KeptCount   int
	PrunedCount int
	PrunedIDs   []string
}

// PruneDeletions removes deletion records older than the specified retention period.
// Returns PruneResult with counts and IDs of pruned records.
// If the file doesn't exist or is empty, returns zero counts with no error.
func PruneDeletions(path string, retentionDays int) (*PruneResult, error) {
	result := &PruneResult{
		PrunedIDs: []string{},
	}

	loadResult, err := LoadDeletions(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load deletions: %w", err)
	}

	if len(loadResult.Records) == 0 {
		return result, nil
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	var kept []DeletionRecord

	// Convert map to sorted slice for deterministic iteration (bd-wmo)
	var allRecords []DeletionRecord
	for _, record := range loadResult.Records {
		allRecords = append(allRecords, record)
	}
	sort.Slice(allRecords, func(i, j int) bool {
		return allRecords[i].ID < allRecords[j].ID
	})

	for _, record := range allRecords {
		if record.Timestamp.After(cutoff) || record.Timestamp.Equal(cutoff) {
			kept = append(kept, record)
		} else {
			result.PrunedCount++
			result.PrunedIDs = append(result.PrunedIDs, record.ID)
		}
	}

	result.KeptCount = len(kept)

	// Only rewrite if we actually pruned something
	if result.PrunedCount > 0 {
		if err := WriteDeletions(path, kept); err != nil {
			return nil, fmt.Errorf("failed to write pruned deletions: %w", err)
		}
	}

	return result, nil
}
