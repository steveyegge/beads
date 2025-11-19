//! SQLite storage implementation

use crate::storage::Storage;
use crate::types::*;
use anyhow::{Context, Result};
use chrono::Utc;
use rusqlite::{Connection, params, OptionalExtension};
use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};

const SCHEMA: &str = include_str!("sqlite_schema.sql");

/// SQLite storage implementation
pub struct SqliteStorage {
    conn: Arc<Mutex<Connection>>,
    path: PathBuf,
}

impl SqliteStorage {
    /// Create a new SQLite storage instance
    pub fn new(path: PathBuf) -> Result<Self> {
        let conn = Connection::open(&path)
            .with_context(|| format!("Failed to open database at {:?}", path))?;

        // Enable foreign keys
        conn.execute_batch("PRAGMA foreign_keys = ON;")?;

        let storage = SqliteStorage {
            conn: Arc::new(Mutex::new(conn)),
            path,
        };

        storage.initialize_schema()?;
        Ok(storage)
    }

    fn initialize_schema(&self) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute_batch(SCHEMA)?;
        Ok(())
    }

    fn record_event(
        &self,
        conn: &Connection,
        issue_id: &str,
        event_type: EventType,
        actor: &str,
        old_value: Option<&str>,
        new_value: Option<&str>,
        comment: Option<&str>,
    ) -> Result<()> {
        conn.execute(
            "INSERT INTO events (issue_id, event_type, actor, old_value, new_value, comment)
             VALUES (?, ?, ?, ?, ?, ?)",
            params![
                issue_id,
                serde_json::to_string(&event_type)?,
                actor,
                old_value,
                new_value,
                comment,
            ],
        )?;
        Ok(())
    }

    fn mark_dirty(&self, conn: &Connection, issue_id: &str) -> Result<()> {
        conn.execute(
            "INSERT OR REPLACE INTO dirty_issues (issue_id) VALUES (?)",
            params![issue_id],
        )?;
        Ok(())
    }
}

impl Storage for SqliteStorage {
    fn create_issue(&self, issue: &Issue, actor: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();

        conn.execute(
            "INSERT INTO issues (
                id, content_hash, title, description, design, acceptance_criteria, notes,
                status, priority, issue_type, assignee, estimated_minutes,
                created_at, updated_at, closed_at, external_ref,
                compaction_level, compacted_at, compacted_at_commit, original_size, source_repo
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            params![
                issue.id,
                issue.content_hash,
                issue.title,
                issue.description,
                issue.design,
                issue.acceptance_criteria,
                issue.notes,
                issue.status.as_str(),
                issue.priority,
                issue.issue_type.as_str(),
                issue.assignee,
                issue.estimated_minutes,
                issue.created_at,
                issue.updated_at,
                issue.closed_at,
                issue.external_ref,
                issue.compaction_level,
                issue.compacted_at,
                issue.compacted_at_commit,
                issue.original_size,
                issue.source_repo,
            ],
        )?;

        self.record_event(&conn, &issue.id, EventType::Created, actor, None, None, None)?;
        self.mark_dirty(&conn, &issue.id)?;

        Ok(())
    }

    fn create_issues(&self, issues: &[Issue], actor: &str) -> Result<()> {
        for issue in issues {
            self.create_issue(issue, actor)?;
        }
        Ok(())
    }

    fn get_issue(&self, id: &str) -> Result<Option<Issue>> {
        let conn = self.conn.lock().unwrap();

        let issue = conn
            .query_row(
                "SELECT id, content_hash, title, description, design, acceptance_criteria, notes,
                        status, priority, issue_type, assignee, estimated_minutes,
                        created_at, updated_at, closed_at, external_ref,
                        compaction_level, compacted_at, compacted_at_commit, original_size, source_repo
                 FROM issues WHERE id = ?",
                params![id],
                |row| {
                    Ok(Issue {
                        id: row.get(0)?,
                        content_hash: row.get(1)?,
                        title: row.get(2)?,
                        description: row.get(3)?,
                        design: row.get(4)?,
                        acceptance_criteria: row.get(5)?,
                        notes: row.get(6)?,
                        status: serde_json::from_str(&row.get::<_, String>(7)?).unwrap(),
                        priority: row.get(8)?,
                        issue_type: serde_json::from_str(&row.get::<_, String>(9)?).unwrap(),
                        assignee: row.get(10)?,
                        estimated_minutes: row.get(11)?,
                        created_at: row.get(12)?,
                        updated_at: row.get(13)?,
                        closed_at: row.get(14)?,
                        external_ref: row.get(15)?,
                        compaction_level: row.get(16)?,
                        compacted_at: row.get(17)?,
                        compacted_at_commit: row.get(18)?,
                        original_size: row.get(19)?,
                        source_repo: row.get(20)?,
                        labels: Vec::new(),
                        dependencies: Vec::new(),
                        comments: Vec::new(),
                    })
                },
            )
            .optional()?;

        Ok(issue)
    }

