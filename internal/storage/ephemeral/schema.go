package ephemeral

// schema defines the SQLite-compatible database schema for ephemeral storage.
// This mirrors the Dolt schema but adapts for SQLite dialect:
// - No ON UPDATE CURRENT_TIMESTAMP (handled in Go)
// - JSON → TEXT
// - TINYINT → INTEGER
// - No AUTO_INCREMENT (use AUTOINCREMENT)
// - No DOLT_COMMIT
const schema = `
-- Issues table
CREATE TABLE IF NOT EXISTS issues (
    id TEXT PRIMARY KEY,
    content_hash TEXT,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    design TEXT NOT NULL DEFAULT '',
    acceptance_criteria TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',
    priority INTEGER NOT NULL DEFAULT 2,
    issue_type TEXT NOT NULL DEFAULT 'task',
    assignee TEXT,
    estimated_minutes INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S', 'now')),
    created_by TEXT DEFAULT '',
    owner TEXT DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S', 'now')),
    closed_at TEXT,
    closed_by_session TEXT DEFAULT '',
    external_ref TEXT,
    spec_id TEXT,
    compaction_level INTEGER DEFAULT 0,
    compacted_at TEXT,
    compacted_at_commit TEXT,
    original_size INTEGER,
    sender TEXT DEFAULT '',
    ephemeral INTEGER DEFAULT 0,
    wisp_type TEXT DEFAULT '',
    pinned INTEGER DEFAULT 0,
    is_template INTEGER DEFAULT 0,
    crystallizes INTEGER DEFAULT 0,
    mol_type TEXT DEFAULT '',
    work_type TEXT DEFAULT 'mutex',
    quality_score REAL,
    source_system TEXT DEFAULT '',
    metadata TEXT DEFAULT '{}',
    source_repo TEXT DEFAULT '',
    close_reason TEXT DEFAULT '',
    event_kind TEXT DEFAULT '',
    actor TEXT DEFAULT '',
    target TEXT DEFAULT '',
    payload TEXT DEFAULT '',
    await_type TEXT DEFAULT '',
    await_id TEXT DEFAULT '',
    timeout_ns INTEGER DEFAULT 0,
    waiters TEXT DEFAULT '',
    hook_bead TEXT DEFAULT '',
    role_bead TEXT DEFAULT '',
    agent_state TEXT DEFAULT '',
    last_activity TEXT,
    role_type TEXT DEFAULT '',
    rig TEXT DEFAULT '',
    due_at TEXT,
    defer_until TEXT
);

CREATE INDEX IF NOT EXISTS idx_eph_issues_status ON issues(status);
CREATE INDEX IF NOT EXISTS idx_eph_issues_priority ON issues(priority);
CREATE INDEX IF NOT EXISTS idx_eph_issues_type ON issues(issue_type);

-- Dependencies table
CREATE TABLE IF NOT EXISTS dependencies (
    issue_id TEXT NOT NULL,
    depends_on_id TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'blocks',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S', 'now')),
    created_by TEXT NOT NULL,
    metadata TEXT DEFAULT '{}',
    thread_id TEXT DEFAULT '',
    PRIMARY KEY (issue_id, depends_on_id),
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_eph_dep_issue ON dependencies(issue_id);
CREATE INDEX IF NOT EXISTS idx_eph_dep_depends ON dependencies(depends_on_id);

-- Labels table
CREATE TABLE IF NOT EXISTS labels (
    issue_id TEXT NOT NULL,
    label TEXT NOT NULL,
    PRIMARY KEY (issue_id, label),
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

-- Comments table
CREATE TABLE IF NOT EXISTS comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id TEXT NOT NULL,
    author TEXT NOT NULL,
    text TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S', 'now')),
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

-- Events table
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    actor TEXT NOT NULL,
    old_value TEXT,
    new_value TEXT,
    comment TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%S', 'now')),
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);

-- Config table
CREATE TABLE IF NOT EXISTS config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`
