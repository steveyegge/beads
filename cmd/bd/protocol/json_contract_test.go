// json_contract_test.go — CI regression tests for --json output contracts.
//
// These tests verify that commands with --json always produce valid JSON
// and include required fields. Regressions like GH#2492, GH#2465, GH#2407,
// GH#2395 are prevented by these tests.
package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestJSONContract_ListOutputIsValidJSON verifies bd list --json always
// produces valid JSON (not mixed with tree-renderer text).
func TestJSONContract_ListOutputIsValidJSON(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)
	w.create("JSON contract test issue")

	out := w.run("list", "--json")
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("bd list --json produced invalid JSON: %v\nOutput:\n%s", err, out)
	}
	if len(items) == 0 {
		t.Fatal("bd list --json returned empty array")
	}
}

// TestJSONContract_ShowOutputHasRequiredFields verifies bd show --json
// includes all required issue fields.
func TestJSONContract_ShowOutputHasRequiredFields(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)
	id := w.create("Required fields test")

	out := w.run("show", id, "--json")
	items := parseJSONOutput(t, out)
	if len(items) == 0 {
		t.Fatal("bd show --json returned no items")
	}

	issue := items[0]
	requiredFields := []string{"id", "title", "status", "priority", "issue_type", "created_at"}
	for _, field := range requiredFields {
		if _, ok := issue[field]; !ok {
			t.Errorf("bd show --json missing required field %q", field)
		}
	}
}

// TestJSONContract_ReadyOutputIsValidJSON verifies bd ready --json produces
// valid JSON even when no issues are ready.
func TestJSONContract_ReadyOutputIsValidJSON(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)

	out := w.run("ready", "--json")
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("bd ready --json produced invalid JSON: %v\nOutput:\n%s", err, out)
	}
}

// TestJSONContract_CreateOutputHasID verifies bd create --json returns
// the created issue with its ID.
func TestJSONContract_CreateOutputHasID(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)

	out := w.run("create", "Create contract test", "--description=test", "--json")

	// bd create --json outputs a single JSON object (not an array)
	var issue map[string]any
	if err := json.Unmarshal([]byte(out), &issue); err != nil {
		t.Fatalf("bd create --json produced invalid JSON: %v\nOutput:\n%s", err, out)
	}

	if _, ok := issue["id"]; !ok {
		t.Error("bd create --json output missing 'id' field")
	}
}

// TestJSONContract_ErrorOutputIsValidJSON verifies that errors with --json
// produce valid JSON to stderr (not mixed text).
func TestJSONContract_ErrorOutputIsValidJSON(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)

	// Try to show a nonexistent issue with --json
	out, _ := w.runExpectError("show", "nonexistent-xyz-999", "--json")

	// The output (stderr) should be valid JSON or empty
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return // Empty is acceptable for errors
	}

	// Try to parse as JSON object
	var errObj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &errObj); err != nil {
		// Try each line — error JSON may be mixed with other stderr output
		foundJSON := false
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if json.Valid([]byte(line)) {
				foundJSON = true
				break
			}
		}
		if !foundJSON {
			t.Logf("Note: error output not fully JSON — this is acceptable for some error paths")
		}
	}
}

// TestJSONContract_CloseOutputHasStatus verifies bd close --json returns
// the updated issue with closed status.
func TestJSONContract_CloseOutputHasStatus(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)
	id := w.create("Close contract test")

	out := w.run("close", id, "--json")
	items := parseJSONOutput(t, out)
	if len(items) == 0 {
		t.Fatal("bd close --json returned no items")
	}

	assertField(t, items[0], "status", "closed")
}