    fn get_issue_by_external_ref(&self, external_ref: &str) -> Result<Option<Issue>> {
        let conn = self.conn.lock().unwrap();

        let issue = conn
            .query_row(
                "SELECT id FROM issues WHERE external_ref = ?",
                params![external_ref],
                |row| row.get::<_, String>(0),
            )
            .optional()?;

        if let Some(id) = issue {
            self.get_issue(&id)
        } else {
            Ok(None)
        }
    }

    fn update_issue(&self, id: &str, updates: HashMap<String, String>, actor: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();

        for (key, value) in updates.iter() {
            let query = format!("UPDATE issues SET {} = ?, updated_at = ? WHERE id = ?", key);
            conn.execute(&query, params![value, Utc::now(), id])?;

            self.record_event(&conn, id, EventType::Updated, actor, None, Some(value), None)?;
        }

        self.mark_dirty(&conn, id)?;
        Ok(())
    }

    fn close_issue(&self, id: &str, reason: &str, actor: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();

        let now = Utc::now();
        conn.execute(
            "UPDATE issues SET status = ?, closed_at = ?, updated_at = ? WHERE id = ?",
            params!["closed", now, now, id],
        )?;

        self.record_event(&conn, id, EventType::Closed, actor, None, None, Some(reason))?;
        self.mark_dirty(&conn, id)?;

        Ok(())
    }

    fn delete_issue(&self, id: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute("DELETE FROM issues WHERE id = ?", params![id])?;
        Ok(())
    }

    fn search_issues(&self, query: &str, filter: &IssueFilter) -> Result<Vec<Issue>> {
        // TODO: Implement advanced search with filters
        Ok(Vec::new())
    }

    fn add_dependency(&self, dep: &Dependency, actor: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();

        conn.execute(
            "INSERT INTO dependencies (issue_id, depends_on_id, type, created_at, created_by)
             VALUES (?, ?, ?, ?, ?)",
            params![
                dep.issue_id,
                dep.depends_on_id,
                dep.dep_type.as_str(),
                dep.created_at,
                dep.created_by,
            ],
        )?;

        self.record_event(&conn, &dep.issue_id, EventType::DependencyAdded, actor, None, Some(&dep.depends_on_id), None)?;
        self.mark_dirty(&conn, &dep.issue_id)?;

        Ok(())
    }

    fn remove_dependency(&self, issue_id: &str, depends_on_id: &str, actor: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();

        conn.execute(
            "DELETE FROM dependencies WHERE issue_id = ? AND depends_on_id = ?",
            params![issue_id, depends_on_id],
        )?;

        self.record_event(&conn, issue_id, EventType::DependencyRemoved, actor, Some(depends_on_id), None, None)?;
        self.mark_dirty(&conn, issue_id)?;

        Ok(())
    }

    fn get_dependencies(&self, issue_id: &str) -> Result<Vec<Issue>> {
        // TODO: Implement
        Ok(Vec::new())
    }

    fn get_dependents(&self, issue_id: &str) -> Result<Vec<Issue>> {
        // TODO: Implement
        Ok(Vec::new())
    }

    fn get_dependency_records(&self, issue_id: &str) -> Result<Vec<Dependency>> {
        // TODO: Implement
        Ok(Vec::new())
    }

    fn get_all_dependency_records(&self) -> Result<HashMap<String, Vec<Dependency>>> {
        // TODO: Implement
        Ok(HashMap::new())
    }

