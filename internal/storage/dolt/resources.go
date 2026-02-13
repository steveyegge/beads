//go:build cgo

package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// GetResources retrieves resources matching the given filter
func (s *DoltStore) GetResources(ctx context.Context, filter types.ResourceFilter) ([]*types.Resource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT r.id, r.type_id, r.name, r.identifier, r.source, r.external_id, 
		       r.config_json, r.is_active, r.created_at, r.updated_at,
		       GROUP_CONCAT(rt.tag) as tags
		FROM resources r
		JOIN resource_types rt_type ON r.type_id = rt_type.id
		LEFT JOIN resource_tags rt ON r.id = rt.resource_id
		WHERE r.is_active = TRUE
	`
	args := []interface{}{}

	if filter.Type != nil {
		query += " AND rt_type.name = ?"
		args = append(args, *filter.Type)
	}

	if filter.Source != nil {
		query += " AND r.source = ?"
		args = append(args, *filter.Source)
	}

	if len(filter.Tags) > 0 {
		for _, tag := range filter.Tags {
			query += ` AND EXISTS (
				SELECT 1 FROM resource_tags 
				WHERE resource_id = r.id AND tag = ?
			)`
			args = append(args, tag)
		}
	}

	query += " GROUP BY r.id ORDER BY r.identifier"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query resources: %w", err)
	}
	defer rows.Close()

	var resources []*types.Resource
	for rows.Next() {
		resource, err := scanResourceWithTags(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan resource: %w", err)
		}
		resources = append(resources, resource)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating resources: %w", err)
	}

	return resources, nil
}

// GetResourceByID retrieves a resource by its database ID
func (s *DoltStore) GetResourceByID(ctx context.Context, id int64) (*types.Resource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT r.id, r.type_id, r.name, r.identifier, r.source, r.external_id, 
		       r.config_json, r.is_active, r.created_at, r.updated_at
		FROM resources r
		WHERE r.id = ?
	`

	row := s.db.QueryRowContext(ctx, query, id)
	resource, err := scanResource(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get resource by ID: %w", err)
	}

	// Fetch tags
	tags, err := s.GetResourceTags(ctx, resource.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tags for resource %d: %w", resource.ID, err)
	}
	resource.Tags = tags

	return resource, nil
}

// GetResourceByIdentifier retrieves a resource by its unique identifier
func (s *DoltStore) GetResourceByIdentifier(ctx context.Context, identifier string) (*types.Resource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT r.id, r.type_id, r.name, r.identifier, r.source, r.external_id, 
		       r.config_json, r.is_active, r.created_at, r.updated_at
		FROM resources r
		WHERE r.identifier = ?
	`

	row := s.db.QueryRowContext(ctx, query, identifier)
	resource, err := scanResource(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get resource by identifier: %w", err)
	}

	// Fetch tags
	tags, err := s.GetResourceTags(ctx, resource.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tags for resource %d: %w", resource.ID, err)
	}
	resource.Tags = tags

	return resource, nil
}

// CreateResource creates a new resource
func (s *DoltStore) CreateResource(ctx context.Context, resource *types.Resource) error {
	// Get type_id from resource type
	var typeID int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM resource_types WHERE name = ?", resource.Type).Scan(&typeID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("invalid resource type: %s", resource.Type)
	}
	if err != nil {
		return fmt.Errorf("failed to get resource type ID: %w", err)
	}

	now := time.Now().UTC()
	if resource.CreatedAt.IsZero() {
		resource.CreatedAt = now
	}
	if resource.UpdatedAt.IsZero() {
		resource.UpdatedAt = now
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Insert resource
	result, err := tx.ExecContext(ctx, `
		INSERT INTO resources (type_id, name, identifier, source, external_id, config_json, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, typeID, resource.Name, resource.Identifier, resource.Source, nullString(resource.ExternalID), nullString(resource.Config), resource.IsActive, resource.CreatedAt, resource.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert resource: %w", err)
	}

	resourceID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}
	resource.ID = resourceID

	// Insert tags if any
	for _, tag := range resource.Tags {
		if err := insertResourceTag(ctx, tx, resourceID, tag); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// UpdateResource updates an existing resource
func (s *DoltStore) UpdateResource(ctx context.Context, id int64, updates map[string]interface{}) error {
	// Build update query
	setClauses := []string{"updated_at = ?"}
	args := []interface{}{time.Now().UTC()}

	allowedFields := map[string]bool{
		"name":        true,
		"source":      true,
		"external_id": true,
		"config_json": true,
		"is_active":   true,
	}

	for key, value := range updates {
		if !allowedFields[key] {
			return fmt.Errorf("invalid field for update: %s", key)
		}

		setClauses = append(setClauses, fmt.Sprintf("`%s` = ?", key))
		args = append(args, value)
	}

	args = append(args, id)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// nolint:gosec // G201: setClauses contains only column names (e.g. "name = ?"), actual values passed via args
	query := fmt.Sprintf("UPDATE resources SET %s WHERE id = ?", strings.Join(setClauses, ", "))
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update resource: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("resource not found: %d", id)
	}

	return tx.Commit()
}

