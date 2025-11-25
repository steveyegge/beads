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
	"time"
)

// DeletionRecord represents a single deletion entry in the manifest.
type DeletionRecord struct {
	ID        string    `json:"id"`              // Issue ID that was deleted
	Timestamp time.Time `json:"ts"`              // When the deletion occurred
	Actor     string    `json:"by"`              // Who performed the deletion
	Reason    string    `json:"reason,omitempty"` // Optional reason for deletion
}

// LoadDeletions reads the deletions manifest and returns a map for O(1) lookup.
// It returns the records, the count of skipped (corrupt) lines, and any error.
// Corrupt JSON lines are skipped with a warning rather than failing the load.
func LoadDeletions(path string) (map[string]DeletionRecord, int, error) {
	f, err := os.Open(path) // #nosec G304 - controlled path from caller
	if err != nil {
		if os.IsNotExist(err) {
			// No deletions file yet - return empty map
			return make(map[string]DeletionRecord), 0, nil
		}
		return nil, 0, fmt.Errorf("failed to open deletions file: %w", err)
	}
	defer f.Close()

	records := make(map[string]DeletionRecord)
	skipped := 0
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
			// Skip corrupt line with warning to stderr
			fmt.Fprintf(os.Stderr, "Warning: skipping corrupt line %d in deletions manifest: %v\n", lineNo, err)
			skipped++
			continue
		}

		// Validate required fields
		if record.ID == "" {
			fmt.Fprintf(os.Stderr, "Warning: skipping line %d in deletions manifest: missing ID\n", lineNo)
			skipped++
			continue
		}

		// Use the most recent record for each ID (last write wins)
		records[record.ID] = record
	}

	if err := scanner.Err(); err != nil {
		return nil, skipped, fmt.Errorf("error reading deletions file: %w", err)
	}

	return records, skipped, nil
}

// AppendDeletion appends a single deletion record to the manifest.
// Creates the file if it doesn't exist.
func AppendDeletion(path string, record DeletionRecord) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Open file for appending (create if not exists)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) // #nosec G304 - controlled path
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

	return nil
}

// WriteDeletions atomically writes the entire deletions manifest.
// Used for compaction to deduplicate and prune old entries.
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