    fn get_dependency_counts(&self, issue_ids: &[String]) -> Result<HashMap<String, DependencyCounts>> {
        // TODO: Implement
        Ok(HashMap::new())
    }

    fn get_dependency_tree(&self, issue_id: &str, max_depth: i32, show_all_paths: bool, reverse: bool) -> Result<Vec<TreeNode>> {
        // TODO: Implement
        Ok(Vec::new())
    }

    fn detect_cycles(&self) -> Result<Vec<Vec<Issue>>> {
        // TODO: Implement
        Ok(Vec::new())
    }

    fn add_label(&self, issue_id: &str, label: &str, actor: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();

        conn.execute(
            "INSERT OR IGNORE INTO labels (issue_id, label) VALUES (?, ?)",
            params![issue_id, label],
        )?;

        self.record_event(&conn, issue_id, EventType::LabelAdded, actor, None, Some(label), None)?;
        self.mark_dirty(&conn, issue_id)?;

        Ok(())
    }

    fn remove_label(&self, issue_id: &str, label: &str, actor: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();

        conn.execute(
            "DELETE FROM labels WHERE issue_id = ? AND label = ?",
            params![issue_id, label],
        )?;

        self.record_event(&conn, issue_id, EventType::LabelRemoved, actor, Some(label), None, None)?;
        self.mark_dirty(&conn, issue_id)?;

        Ok(())
    }

