// ABOUTME: Storage layer for skills_manifest and skill_bead_links tables (Shadowbook feature)
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Skill represents a skill definition from the skills_manifest table.
// Skills are reusable instructions that can be linked to multiple beads.
type Skill struct {
	ID         string     // Unique identifier (e.g., "tdd", "debugging")
	Name       string     // Display name
	Source     string     // Source CLI: claude | codex | opencode
	Path       string     // File path in source (optional)
	Tier       string     // Importance: must-have | optional
	SHA256     string     // Content hash for drift detection
	Bytes      int64      // File size
	Status     string     // Lifecycle: active | deprecated | archived
	CreatedAt  time.Time  // When the skill was first added
	LastUsedAt *time.Time // When the skill was last linked to a bead
	ArchivedAt *time.Time // When the skill was archived (if applicable)
}

// SkillFilter defines optional filters for listing skills.
type SkillFilter struct {
	Source string // Filter by source (claude, codex, opencode)
	Status string // Filter by status (active, deprecated, archived)
	Tier   string // Filter by tier (must-have, optional)
}

// UpsertSkill inserts or updates a skill in the skills_manifest table.
// Uses ON CONFLICT to update existing records matching the primary key (id).
func (s *SQLiteStorage) UpsertSkill(ctx context.Context, skill *Skill) error {
	if skill == nil {
		return fmt.Errorf("upsert skill: skill cannot be nil")
	}
	if skill.ID == "" {
		return fmt.Errorf("upsert skill: id is required")
	}
	if skill.Name == "" {
		return fmt.Errorf("upsert skill: name is required")
	}
	if skill.Source == "" {
		return fmt.Errorf("upsert skill: source is required")
	}
	if skill.SHA256 == "" {
		return fmt.Errorf("upsert skill: sha256 is required")
	}

	// Validate source
	switch skill.Source {
	case "claude", "codex", "opencode":
		// Valid
	default:
		return fmt.Errorf("upsert skill: invalid source %q (must be claude, codex, or opencode)", skill.Source)
	}

	// Validate tier
	if skill.Tier == "" {
		skill.Tier = "optional" // Default
	}
	switch skill.Tier {
	case "must-have", "optional":
		// Valid
	default:
		return fmt.Errorf("upsert skill: invalid tier %q (must be must-have or optional)", skill.Tier)
	}

	// Validate status
	if skill.Status == "" {
		skill.Status = "active" // Default
	}
	switch skill.Status {
	case "active", "deprecated", "archived":
		// Valid
	default:
		return fmt.Errorf("upsert skill: invalid status %q (must be active, deprecated, or archived)", skill.Status)
	}

	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO skills_manifest (
			id, name, source, path, tier, sha256, bytes, status, created_at, last_used_at, archived_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			source = excluded.source,
			path = excluded.path,
			tier = excluded.tier,
			sha256 = excluded.sha256,
			bytes = excluded.bytes,
			status = excluded.status,
			last_used_at = excluded.last_used_at,
			archived_at = excluded.archived_at
	`,
		skill.ID,
		skill.Name,
		skill.Source,
		skill.Path,
		skill.Tier,
		skill.SHA256,
		skill.Bytes,
		skill.Status,
		skill.CreatedAt,
		skill.LastUsedAt,
		skill.ArchivedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert skill %s: %w", skill.ID, err)
	}
	return nil
}

// GetSkill retrieves a skill by ID from the skills_manifest table.
// Returns nil, nil if the skill is not found.
func (s *SQLiteStorage) GetSkill(ctx context.Context, id string) (*Skill, error) {
	if id == "" {
		return nil, fmt.Errorf("get skill: id is required")
	}

	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	var skill Skill
	var path sql.NullString
	var bytes sql.NullInt64
	var createdAt sql.NullString
	var lastUsedAt sql.NullString
	var archivedAt sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, source, path, tier, sha256, bytes, status, created_at, last_used_at, archived_at
		FROM skills_manifest
		WHERE id = ?
	`, id).Scan(
		&skill.ID,
		&skill.Name,
		&skill.Source,
		&path,
		&skill.Tier,
		&skill.SHA256,
		&bytes,
		&skill.Status,
		&createdAt,
		&lastUsedAt,
		&archivedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get skill %s: %w", id, err)
	}

	// Handle nullable fields
	if path.Valid {
		skill.Path = path.String
	}
	if bytes.Valid {
		skill.Bytes = bytes.Int64
	}
	if createdAt.Valid {
		skill.CreatedAt = parseTimeString(createdAt.String)
	}
	skill.LastUsedAt = parseNullableTimeString(lastUsedAt)
	skill.ArchivedAt = parseNullableTimeString(archivedAt)

	return &skill, nil
}

