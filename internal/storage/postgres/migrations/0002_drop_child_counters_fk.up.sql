-- 0002_drop_child_counters_fk.up.sql — be-b0h
--
-- Mirror Dolt migration 013_drop_child_counters_fk: child_counters.parent_id
-- can reference either issues(id) OR wisps(id) (since migration 007 moved
-- agent beads to wisps), and may also hold hierarchical IDs (mc-4la.1,
-- mc-4la.1.1) whose parents do not exist in either table. The FK is a
-- counter-cache invariant, not a domain relationship; orphaned rows are
-- harmless. Dolt dropped the constraint; PG must too or cross-backend
-- migration fails on real data.

ALTER TABLE child_counters DROP CONSTRAINT IF EXISTS fk_counter_parent;
