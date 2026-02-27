package migrations

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
)

// infraTypes are issue types that belong in the wisps table, not the versioned issues table.
var infraTypes = []string{"agent", "rig", "role", "message"}

// MigrateInfraToWisps moves infrastructure beads (agent, rig, role, message)
// from the versioned issues table to the dolt-ignored wisps table.
// This keeps the issues table clean for real work items and prevents infra
// beads from bloating dolt history and stats.
//
// Idempotent: skips if no matching rows exist in issues table.
func MigrateInfraToWisps(db *sql.DB) error {
	// Check if wisps table exists (migration 004 must have run first)
	exists, err := tableExists(db, "wisps")
	if err != nil {
		return fmt.Errorf("checking wisps table: %w", err)
	}
	if !exists {
		// wisps table doesn't exist yet — nothing to migrate
		return nil
	}

	// Check if any infra beads exist in the issues table
	placeholders := make([]string, len(infraTypes))
	args := make([]interface{}, len(infraTypes))
	for i, t := range infraTypes {
		placeholders[i] = "?"
		args[i] = t
	}
	inClause := strings.Join(placeholders, ",")

	var count int
	//nolint:gosec // G201: placeholders contains only ? markers
	err = db.QueryRow(
		fmt.Sprintf("SELECT COUNT(*) FROM issues WHERE issue_type IN (%s)", inClause),
		args...,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("counting infra beads: %w", err)
	}
	if count == 0 {
		return nil // Nothing to migrate
	}

	log.Printf("migration 007: moving %d infra beads to wisps table", count)

	// Copy infra issues to wisps table.
	// Query wisps column names at runtime to handle schema drift between
	// issues and wisps tables (issues may have columns wisps doesn't).
	wispCols, err := getColumnNames(db, "wisps")
	if err != nil {
		return fmt.Errorf("getting wisps columns: %w", err)
	}
	colList := strings.Join(wispCols, ", ")

	// Use INSERT IGNORE to skip any that already exist in wisps (idempotent).
	//nolint:gosec // G201: inClause built from ? placeholders, colList from DB metadata
	_, err = db.Exec(fmt.Sprintf(`
		INSERT IGNORE INTO wisps (%s)
		SELECT %s FROM issues WHERE issue_type IN (%s)
	`, colList, colList, inClause), args...)
	if err != nil {
		return fmt.Errorf("copying infra issues to wisps: %w", err)
	}

	// Mark as ephemeral in wisps table (issues table may have ephemeral=0)
	//nolint:gosec // G201: inClause built from ? placeholders
	_, err = db.Exec(fmt.Sprintf(`
		UPDATE wisps SET ephemeral = 1 WHERE issue_type IN (%s)
	`, inClause), args...)
	if err != nil {
		return fmt.Errorf("setting ephemeral flag on migrated infra beads: %w", err)
	}

	// Copy labels
	//nolint:gosec // G201: inClause built from ? placeholders
	_, err = db.Exec(fmt.Sprintf(`
		INSERT IGNORE INTO wisp_labels
		SELECT l.* FROM labels l
		INNER JOIN issues i ON l.issue_id = i.id
		WHERE i.issue_type IN (%s)
	`, inClause), args...)
	if err != nil {
		return fmt.Errorf("copying infra labels to wisp_labels: %w", err)
	}

	// Copy dependencies
	//nolint:gosec // G201: inClause built from ? placeholders
	_, err = db.Exec(fmt.Sprintf(`
		INSERT IGNORE INTO wisp_dependencies
		SELECT d.* FROM dependencies d
		INNER JOIN issues i ON d.issue_id = i.id
		WHERE i.issue_type IN (%s)
	`, inClause), args...)
	if err != nil {
		return fmt.Errorf("copying infra dependencies to wisp_dependencies: %w", err)
	}

	// Copy events
	//nolint:gosec // G201: inClause built from ? placeholders
	_, err = db.Exec(fmt.Sprintf(`
		INSERT IGNORE INTO wisp_events
		SELECT e.* FROM events e
		INNER JOIN issues i ON e.issue_id = i.id
		WHERE i.issue_type IN (%s)
	`, inClause), args...)
	if err != nil {
		return fmt.Errorf("copying infra events to wisp_events: %w", err)
	}

	// Copy comments
	//nolint:gosec // G201: inClause built from ? placeholders
	_, err = db.Exec(fmt.Sprintf(`
		INSERT IGNORE INTO wisp_comments
		SELECT c.* FROM comments c
		INNER JOIN issues i ON c.issue_id = i.id
		WHERE i.issue_type IN (%s)
	`, inClause), args...)
	if err != nil {
		return fmt.Errorf("copying infra comments to wisp_comments: %w", err)
	}

	// Now delete originals from versioned tables (order: children first, then issues)
	for _, table := range []string{"comments", "events", "dependencies", "labels"} {
		//nolint:gosec // G201: table from hardcoded list, inClause from ? placeholders
		_, err = db.Exec(fmt.Sprintf(`
			DELETE FROM %s WHERE issue_id IN (
				SELECT id FROM issues WHERE issue_type IN (%s)
			)
		`, table, inClause), args...)
		if err != nil {
			return fmt.Errorf("deleting infra rows from %s: %w", table, err)
		}
	}

	// Delete infra issues themselves
	//nolint:gosec // G201: inClause built from ? placeholders
	_, err = db.Exec(fmt.Sprintf(`
		DELETE FROM issues WHERE issue_type IN (%s)
	`, inClause), args...)
	if err != nil {
		return fmt.Errorf("deleting infra issues: %w", err)
	}

	log.Printf("migration 007: migrated %d infra beads to wisps table", count)
	return nil
}
