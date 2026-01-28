package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/spec"
)

// UpsertSpecRegistry inserts or updates spec_registry rows.
func (s *SQLiteStorage) UpsertSpecRegistry(ctx context.Context, specs []spec.SpecRegistryEntry) error {
	if len(specs) == 0 {
		return nil
	}
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	stmt, err := s.db.PrepareContext(ctx, `
		INSERT INTO spec_registry (
			spec_id, path, title, sha256, mtime, discovered_at, last_scanned_at, missing_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(spec_id) DO UPDATE SET
			path = excluded.path,
			title = excluded.title,
			sha256 = excluded.sha256,
			mtime = excluded.mtime,
			discovered_at = excluded.discovered_at,
			last_scanned_at = excluded.last_scanned_at,
			missing_at = excluded.missing_at
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
func (s *SQLiteStorage) ListSpecRegistry(ctx context.Context) ([]spec.SpecRegistryEntry, error) {
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT spec_id, path, title, sha256, mtime, discovered_at, last_scanned_at, missing_at,
		       lifecycle, completed_at, summary, summary_tokens, archived_at
		FROM spec_registry
		ORDER BY spec_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list spec_registry: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []spec.SpecRegistryEntry
	for rows.Next() {
		var entry spec.SpecRegistryEntry
		var mtime, discoveredAt, lastScannedAt sql.NullString
		var missingAt sql.NullString
		var lifecycle sql.NullString
		var completedAt sql.NullString
		var summary sql.NullString
		var summaryTokens sql.NullInt64
		var archivedAt sql.NullString
		if err := rows.Scan(
			&entry.SpecID,
			&entry.Path,
			&entry.Title,
			&entry.SHA256,
			&mtime,
			&discoveredAt,
			&lastScannedAt,
			&missingAt,
			&lifecycle,
			&completedAt,
			&summary,
			&summaryTokens,
			&archivedAt,
		); err != nil {
			return nil, fmt.Errorf("scan spec_registry: %w", err)
		}
		if mtime.Valid {
			entry.Mtime = parseTimeString(mtime.String)
		}
		if discoveredAt.Valid {
			entry.DiscoveredAt = parseTimeString(discoveredAt.String)
		}
		if lastScannedAt.Valid {
			entry.LastScannedAt = parseTimeString(lastScannedAt.String)
		}
		entry.MissingAt = parseNullableTimeString(missingAt)
		if lifecycle.Valid {
			entry.Lifecycle = lifecycle.String
		}
		entry.CompletedAt = parseNullableTimeString(completedAt)
		if summary.Valid {
			entry.Summary = summary.String
		}
		if summaryTokens.Valid {
			entry.SummaryTokens = int(summaryTokens.Int64)
		}
		entry.ArchivedAt = parseNullableTimeString(archivedAt)
		results = append(results, entry)
	}
	return results, rows.Err()
}

// GetSpecRegistry returns a single spec_registry entry.
func (s *SQLiteStorage) GetSpecRegistry(ctx context.Context, specID string) (*spec.SpecRegistryEntry, error) {
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	var entry spec.SpecRegistryEntry
	var mtime, discoveredAt, lastScannedAt sql.NullString
	var missingAt sql.NullString
	var lifecycle sql.NullString
	var completedAt sql.NullString
	var summary sql.NullString
	var summaryTokens sql.NullInt64
	var archivedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT spec_id, path, title, sha256, mtime, discovered_at, last_scanned_at, missing_at,
		       lifecycle, completed_at, summary, summary_tokens, archived_at
		FROM spec_registry
		WHERE spec_id = ?
	`, specID).Scan(
		&entry.SpecID,
		&entry.Path,
		&entry.Title,
		&entry.SHA256,
		&mtime,
		&discoveredAt,
		&lastScannedAt,
		&missingAt,
		&lifecycle,
		&completedAt,
		&summary,
		&summaryTokens,
		&archivedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get spec_registry: %w", err)
	}
	if mtime.Valid {
		entry.Mtime = parseTimeString(mtime.String)
	}
	if discoveredAt.Valid {
		entry.DiscoveredAt = parseTimeString(discoveredAt.String)
	}
	if lastScannedAt.Valid {
		entry.LastScannedAt = parseTimeString(lastScannedAt.String)
	}
	entry.MissingAt = parseNullableTimeString(missingAt)
	if lifecycle.Valid {
		entry.Lifecycle = lifecycle.String
	}
	entry.CompletedAt = parseNullableTimeString(completedAt)
	if summary.Valid {
		entry.Summary = summary.String
	}
	if summaryTokens.Valid {
		entry.SummaryTokens = int(summaryTokens.Int64)
	}
	entry.ArchivedAt = parseNullableTimeString(archivedAt)
	return &entry, nil
}

