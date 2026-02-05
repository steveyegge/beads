package dolt

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/types"
)

// SetConfig sets a configuration value
func (s *DoltStore) SetConfig(ctx context.Context, key, value string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO config (`+"`key`"+`, value) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE value = VALUES(value)
	`, key, value)
	if err != nil {
		return fmt.Errorf("failed to set config %s: %w", key, err)
	}
	return tx.Commit()
}

// GetConfig retrieves a configuration value
func (s *DoltStore) GetConfig(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM config WHERE `key` = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get config %s: %w", key, err)
	}
	return value, nil
}

// GetAllConfig retrieves all configuration values
func (s *DoltStore) GetAllConfig(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT `key`, value FROM config")
	if err != nil {
		return nil, fmt.Errorf("failed to get all config: %w", err)
	}
	defer rows.Close()

	config := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan config: %w", err)
		}
		config[key] = value
	}
	return config, rows.Err()
}

// DeleteConfig removes a configuration value
func (s *DoltStore) DeleteConfig(ctx context.Context, key string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, "DELETE FROM config WHERE `key` = ?", key)
	if err != nil {
		return fmt.Errorf("failed to delete config %s: %w", key, err)
	}
	return tx.Commit()
}

// SetMetadata sets a metadata value
func (s *DoltStore) SetMetadata(ctx context.Context, key, value string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO metadata (`+"`key`"+`, value) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE value = VALUES(value)
	`, key, value)
	if err != nil {
		return fmt.Errorf("failed to set metadata %s: %w", key, err)
	}
	return tx.Commit()
}

// GetMetadata retrieves a metadata value
func (s *DoltStore) GetMetadata(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM metadata WHERE `key` = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get metadata %s: %w", key, err)
	}
	return value, nil
}

// GetCustomStatuses returns custom status values from config.
// If the database doesn't have custom statuses configured, falls back to config.yaml.
// Returns an empty slice if no custom statuses are configured.
func (s *DoltStore) GetCustomStatuses(ctx context.Context) ([]string, error) {
	value, err := s.GetConfig(ctx, "status.custom")
	if err != nil {
		// On database error, try fallback to config.yaml
		if yamlStatuses := config.GetCustomStatusesFromYAML(); len(yamlStatuses) > 0 {
			return yamlStatuses, nil
		}
		return nil, err
	}
	if value != "" {
		return parseCommaSeparatedList(value), nil
	}

	// Fallback to config.yaml when database doesn't have status.custom set.
	if yamlStatuses := config.GetCustomStatusesFromYAML(); len(yamlStatuses) > 0 {
		return yamlStatuses, nil
	}

	return nil, nil
}

// GetCustomTypes returns custom issue type values from config.
// If the database doesn't have custom types configured, falls back to config.yaml.
// This fallback is essential during operations when the database connection is
// temporarily unavailable or when types.custom hasn't been configured yet.
// Returns an empty slice if no custom types are configured.
func (s *DoltStore) GetCustomTypes(ctx context.Context) ([]string, error) {
	value, err := s.GetConfig(ctx, "types.custom")
	if err != nil {
		// On database error, try fallback to config.yaml
		if yamlTypes := config.GetCustomTypesFromYAML(); len(yamlTypes) > 0 {
			return yamlTypes, nil
		}
		return nil, err
	}
	if value != "" {
		return parseCommaSeparatedList(value), nil
	}

	// Fallback to config.yaml when database doesn't have types.custom set.
	// This allows operations to work with custom types defined in config.yaml
	// before they're persisted to the database.
	if yamlTypes := config.GetCustomTypesFromYAML(); len(yamlTypes) > 0 {
		return yamlTypes, nil
	}

	return nil, nil
}

// GetTypeSchema retrieves the type schema for the given type name.
// Returns nil (not an error) if no schema is defined for the type.
func (s *DoltStore) GetTypeSchema(ctx context.Context, typeName string) (*types.TypeSchema, error) {
	key := types.TypeSchemaConfigPrefix + typeName
	value, err := s.GetConfig(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get type schema %s: %w", typeName, err)
	}
	if value == "" {
		return nil, nil
	}
	var schema types.TypeSchema
	if err := json.Unmarshal([]byte(value), &schema); err != nil {
		return nil, fmt.Errorf("parse type schema %s: %w", typeName, err)
	}
	return &schema, nil
}

// SetTypeSchema stores the type schema for the given type name.
func (s *DoltStore) SetTypeSchema(ctx context.Context, typeName string, schema *types.TypeSchema) error {
	key := types.TypeSchemaConfigPrefix + typeName
	data, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("serialize type schema %s: %w", typeName, err)
	}
	return s.SetConfig(ctx, key, string(data))
}

// parseCommaSeparatedList splits a comma-separated string into a slice of trimmed entries.
// Empty entries are filtered out.
func parseCommaSeparatedList(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
