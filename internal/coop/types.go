package coop

// HealthResponse is returned by GET /api/v1/health.
type HealthResponse struct {
	Status    string       `json:"status"`
	PID       *int         `json:"pid,omitempty"`
	UptimeSec int64        `json:"uptime_secs"`
	AgentType string       `json:"agent_type"`
	Terminal  TerminalSize `json:"terminal"`
	WSClients int          `json:"ws_clients"`
}

// TerminalSize describes the terminal dimensions.
type TerminalSize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// StatusResponse is returned by GET /api/v1/status.
type StatusResponse struct {
	State        string `json:"state"`
	PID          *int   `json:"pid,omitempty"`
	ExitCode     *int   `json:"exit_code,omitempty"`
	ScreenSeq    uint64 `json:"screen_seq"`
	BytesRead    uint64 `json:"bytes_read"`
	BytesWritten uint64 `json:"bytes_written"`
	WSClients    int    `json:"ws_clients"`
}

// AgentStateResponse is returned by GET /api/v1/agent/state.
type AgentStateResponse struct {
	AgentType              string         `json:"agent_type"`
	State                  string         `json:"state"`
	SinceSeq               uint64         `json:"since_seq"`
	ScreenSeq              uint64         `json:"screen_seq"`
	DetectionTier          string         `json:"detection_tier"`
	IdleGraceRemainingSecs *float64       `json:"idle_grace_remaining_secs,omitempty"`
	Prompt                 *PromptContext `json:"prompt,omitempty"`
}

// PromptContext describes the active prompt when the agent is in a prompt state.
type PromptContext struct {
	Type         string   `json:"type"`
	Tool         string   `json:"tool,omitempty"`
	InputPreview string   `json:"input_preview,omitempty"`
	Question     string   `json:"question,omitempty"`
	Options      []string `json:"options,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	ScreenLines  []string `json:"screen_lines,omitempty"`
}

// ScreenResponse is returned by GET /api/v1/screen.
type ScreenResponse struct {
	Lines     []string        `json:"lines"`
	Rows      int             `json:"rows"`
	Cols      int             `json:"cols"`
	Cursor    *CursorPosition `json:"cursor,omitempty"`
	AltScreen bool            `json:"alt_screen"`
	Sequence  uint64          `json:"sequence"`
}

// CursorPosition is the cursor location in the terminal.
type CursorPosition struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// NudgeRequest is the body for POST /api/v1/agent/nudge.
type NudgeRequest struct {
	Message string `json:"message"`
}

// NudgeResponse is returned by POST /api/v1/agent/nudge.
type NudgeResponse struct {
	Delivered   bool   `json:"delivered"`
	StateBefore string `json:"state_before,omitempty"`
	Reason      string `json:"reason,omitempty"`
	State       string `json:"state,omitempty"`
}

// RespondRequest is the body for POST /api/v1/agent/respond.
// Use pointer fields so callers can set exactly one action.
type RespondRequest struct {
	Accept *bool  `json:"accept,omitempty"`
	Option *int   `json:"option,omitempty"`
	Text   string `json:"text,omitempty"`
}

// RespondResponse is returned by POST /api/v1/agent/respond.
type RespondResponse struct {
	Delivered  bool   `json:"delivered"`
	PromptType string `json:"prompt_type,omitempty"`
	Reason     string `json:"reason,omitempty"`
	State      string `json:"state,omitempty"`
}

// SignalRequest is the body for POST /api/v1/signal.
type SignalRequest struct {
	Signal string `json:"signal"`
}

// InputRequest is the body for POST /api/v1/input.
type InputRequest struct {
	Text  string `json:"text"`
	Enter bool   `json:"enter"`
}

// InputResponse is returned by POST /api/v1/input.
type InputResponse struct {
	BytesWritten int `json:"bytes_written"`
}

// ErrorResponse is the standard error body from Coop.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// EnvGetResponse is returned by GET /api/v1/env/:key.
type EnvGetResponse struct {
	Key    string  `json:"key"`
	Value  *string `json:"value"`
	Source string  `json:"source"`
}

// EnvPutRequest is the body for PUT /api/v1/env/:key.
type EnvPutRequest struct {
	Value string `json:"value"`
}

// EnvPutResponse is returned by PUT/DELETE /api/v1/env/:key.
type EnvPutResponse struct {
	Key     string `json:"key"`
	Updated bool   `json:"updated"`
}

// EnvListResponse is returned by GET /api/v1/env.
type EnvListResponse struct {
	Vars    map[string]string `json:"vars"`
	Pending map[string]string `json:"pending"`
}

// CwdResponse is returned by GET /api/v1/session/cwd.
type CwdResponse struct {
	Cwd string `json:"cwd"`
}

// SessionInfo contains metadata about an agent session.
type SessionInfo struct {
	SessionID string `json:"session_id"`
	PID       int    `json:"pid"`
	Uptime    int64  `json:"uptime_secs"`
	Ready     bool   `json:"ready"`
	AgentType string `json:"agent_type"`
	Backend   string `json:"backend"`
}

// Agent state constants matching Coop's agent state enum.
const (
	StateStarting        = "starting"
	StateWorking         = "working"
	StateWaitingForInput = "waiting_for_input"
	StatePermissionPrompt = "permission_prompt"
	StatePlanPrompt      = "plan_prompt"
	StateAskUser         = "ask_user"
	StateError           = "error"
	StateAltScreen       = "alt_screen"
	StateExited          = "exited"
	StateUnknown         = "unknown"
)

// Process state constants from /api/v1/status.
const (
	ProcessRunning = "running"
	ProcessExited  = "exited"
)

// WebSocket message types (server → client).
const (
	WSTypeStateChange = "state_change"
	WSTypeExit        = "exit"
	WSTypeError       = "error"
	WSTypeScreen      = "screen"
	WSTypeOutput      = "output"
	WSTypePong        = "pong"
	WSTypeResize      = "resize"
)

// StateChangeEvent is a WebSocket state_change message.
type StateChangeEvent struct {
	Type   string         `json:"type"`
	Prev   string         `json:"prev"`
	Next   string         `json:"next"`
	Seq    uint64         `json:"seq"`
	Prompt *PromptContext `json:"prompt,omitempty"`
}

// ExitEvent is a WebSocket exit message.
type ExitEvent struct {
	Type   string `json:"type"`
	Code   *int   `json:"code,omitempty"`
	Signal string `json:"signal,omitempty"`
}