// ListSkills returns all skills matching the optional filter criteria.
// If filter fields are empty, no filtering is applied for that field.
func (s *SQLiteStorage) ListSkills(ctx context.Context, filter SkillFilter) ([]*Skill, error) {
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	// Build query with optional filters
	query := `
		SELECT id, name, source, path, tier, sha256, bytes, status, created_at, last_used_at, archived_at
		FROM skills_manifest
		WHERE 1=1
	`
	var args []interface{}

	if filter.Source != "" {
		query += " AND source = ?"
		args = append(args, filter.Source)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.Tier != "" {
		query += " AND tier = ?"
		args = append(args, filter.Tier)
	}

	query += " ORDER BY name ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanSkills(rows)
}

// scanSkills scans multiple skill rows into a slice of Skill pointers.
func scanSkills(rows *sql.Rows) ([]*Skill, error) {
	var skills []*Skill
	for rows.Next() {
		var skill Skill
		var path sql.NullString
		var bytes sql.NullInt64
		var createdAt sql.NullString
		var lastUsedAt sql.NullString
		var archivedAt sql.NullString

		if err := rows.Scan(
			&skill.ID,
			&skill.Name,
			&skill.Source,
			&path,
			&skill.Tier,
			&skill.SHA256,
			&bytes,
			&skill.Status,
			&createdAt,
			&lastUsedAt,
			&archivedAt,
		); err != nil {
			return nil, fmt.Errorf("scan skill: %w", err)
		}

		// Handle nullable fields
		if path.Valid {
			skill.Path = path.String
		}
		if bytes.Valid {
			skill.Bytes = bytes.Int64
		}
		if createdAt.Valid {
			skill.CreatedAt = parseTimeString(createdAt.String)
		}
		skill.LastUsedAt = parseNullableTimeString(lastUsedAt)
		skill.ArchivedAt = parseNullableTimeString(archivedAt)

		skills = append(skills, &skill)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate skills: %w", err)
	}

	return skills, nil
}

// LinkSkillToBead creates a link between a skill and a bead.
// Uses INSERT OR IGNORE to handle duplicate links gracefully.
// Also updates the skill's last_used_at timestamp.
func (s *SQLiteStorage) LinkSkillToBead(ctx context.Context, skillID, beadID string) error {
	if skillID == "" {
		return fmt.Errorf("link skill to bead: skillID is required")
	}
	if beadID == "" {
		return fmt.Errorf("link skill to bead: beadID is required")
	}

	return s.withTx(ctx, func(conn *sql.Conn) error {
		// Insert the link (ignore if already exists)
		_, err := conn.ExecContext(ctx, `
			INSERT OR IGNORE INTO skill_bead_links (skill_id, bead_id, linked_at)
			VALUES (?, ?, ?)
		`, skillID, beadID, time.Now())
		if err != nil {
			return fmt.Errorf("insert skill_bead_link: %w", err)
		}

		// Update skill's last_used_at timestamp
		_, err = conn.ExecContext(ctx, `
			UPDATE skills_manifest SET last_used_at = ? WHERE id = ?
		`, time.Now(), skillID)
		if err != nil {
			return fmt.Errorf("update skill last_used_at: %w", err)
		}

		return nil
	})
}

// UnlinkSkillFromBead removes the link between a skill and a bead.
// Returns no error if the link does not exist.
func (s *SQLiteStorage) UnlinkSkillFromBead(ctx context.Context, skillID, beadID string) error {
	if skillID == "" {
		return fmt.Errorf("unlink skill from bead: skillID is required")
	}
	if beadID == "" {
		return fmt.Errorf("unlink skill from bead: beadID is required")
	}

	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	_, err := s.db.ExecContext(ctx, `
		DELETE FROM skill_bead_links WHERE skill_id = ? AND bead_id = ?
	`, skillID, beadID)
	if err != nil {
		return fmt.Errorf("delete skill_bead_link: %w", err)
	}
	return nil
}

// GetBeadSkills returns all skills linked to a specific bead.
func (s *SQLiteStorage) GetBeadSkills(ctx context.Context, beadID string) ([]*Skill, error) {
	if beadID == "" {
		return nil, fmt.Errorf("get bead skills: beadID is required")
	}

	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT sm.id, sm.name, sm.source, sm.path, sm.tier, sm.sha256, sm.bytes, sm.status,
		       sm.created_at, sm.last_used_at, sm.archived_at
		FROM skills_manifest sm
		JOIN skill_bead_links sbl ON sm.id = sbl.skill_id
		WHERE sbl.bead_id = ?
		ORDER BY sm.name ASC
	`, beadID)
	if err != nil {
		return nil, fmt.Errorf("get bead skills: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanSkills(rows)
}

// GetSkillBeads returns all bead IDs that have the specified skill linked.
func (s *SQLiteStorage) GetSkillBeads(ctx context.Context, skillID string) ([]string, error) {
	if skillID == "" {
		return nil, fmt.Errorf("get skill beads: skillID is required")
	}

	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT bead_id FROM skill_bead_links WHERE skill_id = ? ORDER BY linked_at DESC
	`, skillID)
	if err != nil {
		return nil, fmt.Errorf("get skill beads: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var beadIDs []string
	for rows.Next() {
		var beadID string
		if err := rows.Scan(&beadID); err != nil {
			return nil, fmt.Errorf("scan bead_id: %w", err)
		}
		beadIDs = append(beadIDs, beadID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bead_ids: %w", err)
	}

	return beadIDs, nil
}

// GetUnusedSkills returns all skills that have no bead links.
// This is useful for identifying skills that may need cleanup or are candidates for deprecation.
func (s *SQLiteStorage) GetUnusedSkills(ctx context.Context) ([]*Skill, error) {
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT sm.id, sm.name, sm.source, sm.path, sm.tier, sm.sha256, sm.bytes, sm.status,
		       sm.created_at, sm.last_used_at, sm.archived_at
		FROM skills_manifest sm
		LEFT JOIN skill_bead_links sbl ON sm.id = sbl.skill_id
		WHERE sbl.skill_id IS NULL
		ORDER BY sm.name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("get unused skills: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanSkills(rows)
}

// DeleteSkill removes a skill from the skills_manifest table.
// Note: This will also cascade delete any skill_bead_links due to the foreign key constraint.
// Consider using UpdateSkillStatus to set status to "archived" instead of deleting.
func (s *SQLiteStorage) DeleteSkill(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("delete skill: id is required")
	}

	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	result, err := s.db.ExecContext(ctx, `DELETE FROM skills_manifest WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete skill %s: %w", id, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("delete skill %s: %w", id, ErrNotFound)
	}

	return nil
}

// UpdateSkillStatus updates a skill's status (active, deprecated, archived).
// When setting status to "archived", also sets the archived_at timestamp.
func (s *SQLiteStorage) UpdateSkillStatus(ctx context.Context, id, status string) error {
	if id == "" {
		return fmt.Errorf("update skill status: id is required")
	}

	// Validate status
	switch status {
	case "active", "deprecated", "archived":
		// Valid
	default:
		return fmt.Errorf("update skill status: invalid status %q (must be active, deprecated, or archived)", status)
	}

	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	var result sql.Result
	var err error

	if status == "archived" {
		// Also set archived_at when archiving
		result, err = s.db.ExecContext(ctx, `
			UPDATE skills_manifest SET status = ?, archived_at = ? WHERE id = ?
		`, status, time.Now(), id)
	} else {
		// Clear archived_at when un-archiving
		result, err = s.db.ExecContext(ctx, `
			UPDATE skills_manifest SET status = ?, archived_at = NULL WHERE id = ?
		`, status, id)
	}

	if err != nil {
		return fmt.Errorf("update skill status %s: %w", id, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("update skill status %s: %w", id, ErrNotFound)
	}

	return nil
}

// GetSkillUsageStats returns usage statistics for skills.
// Returns a map of skill_id -> count of linked beads.
func (s *SQLiteStorage) GetSkillUsageStats(ctx context.Context) (map[string]int, error) {
	s.reconnectMu.RLock()
	defer s.reconnectMu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT skill_id, COUNT(*) as bead_count
		FROM skill_bead_links
		GROUP BY skill_id
	`)
	if err != nil {
		return nil, fmt.Errorf("get skill usage stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	stats := make(map[string]int)
	for rows.Next() {
		var skillID string
		var count int
		if err := rows.Scan(&skillID, &count); err != nil {
			return nil, fmt.Errorf("scan skill usage: %w", err)
		}
		stats[skillID] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate skill usage: %w", err)
	}

	return stats, nil
}

// BulkUpsertSkills upserts multiple skills in a single transaction.
// This is more efficient than calling UpsertSkill repeatedly.
func (s *SQLiteStorage) BulkUpsertSkills(ctx context.Context, skills []*Skill) error {
	if len(skills) == 0 {
		return nil
	}

	return s.withTx(ctx, func(conn *sql.Conn) error {
		stmt, err := conn.PrepareContext(ctx, `
			INSERT INTO skills_manifest (
				id, name, source, path, tier, sha256, bytes, status, created_at, last_used_at, archived_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name = excluded.name,
				source = excluded.source,
				path = excluded.path,
				tier = excluded.tier,
				sha256 = excluded.sha256,
				bytes = excluded.bytes,
				status = excluded.status,
				last_used_at = excluded.last_used_at,
				archived_at = excluded.archived_at
		`)
		if err != nil {
			return fmt.Errorf("prepare bulk upsert: %w", err)
		}
		defer func() { _ = stmt.Close() }()

		for _, skill := range skills {
			if skill == nil {
				continue
			}
			if skill.ID == "" || skill.Name == "" || skill.Source == "" || skill.SHA256 == "" {
				continue // Skip invalid entries
			}

			// Apply defaults
			if skill.Tier == "" {
				skill.Tier = "optional"
			}
			if skill.Status == "" {
				skill.Status = "active"
			}

			if _, err := stmt.ExecContext(ctx,
				skill.ID,
				skill.Name,
				skill.Source,
				skill.Path,
				skill.Tier,
				skill.SHA256,
				skill.Bytes,
				skill.Status,
				skill.CreatedAt,
				skill.LastUsedAt,
				skill.ArchivedAt,
			); err != nil {
				return fmt.Errorf("upsert skill %s: %w", skill.ID, err)
			}
		}

		return nil
	})
}

// BulkLinkSkillsToBeads links multiple skills to a single bead in a transaction.
// This is efficient for setting all skills when creating or updating a bead.
func (s *SQLiteStorage) BulkLinkSkillsToBeads(ctx context.Context, beadID string, skillIDs []string) error {
	if beadID == "" {
		return fmt.Errorf("bulk link skills: beadID is required")
	}
	if len(skillIDs) == 0 {
		return nil
	}

	return s.withTx(ctx, func(conn *sql.Conn) error {
		now := time.Now()

		stmt, err := conn.PrepareContext(ctx, `
			INSERT OR IGNORE INTO skill_bead_links (skill_id, bead_id, linked_at)
			VALUES (?, ?, ?)
		`)
		if err != nil {
			return fmt.Errorf("prepare bulk link: %w", err)
		}
		defer func() { _ = stmt.Close() }()

		for _, skillID := range skillIDs {
			if skillID == "" {
				continue
			}
			if _, err := stmt.ExecContext(ctx, skillID, beadID, now); err != nil {
				return fmt.Errorf("link skill %s to bead %s: %w", skillID, beadID, err)
			}
		}

		// Update last_used_at for all linked skills
		if len(skillIDs) > 0 {
			placeholders := strings.Repeat("?,", len(skillIDs))
			placeholders = strings.TrimSuffix(placeholders, ",")
			args := make([]interface{}, 0, len(skillIDs)+1)
			args = append(args, now)
			for _, id := range skillIDs {
				args = append(args, id)
			}
			query := fmt.Sprintf(`UPDATE skills_manifest SET last_used_at = ? WHERE id IN (%s)`, placeholders) // #nosec G201
			if _, err := conn.ExecContext(ctx, query, args...); err != nil {
				return fmt.Errorf("update skills last_used_at: %w", err)
			}
		}

		return nil
	})
}
