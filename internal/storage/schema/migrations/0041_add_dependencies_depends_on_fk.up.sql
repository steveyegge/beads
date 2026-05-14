-- Add fk_dep_depends_on on dependencies.depends_on_id. FOREIGN_KEY_CHECKS is
-- disabled across the ALTER so the constraint can be created even when
-- existing rows are still in violation.
SET FOREIGN_KEY_CHECKS = 0;

SET @needs_add = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'dependencies'
      AND CONSTRAINT_NAME = 'fk_dep_depends_on'
);
SET @sql = IF(@needs_add = 1,
    'ALTER TABLE dependencies ADD CONSTRAINT fk_dep_depends_on FOREIGN KEY (depends_on_id) REFERENCES issues(id) ON DELETE CASCADE',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET FOREIGN_KEY_CHECKS = 1;
