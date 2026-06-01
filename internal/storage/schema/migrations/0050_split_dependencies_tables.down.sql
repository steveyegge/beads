-- Reverse of 0050: drops the three new tables. The dropped ready_issues /
-- blocked_issues views are not restored — their prior definitions reference
-- the legacy dependencies columns and would be stale. Restore from a prior
-- dolt commit if a rollback is needed.
DROP TABLE IF EXISTS issue_external_dependencies;
DROP TABLE IF EXISTS issue_wisp_dependencies;
DROP TABLE IF EXISTS issue_issue_dependencies;
