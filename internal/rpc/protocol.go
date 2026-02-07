package rpc

import (
	"encoding/json"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// Operation constants for all bd commands
const (
	OpPing            = "ping"
	OpStatus          = "status"
	OpHealth          = "health"
	OpMetrics         = "metrics"
	OpCreate          = "create"
	OpUpdate            = "update"
	OpUpdateWithComment = "update_with_comment"
	OpClose             = "close"
	OpList            = "list"
	OpCount           = "count"
	OpShow            = "show"
	OpReady           = "ready"
	OpBlocked         = "blocked"
	OpStale           = "stale"
	OpStats           = "stats"
	OpDepAdd          = "dep_add"
	OpDepRemove       = "dep_remove"
	OpDepTree         = "dep_tree"
	OpDepAddBidirectional    = "dep_add_bidirectional"
	OpDepRemoveBidirectional = "dep_remove_bidirectional"
	OpLabelAdd        = "label_add"
	OpLabelRemove     = "label_remove"
	OpCommentList     = "comment_list"
	OpCommentAdd      = "comment_add"
	OpBatch           = "batch"
	OpResolveID       = "resolve_id"

	OpCompact         = "compact"
	OpCompactStats    = "compact_stats"
	OpExport          = "export"
	OpImport          = "import"
	OpEpicStatus      = "epic_status"
	OpGetMutations        = "get_mutations"
	OpGetMoleculeProgress = "get_molecule_progress"
	OpShutdown            = "shutdown"
	OpDelete              = "delete"
	OpRename              = "rename"
	OpGetWorkerStatus     = "get_worker_status"
	OpGetConfig           = "get_config"

	// Gate operations
	OpGateCreate = "gate_create"
	OpGateList   = "gate_list"
	OpGateShow   = "gate_show"
	OpGateClose  = "gate_close"
	OpGateWait   = "gate_wait"

	// Decision point operations
	OpDecisionCreate  = "decision_create"
	OpDecisionGet     = "decision_get"
	OpDecisionResolve = "decision_resolve"
	OpDecisionList    = "decision_list"
	OpDecisionRemind  = "decision_remind"
	OpDecisionCancel  = "decision_cancel"

	// Mol operations (gt-as9kdm)
	OpMolBond          = "mol_bond"
	OpMolSquash        = "mol_squash"
	OpMolBurn          = "mol_burn"
	OpMolCurrent       = "mol_current"
	OpMolProgressStats = "mol_progress_stats"
	OpMolReadyGated    = "mol_ready_gated"

	// Close operations (bd-ympw)
	OpCloseContinue = "close_continue"

	// Watch operations (bd-la75)
	OpListWatch = "list_watch"

	// Config operations (bd-wmil)
	OpConfigSet   = "config_set"
	OpConfigList  = "config_list"
	OpConfigUnset = "config_unset"

	// Types operation (bd-s091)
	OpTypes = "types"

	// Sync operations (bd-wn2g)
	OpSyncExport = "sync_export"
	OpSyncStatus = "sync_status"

	// State operations (atomic set-state)
	OpSetState = "set_state"

	// Atomic creation with dependencies (for template cloning)
	OpCreateWithDeps = "create_with_deps"

	// Batch label operations (atomic multi-label add)
	OpBatchAddLabels = "batch_add_labels"

	// Molecule operations (bd-jjbl)
	OpCreateMolecule = "create_molecule"

	// Batch operations
	OpBatchAddDependencies = "batch_add_dependencies"
	OpBatchQueryWorkers    = "batch_query_workers"

	// Convoy operations (atomic convoy creation with tracking)
	OpCreateConvoyWithTracking = "create_convoy_with_tracking"

	// Atomic closure chain operation (for MR completion)
	OpAtomicClosureChain = "atomic_closure_chain"

	// Init and Migrate operations (remote database management)
	OpInit    = "init"
	OpMigrate = "migrate"

	// Additional write operations (bd-wj80)
	OpRenamePrefix = "rename_prefix"
	OpMove         = "move"
	OpRefile       = "refile"
	OpCook         = "cook"
	OpPour         = "pour"

	// Formula CRUD operations (gt-pozvwr.24.9)
	OpFormulaList   = "formula_list"
	OpFormulaGet    = "formula_get"
	OpFormulaSave   = "formula_save"
	OpFormulaDelete = "formula_delete"

	// Runbook CRUD operations
	OpRunbookList = "runbook_list"
	OpRunbookGet  = "runbook_get"
	OpRunbookSave = "runbook_save"

	// Event bus operations (gt-wfaq5n)
	OpBusEmit     = "bus_emit"
	OpBusStatus   = "bus_status"
	OpBusHandlers = "bus_handlers"

	// Agent pod operations (gt-el7sxq.7)
	OpAgentPodRegister   = "agent_pod_register"
	OpAgentPodDeregister = "agent_pod_deregister"
	OpAgentPodStatus     = "agent_pod_status"
	OpAgentPodList       = "agent_pod_list"

	// VCS operations (bd-ma0s.2)
	OpVcsCommit        = "vcs_commit"
	OpVcsPush          = "vcs_push"
	OpVcsPull          = "vcs_pull"
	OpVcsMerge         = "vcs_merge"
	OpVcsBranchCreate  = "vcs_branch_create"
	OpVcsBranchDelete  = "vcs_branch_delete"
	OpVcsCheckout      = "vcs_checkout"
	OpVcsActiveBranch  = "vcs_active_branch"
	OpVcsStatus        = "vcs_status"
	OpVcsHasUncommitted = "vcs_has_uncommitted"
	OpVcsBranches      = "vcs_branches"
	OpVcsCurrentCommit = "vcs_current_commit"
	OpVcsCommitExists  = "vcs_commit_exists"
	OpVcsLog           = "vcs_log"

	// Admin operations (bd-ma0s.5)
	OpAdminGC = "admin_gc"

	// Federation operations (bd-ma0s.4)
	OpFedListRemotes  = "fed_list_remotes"
	OpFedSync         = "fed_sync"
	OpFedSyncStatus   = "fed_sync_status"
	OpFedFetch        = "fed_fetch"
	OpFedPushTo       = "fed_push_to"
	OpFedPullFrom     = "fed_pull_from"
	OpFedAddRemote    = "fed_add_remote"
	OpFedRemoveRemote = "fed_remove_remote"
	OpFedAddPeer      = "fed_add_peer"

	// History query operations (bd-ma0s.3)
	OpHistoryIssue            = "history_issue"
	OpHistoryDiff             = "history_diff"
	OpHistoryIssueDiff        = "history_issue_diff"
	OpHistoryConflicts        = "history_conflicts"
	OpHistoryResolveConflicts = "history_resolve_conflicts"
	OpVersionedDiff           = "versioned_diff"
)

// Request represents an RPC request from client to daemon
type Request struct {
	Operation     string          `json:"operation"`
	Args          json.RawMessage `json:"args"`
	Actor         string          `json:"actor,omitempty"`
	RequestID     string          `json:"request_id,omitempty"`
	Cwd           string          `json:"cwd,omitempty"`            // Working directory for database discovery
	ClientVersion string          `json:"client_version,omitempty"` // Client version for compatibility checks
	ExpectedDB    string          `json:"expected_db,omitempty"`    // Expected database path for validation (absolute)
	Token         string          `json:"token,omitempty"`          // Authentication token for TCP connections
}

// Response represents an RPC response from daemon to client
type Response struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// CreateArgs represents arguments for the create operation
type CreateArgs struct {
	ID                 string   `json:"id,omitempty"`
	Parent             string   `json:"parent,omitempty"` // Parent ID for hierarchical issues
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	IssueType          string   `json:"issue_type"`
	Priority           int      `json:"priority"`
	Design             string   `json:"design,omitempty"`
	AcceptanceCriteria string   `json:"acceptance_criteria,omitempty"`
	Notes              string   `json:"notes,omitempty"`
	Assignee           string   `json:"assignee,omitempty"`
	ExternalRef        string   `json:"external_ref,omitempty"`  // Link to external issue trackers
	EstimatedMinutes   *int     `json:"estimated_minutes,omitempty"` // Time estimate in minutes
	Labels             []string `json:"labels,omitempty"`
	Dependencies       []string `json:"dependencies,omitempty"`
	// Waits-for dependencies
	WaitsFor     string `json:"waits_for,omitempty"`      // Spawner issue ID to wait for
	WaitsForGate string `json:"waits_for_gate,omitempty"` // Gate type: all-children or any-children
	// Messaging fields
	Sender    string `json:"sender,omitempty"`    // Who sent this (for messages)
	Ephemeral bool   `json:"ephemeral,omitempty"` // If true, not exported to JSONL; bulk-deleted when closed
	Pinned    bool   `json:"pinned,omitempty"`    // If true, keeps visible (used for agent beads)
	AutoClose bool   `json:"auto_close,omitempty"` // If true, epic auto-closes when all children close
	RepliesTo string `json:"replies_to,omitempty"` // Issue ID for conversation threading
	// ID generation
	IDPrefix  string `json:"id_prefix,omitempty"`  // Override prefix for ID generation (mol, eph, etc.)
	TargetRig string `json:"target_rig,omitempty"` // Create in different rig (resolves prefix from route beads)
	Prefix    string `json:"prefix,omitempty"`     // Directly override issue prefix (from local config.yaml, gt-wnbjj8.3)
	CreatedBy string `json:"created_by,omitempty"` // Who created the issue
	Owner     string `json:"owner,omitempty"`      // Human owner for CV attribution (git author email)
	// Molecule type (for swarm coordination)
	MolType string `json:"mol_type,omitempty"` // swarm, patrol, or work (default)
	// Agent identity fields (only valid when IssueType == "agent")
	RoleType string `json:"role_type,omitempty"` // polecat|crew|witness|refinery|mayor|deacon
	Rig      string `json:"rig,omitempty"`       // Rig name (empty for town-level agents)
	// Event fields (only valid when IssueType == "event")
	EventCategory string `json:"event_category,omitempty"` // Namespaced category (e.g., patrol.muted, agent.started)
	EventActor    string `json:"event_actor,omitempty"`    // Entity URI who caused this event
	EventTarget   string `json:"event_target,omitempty"`   // Entity URI or bead ID affected
	EventPayload  string `json:"event_payload,omitempty"`  // Event-specific JSON data
	// Time-based scheduling fields (GH#820)
	DueAt      string `json:"due_at,omitempty"`      // Relative or ISO format due date
	DeferUntil string `json:"defer_until,omitempty"` // Relative or ISO format defer date
	// Gate fields (async coordination - hq-b0b22c.3)
	AwaitType string        `json:"await_type,omitempty"` // Condition type: gh:run, gh:pr, timer, human, mail, decision
	AwaitID   string        `json:"await_id,omitempty"`   // Condition identifier (run ID, PR number, etc.)
	Timeout   time.Duration `json:"timeout,omitempty"`    // Max wait time before escalation
	Waiters   []string      `json:"waiters,omitempty"`    // Mail addresses to notify when gate clears
	// Skill fields (only valid when IssueType == "skill")
	SkillName       string   `json:"skill_name,omitempty"`
	SkillVersion    string   `json:"skill_version,omitempty"`
	SkillCategory   string   `json:"skill_category,omitempty"`
	SkillInputs     []string `json:"skill_inputs,omitempty"`
	SkillOutputs    []string `json:"skill_outputs,omitempty"`
	SkillExamples   []string `json:"skill_examples,omitempty"`
	ClaudeSkillPath string   `json:"claude_skill_path,omitempty"` // DEPRECATED: Use SkillContent
	SkillContent    string   `json:"skill_content,omitempty"`     // Full SKILL.md content
	// NOTE: Legacy advice targeting fields (AdviceTargetRig, AdviceTargetRole, AdviceTargetAgent)
	// have been removed. Use labels instead: rig:X, role:Y, agent:Z, global.
	// Advice hook fields (hq--uaim)
	AdviceHookCommand   string `json:"advice_hook_command,omitempty"`    // Command to execute
	AdviceHookTrigger   string `json:"advice_hook_trigger,omitempty"`    // Trigger: session-end, before-commit, before-push, before-handoff
	AdviceHookTimeout   int    `json:"advice_hook_timeout,omitempty"`    // Timeout in seconds
	AdviceHookOnFailure string `json:"advice_hook_on_failure,omitempty"` // Failure behavior: block, warn, ignore
}

// UpdateArgs represents arguments for the update operation
type UpdateArgs struct {
	ID                 string   `json:"id"`
	Title              *string  `json:"title,omitempty"`
	Description        *string  `json:"description,omitempty"`
	Status             *string  `json:"status,omitempty"`
	Priority           *int     `json:"priority,omitempty"`
	Design             *string  `json:"design,omitempty"`
	AcceptanceCriteria *string  `json:"acceptance_criteria,omitempty"`
	Notes              *string  `json:"notes,omitempty"`
	Assignee           *string  `json:"assignee,omitempty"`
	ExternalRef        *string  `json:"external_ref,omitempty"` // Link to external issue trackers
	EstimatedMinutes   *int     `json:"estimated_minutes,omitempty"` // Time estimate in minutes
	IssueType          *string  `json:"issue_type,omitempty"`        // Issue type (bug|feature|task|epic|chore)
	AddLabels          []string `json:"add_labels,omitempty"`
	RemoveLabels       []string `json:"remove_labels,omitempty"`
	SetLabels          []string `json:"set_labels,omitempty"`
	// Messaging fields
	Sender    *string `json:"sender,omitempty"`    // Who sent this (for messages)
	Ephemeral *bool   `json:"ephemeral,omitempty"` // If true, not exported to JSONL; bulk-deleted when closed
	RepliesTo *string `json:"replies_to,omitempty"` // Issue ID for conversation threading
	// Graph link fields
	RelatesTo    *string `json:"relates_to,omitempty"`    // JSON array of related issue IDs
	DuplicateOf  *string `json:"duplicate_of,omitempty"`  // Canonical issue ID if duplicate
	SupersededBy *string `json:"superseded_by,omitempty"` // Replacement issue ID if obsolete
	// Pinned field
	Pinned *bool `json:"pinned,omitempty"` // If true, issue is a persistent context marker
	// Reparenting field
	Parent *string `json:"parent,omitempty"` // New parent issue ID (reparents the issue)
	// Agent slot fields
	HookBead *string `json:"hook_bead,omitempty"` // Current work on agent's hook (0..1)
	RoleBead *string `json:"role_bead,omitempty"` // Role definition bead for agent
	// Agent state fields
	AgentState   *string `json:"agent_state,omitempty"`   // Agent state (idle|running|stuck|stopped|dead)
	LastActivity *bool   `json:"last_activity,omitempty"` // If true, update last_activity to now
	// Agent identity fields
	RoleType *string `json:"role_type,omitempty"` // polecat|crew|witness|refinery|mayor|deacon
	Rig      *string `json:"rig,omitempty"`       // Rig name (empty for town-level agents)
	// Agent pod fields (gt-el7sxq.7)
	PodName       *string `json:"pod_name,omitempty"`       // K8s pod name
	PodIP         *string `json:"pod_ip,omitempty"`         // Pod IP address
	PodNode       *string `json:"pod_node,omitempty"`       // K8s node name
	PodStatus     *string `json:"pod_status,omitempty"`     // Pod status: pending|running|terminating|terminated
	ScreenSession *string `json:"screen_session,omitempty"` // Screen/tmux session name
	// Event fields (only valid when IssueType == "event")
	EventCategory *string `json:"event_category,omitempty"` // Namespaced category (e.g., patrol.muted, agent.started)
	EventActor    *string `json:"event_actor,omitempty"`    // Entity URI who caused this event
	EventTarget   *string `json:"event_target,omitempty"`   // Entity URI or bead ID affected
	EventPayload  *string `json:"event_payload,omitempty"`  // Event-specific JSON data
	// Work queue claim operation
	Claim bool `json:"claim,omitempty"` // If true, atomically claim issue (set assignee+status, fail if already claimed)
	// Time-based scheduling fields (GH#820)
	DueAt      *string `json:"due_at,omitempty"`      // Relative or ISO format due date
	DeferUntil *string `json:"defer_until,omitempty"` // Relative or ISO format defer date
	// Gate fields
	AwaitID *string  `json:"await_id,omitempty"` // Condition identifier for gates (run ID, PR number, etc.)
	Waiters []string `json:"waiters,omitempty"`  // Mail addresses to notify when gate clears
	// Slot fields
	Holder *string `json:"holder,omitempty"` // Who currently holds the slot (for type=slot beads)
	// NOTE: Legacy advice targeting fields removed - use labels instead
	// Advice hook fields (hq--uaim)
	AdviceHookCommand   *string `json:"advice_hook_command,omitempty"`    // Command to execute
	AdviceHookTrigger   *string `json:"advice_hook_trigger,omitempty"`    // Trigger: session-end, before-commit, before-push, before-handoff
	AdviceHookTimeout   *int    `json:"advice_hook_timeout,omitempty"`    // Timeout in seconds
	AdviceHookOnFailure *string `json:"advice_hook_on_failure,omitempty"` // Failure behavior: block, warn, ignore
	// Advice subscription fields (gt-w2mh8a.6)
	AdviceSubscriptions        []string `json:"advice_subscriptions,omitempty"`         // Additional labels to subscribe to
	AdviceSubscriptionsExclude []string `json:"advice_subscriptions_exclude,omitempty"` // Labels to exclude from receiving advice
}

// UpdateWithCommentArgs represents arguments for atomic update + comment operation.
// This performs an issue update and optionally adds a comment in a single transaction.
type UpdateWithCommentArgs struct {
	UpdateArgs           // Embedded update fields
	CommentText   string `json:"comment_text,omitempty"`   // Optional comment to add
	CommentAuthor string `json:"comment_author,omitempty"` // Comment author (defaults to actor)
}

// CloseArgs represents arguments for the close operation
type CloseArgs struct {
	ID          string `json:"id"`
	Reason      string `json:"reason,omitempty"`
	Session     string `json:"session,omitempty"`      // Claude Code session ID that closed this issue
	SuggestNext bool   `json:"suggest_next,omitempty"` // Return newly unblocked issues (GH#679)
	Force       bool   `json:"force,omitempty"`        // Force close even with open blockers (GH#962)
}

// CloseResult is returned when SuggestNext is true (GH#679)
// When SuggestNext is false, just the closed issue is returned for backward compatibility
type CloseResult struct {
	Closed    *types.Issue   `json:"closed"`              // The issue that was closed
	Unblocked []*types.Issue `json:"unblocked,omitempty"` // Issues newly unblocked by closing
}

// DeleteArgs represents arguments for the delete operation
type DeleteArgs struct {
	IDs        []string `json:"ids"`                  // Issue IDs to delete
	Force      bool     `json:"force,omitempty"`      // Force deletion without confirmation
	DryRun     bool     `json:"dry_run,omitempty"`    // Preview mode
	Cascade    bool     `json:"cascade,omitempty"`    // Recursively delete dependents
	Reason     string   `json:"reason,omitempty"`     // Reason for deletion
	HardDelete bool     `json:"hard_delete,omitempty"` // Permanently delete (skip tombstones, cannot sync)
}

// RenameArgs represents arguments for the rename operation
type RenameArgs struct {
	OldID string `json:"old_id"` // Current issue ID
	NewID string `json:"new_id"` // New issue ID
}

// RenameResult represents the result of a rename operation
type RenameResult struct {
	OldID            string `json:"old_id"`             // Original issue ID
	NewID            string `json:"new_id"`             // New issue ID
	ReferencesUpdated int    `json:"references_updated"` // Number of text references updated in other issues
}

// ListArgs represents arguments for the list operation
type ListArgs struct {
	Query     string   `json:"query,omitempty"`
	Status    string   `json:"status,omitempty"`
	Priority  *int     `json:"priority,omitempty"`
	IssueType string   `json:"issue_type,omitempty"`
	Assignee  string   `json:"assignee,omitempty"`
	Label     string   `json:"label,omitempty"`      // Deprecated: use Labels
	Labels    []string `json:"labels,omitempty"`     // AND semantics
	LabelsAny []string `json:"labels_any,omitempty"` // OR semantics
	IDs       []string `json:"ids,omitempty"`        // Filter by specific issue IDs
	Limit     int      `json:"limit,omitempty"`
	
	// Pattern matching
	TitleContains       string `json:"title_contains,omitempty"`
	DescriptionContains string `json:"description_contains,omitempty"`
	NotesContains       string `json:"notes_contains,omitempty"`
	
	// Date ranges (ISO 8601 format)
	CreatedAfter  string `json:"created_after,omitempty"`
	CreatedBefore string `json:"created_before,omitempty"`
	UpdatedAfter  string `json:"updated_after,omitempty"`
	UpdatedBefore string `json:"updated_before,omitempty"`
	ClosedAfter   string `json:"closed_after,omitempty"`
	ClosedBefore  string `json:"closed_before,omitempty"`
	
	// Empty/null checks
	EmptyDescription bool `json:"empty_description,omitempty"`
	NoAssignee       bool `json:"no_assignee,omitempty"`
	NoLabels         bool `json:"no_labels,omitempty"`
	
	// Priority range
	PriorityMin *int `json:"priority_min,omitempty"`
	PriorityMax *int `json:"priority_max,omitempty"`

	// Pinned filtering
	Pinned *bool `json:"pinned,omitempty"`

	// Template filtering
	IncludeTemplates bool `json:"include_templates,omitempty"`

	// Parent filtering
	ParentID string `json:"parent_id,omitempty"`

	// Ephemeral filtering
	Ephemeral *bool `json:"ephemeral,omitempty"`

	// Molecule type filtering
	MolType string `json:"mol_type,omitempty"`

	// Status exclusion (for default non-closed behavior, GH#788)
	ExcludeStatus []string `json:"exclude_status,omitempty"`

	// Type exclusion (for hiding internal types like gates, bd-7zka.2)
	ExcludeTypes []string `json:"exclude_types,omitempty"`

	// Time-based scheduling filters (GH#820)
	Deferred    bool   `json:"deferred,omitempty"`     // Filter issues with defer_until set
	DeferAfter  string `json:"defer_after,omitempty"`  // ISO 8601 format
	DeferBefore string `json:"defer_before,omitempty"` // ISO 8601 format
	DueAfter    string `json:"due_after,omitempty"`    // ISO 8601 format
	DueBefore   string `json:"due_before,omitempty"`   // ISO 8601 format
	Overdue     bool   `json:"overdue,omitempty"`      // Filter issues where due_at < now

	// Staleness control (bd-dpkdm)
	AllowStale bool `json:"allow_stale,omitempty"` // Skip staleness check, return potentially stale data

	// Cross-rig listing (bd-rl6y)
	TargetRig string `json:"target_rig,omitempty"` // List from different rig (resolves via route beads)
}

// CountArgs represents arguments for the count operation
type CountArgs struct {
	// Supports all the same filters as ListArgs
	Query     string   `json:"query,omitempty"`
	Status    string   `json:"status,omitempty"`
	Priority  *int     `json:"priority,omitempty"`
	IssueType string   `json:"issue_type,omitempty"`
	Assignee  string   `json:"assignee,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	LabelsAny []string `json:"labels_any,omitempty"`
	IDs       []string `json:"ids,omitempty"`

	// Pattern matching
	TitleContains       string `json:"title_contains,omitempty"`
	DescriptionContains string `json:"description_contains,omitempty"`
	NotesContains       string `json:"notes_contains,omitempty"`

	// Date ranges
	CreatedAfter  string `json:"created_after,omitempty"`
	CreatedBefore string `json:"created_before,omitempty"`
	UpdatedAfter  string `json:"updated_after,omitempty"`
	UpdatedBefore string `json:"updated_before,omitempty"`
	ClosedAfter   string `json:"closed_after,omitempty"`
	ClosedBefore  string `json:"closed_before,omitempty"`

	// Empty/null checks
	EmptyDescription bool `json:"empty_description,omitempty"`
	NoAssignee       bool `json:"no_assignee,omitempty"`
	NoLabels         bool `json:"no_labels,omitempty"`

	// Priority range
	PriorityMin *int `json:"priority_min,omitempty"`
	PriorityMax *int `json:"priority_max,omitempty"`

	// Grouping option (only one can be specified)
	GroupBy string `json:"group_by,omitempty"` // "status", "priority", "type", "assignee", "label"
}

// ShowArgs represents arguments for the show operation
type ShowArgs struct {
	ID string `json:"id"`
}

// ResolveIDArgs represents arguments for the resolve_id operation
type ResolveIDArgs struct {
	ID string `json:"id"`
}

// ReadyArgs represents arguments for the ready operation
type ReadyArgs struct {
	Assignee   string   `json:"assignee,omitempty"`
	Unassigned bool     `json:"unassigned,omitempty"`
	Priority   *int     `json:"priority,omitempty"`
	Type       string   `json:"type,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	SortPolicy string   `json:"sort_policy,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	LabelsAny  []string `json:"labels_any,omitempty"`
	ParentID        string   `json:"parent_id,omitempty"`        // Filter to descendants of this bead/epic
	MolType         string   `json:"mol_type,omitempty"`         // Filter by molecule type: swarm, patrol, or work
	IncludeDeferred bool     `json:"include_deferred,omitempty"` // Include issues with future defer_until (GH#820)
}

// BlockedArgs represents arguments for the blocked operation
type BlockedArgs struct {
	ParentID string `json:"parent_id,omitempty"` // Filter to descendants of this bead/epic
}

// StaleArgs represents arguments for the stale command
type StaleArgs struct {
	Days   int    `json:"days,omitempty"`
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// DepAddArgs represents arguments for adding a dependency
type DepAddArgs struct {
	FromID  string `json:"from_id"`
	ToID    string `json:"to_id"`
	DepType string `json:"dep_type"`
}

// DepRemoveArgs represents arguments for removing a dependency
type DepRemoveArgs struct {
	FromID  string `json:"from_id"`
	ToID    string `json:"to_id"`
	DepType string `json:"dep_type,omitempty"`
}

// DepAddBidirectionalArgs represents arguments for adding a bidirectional relation
type DepAddBidirectionalArgs struct {
	ID1     string `json:"id1"`
	ID2     string `json:"id2"`
	DepType string `json:"dep_type"`
}

// DepRemoveBidirectionalArgs represents arguments for removing a bidirectional relation
type DepRemoveBidirectionalArgs struct {
	ID1     string `json:"id1"`
	ID2     string `json:"id2"`
	DepType string `json:"dep_type,omitempty"`
}

// DepTreeArgs represents arguments for the dep tree operation
type DepTreeArgs struct {
	ID       string `json:"id"`
	MaxDepth int    `json:"max_depth,omitempty"`
}

// LabelAddArgs represents arguments for adding a label
type LabelAddArgs struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// LabelRemoveArgs represents arguments for removing a label
type LabelRemoveArgs struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// BatchAddLabelsArgs represents arguments for adding multiple labels atomically
type BatchAddLabelsArgs struct {
	IssueID string   `json:"issue_id"` // Issue ID to add labels to
	Labels  []string `json:"labels"`   // Labels to add
}

// BatchAddLabelsResult represents the result of a batch add labels operation
type BatchAddLabelsResult struct {
	IssueID    string `json:"issue_id"`    // Issue ID that was modified
	LabelsAdded int    `json:"labels_added"` // Number of labels actually added (excludes duplicates)
}

// CommentListArgs represents arguments for listing comments on an issue
type CommentListArgs struct {
	ID string `json:"id"`
}

// CommentAddArgs represents arguments for adding a comment to an issue
type CommentAddArgs struct {
	ID     string `json:"id"`
	Author string `json:"author"`
	Text   string `json:"text"`
}

// EpicStatusArgs represents arguments for the epic status operation
type EpicStatusArgs struct {
	EligibleOnly bool `json:"eligible_only,omitempty"`
}

// PingResponse is the response for a ping operation
type PingResponse struct {
	Message string `json:"message"`
	Version string `json:"version"`
}

// StatusResponse represents the daemon status metadata
type StatusResponse struct {
	Version              string  `json:"version"`                  // Server/daemon version
	WorkspacePath        string  `json:"workspace_path"`           // Absolute path to workspace root
	DatabasePath         string  `json:"database_path"`            // Absolute path to database file
	SocketPath           string  `json:"socket_path"`              // Path to Unix socket
	PID                  int     `json:"pid"`                      // Process ID
	UptimeSeconds        float64 `json:"uptime_seconds"`           // Time since daemon started
	LastActivityTime     string  `json:"last_activity_time"`       // ISO 8601 timestamp of last request
	ExclusiveLockActive  bool    `json:"exclusive_lock_active"`    // Whether an exclusive lock is held
	ExclusiveLockHolder  string  `json:"exclusive_lock_holder,omitempty"` // Lock holder name if active
	// Daemon configuration
	AutoCommit   bool   `json:"auto_commit"`            // Whether auto-commit is enabled
	AutoPush     bool   `json:"auto_push"`              // Whether auto-push is enabled
	AutoPull     bool   `json:"auto_pull"`              // Whether auto-pull is enabled (periodic remote sync)
	LocalMode    bool   `json:"local_mode"`             // Whether running in local-only mode (no git)
	SyncInterval string `json:"sync_interval"`          // Sync interval (e.g., "5s")
	DaemonMode   string `json:"daemon_mode"`            // Sync mode: "poll" or "events"
}

// HealthResponse is the response for a health check operation
type HealthResponse struct {
	Status         string  `json:"status"`                   // "healthy", "degraded", "unhealthy"
	Version        string  `json:"version"`                  // Server/daemon version
	ClientVersion  string  `json:"client_version,omitempty"` // Client version from request
	Compatible     bool    `json:"compatible"`               // Whether versions are compatible
	Uptime         float64 `json:"uptime_seconds"`
	DBResponseTime float64 `json:"db_response_ms"`
	ActiveConns    int32   `json:"active_connections"`
	MaxConns       int     `json:"max_connections"`
	MemoryAllocMB  uint64  `json:"memory_alloc_mb"`
	Error          string  `json:"error,omitempty"`
}

// BatchArgs represents arguments for batch operations
type BatchArgs struct {
	Operations []BatchOperation `json:"operations"`
}

// BatchOperation represents a single operation in a batch
type BatchOperation struct {
	Operation string          `json:"operation"`
	Args      json.RawMessage `json:"args"`
}

// BatchResponse contains the results of a batch operation
type BatchResponse struct {
	Results []BatchResult `json:"results"`
}

// BatchResult represents the result of a single operation in a batch
type BatchResult struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// CompactArgs represents arguments for the compact operation
type CompactArgs struct {
	IssueID   string `json:"issue_id,omitempty"`   // Empty for --all
	Tier      int    `json:"tier"`                 // 1 or 2
	DryRun    bool   `json:"dry_run"`
	Force     bool   `json:"force"`
	All       bool   `json:"all"`
	APIKey    string `json:"api_key,omitempty"`
	Workers   int    `json:"workers,omitempty"`
	BatchSize int    `json:"batch_size,omitempty"`
}

// CompactStatsArgs represents arguments for compact stats operation
type CompactStatsArgs struct {
	Tier int `json:"tier,omitempty"`
}

// CompactResponse represents the response from a compact operation
type CompactResponse struct {
	Success      bool              `json:"success"`
	IssueID      string            `json:"issue_id,omitempty"`
	Results      []CompactResult   `json:"results,omitempty"`     // For batch operations
	Stats        *CompactStatsData `json:"stats,omitempty"`       // For stats operation
	OriginalSize int               `json:"original_size,omitempty"`
	CompactedSize int              `json:"compacted_size,omitempty"`
	Reduction    string            `json:"reduction,omitempty"`
	Duration     string            `json:"duration,omitempty"`
	DryRun       bool              `json:"dry_run,omitempty"`
}

// CompactResult represents the result of compacting a single issue
type CompactResult struct {
	IssueID       string `json:"issue_id"`
	Success       bool   `json:"success"`
	Error         string `json:"error,omitempty"`
	OriginalSize  int    `json:"original_size,omitempty"`
	CompactedSize int    `json:"compacted_size,omitempty"`
	Reduction     string `json:"reduction,omitempty"`
}

// CompactStatsData represents compaction statistics
type CompactStatsData struct {
	Tier1Candidates int     `json:"tier1_candidates"`
	Tier2Candidates int     `json:"tier2_candidates"`
	TotalClosed     int     `json:"total_closed"`
	Tier1MinAge     string  `json:"tier1_min_age"`
	Tier2MinAge     string  `json:"tier2_min_age"`
	EstimatedSavings string `json:"estimated_savings,omitempty"`
}

// ExportArgs represents arguments for the export operation
type ExportArgs struct {
	JSONLPath string `json:"jsonl_path"` // Path to export JSONL file
}

// ImportArgs represents arguments for the import operation
type ImportArgs struct {
	JSONLPath string `json:"jsonl_path"` // Path to import JSONL file
}

// GetMutationsArgs represents arguments for retrieving recent mutations
type GetMutationsArgs struct {
	Since int64 `json:"since"` // Unix timestamp in milliseconds (0 for all recent)
}

// Gate operations

// GateCreateArgs represents arguments for creating a gate
type GateCreateArgs struct {
	Title     string        `json:"title"`
	AwaitType string        `json:"await_type"` // gh:run, gh:pr, timer, human, mail
	AwaitID   string        `json:"await_id"`   // ID/value for the await type
	Timeout   time.Duration `json:"timeout"`    // Timeout duration
	Waiters   []string      `json:"waiters"`    // Mail addresses to notify when gate clears
}

// GateCreateResult represents the result of creating a gate
type GateCreateResult struct {
	ID string `json:"id"` // Created gate ID
}

// GateListArgs represents arguments for listing gates
type GateListArgs struct {
	All bool `json:"all"` // Include closed gates
}

// GateShowArgs represents arguments for showing a gate
type GateShowArgs struct {
	ID string `json:"id"` // Gate ID (partial or full)
}

// GateCloseArgs represents arguments for closing a gate
type GateCloseArgs struct {
	ID     string `json:"id"`               // Gate ID (partial or full)
	Reason string `json:"reason,omitempty"` // Close reason
}

// GateWaitArgs represents arguments for adding waiters to a gate
type GateWaitArgs struct {
	ID      string   `json:"id"`      // Gate ID (partial or full)
	Waiters []string `json:"waiters"` // Additional waiters to add
}

// GateWaitResult represents the result of adding waiters
type GateWaitResult struct {
	AddedCount int `json:"added_count"` // Number of new waiters added
}

// GetWorkerStatusArgs represents arguments for retrieving worker status
type GetWorkerStatusArgs struct {
	// Assignee filters to a specific worker (optional, empty = all workers)
	Assignee string `json:"assignee,omitempty"`
}

// WorkerStatus represents the status of a single worker and their current work
type WorkerStatus struct {
	Assignee      string `json:"assignee"`                 // Worker identifier
	MoleculeID    string `json:"molecule_id,omitempty"`    // Parent molecule/epic ID (if working on a step)
	MoleculeTitle string `json:"molecule_title,omitempty"` // Parent molecule/epic title
	CurrentStep   int    `json:"current_step,omitempty"`   // Current step number (1-indexed)
	TotalSteps    int    `json:"total_steps,omitempty"`    // Total number of steps in molecule
	StepID        string `json:"step_id,omitempty"`        // Current step issue ID
	StepTitle     string `json:"step_title,omitempty"`     // Current step issue title
	LastActivity  string `json:"last_activity"`            // ISO 8601 timestamp of last update
	Status        string `json:"status"`                   // Current work status (in_progress, blocked, etc.)
}

// GetWorkerStatusResponse is the response for get_worker_status operation
type GetWorkerStatusResponse struct {
	Workers []WorkerStatus `json:"workers"`
}

// GetMoleculeProgressArgs represents arguments for the get_molecule_progress operation
type GetMoleculeProgressArgs struct {
	MoleculeID string `json:"molecule_id"` // The ID of the molecule (parent issue)
}

// MoleculeStep represents a single step within a molecule
type MoleculeStep struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Status    string  `json:"status"`     // "done", "current", "ready", "blocked"
	StartTime *string `json:"start_time"` // ISO 8601 timestamp when step was created
	CloseTime *string `json:"close_time"` // ISO 8601 timestamp when step was closed (if done)
}

// MoleculeProgress represents the progress of a molecule (parent issue with steps)
type MoleculeProgress struct {
	MoleculeID string         `json:"molecule_id"`
	Title      string         `json:"title"`
	Assignee   string         `json:"assignee"`
	Steps      []MoleculeStep `json:"steps"`
}

// GetConfigArgs represents arguments for getting daemon config
type GetConfigArgs struct {
	Key string `json:"key"` // Config key to retrieve (e.g., "issue_prefix")
}

// GetConfigResponse represents the response from get_config operation
type GetConfigResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ConfigSetArgs represents arguments for setting a config value (bd-wmil)
type ConfigSetArgs struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ConfigSetResponse represents the response from config_set operation
type ConfigSetResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ConfigListArgs represents arguments for listing all config values (bd-wmil)
type ConfigListArgs struct {
	// No arguments needed
}

// ConfigListResponse represents the response from config_list operation
type ConfigListResponse struct {
	Config map[string]string `json:"config"`
}

// ConfigUnsetArgs represents arguments for unsetting a config value (bd-wmil)
type ConfigUnsetArgs struct {
	Key string `json:"key"`
}

// ConfigUnsetResponse represents the response from config_unset operation
type ConfigUnsetResponse struct {
	Key string `json:"key"`
}

// Decision point operations

// DecisionCreateArgs represents arguments for creating a decision point
type DecisionCreateArgs struct {
	IssueID       string   `json:"issue_id"`                 // Issue ID to attach decision to
	Prompt        string   `json:"prompt"`                   // Question to ask
	Options       []string `json:"options"`                  // Available choices
	DefaultOption string   `json:"default_option,omitempty"` // Default option if no response
	MaxIterations int      `json:"max_iterations,omitempty"` // Max follow-up iterations (default 3)
	RequestedBy   string   `json:"requested_by,omitempty"`   // Who requested this decision
}

// DecisionGetArgs represents arguments for getting a decision point
type DecisionGetArgs struct {
	IssueID string `json:"issue_id"` // Issue ID to get decision for
}

// DecisionResolveArgs represents arguments for resolving a decision point
type DecisionResolveArgs struct {
	IssueID        string `json:"issue_id"`                  // Issue ID
	SelectedOption string `json:"selected_option"`           // Chosen option
	ResponseText   string `json:"response_text,omitempty"`   // Additional response text
	RespondedBy    string `json:"responded_by,omitempty"`    // Who responded
	Guidance       string `json:"guidance,omitempty"`        // Follow-up guidance
}

// DecisionListArgs represents arguments for listing pending decisions
type DecisionListArgs struct {
	All bool `json:"all,omitempty"` // Include resolved decisions
}

// DecisionResponse represents a single decision with its associated issue
type DecisionResponse struct {
	Decision *types.DecisionPoint `json:"decision"`
	Issue    *types.Issue         `json:"issue,omitempty"`
}

// DecisionListResponse represents a list of decisions
type DecisionListResponse struct {
	Decisions []*DecisionResponse `json:"decisions"`
	Count     int                 `json:"count"`
}

// DecisionRemindArgs represents arguments for the decision remind operation
type DecisionRemindArgs struct {
	IssueID string `json:"issue_id"` // Decision gate issue ID
	Force   bool   `json:"force,omitempty"` // Force reminder even if at max
}

// DecisionRemindResult represents the result of a decision remind operation
type DecisionRemindResult struct {
	IssueID       string `json:"issue_id"`
	ReminderCount int    `json:"reminder_count"`
	MaxReminders  int    `json:"max_reminders"`
	Prompt        string `json:"prompt"`
}

// DecisionCancelArgs represents arguments for the decision cancel operation
type DecisionCancelArgs struct {
	IssueID    string `json:"issue_id"`              // Decision gate issue ID
	Reason     string `json:"reason,omitempty"`      // Cancellation reason
	CanceledBy string `json:"canceled_by,omitempty"` // Who canceled
}

// DecisionCancelResult represents the result of a decision cancel operation
type DecisionCancelResult struct {
	IssueID    string `json:"issue_id"`
	CanceledAt string `json:"canceled_at"` // ISO 8601 timestamp
	Reason     string `json:"reason,omitempty"`
	CanceledBy string `json:"canceled_by,omitempty"`
	Prompt     string `json:"prompt"`
}

// Additional write operations (bd-wj80)

// RenamePrefixArgs represents arguments for the rename-prefix operation
type RenamePrefixArgs struct {
	NewPrefix string `json:"new_prefix"`           // New prefix (e.g., "bd-")
	DryRun    bool   `json:"dry_run,omitempty"`    // Preview changes only
	Repair    bool   `json:"repair,omitempty"`     // Consolidate multiple prefixes
}

// RenamePrefixResult represents the result of a rename-prefix operation
type RenamePrefixResult struct {
	OldPrefix     string `json:"old_prefix"`
	NewPrefix     string `json:"new_prefix"`
	IssuesRenamed int    `json:"issues_renamed"`
	DryRun        bool   `json:"dry_run"`
}

// MoveArgs represents arguments for the move operation
type MoveArgs struct {
	IssueID   string `json:"issue_id"`             // Source issue ID
	TargetRig string `json:"target_rig"`           // Target rig name or prefix
	KeepOpen  bool   `json:"keep_open,omitempty"`  // Don't close source issue
	SkipDeps  bool   `json:"skip_deps,omitempty"`  // Skip dependency remapping
}

// MoveResult represents the result of a move operation
type MoveResult struct {
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	Closed       bool   `json:"closed"`
	DepsRemapped int    `json:"deps_remapped"`
}

// RefileArgs represents arguments for the refile operation
type RefileArgs struct {
	IssueID   string `json:"issue_id"`             // Source issue ID
	TargetRig string `json:"target_rig"`           // Target rig name or prefix
	KeepOpen  bool   `json:"keep_open,omitempty"`  // Don't close source issue
}

// RefileResult represents the result of a refile operation
type RefileResult struct {
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
	Closed   bool   `json:"closed"`
}

// CookArgs represents arguments for the cook operation
type CookArgs struct {
	FormulaName string            `json:"formula_name"`          // Formula file name or path
	DryRun      bool              `json:"dry_run,omitempty"`     // Preview only
	Persist     bool              `json:"persist,omitempty"`     // Write proto to database
	Force       bool              `json:"force,omitempty"`       // Overwrite existing proto
	Prefix      string            `json:"prefix,omitempty"`      // Proto ID prefix
	Vars        map[string]string `json:"vars,omitempty"`        // Variable substitutions
	Mode        string            `json:"mode,omitempty"`        // "compile" or "runtime"
}

// CookResult represents the result of a cook operation
type CookResult struct {
	ProtoID    string          `json:"proto_id"`
	Created    int             `json:"created"`
	Variables  []string        `json:"variables,omitempty"`
	BondPoints []string        `json:"bond_points,omitempty"`
	DryRun     bool            `json:"dry_run"`
	Subgraph   json.RawMessage `json:"subgraph,omitempty"` // Ephemeral mode: TemplateSubgraph as JSON
}

// PourArgs represents arguments for the pour operation
type PourArgs struct {
	ProtoID     string            `json:"proto_id"`              // Proto bead ID or formula name
	Vars        map[string]string `json:"vars,omitempty"`        // Variable substitutions
	DryRun      bool              `json:"dry_run,omitempty"`     // Preview only
	Assignee    string            `json:"assignee,omitempty"`    // Assign root issue
	Attachments []string          `json:"attachments,omitempty"` // Proto IDs to attach
	AttachType  string            `json:"attach_type,omitempty"` // Attachment bonding type
}

// PourResult represents the result of a pour operation
type PourResult struct {
	RootID   string   `json:"root_id"`
	Created  int      `json:"created"`
	Attached int      `json:"attached"`
	Phase    string   `json:"phase"`               // "liquid"
	Runbooks []string `json:"runbooks,omitempty"`   // Runbook refs to auto-materialize (od-dv0.6)
}

// Mol operations (gt-as9kdm)

// MolBondArgs represents arguments for the mol bond operation
type MolBondArgs struct {
	IDa       string            `json:"id_a"`                  // First operand (proto/molecule ID or formula name)
	IDb       string            `json:"id_b"`                  // Second operand
	BondType  string            `json:"bond_type"`             // "sequential", "parallel", "conditional"
	Title     string            `json:"title,omitempty"`       // Custom title for compound proto
	Vars      map[string]string `json:"vars,omitempty"`        // Variable substitutions
	ChildRef  string            `json:"child_ref,omitempty"`   // Custom child reference for dynamic bonding
	Ephemeral bool              `json:"ephemeral,omitempty"`   // Force spawn as vapor (ephemeral)
	Pour      bool              `json:"pour,omitempty"`        // Force spawn as liquid (persistent)
	DryRun    bool              `json:"dry_run,omitempty"`     // Preview mode
}

// MolBondResult represents the result of a mol bond operation
type MolBondResult struct {
	ResultID   string            `json:"result_id"`
	ResultType string            `json:"result_type"`         // "compound_proto" or "compound_molecule"
	BondType   string            `json:"bond_type"`
	Spawned    int               `json:"spawned,omitempty"`   // Number of issues spawned
	IDMapping  map[string]string `json:"id_mapping,omitempty"` // Old ID -> new ID mapping
}

// MolSquashArgs represents arguments for the mol squash operation
type MolSquashArgs struct {
	MoleculeID   string `json:"molecule_id"`
	DryRun       bool   `json:"dry_run,omitempty"`
	KeepChildren bool   `json:"keep_children,omitempty"`
	Summary      string `json:"summary,omitempty"` // Agent-provided summary
}

// MolSquashResult represents the result of a mol squash operation
type MolSquashResult struct {
	MoleculeID    string   `json:"molecule_id"`
	DigestID      string   `json:"digest_id"`
	SquashedIDs   []string `json:"squashed_ids"`
	SquashedCount int      `json:"squashed_count"`
	DeletedCount  int      `json:"deleted_count"`
	KeptChildren  bool     `json:"kept_children"`
}

// MolBurnArgs represents arguments for the mol burn operation
type MolBurnArgs struct {
	MoleculeIDs []string `json:"molecule_ids"` // Can burn multiple
	DryRun      bool     `json:"dry_run,omitempty"`
	Force       bool     `json:"force,omitempty"`
}

// MolBurnResult represents the result of a mol burn operation
type MolBurnResult struct {
	DeletedIDs   []string `json:"deleted_ids"`
	DeletedCount int      `json:"deleted_count"`
	FailedCount  int      `json:"failed_count"`
}

// MolCurrentArgs represents arguments for the mol current operation
type MolCurrentArgs struct {
	MoleculeID string `json:"molecule_id,omitempty"` // Explicit molecule ID (optional)
	Agent      string `json:"agent,omitempty"`       // Agent/assignee filter
	Limit      int    `json:"limit,omitempty"`       // Max steps to return
	RangeStart int    `json:"range_start,omitempty"` // Step range start (1-indexed)
	RangeEnd   int    `json:"range_end,omitempty"`   // Step range end (1-indexed)
}

// MolCurrentStepStatus represents the status of a step in a molecule for RPC
type MolCurrentStepStatus struct {
	IssueID   string `json:"issue_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`      // "done", "current", "ready", "blocked", "pending"
	IsCurrent bool   `json:"is_current"`  // true if this is the in_progress step
	IssueType string `json:"issue_type"`
	Priority  int    `json:"priority"`
}

// MolCurrentProgress holds the progress information for a molecule (detailed for mol current)
type MolCurrentProgress struct {
	MoleculeID    string                  `json:"molecule_id"`
	MoleculeTitle string                  `json:"molecule_title"`
	Assignee      string                  `json:"assignee,omitempty"`
	CurrentStep   *MolCurrentStepStatus   `json:"current_step,omitempty"`
	NextStep      *MolCurrentStepStatus   `json:"next_step,omitempty"`
	Steps         []*MolCurrentStepStatus `json:"steps"`
	Completed     int                     `json:"completed"`
	Total         int                     `json:"total"`
}

// MolCurrentResult represents the result of a mol current operation
type MolCurrentResult struct {
	Molecules []*MolCurrentProgress `json:"molecules"`
}

// MolProgressStatsArgs represents arguments for the mol progress stats operation
type MolProgressStatsArgs struct {
	MoleculeID string `json:"molecule_id"` // The ID of the molecule
}

// MolProgressStatsResult represents the result of a mol progress stats operation
// Uses indexed queries for efficient progress tracking of large molecules
type MolProgressStatsResult struct {
	MoleculeID    string  `json:"molecule_id"`
	MoleculeTitle string  `json:"molecule_title"`
	Total         int     `json:"total"`           // Total steps (direct children)
	Completed     int     `json:"completed"`       // Closed steps
	InProgress    int     `json:"in_progress"`     // Steps currently in progress
	CurrentStepID string  `json:"current_step_id"` // First in_progress step ID (if any)
	FirstClosed   *string `json:"first_closed,omitempty"`
	LastClosed    *string `json:"last_closed,omitempty"`
}

// MolReadyGatedArgs represents arguments for the mol ready --gated operation
type MolReadyGatedArgs struct {
	Limit int `json:"limit,omitempty"` // Maximum number of molecules to return
}

// MolReadyGatedMolecule represents a molecule ready for gate-resume dispatch
type MolReadyGatedMolecule struct {
	MoleculeID     string `json:"molecule_id"`
	MoleculeTitle  string `json:"molecule_title"`
	ClosedGateID   string `json:"closed_gate_id,omitempty"`
	ClosedGateType string `json:"closed_gate_type,omitempty"` // await_type of the closed gate
	ReadyStepID    string `json:"ready_step_id,omitempty"`
	ReadyStepTitle string `json:"ready_step_title,omitempty"`
}

// MolReadyGatedResult represents the result of a mol ready --gated operation
type MolReadyGatedResult struct {
	Molecules []*MolReadyGatedMolecule `json:"molecules"`
	Count     int                      `json:"count"`
}

// Close continue operation (bd-ympw)

// CloseContinueArgs represents arguments for the close --continue operation
type CloseContinueArgs struct {
	ClosedStepID string `json:"closed_step_id"` // The step that was just closed
	AutoClaim    bool   `json:"auto_claim"`     // Whether to auto-claim the next step
	Actor        string `json:"actor"`          // Actor name for updates
}

// CloseContinueResult represents the result of a close --continue operation
type CloseContinueResult struct {
	ClosedStep   *types.Issue `json:"closed_step"`             // The step that was closed
	NextStep     *types.Issue `json:"next_step,omitempty"`     // The next ready step
	AutoAdvanced bool         `json:"auto_advanced"`           // Whether next step was auto-claimed
	MolComplete  bool         `json:"molecule_complete"`       // Whether the molecule is complete
	MoleculeID   string       `json:"molecule_id,omitempty"`   // Parent molecule ID
}

// ListWatchArgs represents arguments for the list_watch operation (bd-la75)
// This is a long-polling endpoint for watch mode that blocks until mutations occur.
type ListWatchArgs struct {
	// All the standard ListArgs filters
	Query     string   `json:"query,omitempty"`
	Status    string   `json:"status,omitempty"`
	Priority  *int     `json:"priority,omitempty"`
	IssueType string   `json:"issue_type,omitempty"`
	Assignee  string   `json:"assignee,omitempty"`
	Label     string   `json:"label,omitempty"`      // Deprecated: use Labels
	Labels    []string `json:"labels,omitempty"`     // AND semantics
	LabelsAny []string `json:"labels_any,omitempty"` // OR semantics
	IDs       []string `json:"ids,omitempty"`
	Limit     int      `json:"limit,omitempty"`

	// Pattern matching
	TitleContains       string `json:"title_contains,omitempty"`
	DescriptionContains string `json:"description_contains,omitempty"`
	NotesContains       string `json:"notes_contains,omitempty"`

	// Date ranges (ISO 8601 format)
	CreatedAfter  string `json:"created_after,omitempty"`
	CreatedBefore string `json:"created_before,omitempty"`
	UpdatedAfter  string `json:"updated_after,omitempty"`
	UpdatedBefore string `json:"updated_before,omitempty"`
	ClosedAfter   string `json:"closed_after,omitempty"`
	ClosedBefore  string `json:"closed_before,omitempty"`

	// Empty/null checks
	EmptyDescription bool `json:"empty_description,omitempty"`
	NoAssignee       bool `json:"no_assignee,omitempty"`
	NoLabels         bool `json:"no_labels,omitempty"`

	// Priority range
	PriorityMin *int `json:"priority_min,omitempty"`
	PriorityMax *int `json:"priority_max,omitempty"`

	// Pinned filtering
	Pinned *bool `json:"pinned,omitempty"`

	// Template filtering
	IncludeTemplates bool `json:"include_templates,omitempty"`

	// Parent filtering
	ParentID string `json:"parent_id,omitempty"`

	// Ephemeral filtering
	Ephemeral *bool `json:"ephemeral,omitempty"`

	// Molecule type filtering
	MolType string `json:"mol_type,omitempty"`

	// Status exclusion (for default non-closed behavior, GH#788)
	ExcludeStatus []string `json:"exclude_status,omitempty"`

	// Type exclusion (for hiding internal types like gates, bd-7zka.2)
	ExcludeTypes []string `json:"exclude_types,omitempty"`

	// Time-based scheduling filters (GH#820)
	Deferred    bool   `json:"deferred,omitempty"`
	DeferAfter  string `json:"defer_after,omitempty"`
	DeferBefore string `json:"defer_before,omitempty"`
	DueAfter    string `json:"due_after,omitempty"`
	DueBefore   string `json:"due_before,omitempty"`
	Overdue     bool   `json:"overdue,omitempty"`

	// Watch-specific parameters
	Since     int64 `json:"since"`               // Unix timestamp in milliseconds (0 = return immediately with initial data)
	TimeoutMs int   `json:"timeout_ms,omitempty"` // Max wait time in milliseconds (default 30000, max 60000)
}

// ListWatchResult represents the result of a list_watch operation (bd-la75)
type ListWatchResult struct {
	Issues         []*types.Issue `json:"issues"`
	LastMutationMs int64          `json:"last_mutation_ms"` // Unix timestamp in milliseconds of latest mutation
	HasMore        bool           `json:"has_more,omitempty"` // True if more mutations occurred during wait
}

// TypesArgs represents arguments for the types operation (bd-s091)
type TypesArgs struct {
	// No arguments needed - types command just lists available types
}

// TypesResult represents the result of a types operation
type TypesResult struct {
	CoreTypes   []TypeInfo `json:"core_types"`
	CustomTypes []string   `json:"custom_types,omitempty"`
}

// TypeInfo describes a single issue type
type TypeInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Sync operations (bd-wn2g)

// SyncExportArgs represents arguments for the sync export operation
type SyncExportArgs struct {
	Force  bool `json:"force,omitempty"`  // Force full export (skip incremental optimization)
	DryRun bool `json:"dry_run,omitempty"` // Preview mode
}

// SyncExportResult represents the result of a sync export operation
type SyncExportResult struct {
	ExportedCount int    `json:"exported_count"`       // Number of issues exported
	ChangedCount  int    `json:"changed_count"`        // Number of issues changed since last sync
	JSONLPath     string `json:"jsonl_path"`           // Path to exported JSONL file
	Skipped       bool   `json:"skipped,omitempty"`    // True if export was skipped (no changes)
	Message       string `json:"message,omitempty"`    // Human-readable status message
}

// SyncStatusArgs represents arguments for the sync status operation
type SyncStatusArgs struct {
	// No arguments needed for status
}

// SyncStatusResult represents the result of a sync status operation
type SyncStatusResult struct {
	SyncMode         string `json:"sync_mode"`                    // git-portable, realtime, dolt-native, belt-and-suspenders
	SyncModeDesc     string `json:"sync_mode_desc"`               // Human-readable description
	ExportOn         string `json:"export_on"`                    // When export happens
	ImportOn         string `json:"import_on"`                    // When import happens
	ConflictStrategy string `json:"conflict_strategy"`            // Conflict resolution strategy
	LastExport       string `json:"last_export,omitempty"`        // ISO 8601 timestamp
	LastExportCommit string `json:"last_export_commit,omitempty"` // Short commit hash
	PendingChanges   int    `json:"pending_changes"`              // Number of dirty issues
	SyncBranch       string `json:"sync_branch,omitempty"`        // Sync branch name if configured
	ConflictCount    int    `json:"conflict_count"`               // Number of unresolved conflicts
	FederationRemote string `json:"federation_remote,omitempty"`  // Federation remote if configured
}

// SetState operations (atomic state change)

// SetStateArgs represents arguments for the set_state operation
type SetStateArgs struct {
	IssueID   string `json:"issue_id"`             // Issue ID to set state on
	Dimension string `json:"dimension"`            // State dimension (e.g., "patrol", "mode", "health")
	NewValue  string `json:"new_value"`            // New value for the dimension
	Reason    string `json:"reason,omitempty"`     // Optional reason for the state change
}

// SetStateResult represents the result of a set_state operation
type SetStateResult struct {
	IssueID   string  `json:"issue_id"`              // Full issue ID
	Dimension string  `json:"dimension"`             // State dimension
	OldValue  *string `json:"old_value"`             // Previous value (nil if none)
	NewValue  string  `json:"new_value"`             // New value
	EventID   string  `json:"event_id"`              // Created event bead ID
	Changed   bool    `json:"changed"`               // Whether the state actually changed
}


// CreateWithDeps operation (atomic issue creation with labels and dependencies)

// CreateWithDepsIssue represents a single issue to create with its labels and dependencies
type CreateWithDepsIssue struct {
	// Core issue fields (subset of CreateArgs)
	ID                 string   `json:"id,omitempty"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	IssueType          string   `json:"issue_type"`
	Priority           int      `json:"priority"`
	Design             string   `json:"design,omitempty"`
	AcceptanceCriteria string   `json:"acceptance_criteria,omitempty"`
	Assignee           string   `json:"assignee,omitempty"`
	EstimatedMinutes   *int     `json:"estimated_minutes,omitempty"`
	Ephemeral          bool     `json:"ephemeral,omitempty"`
	IDPrefix           string   `json:"id_prefix,omitempty"`
	Labels             []string `json:"labels,omitempty"`
}

// CreateWithDepsDependency represents a dependency to create between issues
type CreateWithDepsDependency struct {
	FromID  string `json:"from_id"`  // Issue ID (can reference issues being created by their old ID)
	ToID    string `json:"to_id"`    // Dependency target ID (can reference issues being created or existing issues)
	DepType string `json:"dep_type"` // Dependency type (blocks, parent-child, requires-skill, etc.)
}

// CreateWithDepsArgs represents arguments for the create_with_deps operation
type CreateWithDepsArgs struct {
	Issues       []CreateWithDepsIssue      `json:"issues"`       // Issues to create
	Dependencies []CreateWithDepsDependency `json:"dependencies"` // Dependencies to create after all issues exist
}

// CreateWithDepsResult represents the result of a create_with_deps operation
type CreateWithDepsResult struct {
	IDMapping map[string]string `json:"id_mapping"` // Old ID (or index) -> new ID mapping
	Created   int               `json:"created"`    // Number of issues created
}

// CreateMolecule operations (bd-jjbl)

// IssueCreateSpec specifies an issue to create as part of a molecule.
// Uses a template ID for cross-referencing in dependencies.
type IssueCreateSpec struct {
	TemplateID string     `json:"template_id"` // Local ID for dependency references
	CreateArgs CreateArgs `json:"create_args"` // Issue creation arguments
}

// DepSpec specifies a dependency between issues using template IDs.
type DepSpec struct {
	FromTemplateID string `json:"from_template_id"` // Template ID of the dependent issue
	ToTemplateID   string `json:"to_template_id"`   // Template ID of the dependency
	DepType        string `json:"dep_type"`         // Dependency type (blocks, parent-child, etc.)
}

// CreateMoleculeArgs represents arguments for the create_molecule operation.
// This creates multiple issues and their dependencies atomically in a single transaction.
type CreateMoleculeArgs struct {
	Issues       []IssueCreateSpec `json:"issues"`                  // Issues to create with template IDs
	Dependencies []DepSpec         `json:"dependencies"`            // Dependencies using template IDs
	Prefix       string            `json:"prefix,omitempty"`        // ID prefix for generated IDs (mol, wisp, etc.)
	Ephemeral    bool              `json:"ephemeral,omitempty"`     // Whether issues are ephemeral/wisps
	RootTemplate string            `json:"root_template,omitempty"` // Template ID of the root issue (for result)
}

// CreateMoleculeResult represents the result of a create_molecule operation.
type CreateMoleculeResult struct {
	IDMapping map[string]string `json:"id_mapping"` // Template ID → new issue ID
	RootID    string            `json:"root_id"`    // New ID of the root issue (if RootTemplate specified)
	Created   int               `json:"created"`    // Number of issues created
}

// Batch operations

// BatchDependency represents a single dependency to add in a batch
type BatchDependency struct {
	FromID  string `json:"from_id"`  // Issue ID that depends on another
	ToID    string `json:"to_id"`    // Issue ID being depended on
	Type    string `json:"type"`     // Dependency type (blocks, parent-child, etc.)
}

// BatchAddDependenciesArgs represents arguments for the batch_add_dependencies operation.
// This adds multiple dependencies atomically in a single transaction.
type BatchAddDependenciesArgs struct {
	Dependencies []BatchDependency `json:"dependencies"` // Dependencies to add
}

// BatchAddDependenciesResult represents the result of a batch_add_dependencies operation.
type BatchAddDependenciesResult struct {
	Added  int      `json:"added"`            // Number of dependencies successfully added
	Errors []string `json:"errors,omitempty"` // Any errors encountered (non-fatal)
}

// BatchQueryWorkersArgs represents arguments for the batch_query_workers operation.
// This queries worker assignments for multiple issues at once.
type BatchQueryWorkersArgs struct {
	IssueIDs []string `json:"issue_ids"` // Issue IDs to query workers for
}

// WorkerInfo represents worker assignment information for a single issue
type WorkerInfo struct {
	IssueID  string `json:"issue_id"`            // Issue ID
	Assignee string `json:"assignee,omitempty"`  // Assigned worker (if any)
	Owner    string `json:"owner,omitempty"`     // Human owner (if any)
	Status   string `json:"status,omitempty"`    // Issue status
}

// BatchQueryWorkersResult represents the result of a batch_query_workers operation.
type BatchQueryWorkersResult struct {
	Workers map[string]*WorkerInfo `json:"workers"` // Issue ID -> WorkerInfo mapping
}

// Convoy operations

// CreateConvoyWithTrackingArgs represents arguments for atomic convoy creation with tracking relations.
// This creates a convoy issue and adds tracking dependencies for all specified issues in a single transaction.
type CreateConvoyWithTrackingArgs struct {
	ConvoyID      string   `json:"convoy_id,omitempty"`      // Optional convoy ID (auto-generated if empty)
	Name          string   `json:"name"`                     // Convoy name/title
	TrackedIssues []string `json:"tracked_issues"`           // Issue IDs to track
	Owner         string   `json:"owner,omitempty"`          // Human owner for CV attribution
	NotifyAddress string   `json:"notify_address,omitempty"` // Mail address to notify on convoy events
}

// CreateConvoyWithTrackingResult represents the result of atomic convoy creation.
type CreateConvoyWithTrackingResult struct {
	ConvoyID     string   `json:"convoy_id"`     // The created convoy's issue ID
	TrackedCount int      `json:"tracked_count"` // Number of tracking dependencies added
	TrackedIDs   []string `json:"tracked_ids"`   // IDs of issues being tracked
}

// Atomic closure chain operation (for MR completion)

// AtomicClosureChainArgs represents arguments for the atomic_closure_chain operation.
// This closes multiple related issues and optionally updates an agent bead in a single transaction.
// Used for MR completion where we need to atomically close the MR, its source issue, and update the agent.
type AtomicClosureChainArgs struct {
	MRID              string                 `json:"mr_id"`                   // MR bead ID to close
	MRCloseReason     string                 `json:"mr_close_reason"`         // Close reason for MR (e.g., "merged")
	SourceIssueID     string                 `json:"source_issue_id"`         // Source issue ID to close
	SourceCloseReason string                 `json:"source_close_reason"`     // Close reason for source issue
	AgentBeadID       string                 `json:"agent_bead_id,omitempty"` // Optional: Agent bead to update
	AgentUpdates      map[string]interface{} `json:"agent_updates,omitempty"` // Optional: Fields to update on agent
}

// AtomicClosureChainResult represents the result of an atomic_closure_chain operation.
type AtomicClosureChainResult struct {
	MRClosed          bool   `json:"mr_closed"`                   // Whether MR was successfully closed
	SourceIssueClosed bool   `json:"source_issue_closed"`         // Whether source issue was successfully closed
	AgentUpdated      bool   `json:"agent_updated"`               // Whether agent bead was updated
	MRCloseTime       string `json:"mr_close_time,omitempty"`     // ISO 8601 timestamp when MR was closed
	SourceCloseTime   string `json:"source_close_time,omitempty"` // ISO 8601 timestamp when source was closed
}

// Init and Migrate operations (remote database management)

// InitArgs represents arguments for the init operation (remote database initialization)
type InitArgs struct {
	Prefix    string `json:"prefix,omitempty"`     // Issue prefix (auto-detected if empty)
	Backend   string `json:"backend,omitempty"`    // Storage backend: sqlite (default) or dolt
	Branch    string `json:"branch,omitempty"`     // Git branch for beads commits
	Force     bool   `json:"force,omitempty"`      // Force re-initialization even if database exists
	FromJSONL bool   `json:"from_jsonl,omitempty"` // Import from local JSONL file instead of git history
	Quiet     bool   `json:"quiet,omitempty"`      // Suppress output
}

// InitResult represents the result of an init operation
type InitResult struct {
	DatabasePath  string `json:"database_path"`            // Path to the created database
	Prefix        string `json:"prefix"`                   // Issue prefix that was set
	Backend       string `json:"backend"`                  // Storage backend used (sqlite or dolt)
	ImportedCount int    `json:"imported_count,omitempty"` // Number of issues imported (if any)
	Message       string `json:"message,omitempty"`        // Human-readable status message
}

// MigrateArgs represents arguments for the migrate operation (remote database migration)
type MigrateArgs struct {
	DryRun       bool `json:"dry_run,omitempty"`        // Show what would be done without making changes
	Cleanup      bool `json:"cleanup,omitempty"`        // Remove old database files after migration
	Yes          bool `json:"yes,omitempty"`            // Auto-confirm cleanup prompts
	UpdateRepoID bool `json:"update_repo_id,omitempty"` // Update repository ID (use after changing git remote)
	Inspect      bool `json:"inspect,omitempty"`        // Show migration plan and database state
	ToDolt       bool `json:"to_dolt,omitempty"`        // Migrate from SQLite to Dolt backend
	ToSQLite     bool `json:"to_sqlite,omitempty"`      // Migrate from Dolt to SQLite backend (escape hatch)
}

// MigrateResult represents the result of a migrate operation
type MigrateResult struct {
	Status          string   `json:"status"`                     // "success", "noop", "error"
	CurrentDatabase string   `json:"current_database,omitempty"` // Current database name
	Version         string   `json:"version,omitempty"`          // Schema version after migration
	Migrated        bool     `json:"migrated,omitempty"`         // Whether migration was performed
	VersionUpdated  bool     `json:"version_updated,omitempty"`  // Whether version was updated
	CleanedUp       bool     `json:"cleaned_up,omitempty"`       // Whether cleanup was performed
	OldDatabases    []string `json:"old_databases,omitempty"`    // List of old databases found
	Message         string   `json:"message,omitempty"`          // Human-readable status message
}

// Formula CRUD operations (gt-pozvwr.24.9)

// FormulaListArgs represents arguments for the formula_list operation
type FormulaListArgs struct {
	Type   string `json:"type,omitempty"`   // Filter by formula type (workflow, expansion, aspect)
	Phase  string `json:"phase,omitempty"`  // Filter by phase (liquid, vapor)
	Limit  int    `json:"limit,omitempty"`  // Max results to return
}

// FormulaSummary is a compact representation of a formula for list results
type FormulaSummary struct {
	ID          string `json:"id"`                    // Issue ID in database
	Name        string `json:"name"`                  // Formula name
	Description string `json:"description,omitempty"` // Formula description
	Type        string `json:"type,omitempty"`        // Formula type (workflow, expansion, aspect)
	Phase       string `json:"phase,omitempty"`       // Recommended phase (liquid, vapor)
	Version     int    `json:"version,omitempty"`     // Formula schema version
	Source      string `json:"source,omitempty"`      // Where formula was loaded from
}

// FormulaListResult represents the result of a formula_list operation
type FormulaListResult struct {
	Formulas []FormulaSummary `json:"formulas"`
	Count    int              `json:"count"`
}

// FormulaGetArgs represents arguments for the formula_get operation
type FormulaGetArgs struct {
	ID   string `json:"id,omitempty"`   // Issue ID (exact match)
	Name string `json:"name,omitempty"` // Formula name (searches by title)
}

// FormulaGetResult represents the result of a formula_get operation
type FormulaGetResult struct {
	ID       string          `json:"id"`                 // Issue ID
	Name     string          `json:"name"`               // Formula name
	Formula  json.RawMessage `json:"formula"`             // Full formula JSON content
	Source   string          `json:"source,omitempty"`    // Where formula was loaded from
}

// FormulaSaveArgs represents arguments for the formula_save operation
type FormulaSaveArgs struct {
	Formula  json.RawMessage `json:"formula"`            // Full formula JSON content
	IDPrefix string          `json:"id_prefix,omitempty"` // Override prefix for formula ID
	Force    bool            `json:"force,omitempty"`     // Overwrite existing formula
}

// FormulaSaveResult represents the result of a formula_save operation
type FormulaSaveResult struct {
	ID      string `json:"id"`                // Issue ID assigned to formula
	Name    string `json:"name"`              // Formula name
	Created bool   `json:"created"`           // True if new, false if updated
}

// FormulaDeleteArgs represents arguments for the formula_delete operation
type FormulaDeleteArgs struct {
	ID     string `json:"id,omitempty"`     // Issue ID (exact match)
	Name   string `json:"name,omitempty"`   // Formula name (searches by title)
	Reason string `json:"reason,omitempty"` // Reason for deletion
}

// FormulaDeleteResult represents the result of a formula_delete operation
type FormulaDeleteResult struct {
	ID      string `json:"id"`              // Issue ID that was deleted
	Name    string `json:"name"`            // Formula name
}

// RunbookListArgs represents arguments for the runbook_list operation
type RunbookListArgs struct {
	// No filters needed for now
}

// RunbookSummary is a compact representation of a runbook for list results
type RunbookSummary struct {
	Name     string `json:"name"`               // Runbook name
	Format   string `json:"format"`             // File format (hcl, toml, json)
	Source   string `json:"source,omitempty"`   // Where loaded from
	Jobs     int    `json:"jobs"`               // Number of jobs
	Commands int    `json:"commands"`           // Number of commands
	Workers  int    `json:"workers"`            // Number of workers
}

// RunbookListResult represents the result of a runbook_list operation
type RunbookListResult struct {
	Runbooks []RunbookSummary `json:"runbooks"`
	Count    int              `json:"count"`
}

// RunbookGetArgs represents arguments for the runbook_get operation
type RunbookGetArgs struct {
	ID   string `json:"id,omitempty"`   // Issue ID (exact match)
	Name string `json:"name,omitempty"` // Runbook name (searches by title/slug)
}

// RunbookGetResult represents the result of a runbook_get operation
type RunbookGetResult struct {
	ID      string          `json:"id"`              // Issue ID
	Name    string          `json:"name"`            // Runbook name
	Content json.RawMessage `json:"content"`         // Full runbook metadata JSON
}

// RunbookSaveArgs represents arguments for the runbook_save operation
type RunbookSaveArgs struct {
	Content  json.RawMessage `json:"content"`             // Full RunbookContent JSON
	IDPrefix string          `json:"id_prefix,omitempty"` // Override prefix for runbook ID
	Force    bool            `json:"force,omitempty"`     // Overwrite existing runbook
}

// RunbookSaveResult represents the result of a runbook_save operation
type RunbookSaveResult struct {
	ID      string `json:"id"`      // Issue ID assigned to runbook
	Name    string `json:"name"`    // Runbook name
	Created bool   `json:"created"` // True if new, false if updated
}

// BusEmitArgs represents arguments for the bus_emit operation
type BusEmitArgs struct {
	HookType  string          `json:"hook_type"`            // Event type (SessionStart, Stop, etc.)
	EventJSON json.RawMessage `json:"event_json"`           // Raw stdin JSON from Claude Code
	SessionID string          `json:"session_id,omitempty"` // Session ID (extracted from event)
}

// BusEmitResult represents the result of a bus_emit operation
type BusEmitResult struct {
	Block    bool     `json:"block,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Inject   []string `json:"inject,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// BusStatusResult represents the result of a bus_status operation
type BusStatusResult struct {
	NATSEnabled  bool   `json:"nats_enabled"`
	NATSStatus   string `json:"nats_status,omitempty"`   // "running", "stopped", "error"
	NATSPort     int    `json:"nats_port,omitempty"`
	Connections  int    `json:"connections,omitempty"`
	JetStream    bool   `json:"jetstream,omitempty"`
	Streams      int    `json:"streams,omitempty"`
	HandlerCount int    `json:"handler_count"`
}

// BusHandlersResult represents the result of a bus_handlers operation
type BusHandlersResult struct {
	Handlers []BusHandlerInfo `json:"handlers"`
}

// BusHandlerInfo describes a registered event bus handler.
type BusHandlerInfo struct {
	ID       string   `json:"id"`
	Priority int      `json:"priority"`
	Handles  []string `json:"handles"`
}

// ===== Agent Pod Operations (gt-el7sxq.7) =====

// AgentPodRegisterArgs registers pod fields on an agent bead.
type AgentPodRegisterArgs struct {
	AgentID       string `json:"agent_id"`                 // Agent bead ID
	PodName       string `json:"pod_name"`                 // K8s pod name
	PodIP         string `json:"pod_ip,omitempty"`         // Pod IP address
	PodNode       string `json:"pod_node,omitempty"`       // K8s node name
	PodStatus     string `json:"pod_status,omitempty"`     // Pod status (default: "running")
	ScreenSession string `json:"screen_session,omitempty"` // Screen/tmux session name
}

// AgentPodRegisterResult is returned after pod registration.
type AgentPodRegisterResult struct {
	AgentID   string `json:"agent_id"`
	PodName   string `json:"pod_name"`
	PodStatus string `json:"pod_status"`
}

// AgentPodDeregisterArgs clears all pod fields on an agent bead.
type AgentPodDeregisterArgs struct {
	AgentID string `json:"agent_id"` // Agent bead ID
}

// AgentPodDeregisterResult is returned after pod deregistration.
type AgentPodDeregisterResult struct {
	AgentID string `json:"agent_id"`
}

// AgentPodStatusArgs updates the pod_status field on an agent bead.
type AgentPodStatusArgs struct {
	AgentID   string `json:"agent_id"`   // Agent bead ID
	PodStatus string `json:"pod_status"` // New pod status
}

// AgentPodStatusResult is returned after pod status update.
type AgentPodStatusResult struct {
	AgentID   string `json:"agent_id"`
	PodStatus string `json:"pod_status"`
}

// AgentPodListArgs queries agents with active pods.
type AgentPodListArgs struct {
	Rig string `json:"rig,omitempty"` // Filter by rig (optional)
}

// AgentPodInfo represents an agent with pod information.
type AgentPodInfo struct {
	AgentID       string `json:"agent_id"`
	PodName       string `json:"pod_name"`
	PodIP         string `json:"pod_ip,omitempty"`
	PodNode       string `json:"pod_node,omitempty"`
	PodStatus     string `json:"pod_status"`
	ScreenSession string `json:"screen_session,omitempty"`
	AgentState    string `json:"agent_state,omitempty"`
	Rig           string `json:"rig,omitempty"`
	RoleType      string `json:"role_type,omitempty"`
}

// AgentPodListResult is returned from pod list queries.
type AgentPodListResult struct {
	Agents []AgentPodInfo `json:"agents"`
}

// ===== VCS Operations (bd-ma0s.2) =====

// VcsCommitArgs represents arguments for the vcs_commit operation.
type VcsCommitArgs struct {
	Message string `json:"message"` // Commit message
}

// VcsCommitResult represents the result of a vcs_commit operation.
type VcsCommitResult struct {
	Success bool `json:"success"`
}

// VcsPushResult represents the result of a vcs_push operation.
type VcsPushResult struct {
	Success bool `json:"success"`
}

// VcsPullResult represents the result of a vcs_pull operation.
type VcsPullResult struct {
	Success bool `json:"success"`
}

// VcsMergeArgs represents arguments for the vcs_merge operation.
type VcsMergeArgs struct {
	Branch string `json:"branch"` // Branch to merge into current branch
}

// VcsConflict represents a single merge conflict.
type VcsConflict struct {
	IssueID     string `json:"issue_id,omitempty"`
	Field       string `json:"field,omitempty"`
	OursValue   string `json:"ours_value,omitempty"`
	TheirsValue string `json:"theirs_value,omitempty"`
}

// ===== Federation Operations (bd-ma0s.4) =====

// FedListRemotesArgs represents arguments for the fed_list_remotes operation.
type FedListRemotesArgs struct {
	// No arguments needed
}

// FedRemoteInfo represents a single remote in the response.
type FedRemoteInfo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// FedListRemotesResult represents the result of a fed_list_remotes operation.
type FedListRemotesResult struct {
	Remotes []FedRemoteInfo `json:"remotes"`
}

// FedSyncArgs represents arguments for the fed_sync operation.
type FedSyncArgs struct {
	Peer     string `json:"peer"`               // Peer name to sync with
	Strategy string `json:"strategy,omitempty"`  // Conflict resolution strategy: "ours" or "theirs"
}

// FedConflictInfo represents a single merge conflict in the response.
type FedConflictInfo struct {
	IssueID     string `json:"issue_id,omitempty"`
	Field       string `json:"field"`
	OursValue   string `json:"ours_value,omitempty"`
	TheirsValue string `json:"theirs_value,omitempty"`
}

// VcsMergeResult represents the result of a vcs_merge operation.
type VcsMergeResult struct {
	Success   bool          `json:"success"`
	Conflicts []VcsConflict `json:"conflicts,omitempty"`
}

// VcsBranchCreateArgs represents arguments for the vcs_branch_create operation.
type VcsBranchCreateArgs struct {
	Name string `json:"name"` // Branch name to create
}

// VcsBranchCreateResult represents the result of a vcs_branch_create operation.
type VcsBranchCreateResult struct {
	Name string `json:"name"`
}

// VcsBranchDeleteArgs represents arguments for the vcs_branch_delete operation.
type VcsBranchDeleteArgs struct {
	Name string `json:"name"` // Branch name to delete
}

// VcsBranchDeleteResult represents the result of a vcs_branch_delete operation.
type VcsBranchDeleteResult struct {
	Name string `json:"name"`
}

// VcsCheckoutArgs represents arguments for the vcs_checkout operation.
type VcsCheckoutArgs struct {
	Branch string `json:"branch"` // Branch to check out
}

// VcsCheckoutResult represents the result of a vcs_checkout operation.
type VcsCheckoutResult struct {
	Branch string `json:"branch"`
}

// VcsActiveBranchResult represents the result of a vcs_active_branch operation.
type VcsActiveBranchResult struct {
	Branch string `json:"branch"`
}

// VcsStatusEntry represents a changed table in Dolt status.
type VcsStatusEntry struct {
	Table  string `json:"table"`
	Status string `json:"status"` // "new", "modified", "deleted"
}

// VcsStatusResult represents the result of a vcs_status operation.
type VcsStatusResult struct {
	Staged   []VcsStatusEntry `json:"staged"`
	Unstaged []VcsStatusEntry `json:"unstaged"`
}

// VcsHasUncommittedResult represents the result of a vcs_has_uncommitted operation.
type VcsHasUncommittedResult struct {
	HasUncommitted bool `json:"has_uncommitted"`
}

// VcsBranchesResult represents the result of a vcs_branches operation.
type VcsBranchesResult struct {
	Branches []string `json:"branches"`
}

// VcsCurrentCommitResult represents the result of a vcs_current_commit operation.
type VcsCurrentCommitResult struct {
	Hash string `json:"hash"`
}

// VcsCommitExistsArgs represents arguments for the vcs_commit_exists operation.
type VcsCommitExistsArgs struct {
	Hash string `json:"hash"` // Commit hash to check
}

// VcsCommitExistsResult represents the result of a vcs_commit_exists operation.
type VcsCommitExistsResult struct {
	Exists bool `json:"exists"`
}

// VcsLogArgs represents arguments for the vcs_log operation.
type VcsLogArgs struct {
	Limit int `json:"limit,omitempty"` // Max entries to return (default 10)
}

// VcsLogEntry represents a single commit log entry.
type VcsLogEntry struct {
	Hash    string    `json:"hash"`
	Author  string    `json:"author"`
	Email   string    `json:"email"`
	Date    time.Time `json:"date"`
	Message string    `json:"message"`
}

// VcsLogResult represents the result of a vcs_log operation.
type VcsLogResult struct {
	Commits []VcsLogEntry `json:"commits"`
}

// AdminGCArgs represents arguments for the admin_gc operation (bd-ma0s.5).
type AdminGCArgs struct {
	DryRun bool `json:"dry_run,omitempty"` // If true, report sizes without running GC
}

// AdminGCResult represents the result of a dolt gc operation.
type AdminGCResult struct {
	DoltPath    string `json:"dolt_path"`
	BytesBefore int64  `json:"bytes_before"`
	BytesAfter  int64  `json:"bytes_after"`
	SpaceFreed  int64  `json:"space_freed"`
	DryRun      bool   `json:"dry_run,omitempty"`
	ElapsedMs   int64  `json:"elapsed_ms"`
}

// FedSyncResult represents the result of a fed_sync operation.
type FedSyncResult struct {
	Peer              string            `json:"peer"`
	Fetched           bool              `json:"fetched"`
	Merged            bool              `json:"merged"`
	Pushed            bool              `json:"pushed"`
	PulledCommits     int               `json:"pulled_commits"`
	Conflicts         []FedConflictInfo `json:"conflicts,omitempty"`
	ConflictsResolved bool              `json:"conflicts_resolved"`
	PushError         string            `json:"push_error,omitempty"`
	DurationMs        int64             `json:"duration_ms"`
}

// FedSyncStatusArgs represents arguments for the fed_sync_status operation.
type FedSyncStatusArgs struct {
	Peer string `json:"peer"` // Peer name to check
}

// FedSyncStatusResult represents the result of a fed_sync_status operation.
type FedSyncStatusResult struct {
	Peer         string `json:"peer"`
	LocalAhead   int    `json:"local_ahead"`
	LocalBehind  int    `json:"local_behind"`
	HasConflicts bool   `json:"has_conflicts"`
	LastSync     string `json:"last_sync,omitempty"` // ISO 8601 timestamp
}

// FedFetchArgs represents arguments for the fed_fetch operation.
type FedFetchArgs struct {
	Peer string `json:"peer"` // Peer name to fetch from
}

// FedFetchResult represents the result of a fed_fetch operation.
type FedFetchResult struct {
	Peer string `json:"peer"`
}

// FedPushToArgs represents arguments for the fed_push_to operation.
type FedPushToArgs struct {
	Peer string `json:"peer"` // Peer name to push to
}

// FedPushToResult represents the result of a fed_push_to operation.
type FedPushToResult struct {
	Peer string `json:"peer"`
}

// FedPullFromArgs represents arguments for the fed_pull_from operation.
type FedPullFromArgs struct {
	Peer string `json:"peer"` // Peer name to pull from
}

// FedPullFromResult represents the result of a fed_pull_from operation.
type FedPullFromResult struct {
	Peer      string            `json:"peer"`
	Conflicts []FedConflictInfo `json:"conflicts,omitempty"`
}

// FedAddRemoteArgs represents arguments for the fed_add_remote operation.
type FedAddRemoteArgs struct {
	Name string `json:"name"` // Remote name
	URL  string `json:"url"`  // Remote URL
}

// FedAddRemoteResult represents the result of a fed_add_remote operation.
type FedAddRemoteResult struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// FedRemoveRemoteArgs represents arguments for the fed_remove_remote operation.
type FedRemoveRemoteArgs struct {
	Name string `json:"name"` // Remote name to remove
}

// FedRemoveRemoteResult represents the result of a fed_remove_remote operation.
type FedRemoveRemoteResult struct {
	Name string `json:"name"`
}

// FedAddPeerArgs represents arguments for the fed_add_peer operation.
type FedAddPeerArgs struct {
	Name        string `json:"name"`                  // Peer name
	URL         string `json:"url"`                   // Remote URL
	Username    string `json:"username,omitempty"`     // SQL username
	Password    string `json:"password,omitempty"`     // SQL password
	Sovereignty string `json:"sovereignty,omitempty"`  // Sovereignty tier: T1, T2, T3, T4
}

// FedAddPeerResult represents the result of a fed_add_peer operation.
type FedAddPeerResult struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	HasAuth     bool   `json:"has_auth"`
	Sovereignty string `json:"sovereignty,omitempty"`
}

// ===== History Query Operations (bd-ma0s.3) =====

// HistoryIssueArgs represents arguments for the history_issue operation.
type HistoryIssueArgs struct {
	IssueID string `json:"issue_id"` // Issue ID to get history for
}

// HistoryEntryRPC represents a single history entry for RPC transport.
type HistoryEntryRPC struct {
	CommitHash string       `json:"commit_hash"`
	Committer  string       `json:"committer"`
	CommitDate time.Time    `json:"commit_date"`
	Issue      *types.Issue `json:"issue"`
}

// HistoryIssueResult represents the result of a history_issue operation.
type HistoryIssueResult struct {
	Entries []HistoryEntryRPC `json:"entries"`
}

// HistoryDiffArgs represents arguments for the history_diff operation.
// Returns low-level table-level diffs between two commits.
type HistoryDiffArgs struct {
	FromRef string `json:"from_ref"` // Source commit/branch ref
	ToRef   string `json:"to_ref"`   // Target commit/branch ref
}

// HistoryDiffEntryRPC represents a low-level diff entry for RPC transport.
type HistoryDiffEntryRPC struct {
	TableName  string `json:"table_name"`
	DiffType   string `json:"diff_type"`   // "added", "modified", "removed"
	FromCommit string `json:"from_commit"`
	ToCommit   string `json:"to_commit"`
}

// HistoryDiffResult represents the result of a history_diff operation.
type HistoryDiffResult struct {
	Entries []HistoryDiffEntryRPC `json:"entries"`
}

// HistoryIssueDiffArgs represents arguments for the history_issue_diff operation.
// Returns detailed changes to a specific issue between two commits.
type HistoryIssueDiffArgs struct {
	IssueID string `json:"issue_id"` // Issue ID to get diff for
	FromRef string `json:"from_ref"` // Source commit/branch ref
	ToRef   string `json:"to_ref"`   // Target commit/branch ref
}

// HistoryIssueDiffResult represents the result of a history_issue_diff operation.
type HistoryIssueDiffResult struct {
	DiffType        string `json:"diff_type"` // "added", "modified", "removed"
	FromID          string `json:"from_id,omitempty"`
	ToID            string `json:"to_id,omitempty"`
	FromTitle       string `json:"from_title,omitempty"`
	ToTitle         string `json:"to_title,omitempty"`
	FromStatus      string `json:"from_status,omitempty"`
	ToStatus        string `json:"to_status,omitempty"`
	FromDescription string `json:"from_description,omitempty"`
	ToDescription   string `json:"to_description,omitempty"`
	Found           bool   `json:"found"` // Whether the issue was found in the diff
}

// HistoryConflictsArgs represents arguments for the history_conflicts operation.
type HistoryConflictsArgs struct {
	// No arguments needed
}

// HistoryConflictRPC represents a table-level merge conflict for RPC transport.
type HistoryConflictRPC struct {
	TableName    string `json:"table_name"`
	NumConflicts int    `json:"num_conflicts"`
}

// HistoryConflictsResult represents the result of a history_conflicts operation.
type HistoryConflictsResult struct {
	Conflicts []HistoryConflictRPC `json:"conflicts"`
}

// HistoryResolveConflictsArgs represents arguments for the history_resolve_conflicts operation.
type HistoryResolveConflictsArgs struct {
	Table    string `json:"table"`    // Table name to resolve conflicts for
	Strategy string `json:"strategy"` // "ours" or "theirs"
}

// HistoryResolveConflictsResult represents the result of a history_resolve_conflicts operation.
type HistoryResolveConflictsResult struct {
	Resolved bool `json:"resolved"`
}

// VersionedDiffArgs represents arguments for the versioned_diff operation.
// Returns issue-level diffs with full Issue data between two commits.
type VersionedDiffArgs struct {
	FromRef string `json:"from_ref"` // Source commit/branch ref
	ToRef   string `json:"to_ref"`   // Target commit/branch ref
}

// VersionedDiffEntryRPC represents an issue-level diff entry for RPC transport.
type VersionedDiffEntryRPC struct {
	IssueID  string       `json:"issue_id"`
	DiffType string       `json:"diff_type"` // "added", "modified", "removed"
	OldValue *types.Issue `json:"old_value,omitempty"`
	NewValue *types.Issue `json:"new_value,omitempty"`
}

// VersionedDiffResult represents the result of a versioned_diff operation.
type VersionedDiffResult struct {
	Entries []VersionedDiffEntryRPC `json:"entries"`
}
