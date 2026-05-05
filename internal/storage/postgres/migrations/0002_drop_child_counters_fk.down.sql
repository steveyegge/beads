-- 0002_drop_child_counters_fk.down.sql — be-b0h
--
-- Restore the FK only when child_counters has no orphan rows (rows whose
-- parent_id is neither in issues nor wisps). Without this guard a downgrade
-- on a database with hierarchical or wisp-parented counters would fail.
-- This down migration matches the behavior we want: bd does not roll back
-- past migration 0002 unless the data is FK-safe.

DELETE FROM child_counters cc
 WHERE NOT EXISTS (SELECT 1 FROM issues i WHERE i.id = cc.parent_id)
   AND NOT EXISTS (SELECT 1 FROM wisps  w WHERE w.id = cc.parent_id);

ALTER TABLE child_counters
  ADD CONSTRAINT fk_counter_parent
  FOREIGN KEY (parent_id) REFERENCES issues(id) ON DELETE CASCADE;
