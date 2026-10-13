-- Roll back the recurrence columns. Guarded so an issues-only or
-- partially-applied workspace rolls back as safely as it migrated up
-- (0054/0060 precedent).
--
-- The due-date BACKFILL is deliberately not reversed: after rolling back,
-- due_at no longer records whether a date was backfilled or chosen, so
-- nulling "the backfilled ones" would be a guess that could destroy a real
-- deadline. Leaving the dates is the safe direction.

SET @has_col = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'issues' AND COLUMN_NAME = 'repeat_pattern'
);
SET @sql = IF(@has_col > 0, 'ALTER TABLE issues DROP COLUMN repeat_pattern', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'issues' AND COLUMN_NAME = 'repeat_start'
);
SET @sql = IF(@has_col > 0, 'ALTER TABLE issues DROP COLUMN repeat_start', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'issues' AND COLUMN_NAME = 'repeat_end'
);
SET @sql = IF(@has_col > 0, 'ALTER TABLE issues DROP COLUMN repeat_end', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'issues' AND COLUMN_NAME = 'due_source'
);
SET @sql = IF(@has_col > 0, 'ALTER TABLE issues DROP COLUMN due_source', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'wisps' AND COLUMN_NAME = 'repeat_pattern'
);
SET @sql = IF(@has_col > 0, 'ALTER TABLE wisps DROP COLUMN repeat_pattern', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'wisps' AND COLUMN_NAME = 'repeat_start'
);
SET @sql = IF(@has_col > 0, 'ALTER TABLE wisps DROP COLUMN repeat_start', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'wisps' AND COLUMN_NAME = 'repeat_end'
);
SET @sql = IF(@has_col > 0, 'ALTER TABLE wisps DROP COLUMN repeat_end', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_col = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'wisps' AND COLUMN_NAME = 'due_source'
);
SET @sql = IF(@has_col > 0, 'ALTER TABLE wisps DROP COLUMN due_source', 'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
