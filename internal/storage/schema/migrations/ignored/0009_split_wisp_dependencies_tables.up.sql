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
