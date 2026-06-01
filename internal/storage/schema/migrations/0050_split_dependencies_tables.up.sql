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

DROP VIEW IF EXISTS ready_issues;
DROP VIEW IF EXISTS blocked_issues;

