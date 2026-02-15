package hooks

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// EventHook defines a config-driven hook that fires on bead events.
// Configured in .beads/config.yaml under the "event-hooks" key.
type EventHook struct {
	Event   string `mapstructure:"event" json:"event"`     // post-create, post-update, post-close, post-comment, post-write
	Command string `mapstructure:"command" json:"command"`  // Command with ${BEAD_*} variables
	Async   bool   `mapstructure:"async" json:"async"`      // Run without blocking bd command
	Filter  string `mapstructure:"filter" json:"filter"`    // Optional: "priority:P0,P1", "type:bug", etc.
}

// ValidEvents lists all supported event types.
var ValidEvents = []string{
	"post-create",
	"post-update",
	"post-close",
	"post-comment",
	"post-write",
}

// IsValidEventName checks if an event name is recognized (exported for CLI).
func IsValidEventName(event string) bool {
	return isValidEvent(event)
}

// isValidEvent checks if an event name is recognized.
func isValidEvent(event string) bool {
	for _, e := range ValidEvents {
		if e == event {
			return true
		}
	}
	return false
}

// LoadEventHooks reads [[event-hooks]] from the viper config.
// Returns nil (not an error) if no hooks are configured.
func LoadEventHooks(v *viper.Viper) ([]EventHook, error) {
	if v == nil {
		return nil, nil
	}

	raw := v.Get("event-hooks")
	if raw == nil {
		return nil, nil
	}

	// Viper returns []interface{} for YAML arrays
	rawSlice, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("event-hooks: expected array, got %T", raw)
	}

	var hooks []EventHook
	for i, item := range rawSlice {
		m, ok := item.(map[interface{}]interface{})
		if !ok {
			// Try map[string]interface{} (some viper versions)
			if ms, ok2 := item.(map[string]interface{}); ok2 {
				h, err := parseHookFromStringMap(ms, i)
				if err != nil {
					return nil, err
				}
				hooks = append(hooks, h)
				continue
			}
			return nil, fmt.Errorf("event-hooks[%d]: expected map, got %T", i, item)
		}
		h, err := parseHookFromMap(m, i)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, h)
	}

	return hooks, nil
}

func parseHookFromMap(m map[interface{}]interface{}, idx int) (EventHook, error) {
	sm := make(map[string]interface{}, len(m))
	for k, v := range m {
		sm[fmt.Sprintf("%v", k)] = v
	}
	return parseHookFromStringMap(sm, idx)
}

func parseHookFromStringMap(m map[string]interface{}, idx int) (EventHook, error) {
	h := EventHook{}

	event, _ := m["event"].(string)
	if event == "" {
		return h, fmt.Errorf("event-hooks[%d]: missing required 'event' field", idx)
	}
	if !isValidEvent(event) {
		return h, fmt.Errorf("event-hooks[%d]: unknown event %q (valid: %s)",
			idx, event, strings.Join(ValidEvents, ", "))
	}
	h.Event = event

	command, _ := m["command"].(string)
	if command == "" {
		return h, fmt.Errorf("event-hooks[%d]: missing required 'command' field", idx)
	}
	h.Command = command

	if async, ok := m["async"].(bool); ok {
		h.Async = async
	}

	if filter, ok := m["filter"].(string); ok {
		h.Filter = filter
	}

	return h, nil
}
