-- 0033_widen_text_columns.up.sql
-- Widen TEXT columns to MEDIUMTEXT on tables that hold large per-row content
-- (notes, events, comments, snapshots, and their wisp counterparts). MySQL/Dolt
-- TEXT caps at 65,535 bytes per row, and high-volume issues exceed that on
-- cumulative bd note appends. MEDIUMTEXT raises the cap to 16 MiB
-- (256x capacity) and is metadata-only on Dolt's prolly-tree storage --
-- existing rows are preserved without rewrite.
--
-- 12 ALTER statements covering 12 unique columns (issues.notes + events x3 +
-- comments.text + wisps.notes + wisp_events x3 + wisp_comments.text +
-- issue_snapshots x2). The wisps.notes widening preserves schema parity
-- with issues.notes per TestSchemaParityIssuesVsWisps.

ALTER TABLE issues          MODIFY COLUMN notes            MEDIUMTEXT NOT NULL;
ALTER TABLE events          MODIFY COLUMN old_value        MEDIUMTEXT;
ALTER TABLE events          MODIFY COLUMN new_value        MEDIUMTEXT;
ALTER TABLE events          MODIFY COLUMN comment          MEDIUMTEXT;
ALTER TABLE comments        MODIFY COLUMN text             MEDIUMTEXT NOT NULL;
ALTER TABLE wisps           MODIFY COLUMN notes            MEDIUMTEXT NOT NULL DEFAULT '';
ALTER TABLE wisp_events     MODIFY COLUMN old_value        MEDIUMTEXT DEFAULT '';
ALTER TABLE wisp_events     MODIFY COLUMN new_value        MEDIUMTEXT DEFAULT '';
ALTER TABLE wisp_events     MODIFY COLUMN comment          MEDIUMTEXT DEFAULT '';
ALTER TABLE wisp_comments   MODIFY COLUMN text             MEDIUMTEXT NOT NULL;
ALTER TABLE issue_snapshots MODIFY COLUMN original_content MEDIUMTEXT NOT NULL;
ALTER TABLE issue_snapshots MODIFY COLUMN archived_events  MEDIUMTEXT;
