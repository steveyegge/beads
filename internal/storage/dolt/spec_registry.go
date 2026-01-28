package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/spec"
)

// UpsertSpecRegistry inserts or updates spec_registry rows.
func (s *DoltStore) UpsertSpecRegistry(ctx context.Context, specs []spec.SpecRegistryEntry) error {
	if len(specs) == 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	stmt, err := s.db.PrepareContext(ctx, `
		INSERT INTO spec_registry (
			spec_id, path, title, sha256, mtime, discovered_at, last_scanned_at, missing_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			path = VALUES(path),
			title = VALUES(title),
			sha256 = VALUES(sha256),
			mtime = VALUES(mtime),
			discovered_at = VALUES(discovered_at),
			last_scanned_at = VALUES(last_scanned_at),
			missing_at = VALUES(missing_at)
	`)
	if err != nil {
		return fmt.Errorf("prepare spec_registry upsert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, entry := range specs {
		if _, err := stmt.ExecContext(ctx,
			entry.SpecID,
			entry.Path,
			entry.Title,
			entry.SHA256,
			entry.Mtime,
			entry.DiscoveredAt,
			entry.LastScannedAt,
			entry.MissingAt,
		); err != nil {
			return fmt.Errorf("upsert spec_registry %s: %w", entry.SpecID, err)
		}
	}
	return nil
}

// ListSpecRegistry returns all spec_registry entries.
func (s *DoltStore) ListSpecRegistry(ctx context.Context) ([]spec.SpecRegistryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT spec_id, path, title, sha256, mtime, discovered_at, last_scanned_at, missing_at
		FROM spec_registry
		ORDER BY spec_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list spec_registry: %w", err)
	}
	defer rows.Close()

	var results []spec.SpecRegistryEntry
	for rows.Next() {
		var entry spec.SpecRegistryEntry
		var missingAt *time.Time
		if err := rows.Scan(
			&entry.SpecID,
			&entry.Path,
			&entry.Title,
			&entry.SHA256,
			&entry.Mtime,
			&entry.DiscoveredAt,
			&entry.LastScannedAt,
			&missingAt,
		); err != nil {
			return nil, fmt.Errorf("scan spec_registry: %w", err)
		}
		entry.MissingAt = missingAt
		results = append(results, entry)
	}
	return results, rows.Err()
}

// GetSpecRegistry returns a single spec_registry entry.
func (s *DoltStore) GetSpecRegistry(ctx context.Context, specID string) (*spec.SpecRegistryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var entry spec.SpecRegistryEntry
	var missingAt *time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT spec_id, path, title, sha256, mtime, discovered_at, last_scanned_at, missing_at
		FROM spec_registry
		WHERE spec_id = ?
	`, specID).Scan(
		&entry.SpecID,
		&entry.Path,
		&entry.Title,
		&entry.SHA256,
		&entry.Mtime,
		&entry.DiscoveredAt,
		&entry.LastScannedAt,
		&missingAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get spec_registry: %w", err)
	}
	entry.MissingAt = missingAt
	return &entry, nil
}

// ListSpecRegistryWithCounts returns registry entries with bead counts.
func (s *DoltStore) ListSpecRegistryWithCounts(ctx context.Context) ([]spec.SpecRegistryCount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT s.spec_id, s.path, s.title, s.sha256, s.mtime, s.discovered_at, s.last_scanned_at, s.missing_at,
		       COUNT(i.id) AS bead_count,
		       SUM(CASE WHEN i.spec_changed_at IS NOT NULL THEN 1 ELSE 0 END) AS changed_count
		FROM spec_registry s
		LEFT JOIN issues i ON i.spec_id = s.spec_id
		GROUP BY s.spec_id
		ORDER BY s.spec_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list spec_registry with counts: %w", err)
	}
	defer rows.Close()

	var results []spec.SpecRegistryCount
	for rows.Next() {
		var entry spec.SpecRegistryEntry
		var missingAt *time.Time
		var beadCount, changedCount int
		if err := rows.Scan(
			&entry.SpecID,
			&entry.Path,
			&entry.Title,
			&entry.SHA256,
			&entry.Mtime,
			&entry.DiscoveredAt,
			&entry.LastScannedAt,
			&missingAt,
			&beadCount,
			&changedCount,
		); err != nil {
			return nil, fmt.Errorf("scan spec_registry with counts: %w", err)
		}
		entry.MissingAt = missingAt
		results = append(results, spec.SpecRegistryCount{
			Spec:             entry,
			BeadCount:        beadCount,
			ChangedBeadCount: changedCount,
		})
	}
	return results, rows.Err()
}

// MarkSpecsMissing sets missing_at for specs not found on disk.
func (s *DoltStore) MarkSpecsMissing(ctx context.Context, specIDs []string, missingAt time.Time) error {
	if len(specIDs) == 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	placeholders := strings.Repeat("?,", len(specIDs))
	placeholders = strings.TrimSuffix(placeholders, ",")
	args := make([]interface{}, 0, len(specIDs)+1)
	args = append(args, missingAt)
	for _, id := range specIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`UPDATE spec_registry SET missing_at = ? WHERE spec_id IN (%s)`, placeholders) // #nosec G201
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("mark specs missing: %w", err)
	}
	return nil
}

// ClearSpecsMissing clears missing_at for specs seen on disk.
func (s *DoltStore) ClearSpecsMissing(ctx context.Context, specIDs []string) error {
	if len(specIDs) == 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	placeholders := strings.Repeat("?,", len(specIDs))
	placeholders = strings.TrimSuffix(placeholders, ",")
	args := make([]interface{}, 0, len(specIDs))
	for _, id := range specIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`UPDATE spec_registry SET missing_at = NULL WHERE spec_id IN (%s)`, placeholders) // #nosec G201
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("clear specs missing: %w", err)
	}
	return nil
}

// MarkSpecChangedBySpecIDs sets spec_changed_at for issues linked to changed specs.
func (s *DoltStore) MarkSpecChangedBySpecIDs(ctx context.Context, specIDs []string, changedAt time.Time) (int, error) {
	if len(specIDs) == 0 {
		return 0, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	placeholders := strings.Repeat("?,", len(specIDs))
	placeholders = strings.TrimSuffix(placeholders, ",")
	args := make([]interface{}, 0, len(specIDs)+1)
	args = append(args, changedAt)
	for _, id := range specIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`UPDATE issues SET spec_changed_at = ? WHERE spec_id IN (%s)`, placeholders) // #nosec G201
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("mark spec_changed_at: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}

	idQuery := fmt.Sprintf(`SELECT id FROM issues WHERE spec_id IN (%s)`, placeholders) // #nosec G201
	rows, err := tx.QueryContext(ctx, idQuery, args[1:]...)
	if err != nil {
		return int(affected), fmt.Errorf("list issues for spec change: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var issueID string
		if err := rows.Scan(&issueID); err != nil {
			return int(affected), fmt.Errorf("scan issue id: %w", err)
		}
		if err := markDirty(ctx, tx, issueID); err != nil {
			return int(affected), fmt.Errorf("mark dirty issue %s: %w", issueID, err)
		}
	}
	if err := rows.Err(); err != nil {
		return int(affected), fmt.Errorf("iterate issue ids: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return int(affected), fmt.Errorf("commit spec change: %w", err)
	}
	return int(affected), nil
}
