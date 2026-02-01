package migrations

import (
	"database/sql"
	"fmt"
)

// MigrateAdviceSubscriptionFields adds advice subscription columns to the issues table (gt-w2mh8a.4).
// These fields allow agents to customize which advice they receive beyond the auto-subscribed
// context labels (global, rig:X, role:Y, agent:Z).
//
// New columns:
//   - advice_subscriptions: JSON array of additional labels to subscribe to
//   - advice_subscriptions_exclude: JSON array of labels to exclude from receiving advice
func MigrateAdviceSubscriptionFields(db *sql.DB) error {
	columns := []struct {
		name    string
		sqlType string
	}{
		{"advice_subscriptions", "TEXT DEFAULT ''"},
		{"advice_subscriptions_exclude", "TEXT DEFAULT ''"},
	}

	for _, col := range columns {
		// Check if column already exists
		var columnExists bool
		err := db.QueryRow(`
			SELECT COUNT(*) > 0
			FROM pragma_table_info('issues')
			WHERE name = ?
		`, col.name).Scan(&columnExists)
		if err != nil {
			return fmt.Errorf("failed to check %s column: %w", col.name, err)
		}

		if columnExists {
			continue
		}

		// Add the column
		_, err = db.Exec(fmt.Sprintf(`ALTER TABLE issues ADD COLUMN %s %s`, col.name, col.sqlType))
		if err != nil {
			return fmt.Errorf("failed to add %s column: %w", col.name, err)
		}
	}

	return nil
}
