-- 0001_initial.up.sql — initial PG schema for beads.
--
-- Generated from internal/storage/schema/migrations/*.up.sql at the post-34
-- migrations Dolt schema state. ADR be-l7t.3 §3 specifies the translation
-- rules; migrations 33 (idx_issues_status_updated_at + idx_issues_defer_until)
-- and 34 (LONGTEXT widening — no-op for PG TEXT) are absorbed inline.
--
-- MUST be applied as a single migration; bd_schema_migrations records version=1.

-- Section 1: Trigger function for updated_at (mirrors MySQL's ON UPDATE CURRENT_TIMESTAMP).
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Section 2: Issue storage.
CREATE TABLE IF NOT EXISTS issues (
    id                    VARCHAR(255) PRIMARY KEY,
    content_hash          VARCHAR(64),
    title                 VARCHAR(500) NOT NULL,
    description           TEXT NOT NULL,
    design                TEXT NOT NULL,
    acceptance_criteria   TEXT NOT NULL,
    notes                 TEXT NOT NULL,
    status                VARCHAR(32) NOT NULL DEFAULT 'open',
    priority              INTEGER NOT NULL DEFAULT 2,
    issue_type            VARCHAR(32) NOT NULL DEFAULT 'task',
    assignee              VARCHAR(255),
    estimated_minutes     INTEGER,
    created_at            TIMESTAMP NOT NULL DEFAULT NOW(),
    created_by            VARCHAR(255) DEFAULT '',
    owner                 VARCHAR(255) DEFAULT '',
    updated_at            TIMESTAMP NOT NULL DEFAULT NOW(),
    closed_at             TIMESTAMP,
    closed_by_session     VARCHAR(255) DEFAULT '',
    external_ref          VARCHAR(255),
    spec_id               VARCHAR(1024),
    compaction_level      INTEGER DEFAULT 0,
    compacted_at          TIMESTAMP,
    compacted_at_commit   VARCHAR(64),
    original_size         INTEGER,
    sender                VARCHAR(255) DEFAULT '',
    ephemeral             BOOLEAN DEFAULT FALSE,
    wisp_type             VARCHAR(32) DEFAULT '',
    pinned                BOOLEAN DEFAULT FALSE,
    is_template           BOOLEAN DEFAULT FALSE,
    mol_type              VARCHAR(32) DEFAULT '',
    work_type             VARCHAR(32) DEFAULT 'mutex',
    source_system         VARCHAR(255) DEFAULT '',
    metadata              JSONB DEFAULT '{}'::jsonb,
    source_repo           VARCHAR(512) DEFAULT '',
    close_reason          TEXT DEFAULT '',
    event_kind            VARCHAR(32) DEFAULT '',
    actor                 VARCHAR(255) DEFAULT '',
    target                VARCHAR(255) DEFAULT '',
    payload               TEXT DEFAULT '',
    await_type            VARCHAR(32) DEFAULT '',
    await_id              VARCHAR(255) DEFAULT '',
    timeout_ns            BIGINT DEFAULT 0,
    waiters               TEXT DEFAULT '',
    hook_bead             VARCHAR(255) DEFAULT '',
    role_bead             VARCHAR(255) DEFAULT '',
    agent_state           VARCHAR(32) DEFAULT '',
    last_activity         TIMESTAMP,
    role_type             VARCHAR(32) DEFAULT '',
    rig                   VARCHAR(255) DEFAULT '',
    due_at                TIMESTAMP,
    defer_until           TIMESTAMP,
    started_at            TIMESTAMP,
    no_history            BOOLEAN DEFAULT FALSE
);

-- Indexes (post-migration-0033 shape: composite (status, updated_at) replaces single-col status idx).
CREATE INDEX IF NOT EXISTS idx_issues_status_updated_at  ON issues (status, updated_at);
CREATE INDEX IF NOT EXISTS idx_issues_defer_until        ON issues (defer_until);
CREATE INDEX IF NOT EXISTS idx_issues_priority           ON issues (priority);
CREATE INDEX IF NOT EXISTS idx_issues_issue_type         ON issues (issue_type);
CREATE INDEX IF NOT EXISTS idx_issues_assignee           ON issues (assignee);
CREATE INDEX IF NOT EXISTS idx_issues_created_at         ON issues (created_at);
CREATE INDEX IF NOT EXISTS idx_issues_spec_id            ON issues (spec_id);
CREATE INDEX IF NOT EXISTS idx_issues_external_ref       ON issues (external_ref);

DROP TRIGGER IF EXISTS issues_set_updated_at ON issues;
CREATE TRIGGER issues_set_updated_at
    BEFORE UPDATE ON issues
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Section 3: Wisps (parallel ephemeral table; same shape as issues).
CREATE TABLE IF NOT EXISTS wisps (
    id                    VARCHAR(255) PRIMARY KEY,
    content_hash          VARCHAR(64),
    title                 VARCHAR(500) NOT NULL,
    description           TEXT NOT NULL DEFAULT '',
    design                TEXT NOT NULL DEFAULT '',
    acceptance_criteria   TEXT NOT NULL DEFAULT '',
    notes                 TEXT NOT NULL DEFAULT '',
    status                VARCHAR(32) NOT NULL DEFAULT 'open',
    priority              INTEGER NOT NULL DEFAULT 2,
    issue_type            VARCHAR(32) NOT NULL DEFAULT 'task',
    assignee              VARCHAR(255),
    estimated_minutes     INTEGER,
    created_at            TIMESTAMP NOT NULL DEFAULT NOW(),
    created_by            VARCHAR(255) DEFAULT '',
    owner                 VARCHAR(255) DEFAULT '',
    updated_at            TIMESTAMP NOT NULL DEFAULT NOW(),
    closed_at             TIMESTAMP,
    closed_by_session     VARCHAR(255) DEFAULT '',
    external_ref          VARCHAR(255),
    spec_id               VARCHAR(1024),
    compaction_level      INTEGER DEFAULT 0,
    compacted_at          TIMESTAMP,
    compacted_at_commit   VARCHAR(64),
    original_size         INTEGER,
    sender                VARCHAR(255) DEFAULT '',
    ephemeral             BOOLEAN DEFAULT FALSE,
    wisp_type             VARCHAR(32) DEFAULT '',
    pinned                BOOLEAN DEFAULT FALSE,
    is_template           BOOLEAN DEFAULT FALSE,
    mol_type              VARCHAR(32) DEFAULT '',
    work_type             VARCHAR(32) DEFAULT 'mutex',
    source_system         VARCHAR(255) DEFAULT '',
    metadata              JSONB DEFAULT '{}'::jsonb,
    source_repo           VARCHAR(512) DEFAULT '',
    close_reason          TEXT DEFAULT '',
    event_kind            VARCHAR(32) DEFAULT '',
    actor                 VARCHAR(255) DEFAULT '',
    target                VARCHAR(255) DEFAULT '',
    payload               TEXT DEFAULT '',
    await_type            VARCHAR(32) DEFAULT '',
    await_id              VARCHAR(255) DEFAULT '',
    timeout_ns            BIGINT DEFAULT 0,
    waiters               TEXT DEFAULT '',
    hook_bead             VARCHAR(255) DEFAULT '',
    role_bead             VARCHAR(255) DEFAULT '',
    agent_state           VARCHAR(32) DEFAULT '',
    last_activity         TIMESTAMP,
    role_type             VARCHAR(32) DEFAULT '',
    rig                   VARCHAR(255) DEFAULT '',
    due_at                TIMESTAMP,
    defer_until           TIMESTAMP,
    started_at            TIMESTAMP,
    no_history            BOOLEAN DEFAULT FALSE
);
CREATE INDEX IF NOT EXISTS idx_wisps_status        ON wisps (status);
CREATE INDEX IF NOT EXISTS idx_wisps_priority      ON wisps (priority);
CREATE INDEX IF NOT EXISTS idx_wisps_issue_type    ON wisps (issue_type);
CREATE INDEX IF NOT EXISTS idx_wisps_assignee      ON wisps (assignee);
CREATE INDEX IF NOT EXISTS idx_wisps_created_at    ON wisps (created_at);
CREATE INDEX IF NOT EXISTS idx_wisps_spec_id       ON wisps (spec_id);
CREATE INDEX IF NOT EXISTS idx_wisps_external_ref  ON wisps (external_ref);

DROP TRIGGER IF EXISTS wisps_set_updated_at ON wisps;
CREATE TRIGGER wisps_set_updated_at
    BEFORE UPDATE ON wisps
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Section 4: Dependencies + wisp_dependencies.
CREATE TABLE IF NOT EXISTS dependencies (
    issue_id        VARCHAR(255) NOT NULL,
    depends_on_id   VARCHAR(255) NOT NULL,
    type            VARCHAR(32) NOT NULL DEFAULT 'blocks',
    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    created_by      VARCHAR(255) NOT NULL DEFAULT '',
    metadata        JSONB DEFAULT '{}'::jsonb,
    thread_id       VARCHAR(255) DEFAULT '',
    PRIMARY KEY (issue_id, depends_on_id),
    CONSTRAINT fk_dep_issue FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_dependencies_issue              ON dependencies (issue_id);
CREATE INDEX IF NOT EXISTS idx_dependencies_depends_on         ON dependencies (depends_on_id);
CREATE INDEX IF NOT EXISTS idx_dependencies_depends_on_type    ON dependencies (depends_on_id, type);
CREATE INDEX IF NOT EXISTS idx_dependencies_thread             ON dependencies (thread_id);

CREATE TABLE IF NOT EXISTS wisp_dependencies (
    issue_id        VARCHAR(255) NOT NULL,
    depends_on_id   VARCHAR(255) NOT NULL,
    type            VARCHAR(32) NOT NULL DEFAULT 'blocks',
    created_at      TIMESTAMP DEFAULT NOW(),
    created_by      VARCHAR(255) DEFAULT '',
    metadata        JSONB DEFAULT '{}'::jsonb,
    thread_id       VARCHAR(255) DEFAULT '',
    PRIMARY KEY (issue_id, depends_on_id)
);
CREATE INDEX IF NOT EXISTS idx_wisp_dep_depends         ON wisp_dependencies (depends_on_id);
CREATE INDEX IF NOT EXISTS idx_wisp_dep_type            ON wisp_dependencies (type);
CREATE INDEX IF NOT EXISTS idx_wisp_dep_type_depends    ON wisp_dependencies (type, depends_on_id);

-- Section 5: Labels, comments, events (and wisp_* counterparts).
CREATE TABLE IF NOT EXISTS labels (
    issue_id   VARCHAR(255) NOT NULL,
    label      VARCHAR(255) NOT NULL,
    PRIMARY KEY (issue_id, label),
    CONSTRAINT fk_labels_issue FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_labels_label ON labels (label);

CREATE TABLE IF NOT EXISTS wisp_labels (
    issue_id   VARCHAR(255) NOT NULL,
    label      VARCHAR(255) NOT NULL,
    PRIMARY KEY (issue_id, label)
);
CREATE INDEX IF NOT EXISTS idx_wisp_labels_label ON wisp_labels (label);

CREATE TABLE IF NOT EXISTS comments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id    VARCHAR(255) NOT NULL,
    author      VARCHAR(255) NOT NULL,
    text        TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_comments_issue FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_comments_issue       ON comments (issue_id);
CREATE INDEX IF NOT EXISTS idx_comments_created_at  ON comments (created_at);

CREATE TABLE IF NOT EXISTS wisp_comments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id    VARCHAR(255) NOT NULL,
    author      VARCHAR(255) DEFAULT '',
    text        TEXT NOT NULL,
    created_at  TIMESTAMP DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wisp_comments_issue  ON wisp_comments (issue_id);

-- migration 0034: events.old_value/new_value are TEXT (PG has no length cap, so the
-- LONGTEXT widening from MySQL is a no-op here).
CREATE TABLE IF NOT EXISTS events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id    VARCHAR(255) NOT NULL,
    event_type  VARCHAR(32) NOT NULL,
    actor       VARCHAR(255) NOT NULL,
    old_value   TEXT,
    new_value   TEXT,
    comment     TEXT,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_events_issue FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_events_issue       ON events (issue_id);
CREATE INDEX IF NOT EXISTS idx_events_created_at  ON events (created_at);

CREATE TABLE IF NOT EXISTS wisp_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id    VARCHAR(255) NOT NULL,
    event_type  VARCHAR(32) NOT NULL,
    actor       VARCHAR(255) DEFAULT '',
    old_value   TEXT DEFAULT '',
    new_value   TEXT DEFAULT '',
    comment     TEXT DEFAULT '',
    created_at  TIMESTAMP DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wisp_events_issue       ON wisp_events (issue_id);
CREATE INDEX IF NOT EXISTS idx_wisp_events_created_at  ON wisp_events (created_at);

-- Section 6: Configuration (config + metadata + local_metadata + custom_*).
CREATE TABLE IF NOT EXISTS config (
    key    VARCHAR(255) PRIMARY KEY,
    value  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS metadata (
    key    VARCHAR(255) PRIMARY KEY,
    value  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS local_metadata (
    key    VARCHAR(255) PRIMARY KEY,
    value  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS custom_statuses (
    name      VARCHAR(64) PRIMARY KEY,
    category  VARCHAR(32) NOT NULL DEFAULT 'unspecified'
);

CREATE TABLE IF NOT EXISTS custom_types (
    name  VARCHAR(64) PRIMARY KEY
);

-- Seed default config (matches Dolt migration 0016).
INSERT INTO config (key, value) VALUES
    ('compaction_enabled', 'false'),
    ('compact_tier1_days', '30'),
    ('compact_tier1_dep_levels', '2'),
    ('compact_tier2_days', '90'),
    ('compact_tier2_dep_levels', '5'),
    ('compact_tier2_commits', '100'),
    ('compact_batch_size', '50'),
    ('compact_parallel_workers', '5'),
    ('auto_compact_enabled', 'false')
ON CONFLICT (key) DO NOTHING;

-- Section 7: Counters.
CREATE TABLE IF NOT EXISTS child_counters (
    parent_id   VARCHAR(255) PRIMARY KEY,
    last_child  INTEGER NOT NULL DEFAULT 0,
    CONSTRAINT fk_counter_parent FOREIGN KEY (parent_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS issue_counter (
    prefix   VARCHAR(255) PRIMARY KEY,
    last_id  INTEGER NOT NULL DEFAULT 0
);

-- Section 8: Compaction snapshots.
CREATE TABLE IF NOT EXISTS issue_snapshots (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id            VARCHAR(255) NOT NULL,
    snapshot_time       TIMESTAMP NOT NULL,
    compaction_level    INTEGER NOT NULL,
    original_size       INTEGER NOT NULL,
    compressed_size     INTEGER NOT NULL,
    original_content    TEXT NOT NULL,
    archived_events     TEXT,
    CONSTRAINT fk_snapshots_issue FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_snapshots_issue  ON issue_snapshots (issue_id);
CREATE INDEX IF NOT EXISTS idx_snapshots_level  ON issue_snapshots (compaction_level);

CREATE TABLE IF NOT EXISTS compaction_snapshots (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id          VARCHAR(255) NOT NULL,
    compaction_level  INTEGER NOT NULL,
    snapshot_json     BYTEA NOT NULL,
    created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_comp_snap_issue FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_comp_snap_issue
    ON compaction_snapshots (issue_id, compaction_level, created_at DESC);

-- Section 9: Operational.
CREATE TABLE IF NOT EXISTS repo_mtimes (
    repo_path     VARCHAR(512) PRIMARY KEY,
    jsonl_path    VARCHAR(512) NOT NULL,
    mtime_ns      BIGINT NOT NULL,
    last_checked  TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_repo_mtimes_checked ON repo_mtimes (last_checked);

-- Section 10: Views.
CREATE OR REPLACE VIEW ready_issues AS
WITH RECURSIVE
  blocked_directly AS (
    SELECT DISTINCT d.issue_id
    FROM dependencies d
    WHERE d.type = 'blocks'
      AND EXISTS (
        SELECT 1 FROM issues blocker
        WHERE blocker.id = d.depends_on_id
          AND blocker.status NOT IN ('closed', 'pinned')
      )
  ),
  blocked_transitively AS (
    SELECT issue_id, 0 AS depth FROM blocked_directly
    UNION ALL
    SELECT d.issue_id, bt.depth + 1
    FROM blocked_transitively bt
    JOIN dependencies d ON d.depends_on_id = bt.issue_id
    WHERE d.type = 'parent-child' AND bt.depth < 50
  )
SELECT i.*
FROM issues i
LEFT JOIN blocked_transitively bt ON bt.issue_id = i.id
WHERE (
    i.status = 'open'
    OR i.status IN (SELECT name FROM custom_statuses WHERE category = 'active')
  )
  AND (i.ephemeral = FALSE OR i.ephemeral IS NULL)
  AND bt.issue_id IS NULL
  AND (i.defer_until IS NULL OR i.defer_until <= NOW())
  AND NOT EXISTS (
    SELECT 1 FROM dependencies d_parent
    JOIN issues parent ON parent.id = d_parent.depends_on_id
    WHERE d_parent.issue_id = i.id
      AND d_parent.type = 'parent-child'
      AND parent.defer_until IS NOT NULL
      AND parent.defer_until > NOW()
  );

CREATE OR REPLACE VIEW blocked_issues AS
WITH done_frozen AS (
    SELECT name FROM custom_statuses WHERE category IN ('done', 'frozen')
)
SELECT
    i.*,
    (SELECT COUNT(*)
     FROM dependencies d
     WHERE d.issue_id = i.id
       AND d.type = 'blocks'
       AND EXISTS (
         SELECT 1 FROM issues blocker
         WHERE blocker.id = d.depends_on_id
           AND blocker.status NOT IN ('closed', 'pinned')
           AND blocker.status NOT IN (SELECT name FROM done_frozen)
       )
    ) AS blocked_by_count
FROM issues i
WHERE i.status NOT IN ('closed', 'pinned')
  AND i.status NOT IN (SELECT name FROM done_frozen)
  AND EXISTS (
    SELECT 1 FROM dependencies d
    WHERE d.issue_id = i.id
      AND d.type = 'blocks'
      AND EXISTS (
        SELECT 1 FROM issues blocker
        WHERE blocker.id = d.depends_on_id
          AND blocker.status NOT IN ('closed', 'pinned')
          AND blocker.status NOT IN (SELECT name FROM done_frozen)
      )
  );
