SET @needs_index = (
    SELECT IF(COUNT(*) = 0, 1, 0)
    FROM INFORMATION_SCHEMA.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'issues'
      AND INDEX_NAME = 'idx_issues_ready_assignee'
);
SET @sql = IF(@needs_index = 1,
    'CREATE INDEX idx_issues_ready_assignee ON issues (assignee, status, priority, created_at, id)',
    'SELECT 1');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
