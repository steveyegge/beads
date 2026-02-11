package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateResourceTables adds tables for centralized resource management.
// This supports tracking agents, skills, and models from various sources (local, linear, etc).
func MigrateResourceTables(db *sql.DB) error {
	// Check if resource_types table already exists
	var tableExists bool
	err := db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM information_schema.tables
		WHERE table_schema = database() AND table_name = 'resource_types'
	`).Scan(&tableExists)
	if err != nil {
		return fmt.Errorf("failed to check resource_types table: %w", err)
	}

	if tableExists {
		return nil
	}

	// Create resource_types table
	// Note: Dolt doesn't support INTEGER PRIMARY KEY AUTOINCREMENT syntax directly for PK
	// Use INT PRIMARY KEY AUTO_INCREMENT
	_, err = db.Exec(`
		CREATE TABLE resource_types (
			id INT PRIMARY KEY,
			name VARCHAR(255) NOT NULL UNIQUE -- 'model', 'agent', 'skill'
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create resource_types table: %w", err)
	}

	// Populate initial resource types
	_, err = db.Exec(`
		INSERT INTO resource_types (id, name) VALUES
		(1, 'model'),
		(2, 'agent'),
		(3, 'skill');
	`)
	if err != nil {
		return fmt.Errorf("failed to populate resource_types: %w", err)
	}

	// Create resources table
	_, err = db.Exec(`
		CREATE TABLE resources (
			id INT PRIMARY KEY AUTO_INCREMENT,
			type_id INT,
			name VARCHAR(255) NOT NULL,          -- Display Name
			identifier VARCHAR(255) NOT NULL UNIQUE, -- System ID
			source VARCHAR(255) NOT NULL,        -- 'local', 'linear', 'jira', 'config'
			external_id VARCHAR(255),            -- ID in the external system (e.g., Jira Component ID)
			config_json TEXT,
			is_active BOOLEAN DEFAULT TRUE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (type_id) REFERENCES resource_types(id)
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create resources table: %w", err)
	}

	// Create resource_tags table for capability-based tagging
	_, err = db.Exec(`
		CREATE TABLE resource_tags (
			resource_id INT NOT NULL,
			tag VARCHAR(255) NOT NULL,
			PRIMARY KEY (resource_id, tag),
			FOREIGN KEY (resource_id) REFERENCES resources(id) ON DELETE CASCADE
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create resource_tags table: %w", err)
	}

	// Add indexes
	indexes := []string{
		"CREATE INDEX idx_resources_type ON resources(type_id)",
		"CREATE INDEX idx_resources_identifier ON resources(identifier)",
		"CREATE INDEX idx_resource_tags_tag ON resource_tags(tag)",
	}
	for _, idx := range indexes {
		_, err = db.Exec(idx)
		if err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}
