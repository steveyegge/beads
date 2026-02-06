package migrations

import (
	"database/sql"
	"fmt"
)

// MigratePodFields adds K8s pod fields to the issues table.
// These fields support the agent pod controller pattern:
//   - pod_name: K8s pod name running this agent
//   - pod_ip: Pod IP address
//   - pod_node: K8s node the pod is scheduled on
//   - pod_status: Pod status (pending|running|terminating|terminated)
//   - screen_session: Screen/tmux session name inside the pod
func MigratePodFields(db *sql.DB) error {
	columns := []struct {
		name    string
		sqlType string
	}{
		{"pod_name", "TEXT DEFAULT ''"},
		{"pod_ip", "TEXT DEFAULT ''"},
		{"pod_node", "TEXT DEFAULT ''"},
		{"pod_status", "TEXT DEFAULT ''"},
		{"screen_session", "TEXT DEFAULT ''"},
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
