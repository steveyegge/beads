package gate

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// DefaultStaleThreshold is the default time after which a session is considered stale.
const DefaultStaleThreshold = 30 * time.Minute

// RegisterUserPromptSubmitGates registers the built-in UserPromptSubmit gates.
func RegisterUserPromptSubmitGates(reg *Registry) {
	_ = reg.Register(ContextInjectionGate())
	_ = reg.Register(StaleContextGate())
}

// ContextInjectionGate returns the "context-injection" gate definition.
// Warns when there are pending inject queue items that haven't been drained.
func ContextInjectionGate() *Gate {
	return &Gate{
		ID:          "context-injection",
		Hook:        HookUserPromptSubmit,
		Description: "pending context injections not drained",
		Mode:        GateModeSoft,
		AutoCheck:   checkInjectQueueEmpty, // reuse from builtin_precompact.go
		Hint:        "pending context injections not drained — run gt inject drain",
	}
}

// StaleContextGate returns the "stale-context" gate definition.
// Warns when the session has been idle for too long.
func StaleContextGate() *Gate {
	return &Gate{
		ID:          "stale-context",
		Hook:        HookUserPromptSubmit,
		Description: "session context may be stale",
		Mode:        GateModeSoft,
		AutoCheck:   checkContextFresh,
		Hint:        "session may have stale context — consider running gt prime for fresh context",
	}
}

// checkContextFresh returns true if the session has had recent activity.
// Checks the last-activity timestamp in .runtime/activity/<session-id>.
func checkContextFresh(ctx GateContext) bool {
	if ctx.WorkDir == "" || ctx.SessionID == "" {
		return true // can't check, fail open
	}

	activityFile := filepath.Join(ctx.WorkDir, ".runtime", "activity", ctx.SessionID)
	data, err := os.ReadFile(activityFile)
	if err != nil {
		return true // no activity file = can't determine staleness, fail open
	}

	// Parse Unix timestamp from the activity file
	ts, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return true
	}

	lastActivity := time.Unix(ts, 0)
	threshold := DefaultStaleThreshold

	// Allow override via BD_STALE_THRESHOLD_MINUTES env var
	if threshStr := os.Getenv("BD_STALE_THRESHOLD_MINUTES"); threshStr != "" {
		if mins, err := strconv.Atoi(threshStr); err == nil && mins > 0 {
			threshold = time.Duration(mins) * time.Minute
		}
	}

	return time.Since(lastActivity) < threshold
}

// TouchActivity updates the activity timestamp for the session.
// Called by hook chains to record that the session is active.
func TouchActivity(workDir, sessionID string) error {
	dir := filepath.Join(workDir, ".runtime", "activity")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	return os.WriteFile(filepath.Join(dir, sessionID), []byte(ts), 0o644)
}
