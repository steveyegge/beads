-- Migration 0050: Split `dependencies` into three target-typed tables.
--
-- The legacy `dependencies` table uses a surrogate UUID primary key (added in
-- 0043) with three nullable target columns guarded by a CHECK constraint.
-- The UUID PK causes Dolt merge conflicts: two clones that independently add
-- the same logical edge generate distinct UUIDs and Dolt sees two rows.
--
-- This migration creates three replacement tables, one per target kind, each
-- with a composite natural primary key `(source_id, depends_on_<k>_id)`.
-- Identical logical edges from independent clones now share a PK and converge
-- under Dolt's three-way merge.
--
-- Data copy lives in a follow-up step (task 3); legacy table drop lives in
-- migration 0051 so a binary downgrade is possible for one release.
--
-- Wisp-source dependency tables are split in parallel under
-- internal/storage/schema/migrations/ignored/0009 because the `wisps` table is
-- nonlocal (registered via dolt_ignore in 0019).

CREATE TABLE IF NOT EXISTS issue_issue_dependencies (
    source_id           VARCHAR(255) NOT NULL,
    depends_on_issue_id VARCHAR(255) NOT NULL,
    type                VARCHAR(32)  NOT NULL DEFAULT 'blocks',
    created_at          DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by          VARCHAR(255) NOT NULL DEFAULT '',
    metadata            JSON                  DEFAULT (JSON_OBJECT()),
    thread_id           VARCHAR(255)          DEFAULT '',
    PRIMARY KEY (source_id, depends_on_issue_id),
    INDEX idx_iid_source (source_id),
    INDEX idx_iid_target (depends_on_issue_id),
    INDEX idx_iid_target_type (depends_on_issue_id, type),
    INDEX idx_iid_thread (thread_id),
    CONSTRAINT fk_iid_source FOREIGN KEY (source_id) REFERENCES issues(id) ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT fk_iid_target FOREIGN KEY (depends_on_issue_id) REFERENCES issues(id) ON DELETE CASCADE ON UPDATE CASCADE
);

-- No target FK to wisps: the wisps table is nonlocal/optional and this
-- migration lives in the regular (non-ignored) directory. The application
-- validates wisp-target references in Go (same as the legacy `dependencies`
-- table, which has no FK on its `depends_on_wisp_id` column either).
CREATE TABLE IF NOT EXISTS issue_wisp_dependencies (
    source_id          VARCHAR(255) NOT NULL,
    depends_on_wisp_id VARCHAR(255) NOT NULL,
    type               VARCHAR(32)  NOT NULL DEFAULT 'blocks',
    created_at         DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by         VARCHAR(255) NOT NULL DEFAULT '',
    metadata           JSON                  DEFAULT (JSON_OBJECT()),
    thread_id          VARCHAR(255)          DEFAULT '',
    PRIMARY KEY (source_id, depends_on_wisp_id),
    INDEX idx_iwd_source (source_id),
    INDEX idx_iwd_target (depends_on_wisp_id),
    INDEX idx_iwd_target_type (depends_on_wisp_id, type),
    INDEX idx_iwd_thread (thread_id),
    CONSTRAINT fk_iwd_source FOREIGN KEY (source_id) REFERENCES issues(id) ON DELETE CASCADE ON UPDATE CASCADE
);

-- External targets have no referent; no target FK is possible.
CREATE TABLE IF NOT EXISTS issue_external_dependencies (
    source_id              VARCHAR(255) NOT NULL,
    depends_on_external_id VARCHAR(255) NOT NULL,
    type                   VARCHAR(32)  NOT NULL DEFAULT 'blocks',
    created_at             DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by             VARCHAR(255) NOT NULL DEFAULT '',
    metadata               JSON                  DEFAULT (JSON_OBJECT()),
    thread_id              VARCHAR(255)          DEFAULT '',
    PRIMARY KEY (source_id, depends_on_external_id),
    INDEX idx_ied_source (source_id),
    INDEX idx_ied_target (depends_on_external_id),
    INDEX idx_ied_target_type (depends_on_external_id, type),
    INDEX idx_ied_thread (thread_id),
    CONSTRAINT fk_ied_source FOREIGN KEY (source_id) REFERENCES issues(id) ON DELETE CASCADE ON UPDATE CASCADE
);

-- Data copy from legacy `dependencies`. INSERT IGNORE makes the copy
-- idempotent across re-runs and Dolt merges. The `dependencies` table is
-- created in 0002 and always exists at this point, so on a fresh install
-- these SELECTs simply copy zero rows.
INSERT IGNORE INTO issue_issue_dependencies
    (source_id, depends_on_issue_id, type, created_at, created_by, metadata, thread_id)
SELECT issue_id, depends_on_issue_id, type, created_at, created_by, metadata, thread_id
FROM dependencies
WHERE depends_on_issue_id IS NOT NULL;

INSERT IGNORE INTO issue_wisp_dependencies
    (source_id, depends_on_wisp_id, type, created_at, created_by, metadata, thread_id)
SELECT issue_id, depends_on_wisp_id, type, created_at, created_by, metadata, thread_id
FROM dependencies
WHERE depends_on_wisp_id IS NOT NULL;

INSERT IGNORE INTO issue_external_dependencies
    (source_id, depends_on_external_id, type, created_at, created_by, metadata, thread_id)
SELECT issue_id, depends_on_external, type, created_at, created_by, metadata, thread_id
FROM dependencies
WHERE depends_on_external IS NOT NULL;

-- Drop the legacy `ready_issues` and `blocked_issues` views. They are not
-- referenced by any Go code (verified via grep) and reference the legacy
-- `dependencies` table which is being dropped in 0051. Modern ready/blocked
-- queries use the denormalized `is_blocked` column added in 0046.
DROP VIEW IF EXISTS ready_issues;
DROP VIEW IF EXISTS blocked_issues;
