-- 0033_widen_text_columns.down.sql
-- Reverse migration: narrow MEDIUMTEXT back to TEXT.
-- IMPORTANT: this is operationally one-way for any database that has
-- accumulated rows >65,535 bytes -- MySQL silently truncates on
-- column-narrow. Provided for completeness; do not run against a database
-- that has carried >64KB content under any of the widened columns.

ALTER TABLE issues          MODIFY COLUMN notes            TEXT NOT NULL;
ALTER TABLE events          MODIFY COLUMN old_value        TEXT;
ALTER TABLE events          MODIFY COLUMN new_value        TEXT;
ALTER TABLE events          MODIFY COLUMN comment          TEXT;
ALTER TABLE comments        MODIFY COLUMN text             TEXT NOT NULL;
ALTER TABLE wisps           MODIFY COLUMN notes            TEXT NOT NULL DEFAULT '';
ALTER TABLE wisp_events     MODIFY COLUMN old_value        TEXT DEFAULT '';
ALTER TABLE wisp_events     MODIFY COLUMN new_value        TEXT DEFAULT '';
ALTER TABLE wisp_events     MODIFY COLUMN comment          TEXT DEFAULT '';
ALTER TABLE wisp_comments   MODIFY COLUMN text             TEXT NOT NULL;
ALTER TABLE issue_snapshots MODIFY COLUMN original_content TEXT NOT NULL;
ALTER TABLE issue_snapshots MODIFY COLUMN archived_events  TEXT;
