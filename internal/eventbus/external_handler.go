package eventbus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ExternalHandlerConfig is the serializable configuration for an external handler.
// Stored in the config table as JSON under keys like "bus.handler.<id>".
type ExternalHandlerConfig struct {
	ID       string   `json:"id"`
	Command  string   `json:"command"`            // Shell command to run
	Events   []string `json:"events"`             // Event types to handle
	Priority int      `json:"priority,omitempty"`  // Default 50
	Shell    string   `json:"shell,omitempty"`     // Default "sh"
}

// ExternalHandler runs a shell command for each matching event.
//
// Protocol:
//   - Event JSON is passed on stdin
//   - Handler writes result JSON to stdout
//   - Exit 0 = success (stdout parsed as Result)
//   - Exit 1 = error (logged, chain continues)
//   - Exit 2 = fatal (logged, chain continues — reserved for future use)
//
// If the command produces no stdout, an empty Result is used.
type ExternalHandler struct {
	config ExternalHandlerConfig
	events []EventType
}

// NewExternalHandler creates a handler from a persisted config.
func NewExternalHandler(cfg ExternalHandlerConfig) *ExternalHandler {
	if cfg.Priority == 0 {
		cfg.Priority = 50
	}
	if cfg.Shell == "" {
		cfg.Shell = "sh"
	}
	events := make([]EventType, len(cfg.Events))
	for i, e := range cfg.Events {
		events[i] = EventType(e)
	}
	return &ExternalHandler{
		config: cfg,
		events: events,
	}
}

func (h *ExternalHandler) ID() string          { return h.config.ID }
func (h *ExternalHandler) Handles() []EventType { return h.events }
func (h *ExternalHandler) Priority() int        { return h.config.Priority }

// Config returns the serializable configuration for persistence.
func (h *ExternalHandler) Config() ExternalHandlerConfig { return h.config }

func (h *ExternalHandler) Handle(ctx context.Context, event *Event, result *Result) error {
	// Marshal the event to pass on stdin.
	var input []byte
	if len(event.Raw) > 0 {
		input = event.Raw
	} else {
		var err error
		input, err = json.Marshal(event)
		if err != nil {
			return fmt.Errorf("external handler %s: marshal event: %w", h.config.ID, err)
		}
	}

	cmd := exec.CommandContext(ctx, h.config.Shell, "-c", h.config.Command)
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			stderrStr := strings.TrimSpace(stderr.String())
			if stderrStr == "" {
				stderrStr = strings.TrimSpace(stdout.String())
			}
			return fmt.Errorf("external handler %s: exit %d: %s", h.config.ID, code, stderrStr)
		}
		return fmt.Errorf("external handler %s: exec: %w", h.config.ID, err)
	}

	// Parse stdout as result JSON if non-empty.
	out := strings.TrimSpace(stdout.String())
	if out != "" {
		var handlerResult Result
		if jsonErr := json.Unmarshal([]byte(out), &handlerResult); jsonErr == nil {
			if handlerResult.Block {
				result.Block = true
				result.Reason = handlerResult.Reason
			}
			result.Inject = append(result.Inject, handlerResult.Inject...)
			result.Warnings = append(result.Warnings, handlerResult.Warnings...)
		}
		// If stdout isn't valid JSON, ignore it (handler may just print logs).
	}

	return nil
}

// HandlerConfigPrefix is the config key prefix for persisted external handlers.
const HandlerConfigPrefix = "bus.handler."
