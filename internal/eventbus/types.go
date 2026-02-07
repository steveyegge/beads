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
