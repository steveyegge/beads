package eventbus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// PrimeHandler injects bd prime workflow context on SessionStart and PreCompact.
// Priority 10 (runs first — context injection should happen before gates).
type PrimeHandler struct{}

func (h *PrimeHandler) ID() string              { return "prime" }
func (h *PrimeHandler) Handles() []EventType     { return []EventType{EventSessionStart, EventPreCompact} }
func (h *PrimeHandler) Priority() int            { return 10 }

func (h *PrimeHandler) Handle(ctx context.Context, event *Event, result *Result) error {
	stdout, _, err := runBDCommand(ctx, event.CWD, "prime")
	if err != nil {
		// bd prime exits 0 even on error (silent fail-safe).
		// Only log if the process didn't start.
		return fmt.Errorf("prime: %w", err)
	}
	if stdout != "" {
		result.Inject = append(result.Inject, stdout)
	}
	return nil
}

// StopDecisionHandler creates a decision point when Claude tries to stop,
// blocking until the human responds. Priority 15 (after prime, before gate).
type StopDecisionHandler struct{}

func (h *StopDecisionHandler) ID() string          { return "stop-decision" }
func (h *StopDecisionHandler) Handles() []EventType { return []EventType{EventStop} }
func (h *StopDecisionHandler) Priority() int        { return 15 }

func (h *StopDecisionHandler) Handle(ctx context.Context, event *Event, result *Result) error {
	// Check stop_hook_active from event JSON to prevent infinite loop.
	// When this handler blocks the stop, Claude resumes and may stop again;
	// the bus emit sets stop_hook_active=true to avoid re-entering.
	if len(event.Raw) > 0 {
		var raw map[string]interface{}
		if err := json.Unmarshal(event.Raw, &raw); err == nil {
			if active, ok := raw["stop_hook_active"]; ok {
				if boolVal, isBool := active.(bool); isBool && boolVal {
					return nil // already inside a stop hook — skip
				}
			}
		}
	}

	stdout, _, err := runBDCommand(ctx, event.CWD, "decision", "stop-check", "--json")
	if err != nil {
		// Exit code 1 means block (human said "continue").
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			var resp stopCheckResponse
			if jsonErr := json.Unmarshal([]byte(stdout), &resp); jsonErr == nil {
				if resp.Decision == "block" {
					result.Block = true
					result.Reason = resp.Reason
					return nil
				}
			}
			// Couldn't parse — treat as block with raw reason.
			result.Block = true
			result.Reason = strings.TrimSpace(stdout)
			return nil
		}
		// Other errors — log and allow stop (fail-open).
		return fmt.Errorf("stop-decision: %w", err)
	}

	// Exit 0 = allow stop.
	return nil
}

// stopCheckResponse mirrors the JSON output from `bd decision stop-check`.
type stopCheckResponse struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

// GateHandler evaluates session gates on Stop and PreToolUse hooks.
// Priority 20 (runs after context injection).
type GateHandler struct{}

func (h *GateHandler) ID() string              { return "gate" }
func (h *GateHandler) Handles() []EventType     { return []EventType{EventStop, EventPreToolUse} }
func (h *GateHandler) Priority() int            { return 20 }

func (h *GateHandler) Handle(ctx context.Context, event *Event, result *Result) error {
	hookName := string(event.Type)
	stdout, _, err := runBDCommand(ctx, event.CWD, "gate", "session-check", "--hook", hookName, "--json")
	if err != nil {
		// Exit code 1 means blocked. Parse the JSON to get the reason.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// Parse JSON from stdout for block details.
			var resp gateCheckResponse
			if jsonErr := json.Unmarshal([]byte(stdout), &resp); jsonErr == nil {
				if resp.Decision == "block" {
					result.Block = true
					result.Reason = resp.Reason
				}
				for _, w := range resp.Warnings {
					result.Warnings = append(result.Warnings, w)
				}
				return nil
			}
			// Couldn't parse — treat as block with raw reason.
			result.Block = true
			result.Reason = strings.TrimSpace(stdout)
			return nil
		}
		return fmt.Errorf("gate: %w", err)
	}

	// Exit 0 = all gates satisfied. Check for warnings.
	var resp gateCheckResponse
	if jsonErr := json.Unmarshal([]byte(stdout), &resp); jsonErr == nil {
		for _, w := range resp.Warnings {
			result.Warnings = append(result.Warnings, w)
		}
	}
	return nil
}

// gateCheckResponse mirrors gate.CheckResponse for JSON parsing.
type gateCheckResponse struct {
	Decision string   `json:"decision"`
	Reason   string   `json:"reason,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// DecisionHandler injects decision responses on SessionStart and PreCompact.
// Priority 30 (runs after prime, before gate — decisions are informational).
type DecisionHandler struct{}

func (h *DecisionHandler) ID() string              { return "decision" }
func (h *DecisionHandler) Handles() []EventType     { return []EventType{EventSessionStart, EventPreCompact} }
func (h *DecisionHandler) Priority() int            { return 30 }

func (h *DecisionHandler) Handle(ctx context.Context, event *Event, result *Result) error {
	stdout, _, err := runBDCommand(ctx, event.CWD, "decision", "check", "--inject")
	if err != nil {
		// --inject mode always exits 0. An error here means process failure.
		return fmt.Errorf("decision: %w", err)
	}
	if stdout != "" {
		result.Inject = append(result.Inject, stdout)
	}
	return nil
}

// runBDCommand executes a bd subcommand and captures stdout/stderr.
// The CWD parameter sets the working directory for the subprocess.
func runBDCommand(ctx context.Context, cwd string, args ...string) (string, string, error) {
	bdPath, err := findBDBinary()
	if err != nil {
		return "", "", err
	}

	cmd := exec.CommandContext(ctx, bdPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if cwd != "" {
		cmd.Dir = cwd
	}

	// Pass through environment but ensure no daemon socket override
	// (subprocess should discover daemon via normal socket discovery).
	cmd.Env = os.Environ()

	err = cmd.Run()
	return strings.TrimRight(stdout.String(), "\n"), strings.TrimRight(stderr.String(), "\n"), err
}

// findBDBinary locates the bd binary.
func findBDBinary() (string, error) {
	path, err := exec.LookPath("bd")
	if err != nil {
		return "", fmt.Errorf("bd binary not found in PATH: %w", err)
	}
	return path, nil
}

// DefaultHandlers returns the standard set of event bus handlers for daemon registration.
func DefaultHandlers() []Handler {
	return []Handler{
		&PrimeHandler{},          // 10
		&StopDecisionHandler{},   // 15
		&GateHandler{},           // 20
		&DecisionHandler{},       // 30
	}
}
