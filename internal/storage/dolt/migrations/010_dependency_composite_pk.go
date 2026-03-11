package migrations

import (
	"database/sql"
	"fmt"
	"strings"
)

// MigrateDependencyCompositePK changes the dependencies and wisp_dependencies
// primary keys from (issue_id, depends_on_id) to (issue_id, depends_on_id, type).
// This allows multiple dependency types between the same issue pair.
func MigrateDependencyCompositePK(db *sql.DB) error {
	for _, table := range []string{"dependencies", "wisp_dependencies"} {
		exists, err := tableExists(db, table)
		if err != nil {
			return fmt.Errorf("checking table %s: %w", table, err)
		}
		if !exists {
			continue
		}

		// Check if type is already part of the PK
		if pkIncludesType(db, table) {
			continue
		}

		// Try primary path: ALTER TABLE DROP/ADD PK
		err = alterPKDirect(db, table)
		if err != nil {
			// Fallback: create-insert-rename
			if err2 := alterPKViaRename(db, table); err2 != nil {
				return fmt.Errorf("failed to alter PK on %s (direct: %v, rename: %v)", table, err, err2)
			}
		}
	}
	return nil
}

// pkIncludesType checks if the primary key already includes the type column.
func pkIncludesType(db *sql.DB, table string) bool {
	rows, err := db.Query("SHOW INDEX FROM `" + table + "` WHERE Key_name = 'PRIMARY' AND Column_name = 'type'") //nolint:gosec // G202: table name from hardcoded list
	if err != nil {
		return false
	}
	defer rows.Close()
	return rows.Next()
}

func alterPKDirect(db *sql.DB, table string) error {
	//nolint:gosec // G202: table name from hardcoded list
	if _, err := db.Exec("ALTER TABLE `" + table + "` DROP PRIMARY KEY"); err != nil {
		return err
	}
	//nolint:gosec // G202
	if _, err := db.Exec("ALTER TABLE `" + table + "` ADD PRIMARY KEY (issue_id, depends_on_id, type)"); err != nil {
		return err
	}
	return nil
}

func alterPKViaRename(db *sql.DB, table string) error {
	newTable := table + "_new"

	// Get CREATE TABLE statement to recreate structure
	var tblName, createSQL string
	//nolint:gosec // G202: table name from hardcoded list
	if err := db.QueryRow("SHOW CREATE TABLE `" + table + "`").Scan(&tblName, &createSQL); err != nil {
		return fmt.Errorf("show create table: %w", err)
	}

	// Replace table name and PK in the CREATE statement
	createSQL = strings.Replace(createSQL, "CREATE TABLE `"+table+"`", "CREATE TABLE `"+newTable+"`", 1)
	createSQL = strings.Replace(createSQL, "PRIMARY KEY (`issue_id`,`depends_on_id`)", "PRIMARY KEY (`issue_id`,`depends_on_id`,`type`)", 1)

	if _, err := db.Exec(createSQL); err != nil {
		return fmt.Errorf("create new table: %w", err)
	}

	//nolint:gosec // G202: table names from hardcoded list
	if _, err := db.Exec("INSERT INTO `" + newTable + "` SELECT * FROM `" + table + "`"); err != nil {
		db.Exec("DROP TABLE IF EXISTS `" + newTable + "`") //nolint:errcheck,gosec
		return fmt.Errorf("copy data: %w", err)
	}

	//nolint:gosec // G202
	if _, err := db.Exec("DROP TABLE `" + table + "`"); err != nil {
		return fmt.Errorf("drop old table: %w", err)
	}

	//nolint:gosec // G202
	if _, err := db.Exec("RENAME TABLE `" + newTable + "` TO `" + table + "`"); err != nil {
		return fmt.Errorf("rename table: %w", err)
	}

	return nil
}
