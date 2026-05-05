-- 0001_initial.down.sql — drop everything created by 0001_initial.up.sql in reverse order.
-- Exercised only by tests; production never invokes "down" migrations.

DROP VIEW IF EXISTS blocked_issues;
DROP VIEW IF EXISTS ready_issues;

DROP TABLE IF EXISTS repo_mtimes;
DROP TABLE IF EXISTS compaction_snapshots;
DROP TABLE IF EXISTS issue_snapshots;
DROP TABLE IF EXISTS issue_counter;
DROP TABLE IF EXISTS child_counters;

DROP TABLE IF EXISTS custom_types;
DROP TABLE IF EXISTS custom_statuses;
DROP TABLE IF EXISTS local_metadata;
DROP TABLE IF EXISTS metadata;
DROP TABLE IF EXISTS config;

DROP TABLE IF EXISTS wisp_events;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS wisp_comments;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS wisp_labels;
DROP TABLE IF EXISTS labels;
DROP TABLE IF EXISTS wisp_dependencies;
DROP TABLE IF EXISTS dependencies;

DROP TRIGGER IF EXISTS wisps_set_updated_at ON wisps;
DROP TABLE IF EXISTS wisps;
DROP TRIGGER IF EXISTS issues_set_updated_at ON issues;
DROP TABLE IF EXISTS issues;

DROP FUNCTION IF EXISTS set_updated_at();
