package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/steveyegge/beads/internal/types"
)

// DecisionHookPayload is the JSON payload sent to decision hooks.
// It combines the decision point data with the event type.
// (hq-946577.27)
type DecisionHookPayload struct {
	// ID is the decision point ID (e.g., "gt-abc123.decision-1")
	ID string `json:"id"`

	// Prompt is the question being asked
	Prompt string `json:"prompt"`

	// Options are the available choices
	Options []types.DecisionOption `json:"options"`

	// Event is the hook event type: "create", "respond", or "timeout"
	Event string `json:"event"`

	// Response contains the human's response (only for respond/timeout events)
	Response *DecisionResponsePayload `json:"response,omitempty"`

	// Iteration is the current iteration number
	Iteration int `json:"iteration"`

	// MaxIterations is the maximum allowed iterations
	MaxIterations int `json:"max_iterations"`

	// Guidance is the prior iteration's text guidance (if iteration > 1)
	Guidance string `json:"guidance,omitempty"`
}

// DecisionResponsePayload contains the response details.
type DecisionResponsePayload struct {
	// Selected is the option ID chosen (may be empty for text-only)
	Selected string `json:"selected,omitempty"`

	// Text is the custom text response/guidance
	Text string `json:"text,omitempty"`

	// RespondedBy is who responded (email, user ID)
	RespondedBy string `json:"responded_by,omitempty"`

	// IsTimeout is true if this response was due to timeout
	IsTimeout bool `json:"is_timeout,omitempty"`
}

// RunDecision executes a decision hook if it exists.
// Runs asynchronously - returns immediately, hook runs in background.
func (r *Runner) RunDecision(event string, dp *types.DecisionPoint, response *DecisionResponsePayload) {
	hookName := eventToHook(event)
	if hookName == "" {
		return
	}

	hookPath := filepath.Join(r.hooksDir, hookName)

	// Check if hook exists and is executable
	info, err := os.Stat(hookPath)
	if err != nil || info.IsDir() {
		return // Hook doesn't exist, skip silently
	}

	if info.Mode()&0111 == 0 {
		return // Not executable, skip
	}

	// Run asynchronously (ignore error as this is fire-and-forget)
	go func() {
		_ = r.runDecisionHook(hookPath, event, dp, response)
	}()
}

// RunDecisionSync executes a decision hook synchronously and returns any error.
// Useful for testing or when you need to wait for the hook.
func (r *Runner) RunDecisionSync(event string, dp *types.DecisionPoint, response *DecisionResponsePayload) error {
	hookName := eventToHook(event)
	if hookName == "" {
		return nil
	}

	hookPath := filepath.Join(r.hooksDir, hookName)

	// Check if hook exists and is executable
	info, err := os.Stat(hookPath)
	if err != nil || info.IsDir() {
		return nil // Hook doesn't exist, skip silently
	}

	if info.Mode()&0111 == 0 {
		return nil // Not executable, skip
	}

	return r.runDecisionHook(hookPath, event, dp, response)
}

// runDecisionHook executes the decision hook script.
func (r *Runner) runDecisionHook(hookPath, event string, dp *types.DecisionPoint, response *DecisionResponsePayload) error {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()

	// Build payload
	payload := DecisionHookPayload{
		ID:            dp.IssueID,
		Prompt:        dp.Prompt,
		Event:         event,
		Response:      response,
		Iteration:     dp.Iteration,
		MaxIterations: dp.MaxIterations,
		Guidance:      dp.Guidance,
	}

	// Parse options
	if dp.Options != "" {
		_ = json.Unmarshal([]byte(dp.Options), &payload.Options)
	}

	// Serialize to JSON
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Create command: hook_script <decision_id> <event_type>
	// #nosec G204 -- hookPath is from controlled .beads/hooks directory
	cmd := exec.CommandContext(ctx, hookPath, dp.IssueID, event)
	cmd.Stdin = bytes.NewReader(payloadJSON)

	// Capture output for debugging (but don't block on it)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	return cmd.Run()
}
