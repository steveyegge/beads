package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func createTestDecisionHook(t *testing.T, dir, hookName, content string) {
	t.Helper()
	hookPath := filepath.Join(dir, hookName)
	err := os.WriteFile(hookPath, []byte(content), 0755)
	if err != nil {
		t.Fatalf("failed to create hook: %v", err)
	}
}

func TestRunDecisionSync_Create(t *testing.T) {
	// Create temp hooks directory
	tmpDir := t.TempDir()

	// Create a hook that writes the payload to a file
	outputFile := filepath.Join(tmpDir, "output.json")
	hookScript := `#!/bin/sh
cat > "` + outputFile + `"
`
	createTestDecisionHook(t, tmpDir, HookOnDecisionCreate, hookScript)

	runner := NewRunner(tmpDir)
	dp := &types.DecisionPoint{
		IssueID:       "test.decision-1",
		Prompt:        "Which option?",
		Options:       `[{"id":"a","short":"A","label":"Option A"}]`,
		DefaultOption: "a",
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     time.Now(),
	}

	err := runner.RunDecisionSync(EventDecisionCreate, dp, nil)
	if err != nil {
		t.Fatalf("RunDecisionSync failed: %v", err)
	}

	// Read and verify output
	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	var payload DecisionHookPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("failed to parse payload: %v", err)
	}

	if payload.ID != "test.decision-1" {
		t.Errorf("ID = %q, want %q", payload.ID, "test.decision-1")
	}
	if payload.Event != EventDecisionCreate {
		t.Errorf("Event = %q, want %q", payload.Event, EventDecisionCreate)
	}
	if payload.Prompt != "Which option?" {
		t.Errorf("Prompt = %q, want %q", payload.Prompt, "Which option?")
	}
	if len(payload.Options) != 1 {
		t.Errorf("Options count = %d, want 1", len(payload.Options))
	}
	if payload.Response != nil {
		t.Error("Response should be nil for create event")
	}
}

func TestRunDecisionSync_Respond(t *testing.T) {
	tmpDir := t.TempDir()

	outputFile := filepath.Join(tmpDir, "output.json")
	hookScript := `#!/bin/sh
cat > "` + outputFile + `"
`
	createTestDecisionHook(t, tmpDir, HookOnDecisionRespond, hookScript)

	runner := NewRunner(tmpDir)
	dp := &types.DecisionPoint{
		IssueID:       "test.decision-1",
		Prompt:        "Which option?",
		Options:       `[{"id":"a","short":"A","label":"Option A"}]`,
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     time.Now(),
	}
	response := &DecisionResponsePayload{
		Selected:    "a",
		Text:        "Additional notes",
		RespondedBy: "user@example.com",
	}

	err := runner.RunDecisionSync(EventDecisionRespond, dp, response)
	if err != nil {
		t.Fatalf("RunDecisionSync failed: %v", err)
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	var payload DecisionHookPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("failed to parse payload: %v", err)
	}

	if payload.Event != EventDecisionRespond {
		t.Errorf("Event = %q, want %q", payload.Event, EventDecisionRespond)
	}
	if payload.Response == nil {
		t.Fatal("Response should not be nil")
	}
	if payload.Response.Selected != "a" {
		t.Errorf("Selected = %q, want %q", payload.Response.Selected, "a")
	}
	if payload.Response.Text != "Additional notes" {
		t.Errorf("Text = %q, want %q", payload.Response.Text, "Additional notes")
	}
	if payload.Response.RespondedBy != "user@example.com" {
		t.Errorf("RespondedBy = %q, want %q", payload.Response.RespondedBy, "user@example.com")
	}
}

func TestRunDecisionSync_Timeout(t *testing.T) {
	tmpDir := t.TempDir()

	outputFile := filepath.Join(tmpDir, "output.json")
	hookScript := `#!/bin/sh
cat > "` + outputFile + `"
`
	createTestDecisionHook(t, tmpDir, HookOnDecisionTimeout, hookScript)

	runner := NewRunner(tmpDir)
	dp := &types.DecisionPoint{
		IssueID:       "test.decision-1",
		Prompt:        "Which option?",
		Options:       `[{"id":"a","short":"A","label":"Option A"},{"id":"b","short":"B","label":"Option B"}]`,
		DefaultOption: "a",
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     time.Now(),
	}
	response := &DecisionResponsePayload{
		Selected:  "a", // Default option applied
		IsTimeout: true,
	}

	err := runner.RunDecisionSync(EventDecisionTimeout, dp, response)
	if err != nil {
		t.Fatalf("RunDecisionSync failed: %v", err)
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	var payload DecisionHookPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("failed to parse payload: %v", err)
	}

	if payload.Event != EventDecisionTimeout {
		t.Errorf("Event = %q, want %q", payload.Event, EventDecisionTimeout)
	}
	if payload.Response == nil {
		t.Fatal("Response should not be nil")
	}
	if !payload.Response.IsTimeout {
		t.Error("IsTimeout should be true")
	}
	if payload.Response.Selected != "a" {
		t.Errorf("Selected = %q, want %q (default)", payload.Response.Selected, "a")
	}
}

func TestRunDecision_NoHook(t *testing.T) {
	tmpDir := t.TempDir()
	runner := NewRunner(tmpDir)

	dp := &types.DecisionPoint{
		IssueID: "test.decision-1",
		Prompt:  "Which option?",
	}

	// Should not error when hook doesn't exist
	err := runner.RunDecisionSync(EventDecisionCreate, dp, nil)
	if err != nil {
		t.Errorf("RunDecisionSync should not error when hook doesn't exist: %v", err)
	}
}

func TestRunDecision_NotExecutable(t *testing.T) {
	tmpDir := t.TempDir()

	// Create hook that's not executable
	hookPath := filepath.Join(tmpDir, HookOnDecisionCreate)
	err := os.WriteFile(hookPath, []byte("#!/bin/sh\necho test"), 0644)
	if err != nil {
		t.Fatalf("failed to create hook: %v", err)
	}

	runner := NewRunner(tmpDir)
	dp := &types.DecisionPoint{
		IssueID: "test.decision-1",
		Prompt:  "Which option?",
	}

	// Should not error when hook is not executable
	err = runner.RunDecisionSync(EventDecisionCreate, dp, nil)
	if err != nil {
		t.Errorf("RunDecisionSync should skip non-executable hook: %v", err)
	}
}

func TestRunDecision_WithGuidance(t *testing.T) {
	tmpDir := t.TempDir()

	outputFile := filepath.Join(tmpDir, "output.json")
	hookScript := `#!/bin/sh
cat > "` + outputFile + `"
`
	createTestDecisionHook(t, tmpDir, HookOnDecisionCreate, hookScript)

	runner := NewRunner(tmpDir)
	dp := &types.DecisionPoint{
		IssueID:       "test.decision-1.r2",
		Prompt:        "Which option?",
		Options:       `[{"id":"a","short":"A","label":"Refined Option A"}]`,
		Iteration:     2,
		MaxIterations: 3,
		Guidance:      "I prefer a hybrid approach",
		CreatedAt:     time.Now(),
	}

	err := runner.RunDecisionSync(EventDecisionCreate, dp, nil)
	if err != nil {
		t.Fatalf("RunDecisionSync failed: %v", err)
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	var payload DecisionHookPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("failed to parse payload: %v", err)
	}

	if payload.Iteration != 2 {
		t.Errorf("Iteration = %d, want 2", payload.Iteration)
	}
	if payload.Guidance != "I prefer a hybrid approach" {
		t.Errorf("Guidance = %q, want %q", payload.Guidance, "I prefer a hybrid approach")
	}
}
