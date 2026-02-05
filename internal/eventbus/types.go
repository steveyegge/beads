package eventbus

import "encoding/json"

// EventType maps 1:1 to Claude Code hook events.
type EventType string

const (
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
}

// Result aggregates handler responses for an event.
type Result struct {
	Block    bool     `json:"block,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Inject   []string `json:"inject,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}