// ListSpecRegistryWithCounts returns registry entries with bead counts.
func (s *SQLiteStorage) ListSpecRegistryWithCounts(ctx context.Context) ([]spec.SpecRegistryCount, error) {
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT s.spec_id, s.path, s.title, s.sha256, s.mtime, s.discovered_at, s.last_scanned_at, s.missing_at,
		       s.lifecycle, s.completed_at, s.summary, s.summary_tokens, s.archived_at,
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
	defer func() { _ = rows.Close() }()

	var results []spec.SpecRegistryCount
	for rows.Next() {
		var entry spec.SpecRegistryEntry
		var mtime, discoveredAt, lastScannedAt sql.NullString
		var missingAt sql.NullString
		var lifecycle sql.NullString
		var completedAt sql.NullString
		var summary sql.NullString
		var summaryTokens sql.NullInt64
		var archivedAt sql.NullString
		var beadCount, changedCount sql.NullInt64
		if err := rows.Scan(
			&entry.SpecID,
			&entry.Path,
			&entry.Title,
			&entry.SHA256,
			&mtime,
			&discoveredAt,
			&lastScannedAt,
			&missingAt,
			&lifecycle,
			&completedAt,
			&summary,
			&summaryTokens,
			&archivedAt,
			&beadCount,
			&changedCount,
		); err != nil {
			return nil, fmt.Errorf("scan spec_registry with counts: %w", err)
		}
		if mtime.Valid {
			entry.Mtime = parseTimeString(mtime.String)
		}
		if discoveredAt.Valid {
			entry.DiscoveredAt = parseTimeString(discoveredAt.String)
		}
		if lastScannedAt.Valid {
			entry.LastScannedAt = parseTimeString(lastScannedAt.String)
		}
		entry.MissingAt = parseNullableTimeString(missingAt)
		if lifecycle.Valid {
			entry.Lifecycle = lifecycle.String
		}
		entry.CompletedAt = parseNullableTimeString(completedAt)
		if summary.Valid {
			entry.Summary = summary.String
		}
		if summaryTokens.Valid {
			entry.SummaryTokens = int(summaryTokens.Int64)
		}
		entry.ArchivedAt = parseNullableTimeString(archivedAt)
		results = append(results, spec.SpecRegistryCount{
			Spec:             entry,
			BeadCount:        int(beadCount.Int64),
			ChangedBeadCount: int(changedCount.Int64),
		})
	}
	return results, rows.Err()
}

