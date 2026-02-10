package eventbus

import (
	"encoding/json"
	"time"
)

// EventType identifies an event flowing through the bus.
// Hook events map 1:1 to Claude Code hook events; decision events are the
// first non-hook event category.
type EventType string

const (
	// Claude Code hook events.
	EventSessionStart       EventType = "SessionStart"
	EventUserPromptSubmit   EventType = "UserPromptSubmit"
	EventPreToolUse         EventType = "PreToolUse"
	EventPostToolUse        EventType = "PostToolUse"
	EventPostToolUseFailure EventType = "PostToolUseFailure"
	EventStop               EventType = "Stop"
	EventPreCompact         EventType = "PreCompact"
	EventSubagentStart      EventType = "SubagentStart"
	EventSubagentStop       EventType = "SubagentStop"
	EventNotification       EventType = "Notification"
	EventSessionEnd         EventType = "SessionEnd"

	// Advice CRUD events (bd-z4cu.2)
	EventAdviceCreated EventType = "advice.created"
	EventAdviceUpdated EventType = "advice.updated"
	EventAdviceDeleted EventType = "advice.deleted"

	// Decision events (od-k3o.15.1).
	EventDecisionCreated   EventType = "DecisionCreated"
	EventDecisionResponded EventType = "DecisionResponded"
	EventDecisionEscalated EventType = "DecisionEscalated"
	EventDecisionExpired   EventType = "DecisionExpired"

	// OddJobs lifecycle events (bd-4q86.4).
	EventOjJobCreated        EventType = "OjJobCreated"
	EventOjStepAdvanced      EventType = "OjStepAdvanced"
	EventOjAgentSpawned      EventType = "OjAgentSpawned"
	EventOjAgentIdle         EventType = "OjAgentIdle"
	EventOjAgentEscalated    EventType = "OjAgentEscalated"
	EventOjJobCompleted      EventType = "OjJobCompleted"
	EventOjJobFailed         EventType = "OjJobFailed"
	EventOjWorkerPollComplete EventType = "OjWorkerPollComplete"

	// Agent lifecycle events (bd-e6vh).
	EventAgentStarted   EventType = "AgentStarted"
	EventAgentStopped   EventType = "AgentStopped"
	EventAgentCrashed   EventType = "AgentCrashed"
	EventAgentIdle      EventType = "AgentIdle"
	EventAgentHeartbeat EventType = "AgentHeartbeat"

	// Mail events (bd-h59f).
	EventMailSent EventType = "MailSent"
	EventMailRead EventType = "MailRead"

	// Bead mutation events (bd-laz4).
	EventMutationCreate  EventType = "MutationCreate"
	EventMutationUpdate  EventType = "MutationUpdate"
	EventMutationDelete  EventType = "MutationDelete"
	EventMutationComment EventType = "MutationComment"
	EventMutationStatus  EventType = "MutationStatus"
)

