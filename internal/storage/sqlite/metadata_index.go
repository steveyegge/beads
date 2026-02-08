package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
)

// updateMetadataIndex refreshes the index rows for a specific issue.
// It must be called within the transaction of Create/Update issue.
//
// For Phase 1, this indexes all top-level scalar values (string, int, float, bool)
// found in the metadata JSON blob. No schema is required.
//
// GH#1589: Phase 1 of the Schema-Indexed Metadata architecture.
func updateMetadataIndex(ctx context.Context, exec dbExecutor, issueID string, metadataJSON string) error {
	// Clear existing index entries for this issue.
	// We do a full delete/re-insert for simplicity and correctness.
	_, err := exec.ExecContext(ctx, `DELETE FROM issue_metadata_index WHERE issue_id = ?`, issueID)
	if err != nil {
		return fmt.Errorf("failed to clear metadata index for %s: %w", issueID, err)
	}

	if metadataJSON == "" || metadataJSON == "{}" {
		return nil
	}

	// Parse metadata JSON
	var meta map[string]any
	if err := json.Unmarshal([]byte(metadataJSON), &meta); err != nil {
		// Invalid JSON is ignored for indexing; validation happens elsewhere
		return nil
	}

	// Index top-level scalars
	return indexFlatKeys(ctx, exec, issueID, "", meta)
}

// indexFlatKeys indexes scalar values from a metadata map, supporting one level of nesting
// for namespaced keys (e.g., "jira.story_points").
func indexFlatKeys(ctx context.Context, exec dbExecutor, issueID, prefix string, meta map[string]any) error {
	stmt := `INSERT OR REPLACE INTO issue_metadata_index (issue_id, key, value_text, value_int, value_real) VALUES (?, ?, ?, ?, ?)`

	for key, val := range meta {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := val.(type) {
		case string:
			if _, err := exec.ExecContext(ctx, stmt, issueID, fullKey, v, nil, nil); err != nil {
				return fmt.Errorf("failed to index metadata key %s: %w", fullKey, err)
			}
		case float64:
			// JSON numbers are float64. Check if it's actually an integer.
			if v == float64(int64(v)) {
				if _, err := exec.ExecContext(ctx, stmt, issueID, fullKey, nil, int64(v), nil); err != nil {
					return fmt.Errorf("failed to index metadata key %s: %w", fullKey, err)
				}
			} else {
				if _, err := exec.ExecContext(ctx, stmt, issueID, fullKey, nil, nil, v); err != nil {
					return fmt.Errorf("failed to index metadata key %s: %w", fullKey, err)
				}
			}
		case bool:
			i := int64(0)
			if v {
				i = 1
			}
			if _, err := exec.ExecContext(ctx, stmt, issueID, fullKey, nil, i, nil); err != nil {
				return fmt.Errorf("failed to index metadata key %s: %w", fullKey, err)
			}
		case map[string]any:
			// Support one level of nesting for namespaced keys (e.g., "jira.story_points")
			if prefix == "" {
				if err := indexFlatKeys(ctx, exec, issueID, key, v); err != nil {
					return err
				}
			}
			// Skip deeper nesting
		default:
			// Skip arrays, nulls, and deeper structures
			continue
		}
	}
	return nil
}

// RebuildMetadataIndex wipes and rebuilds the entire metadata index from the
// canonical metadata column. Safe to call from bd doctor or bd import.
func (s *SQLiteStorage) RebuildMetadataIndex(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Truncate the index
	if _, err := conn.ExecContext(ctx, `DELETE FROM issue_metadata_index`); err != nil {
		return fmt.Errorf("failed to truncate metadata index: %w", err)
	}

	// Scan all issues with non-empty metadata
	rows, err := conn.QueryContext(ctx, `SELECT id, metadata FROM issues WHERE metadata IS NOT NULL AND metadata != '' AND metadata != '{}'`)
	if err != nil {
		return fmt.Errorf("failed to query issues for metadata index rebuild: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id, meta string
		if err := rows.Scan(&id, &meta); err != nil {
			return fmt.Errorf("failed to scan issue for metadata index: %w", err)
		}
		if err := updateMetadataIndex(ctx, conn, id, meta); err != nil {
			return fmt.Errorf("failed to index metadata for %s: %w", id, err)
		}
	}
	return rows.Err()
}