// DeleteResourceByID soft-deletes a resource by setting is_active to false
func (s *DoltStore) DeleteResourceByID(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, "UPDATE resources SET is_active = FALSE, updated_at = ? WHERE id = ?", time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("failed to delete resource: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("resource not found: %d", id)
	}

	return nil
}

// AddResourceTag adds a tag to a resource
func (s *DoltStore) AddResourceTag(ctx context.Context, resourceID int64, tag string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := insertResourceTag(ctx, tx, resourceID, tag); err != nil {
		return err
	}

	return tx.Commit()
}

// RemoveResourceTag removes a tag from a resource
func (s *DoltStore) RemoveResourceTag(ctx context.Context, resourceID int64, tag string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM resource_tags WHERE resource_id = ? AND tag = ?", resourceID, tag)
	if err != nil {
		return fmt.Errorf("failed to remove resource tag: %w", err)
	}
	return nil
}

// GetResourceTags retrieves all tags for a resource
func (s *DoltStore) GetResourceTags(ctx context.Context, resourceID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT tag FROM resource_tags WHERE resource_id = ? ORDER BY tag", resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query resource tags: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}
		tags = append(tags, tag)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tags: %w", err)
	}

	return tags, nil
}

// =============================================================================
// Helper functions
// =============================================================================

// scanResource scans a resource from a database row
func scanResource(scanner interface {
	Scan(dest ...interface{}) error
}) (*types.Resource, error) {
	var resource types.Resource
	var typeID int64
	var externalID, configJSON sql.NullString
	var createdAtStr, updatedAtStr string

	err := scanner.Scan(
		&resource.ID,
		&typeID,
		&resource.Name,
		&resource.Identifier,
		&resource.Source,
		&externalID,
		&configJSON,
		&resource.IsActive,
		&createdAtStr,
		&updatedAtStr,
	)
	if err != nil {
		return nil, err
	}

	// Map type_id to type name
	switch typeID {
	case 1:
		resource.Type = types.ResourceTypeModel
	case 2:
		resource.Type = types.ResourceTypeAgent
	case 3:
		resource.Type = types.ResourceTypeSkill
	default:
		return nil, fmt.Errorf("unknown resource type_id: %d", typeID)
	}

	if externalID.Valid {
		resource.ExternalID = externalID.String
	}
	if configJSON.Valid {
		resource.Config = configJSON.String
	}

	// Parse timestamps
	resource.CreatedAt = parseResourceTimeString(createdAtStr)
	resource.UpdatedAt = parseResourceTimeString(updatedAtStr)

	return &resource, nil
}

func scanResourceWithTags(rows *sql.Rows) (*types.Resource, error) {
	var resource types.Resource
	var typeID int64
	var externalID, configJSON, tagsStr sql.NullString
	var createdAtStr, updatedAtStr string

	err := rows.Scan(
		&resource.ID,
		&typeID,
		&resource.Name,
		&resource.Identifier,
		&resource.Source,
		&externalID,
		&configJSON,
		&resource.IsActive,
		&createdAtStr,
		&updatedAtStr,
		&tagsStr,
	)
	if err != nil {
		return nil, err
	}

	switch typeID {
	case 1:
		resource.Type = types.ResourceTypeModel
	case 2:
		resource.Type = types.ResourceTypeAgent
	case 3:
		resource.Type = types.ResourceTypeSkill
	default:
		return nil, fmt.Errorf("unknown resource type_id: %d", typeID)
	}

	if externalID.Valid {
		resource.ExternalID = externalID.String
	}
	if configJSON.Valid {
		resource.Config = configJSON.String
	}

	resource.CreatedAt = parseResourceTimeString(createdAtStr)
	resource.UpdatedAt = parseResourceTimeString(updatedAtStr)

	if tagsStr.Valid && tagsStr.String != "" {
		resource.Tags = strings.Split(tagsStr.String, ",")
	}

	return &resource, nil
}

