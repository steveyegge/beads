//! Core data structures for the bd issue tracker.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::fmt;

/// Issue represents a trackable work item
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Issue {
    pub id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub content_hash: Option<String>,
    pub title: String,
    pub description: String,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub design: String,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub acceptance_criteria: String,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub notes: String,
    pub status: Status,
    pub priority: i32,
    pub issue_type: IssueType,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub assignee: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub estimated_minutes: Option<i32>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub closed_at: Option<DateTime<Utc>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub external_ref: Option<String>,
    #[serde(skip_serializing_if = "is_zero", default)]
    pub compaction_level: i32,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub compacted_at: Option<DateTime<Utc>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub compacted_at_commit: Option<String>,
    #[serde(skip_serializing_if = "is_zero", default)]
    pub original_size: i32,
    #[serde(skip_serializing_if = "String::is_empty", default)]
    pub source_repo: String,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub labels: Vec<String>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub dependencies: Vec<Dependency>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub comments: Vec<Comment>,
}

fn is_zero(n: &i32) -> bool {
    *n == 0
}

impl Issue {
    /// Creates a deterministic hash of the issue's content.
    /// Uses all substantive fields (excluding ID, timestamps, and compaction metadata)
    /// to ensure that identical content produces identical hashes across all clones.
    pub fn compute_content_hash(&self) -> String {
        let mut hasher = Sha256::new();

        // Hash all substantive fields in a stable order
        hasher.update(self.title.as_bytes());
        hasher.update(&[0u8]); // separator
        hasher.update(self.description.as_bytes());
        hasher.update(&[0u8]);
        hasher.update(self.design.as_bytes());
        hasher.update(&[0u8]);
        hasher.update(self.acceptance_criteria.as_bytes());
        hasher.update(&[0u8]);
        hasher.update(self.notes.as_bytes());
        hasher.update(&[0u8]);
        hasher.update(self.status.as_str().as_bytes());
        hasher.update(&[0u8]);
        hasher.update(format!("{}", self.priority).as_bytes());
        hasher.update(&[0u8]);
        hasher.update(self.issue_type.as_str().as_bytes());
        hasher.update(&[0u8]);
        hasher.update(self.assignee.as_bytes());
        hasher.update(&[0u8]);

        if let Some(ref external_ref) = self.external_ref {
            hasher.update(external_ref.as_bytes());
        }

        format!("{:x}", hasher.finalize())
    }

    /// Validates if the issue has valid field values
    pub fn validate(&self) -> Result<(), String> {
        if self.title.is_empty() {
            return Err("title is required".to_string());
        }
        if self.title.len() > 500 {
            return Err(format!(
                "title must be 500 characters or less (got {})",
                self.title.len()
            ));
        }
        if !(0..=4).contains(&self.priority) {
            return Err(format!(
                "priority must be between 0 and 4 (got {})",
                self.priority
            ));
        }
        if !self.status.is_valid() {
            return Err(format!("invalid status: {}", self.status.as_str()));
        }
        if !self.issue_type.is_valid() {
            return Err(format!("invalid issue type: {}", self.issue_type.as_str()));
        }
        if let Some(minutes) = self.estimated_minutes {
            if minutes < 0 {
                return Err("estimated_minutes cannot be negative".to_string());
            }
        }
        // Enforce closed_at invariant: closed_at should be set if and only if status is closed
        if self.status == Status::Closed && self.closed_at.is_none() {
            return Err("closed issues must have closed_at timestamp".to_string());
        }
        if self.status != Status::Closed && self.closed_at.is_some() {
            return Err("non-closed issues cannot have closed_at timestamp".to_string());
        }
        Ok(())
    }
}

/// Status represents the current state of an issue
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Status {
    Open,
    InProgress,
    Blocked,
    Closed,
}

impl Status {
    pub fn is_valid(&self) -> bool {
        true // All enum variants are valid
    }

    pub fn as_str(&self) -> &'static str {
        match self {
            Status::Open => "open",
            Status::InProgress => "in_progress",
            Status::Blocked => "blocked",
            Status::Closed => "closed",
        }
    }
}

impl fmt::Display for Status {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.as_str())
    }
}

/// IssueType categorizes the kind of work
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum IssueType {
    Bug,
    Feature,
    Task,
    Epic,
    Chore,
}

impl IssueType {
    pub fn is_valid(&self) -> bool {
        true // All enum variants are valid
    }

    pub fn as_str(&self) -> &'static str {
        match self {
            IssueType::Bug => "bug",
            IssueType::Feature => "feature",
            IssueType::Task => "task",
            IssueType::Epic => "epic",
            IssueType::Chore => "chore",
        }
    }
}

impl fmt::Display for IssueType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.as_str())
    }
}

/// Dependency represents a relationship between issues
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Dependency {
    pub issue_id: String,
    pub depends_on_id: String,
    #[serde(rename = "type")]
    pub dep_type: DependencyType,
    pub created_at: DateTime<Utc>,
    pub created_by: String,
}

/// DependencyCounts holds counts for dependencies and dependents
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DependencyCounts {
    pub dependency_count: i32,
    pub dependent_count: i32,
}