// MarkSpecsMissing sets missing_at for specs not found on disk.
func (s *SQLiteStorage) MarkSpecsMissing(ctx context.Context, specIDs []string, missingAt time.Time) error {
	if len(specIDs) == 0 {
		return nil
	}
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

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
func (s *SQLiteStorage) ClearSpecsMissing(ctx context.Context, specIDs []string) error {
	if len(specIDs) == 0 {
		return nil
	}
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

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

// UpdateSpecRegistry updates lifecycle metadata for a spec registry entry.
func (s *SQLiteStorage) UpdateSpecRegistry(ctx context.Context, specID string, updates spec.SpecRegistryUpdate) error {
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	setClauses := make([]string, 0, 5)
	args := make([]interface{}, 0, 6)

	if updates.Lifecycle != nil {
		setClauses = append(setClauses, "lifecycle = ?")
		args = append(args, *updates.Lifecycle)
	}
	if updates.CompletedAt != nil {
		setClauses = append(setClauses, "completed_at = ?")
		args = append(args, *updates.CompletedAt)
	}
	if updates.Summary != nil {
		setClauses = append(setClauses, "summary = ?")
		args = append(args, *updates.Summary)
	}
	if updates.SummaryTokens != nil {
		setClauses = append(setClauses, "summary_tokens = ?")
		args = append(args, *updates.SummaryTokens)
	}
	if updates.ArchivedAt != nil {
		setClauses = append(setClauses, "archived_at = ?")
		args = append(args, *updates.ArchivedAt)
	}

	if len(setClauses) == 0 {
		return nil
	}

	args = append(args, specID)
	query := fmt.Sprintf(`UPDATE spec_registry SET %s WHERE spec_id = ?`, strings.Join(setClauses, ", "))
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("update spec_registry: %w", err)
	}
	return nil
}

// MarkSpecChangedBySpecIDs sets spec_changed_at and updated_at for issues linked to changed specs.
func (s *SQLiteStorage) MarkSpecChangedBySpecIDs(ctx context.Context, specIDs []string, changedAt time.Time) (int, error) {
	if len(specIDs) == 0 {
		return 0, nil
	}
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("open connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	placeholders := strings.Repeat("?,", len(specIDs))
	placeholders = strings.TrimSuffix(placeholders, ",")
	args := make([]interface{}, 0, len(specIDs)+2)
	args = append(args, changedAt) // spec_changed_at
	args = append(args, changedAt) // updated_at
	for _, id := range specIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`UPDATE issues SET spec_changed_at = ?, updated_at = ? WHERE spec_id IN (%s)`, placeholders) // #nosec G201
	res, err := conn.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("mark spec_changed_at: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}

	idQuery := fmt.Sprintf(`SELECT id FROM issues WHERE spec_id IN (%s)`, placeholders) // #nosec G201
	rows, err := conn.QueryContext(ctx, idQuery, args[2:]...)
	if err != nil {
		return int(affected), fmt.Errorf("list issues for spec change: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var issueID string
		if err := rows.Scan(&issueID); err != nil {
			return int(affected), fmt.Errorf("scan issue id: %w", err)
		}
		if err := markDirty(ctx, conn, issueID); err != nil {
			return int(affected), fmt.Errorf("mark dirty issue %s: %w", issueID, err)
		}
	}
	if err := rows.Err(); err != nil {
		return int(affected), fmt.Errorf("iterate issue ids: %w", err)
	}

	return int(affected), nil
}

// AddSpecScanEvents stores scan history records.
func (s *SQLiteStorage) AddSpecScanEvents(ctx context.Context, events []spec.SpecScanEvent) error {
	if len(events) == 0 {
		return nil
	}
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin spec_scan_events tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO spec_scan_events (spec_id, scanned_at, sha256, changed)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare spec_scan_events insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, e := range events {
		changed := 0
		if e.Changed {
			changed = 1
		}
		if _, err := stmt.ExecContext(ctx, e.SpecID, e.ScannedAt, e.SHA256, changed); err != nil {
			return fmt.Errorf("insert spec_scan_event: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit spec_scan_events: %w", err)
	}
	return nil
}

// ListSpecScanEvents returns scan events for a spec since the given time.
func (s *SQLiteStorage) ListSpecScanEvents(ctx context.Context, specID string, since time.Time) ([]spec.SpecScanEvent, error) {
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	args := []interface{}{specID}
	query := `
		SELECT spec_id, scanned_at, sha256, changed
		FROM spec_scan_events
		WHERE spec_id = ?
	`
	if !since.IsZero() {
		query += " AND scanned_at >= ?"
		args = append(args, since)
	}
	query += " ORDER BY scanned_at ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list spec_scan_events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []spec.SpecScanEvent
	for rows.Next() {
		var e spec.SpecScanEvent
		var changedInt int
		if err := rows.Scan(&e.SpecID, &e.ScannedAt, &e.SHA256, &changedInt); err != nil {
			return nil, fmt.Errorf("scan spec_scan_event: %w", err)
		}
		e.Changed = changedInt != 0
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate spec_scan_events: %w", err)
	}
	return events, nil
}