// insertResourceTag inserts a tag for a resource (idempotent)
func insertResourceTag(ctx context.Context, tx *sql.Tx, resourceID int64, tag string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO resource_tags (resource_id, tag)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE resource_id = resource_id
	`, resourceID, tag)
	if err != nil {
		return fmt.Errorf("failed to insert resource tag: %w", err)
	}
	return nil
}

// parseResourceTimeString parses a time string in ISO8601 format
func parseResourceTimeString(s string) time.Time {
	if s == "" {
		return time.Time{}
	}

	// Try different formats
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t
		}
	}

	return time.Time{}
}

// =============================================================================
// Storage Interface Implementation
// =============================================================================

// ListResources implements the Storage interface
func (s *DoltStore) ListResources(ctx context.Context, filter types.ResourceFilter) ([]*types.Resource, error) {
	return s.GetResources(ctx, filter)
}

// GetResource implements the Storage interface
func (s *DoltStore) GetResource(ctx context.Context, identifier string) (*types.Resource, error) {
	return s.GetResourceByIdentifier(ctx, identifier)
}

// SaveResource implements the Storage interface
func (s *DoltStore) SaveResource(ctx context.Context, resource *types.Resource) error {
	// Check if resource exists
	existing, err := s.GetResourceByIdentifier(ctx, resource.Identifier)
	if err != nil {
		return fmt.Errorf("failed to check existing resource: %w", err)
	}

	if existing == nil {
		// Create new resource
		return s.CreateResource(ctx, resource)
	}

	updates := make(map[string]interface{})
	if resource.Name != "" {
		updates["name"] = resource.Name
	}
	if resource.Source != "" {
		updates["source"] = resource.Source
	}
	if resource.ExternalID != "" {
		updates["external_id"] = resource.ExternalID
	}
	if resource.Config != "" {
		updates["config_json"] = resource.Config
	}
	updates["is_active"] = resource.IsActive

	if len(updates) > 0 {
		if err := s.UpdateResource(ctx, existing.ID, updates); err != nil {
			return fmt.Errorf("failed to update resource: %w", err)
		}
	}

	existingTags, err := s.GetResourceTags(ctx, existing.ID)
	if err != nil {
		return fmt.Errorf("failed to get existing tags: %w", err)
	}

	existingTagSet := make(map[string]bool)
	for _, tag := range existingTags {
		existingTagSet[tag] = true
	}

	newTagSet := make(map[string]bool)
	for _, tag := range resource.Tags {
		newTagSet[tag] = true
	}

	for _, tag := range resource.Tags {
		if !existingTagSet[tag] {
			if err := s.AddResourceTag(ctx, existing.ID, tag); err != nil {
				return fmt.Errorf("failed to add tag: %w", err)
			}
		}
	}

	for _, tag := range existingTags {
		if !newTagSet[tag] {
			if err := s.RemoveResourceTag(ctx, existing.ID, tag); err != nil {
				return fmt.Errorf("failed to remove tag: %w", err)
			}
		}
	}

	resource.ID = existing.ID
	return nil
}

// DeleteResource implements the Storage interface
func (s *DoltStore) DeleteResource(ctx context.Context, identifier string) error {
	resource, err := s.GetResourceByIdentifier(ctx, identifier)
	if err != nil {
		return fmt.Errorf("failed to get resource: %w", err)
	}
	if resource == nil {
		return fmt.Errorf("resource not found: %s", identifier)
	}

	return s.DeleteResourceByID(ctx, resource.ID)
}

// SyncResources implements bulk upsert for resources from a given source
func (s *DoltStore) SyncResources(ctx context.Context, source string, resources []*types.Resource) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	typeIDMap := make(map[string]int64)
	rows, err := tx.QueryContext(ctx, "SELECT id, name FROM resource_types")
	if err != nil {
		return fmt.Errorf("failed to query resource types: %w", err)
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan resource type: %w", err)
		}
		typeIDMap[name] = id
	}
	rows.Close()

	seenIdentifiers := make(map[string]bool)

	for _, resource := range resources {
		seenIdentifiers[resource.Identifier] = true

		typeID, ok := typeIDMap[resource.Type]
		if !ok {
			return fmt.Errorf("unknown resource type: %s", resource.Type)
		}

		var existingID int64
		err := tx.QueryRowContext(ctx, "SELECT id FROM resources WHERE identifier = ?", resource.Identifier).Scan(&existingID)
		if err == sql.ErrNoRows {
			now := time.Now().UTC()
			if resource.CreatedAt.IsZero() {
				resource.CreatedAt = now
			}
			if resource.UpdatedAt.IsZero() {
				resource.UpdatedAt = now
			}

			result, err := tx.ExecContext(ctx, `
				INSERT INTO resources (type_id, name, identifier, source, external_id, config_json, is_active, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			`, typeID, resource.Name, resource.Identifier, resource.Source, nullString(resource.ExternalID), nullString(resource.Config), resource.IsActive, resource.CreatedAt, resource.UpdatedAt)
			if err != nil {
				return fmt.Errorf("failed to insert resource %s: %w", resource.Identifier, err)
			}
			existingID, err = result.LastInsertId()
			if err != nil {
				return fmt.Errorf("failed to get last insert ID: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to check existing resource: %w", err)
		} else {
			now := time.Now().UTC()
			_, err := tx.ExecContext(ctx, `
				UPDATE resources SET name = ?, source = ?, external_id = ?, config_json = ?, is_active = ?, updated_at = ?
				WHERE id = ?
			`, resource.Name, resource.Source, nullString(resource.ExternalID), nullString(resource.Config), resource.IsActive, now, existingID)
			if err != nil {
				return fmt.Errorf("failed to update resource %s: %w", resource.Identifier, err)
			}
		}

		existingTags := make(map[string]bool)
		tagRows, err := tx.QueryContext(ctx, "SELECT tag FROM resource_tags WHERE resource_id = ?", existingID)
		if err != nil {
			return fmt.Errorf("failed to query existing tags: %w", err)
		}
		for tagRows.Next() {
			var tag string
			if err := tagRows.Scan(&tag); err != nil {
				tagRows.Close()
				return fmt.Errorf("failed to scan tag: %w", err)
			}
			existingTags[tag] = true
		}
		tagRows.Close()

		for _, tag := range resource.Tags {
			if !existingTags[tag] {
				_, err := tx.ExecContext(ctx, `
					INSERT INTO resource_tags (resource_id, tag)
					VALUES (?, ?)
					ON DUPLICATE KEY UPDATE resource_id = resource_id
				`, existingID, tag)
				if err != nil {
					return fmt.Errorf("failed to insert tag %s: %w", tag, err)
				}
			}
		}

		newTagSet := make(map[string]bool)
		for _, tag := range resource.Tags {
			newTagSet[tag] = true
		}
		for tag := range existingTags {
			if !newTagSet[tag] {
				_, err := tx.ExecContext(ctx, "DELETE FROM resource_tags WHERE resource_id = ? AND tag = ?", existingID, tag)
				if err != nil {
					return fmt.Errorf("failed to remove tag %s: %w", tag, err)
				}
			}
		}
	}

	depRows, err := tx.QueryContext(ctx, `
		SELECT identifier FROM resources WHERE source = ? AND is_active = TRUE
	`, source)
	if err != nil {
		return fmt.Errorf("failed to query existing resources: %w", err)
	}
	defer depRows.Close()

	var toDeactivate []string
	for depRows.Next() {
		var identifier string
		if err := depRows.Scan(&identifier); err != nil {
			return fmt.Errorf("failed to scan identifier: %w", err)
		}
		if !seenIdentifiers[identifier] {
			toDeactivate = append(toDeactivate, identifier)
		}
	}
	depRows.Close()

	for _, identifier := range toDeactivate {
		if _, err := tx.ExecContext(ctx, `
			UPDATE resources SET is_active = FALSE, updated_at = ? WHERE identifier = ?
		`, time.Now().UTC(), identifier); err != nil {
			return fmt.Errorf("failed to deactivate resource %s: %w", identifier, err)
		}
	}

	return tx.Commit()
}
