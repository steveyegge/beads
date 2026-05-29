package schema

// cliCompatibleMigrationSQL returns migration SQL suitable for `dolt sql -q`
// against a fresh test database. The Dolt CLI accepts PREPARE/EXECUTE DDL but
// does not apply some prepared ALTER TABLE statements in this path, so the
// fresh-schema bundle uses direct DDL for migrations that create columns later
// statements depend on. Runtime migrations still use the source files.
func cliCompatibleMigrationSQL(name, sqlText string) string {
	switch name {
	case "0023_add_no_history_column.up.sql":
		return cliMigration0023AddNoHistoryColumn
	case "0027_add_started_at.up.sql":
		return cliMigration0027AddStartedAt
	case "0033_add_wisp_type_column.up.sql":
		return "SELECT 1;"
	case "0034_add_spec_id_column.up.sql":
		return "SELECT 1;"
	case "0041_split_dependencies_target.up.sql":
		return cliMigration0041SplitDependenciesTarget
	case "0043_drop_dependencies_generated_column.up.sql":
		return cliMigration0043DropDependenciesGeneratedColumn
	case "0046_add_is_blocked.up.sql":
		return cliMigration0046AddIsBlocked
	default:
		return sqlText
	}
}

const cliMigration0023AddNoHistoryColumn = `ALTER TABLE issues ADD COLUMN no_history TINYINT(1) DEFAULT 0;
ALTER TABLE wisps ADD COLUMN no_history TINYINT(1) DEFAULT 0;`

const cliMigration0027AddStartedAt = `ALTER TABLE issues ADD COLUMN started_at DATETIME;
ALTER TABLE wisps ADD COLUMN started_at DATETIME;`

const cliMigration0041SplitDependenciesTarget = `DELETE FROM dolt_nonlocal_tables;
CALL DOLT_COMMIT('-Am', 'disable nonlocal tables for fk migrations');
SET FOREIGN_KEY_CHECKS = 0;

ALTER TABLE dependencies ADD COLUMN depends_on_issue_id VARCHAR(255) NULL;
ALTER TABLE dependencies ADD COLUMN depends_on_wisp_id VARCHAR(255) NULL;
ALTER TABLE dependencies ADD COLUMN depends_on_external VARCHAR(255) NULL;

UPDATE dependencies SET depends_on_external = depends_on_id WHERE depends_on_id LIKE 'external:%';
UPDATE dependencies d JOIN wisps w ON w.id = d.depends_on_id SET d.depends_on_wisp_id = d.depends_on_id WHERE d.depends_on_external IS NULL;
UPDATE dependencies d JOIN issues i ON i.id = d.depends_on_id SET d.depends_on_issue_id = d.depends_on_id WHERE d.depends_on_external IS NULL AND d.depends_on_wisp_id IS NULL;
UPDATE dependencies SET depends_on_external = depends_on_id WHERE depends_on_external IS NULL AND depends_on_wisp_id IS NULL AND depends_on_issue_id IS NULL;

ALTER TABLE dependencies DROP INDEX idx_dependencies_depends_on;
ALTER TABLE dependencies DROP INDEX idx_dependencies_depends_on_type;
ALTER TABLE dependencies DROP PRIMARY KEY;
ALTER TABLE dependencies DROP COLUMN depends_on_id;

ALTER TABLE dependencies ADD CONSTRAINT fk_dep_issue_target FOREIGN KEY (depends_on_issue_id) REFERENCES issues(id) ON DELETE CASCADE ON UPDATE CASCADE;
ALTER TABLE dependencies ADD COLUMN depends_on_id VARCHAR(255) AS (COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external)) STORED;
ALTER TABLE dependencies ADD PRIMARY KEY (issue_id, depends_on_id);
ALTER TABLE dependencies ADD INDEX idx_dep_wisp_target (depends_on_wisp_id);
ALTER TABLE dependencies ADD INDEX idx_dep_issue_target (depends_on_issue_id);
ALTER TABLE dependencies ADD INDEX idx_dep_external_target (depends_on_external);
ALTER TABLE dependencies ADD INDEX idx_dep_type_target (type, depends_on_id);
ALTER TABLE dependencies ADD CONSTRAINT ck_dep_one_target CHECK ((depends_on_issue_id IS NOT NULL) + (depends_on_wisp_id IS NOT NULL) + (depends_on_external IS NOT NULL) = 1);

SET FOREIGN_KEY_CHECKS = 1;`

const cliMigration0043DropDependenciesGeneratedColumn = `SET FOREIGN_KEY_CHECKS = 0;

ALTER TABLE dependencies DROP INDEX idx_dep_type_target;
ALTER TABLE dependencies DROP FOREIGN KEY fk_dep_issue_target;
ALTER TABLE dependencies DROP FOREIGN KEY fk_dep_issue;
ALTER TABLE dependencies DROP PRIMARY KEY;
ALTER TABLE dependencies DROP COLUMN depends_on_id;

ALTER TABLE dependencies ADD COLUMN id CHAR(36) NOT NULL DEFAULT (UUID()) PRIMARY KEY FIRST;
ALTER TABLE dependencies ADD UNIQUE KEY uk_dep_issue_target (issue_id, depends_on_issue_id);
ALTER TABLE dependencies ADD UNIQUE KEY uk_dep_wisp_target (issue_id, depends_on_wisp_id);
ALTER TABLE dependencies ADD UNIQUE KEY uk_dep_external_target (issue_id, depends_on_external);
ALTER TABLE dependencies ADD INDEX idx_dep_type_issue (type, depends_on_issue_id);
ALTER TABLE dependencies ADD INDEX idx_dep_type_wisp (type, depends_on_wisp_id);
ALTER TABLE dependencies ADD INDEX idx_dep_type_external (type, depends_on_external);
ALTER TABLE dependencies ADD CONSTRAINT fk_dep_issue FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE ON UPDATE CASCADE;
ALTER TABLE dependencies ADD CONSTRAINT fk_dep_issue_target FOREIGN KEY (depends_on_issue_id) REFERENCES issues(id) ON DELETE CASCADE ON UPDATE CASCADE;

SET FOREIGN_KEY_CHECKS = 1;`

const cliMigration0046AddIsBlocked = `ALTER TABLE issues ADD COLUMN is_blocked TINYINT(1) NOT NULL DEFAULT 0;
CREATE INDEX idx_issues_is_blocked ON issues(is_blocked, status);

UPDATE issues SET is_blocked = 0;

WITH RECURSIVE
  directly_blocked(id) AS (
    SELECT DISTINCT i.id
    FROM issues i
    JOIN dependencies d ON d.issue_id = i.id
    JOIN issues t ON t.id = d.depends_on_issue_id
    WHERE i.status NOT IN ('closed', 'pinned')
      AND d.type IN ('blocks', 'conditional-blocks', 'waits-for')
      AND t.status NOT IN ('closed', 'pinned')
  ),
  recursively_blocked(id) AS (
    SELECT id FROM directly_blocked
    UNION
    SELECT d.issue_id
    FROM dependencies d
    JOIN recursively_blocked rb ON rb.id = d.depends_on_issue_id
    JOIN issues i ON i.id = d.issue_id
    WHERE i.status NOT IN ('closed', 'pinned')
      AND d.type IN ('blocks', 'conditional-blocks', 'waits-for')
  )
UPDATE issues
JOIN recursively_blocked rb ON rb.id = issues.id
SET is_blocked = 1
WHERE issues.status NOT IN ('closed', 'pinned');`
