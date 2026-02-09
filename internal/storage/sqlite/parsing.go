package sqlite

import (
	"database/sql"
	"encoding/json"
	"time"
)

// parseNullableTimeString parses a nullable time string from database TEXT columns.
// The ncruces/go-sqlite3 driver only auto-converts TEXT→time.Time for columns declared
// as DATETIME/DATE/TIME/TIMESTAMP. For TEXT columns (like deleted_at), we must parse manually.
// Supports RFC3339, RFC3339Nano, and SQLite's native format.
func parseNullableTimeString(ns sql.NullString) *time.Time {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	// Try RFC3339Nano first (more precise), then RFC3339, then SQLite format
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, ns.String); err == nil {
			return &t
		}
	}
	return nil // Unparseable - shouldn't happen with valid data
}

// parseTimeString parses a time string from database TEXT columns (non-nullable).
// Similar to parseNullableTimeString but for required timestamp fields like created_at/updated_at.
// Returns zero time if parsing fails, which maintains backwards compatibility.
func parseTimeString(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// Try RFC3339Nano first (more precise), then RFC3339, then SQLite format
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{} // Unparseable - shouldn't happen with valid data
}

// parseJSONStringArray parses a JSON string array from database TEXT column.
// Returns empty slice if the string is empty or invalid JSON.
func parseJSONStringArray(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil // Invalid JSON - shouldn't happen with valid data
	}
	return result
}

// formatJSONStringArray formats a string slice as JSON for database storage.
// Returns empty string if the slice is nil or empty.
func formatJSONStringArray(arr []string) string {
	if len(arr) == 0 {
		return ""
	}
	data, err := json.Marshal(arr)
	if err != nil {
		return ""
	}
	return string(data)
}
