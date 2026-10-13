-- Recurrence + mandatory due dates.
--
-- Four columns on the issues row shape (and its wisps twin, which shares the
-- shape and is scanned by the same column list):
--
--   repeat_pattern  an interval ("+1w") or five-field cron ("0 9 * * 1");
--                   empty means the bead does not repeat
--   repeat_start    earliest occurrence bound, NULL = unbounded
--   repeat_end      last occurrence bound, NULL = unbounded
--   due_source      provenance of due_at: explicit | default | backfill |
--                   repeat; NULL/'' on rows written before this migration
--
-- This migration is SCHEMA ONLY: it adds columns and writes no rows.
--
-- Backfilling due dates onto legacy beads is deliberately NOT done here.
-- Dating tens of thousands of existing beads is a judgement call about other
-- people's work, and a migration is the one place it cannot be previewed,
-- reviewed, or declined — it runs unattended on every clone the moment the
-- binary is upgraded. `bd due backfill` owns that instead: it reports what it
-- would change and writes nothing until a human passes --apply.
--
-- due_source carries the provenance that backfill depends on, so a synthesized
-- date stays distinguishable from one a human chose. It is empty on every row
-- this migration touches; nothing infers a value for history.
--
-- Guarded so the migration is idempotent on a schema_migrations row that
-- regressed without its DDL rolled back (0052/0054/0060 precedent).

-- issues: the four columns.
SET @needs_add = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'issues'
      AND COLUMN_NAME = 'repeat_pattern'
);
SET @sql = IF(@needs_add = 1,
    'ALTER TABLE issues ADD COLUMN repeat_pattern VARCHAR(255) NOT NULL DEFAULT ''''',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @needs_add = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'issues'
      AND COLUMN_NAME = 'repeat_start'
);
SET @sql = IF(@needs_add = 1,
    'ALTER TABLE issues ADD COLUMN repeat_start DATETIME',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @needs_add = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'issues'
      AND COLUMN_NAME = 'repeat_end'
);
SET @sql = IF(@needs_add = 1,
    'ALTER TABLE issues ADD COLUMN repeat_end DATETIME',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @needs_add = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'issues'
      AND COLUMN_NAME = 'due_source'
);
SET @sql = IF(@needs_add = 1,
    'ALTER TABLE issues ADD COLUMN due_source VARCHAR(16) NOT NULL DEFAULT ''''',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- wisps: the same four columns. Guarded on the table existing (older
-- workspaces created issues-only; 0054/0060 precedent). The clone-local twin
-- that carries this through the fresh-clone door is ignored/0026.
SET @has_wisps = (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'wisps'
);

SET @needs_add = IF(@has_wisps > 0 AND
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
        WHERE TABLE_SCHEMA = DATABASE()
          AND TABLE_NAME = 'wisps'
          AND COLUMN_NAME = 'repeat_pattern') = 0,
    1, 0);
SET @sql = IF(@needs_add = 1,
    'ALTER TABLE wisps ADD COLUMN repeat_pattern VARCHAR(255) NOT NULL DEFAULT ''''',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @needs_add = IF(@has_wisps > 0 AND
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
        WHERE TABLE_SCHEMA = DATABASE()
          AND TABLE_NAME = 'wisps'
          AND COLUMN_NAME = 'repeat_start') = 0,
    1, 0);
SET @sql = IF(@needs_add = 1,
    'ALTER TABLE wisps ADD COLUMN repeat_start DATETIME',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @needs_add = IF(@has_wisps > 0 AND
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
        WHERE TABLE_SCHEMA = DATABASE()
          AND TABLE_NAME = 'wisps'
          AND COLUMN_NAME = 'repeat_end') = 0,
    1, 0);
SET @sql = IF(@needs_add = 1,
    'ALTER TABLE wisps ADD COLUMN repeat_end DATETIME',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @needs_add = IF(@has_wisps > 0 AND
    (SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
        WHERE TABLE_SCHEMA = DATABASE()
          AND TABLE_NAME = 'wisps'
          AND COLUMN_NAME = 'due_source') = 0,
    1, 0);
SET @sql = IF(@needs_add = 1,
    'ALTER TABLE wisps ADD COLUMN due_source VARCHAR(16) NOT NULL DEFAULT ''''',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
