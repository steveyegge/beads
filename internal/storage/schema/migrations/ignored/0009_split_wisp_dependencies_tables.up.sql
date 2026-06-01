-- Migration ignored/0009: Split `wisp_dependencies` into three target-typed
-- tables (parallel to regular 0050 for issue-source dependencies).
--
-- The legacy `wisp_dependencies` table uses a surrogate UUID primary key
-- (added in ignored/0005) with three nullable target columns guarded by a
-- CHECK constraint. The UUID PK causes Dolt merge conflicts: two clones that
-- independently add the same logical edge generate distinct UUIDs and Dolt
-- sees two rows.
--
-- This migration creates three replacement tables, one per target kind, each
-- with a composite natural primary key `(source_id, depends_on_<k>_id)`.
--
-- Data copy lives in a follow-up step (task 3); legacy table drop lives in
-- ignored/0010 so a binary downgrade is possible for one release.
--
-- The `wisp_%` glob in dolt_ignore (0019) and `wisp_*` glob in
-- dolt_nonlocal_tables (0040) already cover these new table names; no extra
-- registration is needed.

CREATE TABLE IF NOT EXISTS wisp_issue_dependencies (
    source_id           VARCHAR(255) NOT NULL,
    depends_on_issue_id VARCHAR(255) NOT NULL,
    type                VARCHAR(32)  NOT NULL DEFAULT 'blocks',
    created_at          DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255) NOT NULL DEFAULT '',
    metadata            JSON                  DEFAULT (JSON_OBJECT()),
    thread_id           VARCHAR(255)          DEFAULT '',
    PRIMARY KEY (source_id, depends_on_issue_id),
    INDEX idx_wid_source (source_id),
    INDEX idx_wid_target (depends_on_issue_id),
    INDEX idx_wid_target_type (depends_on_issue_id, type),
    INDEX idx_wid_thread (thread_id),
    CONSTRAINT fk_wid_source FOREIGN KEY (source_id) REFERENCES wisps(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT fk_wid_target FOREIGN KEY (depends_on_issue_id) REFERENCES issues(id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS wisp_wisp_dependencies (
    source_id          VARCHAR(255) NOT NULL,
    depends_on_wisp_id VARCHAR(255) NOT NULL,
    type               VARCHAR(32)  NOT NULL DEFAULT 'blocks',
    created_at         DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by         VARCHAR(255) NOT NULL DEFAULT '',
    metadata           JSON                  DEFAULT (JSON_OBJECT()),
    thread_id          VARCHAR(255)          DEFAULT '',
    PRIMARY KEY (source_id, depends_on_wisp_id),
    INDEX idx_wwd_source (source_id),
    INDEX idx_wwd_target (depends_on_wisp_id),
    INDEX idx_wwd_target_type (depends_on_wisp_id, type),
    INDEX idx_wwd_thread (thread_id),
    CONSTRAINT fk_wwd_source FOREIGN KEY (source_id) REFERENCES wisps(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT fk_wwd_target FOREIGN KEY (depends_on_wisp_id) REFERENCES wisps(id) ON DELETE CASCADE ON UPDATE CASCADE
);

-- External targets have no referent; no target FK is possible.
CREATE TABLE IF NOT EXISTS wisp_external_dependencies (
    source_id              VARCHAR(255) NOT NULL,
    depends_on_external_id VARCHAR(255) NOT NULL,
    type                   VARCHAR(32)  NOT NULL DEFAULT 'blocks',
    created_at             DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by             VARCHAR(255) NOT NULL DEFAULT '',
    metadata               JSON                  DEFAULT (JSON_OBJECT()),
    thread_id              VARCHAR(255)          DEFAULT '',
    PRIMARY KEY (source_id, depends_on_external_id),
    INDEX idx_wed_source (source_id),
    INDEX idx_wed_target (depends_on_external_id),
    INDEX idx_wed_target_type (depends_on_external_id, type),
    INDEX idx_wed_thread (thread_id),
    CONSTRAINT fk_wed_source FOREIGN KEY (source_id) REFERENCES wisps(id) ON DELETE CASCADE ON UPDATE CASCADE
);

-- Data copy from legacy `wisp_dependencies`. INSERT IGNORE makes the copy
-- idempotent across re-runs and Dolt merges. `wisp_dependencies` is created
-- by regular migration 0021, so it always exists at this point.
--
-- Legacy `wisp_dependencies` allows NULL `created_by` and NULL `created_at`
-- (both DEFAULT but not NOT NULL — see ignored/0005). The new tables require
-- both NOT NULL, so COALESCE defensively.
INSERT IGNORE INTO wisp_issue_dependencies
    (source_id, depends_on_issue_id, type, created_at, created_by, metadata, thread_id)
SELECT issue_id,
       depends_on_issue_id,
       type,
       COALESCE(created_at, CURRENT_TIMESTAMP),
       COALESCE(created_by, ''),
       metadata,
       thread_id
FROM wisp_dependencies
WHERE depends_on_issue_id IS NOT NULL;

INSERT IGNORE INTO wisp_wisp_dependencies
    (source_id, depends_on_wisp_id, type, created_at, created_by, metadata, thread_id)
SELECT issue_id,
       depends_on_wisp_id,
       type,
       COALESCE(created_at, CURRENT_TIMESTAMP),
       COALESCE(created_by, ''),
       metadata,
       thread_id
FROM wisp_dependencies
WHERE depends_on_wisp_id IS NOT NULL;

INSERT IGNORE INTO wisp_external_dependencies
    (source_id, depends_on_external_id, type, created_at, created_by, metadata, thread_id)
SELECT issue_id,
       depends_on_external,
       type,
       COALESCE(created_at, CURRENT_TIMESTAMP),
       COALESCE(created_by, ''),
       metadata,
       thread_id
FROM wisp_dependencies
WHERE depends_on_external IS NOT NULL;