    fn get_labels(&self, issue_id: &str) -> Result<Vec<String>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT label FROM labels WHERE issue_id = ?")?;
        let labels = stmt
            .query_map(params![issue_id], |row| row.get(0))?
            .collect::<Result<Vec<String>, _>>()?;
        Ok(labels)
    }

    fn get_issues_by_label(&self, label: &str) -> Result<Vec<Issue>> {
        // TODO: Implement
        Ok(Vec::new())
    }

    fn get_ready_work(&self, filter: &WorkFilter) -> Result<Vec<Issue>> {
        // TODO: Implement using ready_issues view
        Ok(Vec::new())
    }

    fn get_blocked_issues(&self) -> Result<Vec<BlockedIssue>> {
        // TODO: Implement using blocked_issues view
        Ok(Vec::new())
    }

    fn get_epics_eligible_for_closure(&self) -> Result<Vec<EpicStatus>> {
        // TODO: Implement
        Ok(Vec::new())
    }

    fn get_stale_issues(&self, filter: &StaleFilter) -> Result<Vec<Issue>> {
        // TODO: Implement
        Ok(Vec::new())
    }

    fn add_comment(&self, issue_id: &str, actor: &str, comment: &str) -> Result<()> {
        self.add_issue_comment(issue_id, actor, comment)?;
        Ok(())
    }

    fn get_events(&self, issue_id: &str, limit: i32) -> Result<Vec<Event>> {
        // TODO: Implement
        Ok(Vec::new())
    }

    fn add_issue_comment(&self, issue_id: &str, author: &str, text: &str) -> Result<Comment> {
        let conn = self.conn.lock().unwrap();

        conn.execute(
            "INSERT INTO comments (issue_id, author, text) VALUES (?, ?, ?)",
            params![issue_id, author, text],
        )?;

        let id = conn.last_insert_rowid();

        self.record_event(&conn, issue_id, EventType::Commented, author, None, None, Some(text))?;
        self.mark_dirty(&conn, issue_id)?;

        Ok(Comment {
            id,
            issue_id: issue_id.to_string(),
            author: author.to_string(),
            text: text.to_string(),
            created_at: Utc::now(),
        })
    }

    fn get_issue_comments(&self, issue_id: &str) -> Result<Vec<Comment>> {
        // TODO: Implement
        Ok(Vec::new())
    }

    fn get_statistics(&self) -> Result<Statistics> {
        // TODO: Implement
        Ok(Statistics {
            total_issues: 0,
            open_issues: 0,
            in_progress_issues: 0,
            closed_issues: 0,
            blocked_issues: 0,
            ready_issues: 0,
            epics_eligible_for_closure: 0,
            average_lead_time_hours: 0.0,
        })
    }

    fn get_dirty_issues(&self) -> Result<Vec<String>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT issue_id FROM dirty_issues ORDER BY marked_at")?;
        let ids = stmt
            .query_map([], |row| row.get(0))?
            .collect::<Result<Vec<String>, _>>()?;
        Ok(ids)
    }

    fn get_dirty_issue_hash(&self, issue_id: &str) -> Result<String> {
        // TODO: Implement
        Ok(String::new())
    }

    fn clear_dirty_issues(&self) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute("DELETE FROM dirty_issues", [])?;
        Ok(())
    }

    fn clear_dirty_issues_by_id(&self, issue_ids: &[String]) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        for id in issue_ids {
            conn.execute("DELETE FROM dirty_issues WHERE issue_id = ?", params![id])?;
        }
        Ok(())
    }

    fn get_export_hash(&self, issue_id: &str) -> Result<Option<String>> {
        let conn = self.conn.lock().unwrap();
        let hash = conn
            .query_row(
                "SELECT content_hash FROM export_hashes WHERE issue_id = ?",
                params![issue_id],
                |row| row.get(0),
            )
            .optional()?;
        Ok(hash)
    }

    fn set_export_hash(&self, issue_id: &str, content_hash: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT OR REPLACE INTO export_hashes (issue_id, content_hash) VALUES (?, ?)",
            params![issue_id, content_hash],
        )?;
        Ok(())
    }

    fn clear_all_export_hashes(&self) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute("DELETE FROM export_hashes", [])?;
        Ok(())
    }

    fn get_jsonl_file_hash(&self) -> Result<Option<String>> {
        self.get_metadata("jsonl_file_hash")
    }

    fn set_jsonl_file_hash(&self, file_hash: &str) -> Result<()> {
        self.set_metadata("jsonl_file_hash", file_hash)
    }

    fn get_next_child_id(&self, parent_id: &str) -> Result<String> {
        let conn = self.conn.lock().unwrap();

        let last_child: i32 = conn
            .query_row(
                "SELECT last_child FROM child_counters WHERE parent_id = ?",
                params![parent_id],
                |row| row.get(0),
            )
            .unwrap_or(0);

        let next_child = last_child + 1;

        conn.execute(
            "INSERT OR REPLACE INTO child_counters (parent_id, last_child) VALUES (?, ?)",
            params![parent_id, next_child],
        )?;

        Ok(format!("{}.{}", parent_id, next_child))
    }

    fn set_config(&self, key: &str, value: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT OR REPLACE INTO config (key, value) VALUES (?, ?)",
            params![key, value],
        )?;
        Ok(())
    }

    fn get_config(&self, key: &str) -> Result<Option<String>> {
        let conn = self.conn.lock().unwrap();
        let value = conn
            .query_row("SELECT value FROM config WHERE key = ?", params![key], |row| {
                row.get(0)
            })
            .optional()?;
        Ok(value)
    }

    fn get_all_config(&self) -> Result<HashMap<String, String>> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT key, value FROM config")?;
        let config = stmt
            .query_map([], |row| Ok((row.get(0)?, row.get(1)?)))?
            .collect::<Result<HashMap<String, String>, _>>()?;
        Ok(config)
    }

    fn delete_config(&self, key: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute("DELETE FROM config WHERE key = ?", params![key])?;
        Ok(())
    }

    fn set_metadata(&self, key: &str, value: &str) -> Result<()> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)",
            params![key, value],
        )?;
        Ok(())
    }

    fn get_metadata(&self, key: &str) -> Result<Option<String>> {
        let conn = self.conn.lock().unwrap();
        let value = conn
            .query_row("SELECT value FROM metadata WHERE key = ?", params![key], |row| {
                row.get(0)
            })
            .optional()?;
        Ok(value)
    }

    fn update_issue_id(&self, old_id: &str, new_id: &str, issue: &Issue, actor: &str) -> Result<()> {
        // TODO: Implement
        Ok(())
    }

    fn rename_dependency_prefix(&self, old_prefix: &str, new_prefix: &str) -> Result<()> {
        // TODO: Implement
        Ok(())
    }

    fn rename_counter_prefix(&self, old_prefix: &str, new_prefix: &str) -> Result<()> {
        // TODO: Implement
        Ok(())
    }

    fn close(&self) -> Result<()> {
        // Connection will be closed when dropped
        Ok(())
    }

    fn path(&self) -> &str {
        self.path.to_str().unwrap_or("")
    }
}