// Event represents a single hook event flowing through the bus.
type Event struct {
	Type           EventType       `json:"hook_event_name"`
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	CWD            string          `json:"cwd"`
	PermissionMode string          `json:"permission_mode"`
	Raw            json.RawMessage `json:"-"`

	// Hook-specific fields, populated based on Type.
	ToolName     string                 `json:"tool_name,omitempty"`
	ToolInput    map[string]interface{} `json:"tool_input,omitempty"`
	ToolResponse map[string]interface{} `json:"tool_response,omitempty"`
	Prompt       string                 `json:"prompt,omitempty"`
	Source       string                 `json:"source,omitempty"`
	Model        string                 `json:"model,omitempty"`
	AgentID      string                 `json:"agent_id,omitempty"`
	AgentType    string                 `json:"agent_type,omitempty"`
	Error        string                 `json:"error,omitempty"`

	// PublishedAt is set by the bus when publishing to JetStream (not from Claude Code).
	// Only populated when Raw is empty and the Event struct is marshaled.
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// IsDecisionEvent returns true if the event type belongs to the decision
// event category (as opposed to Claude Code hook events).
func (t EventType) IsDecisionEvent() bool {
	switch t {
	case EventDecisionCreated, EventDecisionResponded,
		EventDecisionEscalated, EventDecisionExpired:
		return true
	}
	return false
}

// IsOjEvent returns true if the event type belongs to the OddJobs
// event category.
func (t EventType) IsOjEvent() bool {
	switch t {
	case EventOjJobCreated, EventOjStepAdvanced,
		EventOjAgentSpawned, EventOjAgentIdle,
		EventOjAgentEscalated, EventOjJobCompleted,
		EventOjJobFailed, EventOjWorkerPollComplete:
		return true
	}
	return false
}

// IsAgentEvent returns true if the event type belongs to the agent
// lifecycle event category (bd-e6vh).
func (t EventType) IsAgentEvent() bool {
	switch t {
	case EventAgentStarted, EventAgentStopped,
		EventAgentCrashed, EventAgentIdle,
		EventAgentHeartbeat:
		return true
	}
	return false
}

// IsMailEvent returns true if the event type belongs to the mail
// event category (bd-h59f).
func (t EventType) IsMailEvent() bool {
	switch t {
	case EventMailSent, EventMailRead:
		return true
	}
	return false
}

// IsMutationEvent returns true if the event type belongs to the bead
// mutation event category (bd-laz4).
func (t EventType) IsMutationEvent() bool {
	switch t {
	case EventMutationCreate, EventMutationUpdate,
		EventMutationDelete, EventMutationComment,
		EventMutationStatus:
		return true
	}
	return false
}

// OjJobEventPayload carries data for OJ job lifecycle events.
// Used by OjJobCreated, OjJobCompleted, OjJobFailed.
type OjJobEventPayload struct {
	JobID    string `json:"job_id"`
	JobName  string `json:"job_name,omitempty"`
	Worker   string `json:"worker,omitempty"`
	BeadID   string `json:"bead_id,omitempty"`   // Associated bead (if any)
	ExitCode int    `json:"exit_code,omitempty"` // For completed/failed
	Error    string `json:"error,omitempty"`     // For failed
}

// OjStepEventPayload carries data for OjStepAdvanced events.
type OjStepEventPayload struct {
	JobID    string `json:"job_id"`
	FromStep string `json:"from_step"`
	ToStep   string `json:"to_step"`
	BeadID   string `json:"bead_id,omitempty"`
}

// OjAgentEventPayload carries data for OJ agent lifecycle events.
// Used by OjAgentSpawned, OjAgentIdle, OjAgentEscalated.
type OjAgentEventPayload struct {
	JobID     string `json:"job_id"`
	AgentName string `json:"agent_name"`
	SessionID string `json:"session_id,omitempty"` // tmux/coop session
	BeadID    string `json:"bead_id,omitempty"`
	Reason    string `json:"reason,omitempty"` // For escalated
}

// OjWorkerPollPayload carries data for OjWorkerPollComplete events.
type OjWorkerPollPayload struct {
	Worker    string `json:"worker"`
	Queue     string `json:"queue"`
	ItemCount int    `json:"item_count"` // Items found in poll
}

// AgentEventPayload carries data for agent lifecycle events.
// Used by AgentStarted, AgentStopped, AgentCrashed, AgentIdle, AgentHeartbeat.
type AgentEventPayload struct {
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name,omitempty"`
	RigName   string `json:"rig_name,omitempty"`
	Role      string `json:"role,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Reason    string `json:"reason,omitempty"`     // for stopped/crashed
	Uptime    int64  `json:"uptime_sec,omitempty"` // for heartbeat
}

// MailEventPayload carries data for mail events (bd-h59f).
// Used by MailSent and MailRead events.
type MailEventPayload struct {
	MessageID string `json:"message_id,omitempty"`
	From      string `json:"from"`
	To        string `json:"to"`
	Subject   string `json:"subject"`
	SentAt    string `json:"sent_at,omitempty"`
}

// MutationEventPayload carries data for bead mutation events (bd-laz4).
// Mirrors the rpc.MutationEvent struct for JetStream publishing.
type MutationEventPayload struct {
	Type      string   `json:"type"`                 // create, update, delete, comment, status, etc.
	IssueID   string   `json:"issue_id"`
	Title     string   `json:"title,omitempty"`
	Assignee  string   `json:"assignee,omitempty"`
	Actor     string   `json:"actor,omitempty"`
	Timestamp string   `json:"timestamp"`
	OldStatus string   `json:"old_status,omitempty"`
	NewStatus string   `json:"new_status,omitempty"`
	ParentID  string   `json:"parent_id,omitempty"`
	IssueType string   `json:"issue_type,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	AwaitType string   `json:"await_type,omitempty"`
}

// DecisionEventPayload carries data for decision events in Event.Raw.
type DecisionEventPayload struct {
	DecisionID  string `json:"decision_id"`
	Question    string `json:"question"`
	Urgency     string `json:"urgency,omitempty"`
	RequestedBy string `json:"requested_by,omitempty"`
	Options     int    `json:"option_count"`
	// Populated for responded/escalated events.
	ChosenIndex int    `json:"chosen_index,omitempty"`
	ChosenLabel string `json:"chosen_label,omitempty"`
	ResolvedBy  string `json:"resolved_by,omitempty"`
	Rationale   string `json:"rationale,omitempty"`
}

// Result aggregates handler responses for an event.
type Result struct {
	Block    bool     `json:"block,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Inject   []string `json:"inject,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}