/// IssueWithDependencyMetadata extends Issue with dependency relationship type
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IssueWithDependencyMetadata {
    #[serde(flatten)]
    pub issue: Issue,
    pub dependency_type: DependencyType,
}

/// IssueWithCounts extends Issue with dependency relationship counts
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IssueWithCounts {
    #[serde(flatten)]
    pub issue: Issue,
    pub dependency_count: i32,
    pub dependent_count: i32,
}

/// DependencyType categorizes the relationship
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum DependencyType {
    Blocks,
    Related,
    ParentChild,
    DiscoveredFrom,
}

impl DependencyType {
    pub fn is_valid(&self) -> bool {
        true // All enum variants are valid
    }

    pub fn as_str(&self) -> &'static str {
        match self {
            DependencyType::Blocks => "blocks",
            DependencyType::Related => "related",
            DependencyType::ParentChild => "parent-child",
            DependencyType::DiscoveredFrom => "discovered-from",
        }
    }
}

/// Label represents a tag on an issue
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Label {
    pub issue_id: String,
    pub label: String,
}

/// Comment represents a comment on an issue
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Comment {
    pub id: i64,
    pub issue_id: String,
    pub author: String,
    pub text: String,
    pub created_at: DateTime<Utc>,
}

/// Event represents an audit trail entry
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Event {
    pub id: i64,
    pub issue_id: String,
    pub event_type: EventType,
    pub actor: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub old_value: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub new_value: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub comment: Option<String>,
    pub created_at: DateTime<Utc>,
}

/// EventType categorizes audit trail events
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum EventType {
    Created,
    Updated,
    StatusChanged,
    Commented,
    Closed,
    Reopened,
    DependencyAdded,
    DependencyRemoved,
    LabelAdded,
    LabelRemoved,
    Compacted,
}

/// BlockedIssue extends Issue with blocking information
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BlockedIssue {
    #[serde(flatten)]
    pub issue: Issue,
    pub blocked_by_count: i32,
    pub blocked_by: Vec<String>,
}

/// TreeNode represents a node in a dependency tree
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TreeNode {
    #[serde(flatten)]
    pub issue: Issue,
    pub depth: i32,
    pub parent_id: String,
    pub truncated: bool,
}

/// Statistics provides aggregate metrics
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Statistics {
    pub total_issues: i32,
    pub open_issues: i32,
    pub in_progress_issues: i32,
    pub closed_issues: i32,
    pub blocked_issues: i32,
    pub ready_issues: i32,
    pub epics_eligible_for_closure: i32,
    pub average_lead_time_hours: f64,
}

/// IssueFilter is used to filter issue queries
#[derive(Debug, Clone, Default)]
pub struct IssueFilter {
    pub status: Option<Status>,
    pub priority: Option<i32>,
    pub issue_type: Option<IssueType>,
    pub assignee: Option<String>,
    pub labels: Vec<String>,      // AND semantics
    pub labels_any: Vec<String>,  // OR semantics
    pub title_search: String,
    pub ids: Vec<String>,
    pub limit: i32,

    // Pattern matching
    pub title_contains: String,
    pub description_contains: String,
    pub notes_contains: String,

    // Date ranges
    pub created_after: Option<DateTime<Utc>>,
    pub created_before: Option<DateTime<Utc>>,
    pub updated_after: Option<DateTime<Utc>>,
    pub updated_before: Option<DateTime<Utc>>,
    pub closed_after: Option<DateTime<Utc>>,
    pub closed_before: Option<DateTime<Utc>>,

    // Empty/null checks
    pub empty_description: bool,
    pub no_assignee: bool,
    pub no_labels: bool,

    // Numeric ranges
    pub priority_min: Option<i32>,
    pub priority_max: Option<i32>,
}

/// SortPolicy determines how ready work is ordered
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum SortPolicy {
    /// Prioritizes recent issues by priority, older by age (recent = created within 48 hours)
    Hybrid,
    /// Always sorts by priority first, then creation date
    Priority,
    /// Always sorts by creation date (oldest first)
    Oldest,
}

impl SortPolicy {
    pub fn is_valid(&self) -> bool {
        true // All enum variants are valid
    }
}

impl Default for SortPolicy {
    fn default() -> Self {
        SortPolicy::Hybrid
    }
}

/// WorkFilter is used to filter ready work queries
#[derive(Debug, Clone)]
pub struct WorkFilter {
    pub status: Status,
    pub priority: Option<i32>,
    pub assignee: Option<String>,
    pub labels: Vec<String>,     // AND semantics
    pub labels_any: Vec<String>, // OR semantics
    pub limit: i32,
    pub sort_policy: SortPolicy,
}

impl Default for WorkFilter {
    fn default() -> Self {
        WorkFilter {
            status: Status::Open,
            priority: None,
            assignee: None,
            labels: Vec::new(),
            labels_any: Vec::new(),
            limit: 0,
            sort_policy: SortPolicy::default(),
        }
    }
}

/// StaleFilter is used to filter stale issue queries
#[derive(Debug, Clone)]
pub struct StaleFilter {
    pub days: i32,
    pub status: String,
    pub limit: i32,
}

/// EpicStatus represents an epic with its completion status
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EpicStatus {
    pub epic: Issue,
    pub total_children: i32,
    pub closed_children: i32,
    pub eligible_for_close: bool,
}
