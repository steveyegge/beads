SET @needs_drop = (
    SELECT IF(COUNT(*) > 0, 1, 0)
    FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'dependencies'
      AND CONSTRAINT_NAME = 'fk_dep_depends_on'
);
SET @sql = IF(@needs_drop = 1,
    'ALTER TABLE dependencies DROP FOREIGN KEY fk_dep_depends_on',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
