package schema

import (
	"context"
	"fmt"
)

func verifySplitDependencyMigration(ctx context.Context, db DBConn) error {
	checks := []struct {
		legacyTable  string
		legacyFilter string
		newTable     string
	}{
		{"dependencies", "depends_on_issue_id IS NOT NULL", "issue_issue_dependencies"},
		{"dependencies", "depends_on_wisp_id IS NOT NULL", "issue_wisp_dependencies"},
		{"dependencies", "depends_on_external IS NOT NULL", "issue_external_dependencies"},
		{"wisp_dependencies", "depends_on_issue_id IS NOT NULL", "wisp_issue_dependencies"},
		{"wisp_dependencies", "depends_on_wisp_id IS NOT NULL", "wisp_wisp_dependencies"},
		{"wisp_dependencies", "depends_on_external IS NOT NULL", "wisp_external_dependencies"},
	}

	for _, c := range checks {
		legacyExists, err := tableExists(ctx, db, c.legacyTable)
		if err != nil {
			return fmt.Errorf("checking %s existence: %w", c.legacyTable, err)
		}
		if !legacyExists {
			continue
		}
		newExists, err := tableExists(ctx, db, c.newTable)
		if err != nil {
			return fmt.Errorf("checking %s existence: %w", c.newTable, err)
		}
		if !newExists {
			return fmt.Errorf("split-dependency verification: %s missing after migration 0050/ignored-0009", c.newTable)
		}

		var legacyCount, newCount int64
		if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", c.legacyTable, c.legacyFilter)).Scan(&legacyCount); err != nil {
			return fmt.Errorf("counting legacy %s rows: %w", c.legacyTable, err)
		}
		if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", c.newTable)).Scan(&newCount); err != nil {
			return fmt.Errorf("counting %s rows: %w", c.newTable, err)
		}
		if legacyCount != newCount {
			return fmt.Errorf("split-dependency verification: %s has %d rows but %s (%s) has %d", c.newTable, newCount, c.legacyTable, c.legacyFilter, legacyCount)
		}
	}
	return nil
}

func tableExists(ctx context.Context, db DBConn, name string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?",
		name).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
