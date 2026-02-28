package protocol

import (
	"encoding/json"
	"testing"
)

// TestProtocol_WaitsForAppearsInBlocked asserts that an issue blocked by a
// waits-for dependency appears in bd blocked output. Previously,
// GetBlockedIssues only checked 'blocks' deps, making waits-for blocked
// issues invisible to both bd ready and bd blocked.
func TestProtocol_WaitsForAppearsInBlocked(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)

	// Create a spawner (parent) and a gate issue
	spawner := w.create("--title", "Spawner", "--type", "epic")
	gate := w.create("--title", "Gate issue", "--type", "task")

	// Create a child of the spawner so the waits-for gate is active.
	// The all-children gate blocks while any child of the spawner is active.
	_ = w.create("--title", "Child task", "--type", "task", "--parent", spawner)

	// Add waits-for dependency: gate waits-for spawner's children
	w.run("dep", "add", gate, spawner, "--type", "waits-for")

	// Verify gate does NOT appear in bd ready
	readyIDs := parseReadyIDs(t, w)
	if readyIDs[gate] {
		t.Errorf("gate issue %s should NOT be in bd ready (has waits-for dep on %s)", gate, spawner)
	}

	// Verify gate DOES appear in bd blocked
	blockedOut := w.run("blocked", "--json")
	var blocked []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(blockedOut), &blocked); err != nil {
		t.Fatalf("failed to parse bd blocked --json: %v\noutput: %s", err, blockedOut)
	}

	found := false
	for _, b := range blocked {
		if b.ID == gate {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("gate issue %s should appear in bd blocked (waits-for %s with active child)",
			gate, spawner)
	}
}
