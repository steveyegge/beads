//! Storage layer interface and implementations

pub mod sqlite;

use crate::types::*;
use anyhow::Result;
use std::collections::HashMap;

/// Storage trait defines the interface for issue storage backends
pub trait Storage: Send + Sync {
    // Issues
    fn create_issue(&self, issue: &Issue, actor: &str) -> Result<()>;
    fn create_issues(&self, issues: &[Issue], actor: &str) -> Result<()>;
    fn get_issue(&self, id: &str) -> Result<Option<Issue>>;
    fn get_issue_by_external_ref(&self, external_ref: &str) -> Result<Option<Issue>>;
    fn update_issue(&self, id: &str, updates: HashMap<String, String>, actor: &str) -> Result<()>;
    fn close_issue(&self, id: &str, reason: &str, actor: &str) -> Result<()>;
    fn delete_issue(&self, id: &str) -> Result<()>;
    fn search_issues(&self, query: &str, filter: &IssueFilter) -> Result<Vec<Issue>>;

    // Dependencies
    fn add_dependency(&self, dep: &Dependency, actor: &str) -> Result<()>;
    fn remove_dependency(&self, issue_id: &str, depends_on_id: &str, actor: &str) -> Result<()>;
    fn get_dependencies(&self, issue_id: &str) -> Result<Vec<Issue>>;
    fn get_dependents(&self, issue_id: &str) -> Result<Vec<Issue>>;
    fn get_dependency_records(&self, issue_id: &str) -> Result<Vec<Dependency>>;
    fn get_all_dependency_records(&self) -> Result<HashMap<String, Vec<Dependency>>>;
    fn get_dependency_counts(&self, issue_ids: &[String]) -> Result<HashMap<String, DependencyCounts>>;
    fn get_dependency_tree(&self, issue_id: &str, max_depth: i32, show_all_paths: bool, reverse: bool) -> Result<Vec<TreeNode>>;
    fn detect_cycles(&self) -> Result<Vec<Vec<Issue>>>;

    // Labels
    fn add_label(&self, issue_id: &str, label: &str, actor: &str) -> Result<()>;
    fn remove_label(&self, issue_id: &str, label: &str, actor: &str) -> Result<()>;
    fn get_labels(&self, issue_id: &str) -> Result<Vec<String>>;
    fn get_issues_by_label(&self, label: &str) -> Result<Vec<Issue>>;

    // Ready Work & Blocking
    fn get_ready_work(&self, filter: &WorkFilter) -> Result<Vec<Issue>>;
    fn get_blocked_issues(&self) -> Result<Vec<BlockedIssue>>;
    fn get_epics_eligible_for_closure(&self) -> Result<Vec<EpicStatus>>;
    fn get_stale_issues(&self, filter: &StaleFilter) -> Result<Vec<Issue>>;

    // Events
    fn add_comment(&self, issue_id: &str, actor: &str, comment: &str) -> Result<()>;
    fn get_events(&self, issue_id: &str, limit: i32) -> Result<Vec<Event>>;

    // Comments
    fn add_issue_comment(&self, issue_id: &str, author: &str, text: &str) -> Result<Comment>;
    fn get_issue_comments(&self, issue_id: &str) -> Result<Vec<Comment>>;

    // Statistics
    fn get_statistics(&self) -> Result<Statistics>;

    // Dirty tracking (for incremental JSONL export)
    fn get_dirty_issues(&self) -> Result<Vec<String>>;
    fn get_dirty_issue_hash(&self, issue_id: &str) -> Result<String>;
    fn clear_dirty_issues(&self) -> Result<()>;
    fn clear_dirty_issues_by_id(&self, issue_ids: &[String]) -> Result<()>;

    // Export hash tracking
    fn get_export_hash(&self, issue_id: &str) -> Result<Option<String>>;
    fn set_export_hash(&self, issue_id: &str, content_hash: &str) -> Result<()>;
    fn clear_all_export_hashes(&self) -> Result<()>;

    // JSONL file integrity
    fn get_jsonl_file_hash(&self) -> Result<Option<String>>;
    fn set_jsonl_file_hash(&self, file_hash: &str) -> Result<()>;

    // ID Generation
    fn get_next_child_id(&self, parent_id: &str) -> Result<String>;

    // Config
    fn set_config(&self, key: &str, value: &str) -> Result<()>;
    fn get_config(&self, key: &str) -> Result<Option<String>>;
    fn get_all_config(&self) -> Result<HashMap<String, String>>;
    fn delete_config(&self, key: &str) -> Result<()>;

    // Metadata
    fn set_metadata(&self, key: &str, value: &str) -> Result<()>;
    fn get_metadata(&self, key: &str) -> Result<Option<String>>;

    // Prefix rename operations
    fn update_issue_id(&self, old_id: &str, new_id: &str, issue: &Issue, actor: &str) -> Result<()>;
    fn rename_dependency_prefix(&self, old_prefix: &str, new_prefix: &str) -> Result<()>;
    fn rename_counter_prefix(&self, old_prefix: &str, new_prefix: &str) -> Result<()>;

    // Lifecycle
    fn close(&self) -> Result<()>;

    // Database path
    fn path(&self) -> &str;
}

/// Config holds database configuration
#[derive(Debug, Clone)]
pub struct Config {
    pub backend: String,
    pub path: String,
}

impl Config {
    pub fn sqlite(path: String) -> Self {
        Config {
            backend: "sqlite".to_string(),
            path,
        }
    }
}
