package protocol

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Data integrity: fields set via CLI must round-trip through show --json
// ---------------------------------------------------------------------------

// TestProtocol_FieldsRoundTrip asserts that every field settable via CLI
// survives create/update → show --json. This is a data integrity invariant:
// if the CLI accepts a value, show must reflect it.
func TestProtocol_FieldsRoundTrip(t *testing.T) {
	w := newWorkspace(t)
	id := w.create("--title", "Round-trip subject",
		"--type", "feature",
		"--priority", "1",
		"--description", "Detailed description",
		"--design", "Hexagonal architecture",
		"--acceptance", "All tests pass",
		"--notes", "Initial planning notes",
		"--assignee", "alice",
		"--estimate", "180",
	)

	// Update fields that aren't available on create
	w.run("update", id, "--due", "2099-03-15")
	w.run("update", id, "--defer", "2099-01-15")

	issue := w.showJSON(id)

	// Assert each field
	assertField(t, issue, "title", "Round-trip subject")
	assertField(t, issue, "issue_type", "feature")
	assertFieldFloat(t, issue, "priority", 1)
	assertField(t, issue, "description", "Detailed description")
	assertField(t, issue, "design", "Hexagonal architecture")
	assertField(t, issue, "acceptance_criteria", "All tests pass")
	assertField(t, issue, "notes", "Initial planning notes")
	assertField(t, issue, "assignee", "alice")
	assertFieldFloat(t, issue, "estimated_minutes", 180)

	// Date fields: accept any RFC3339 that starts with the correct date
	assertFieldPrefix(t, issue, "due_at", "2099-03-15")
	assertFieldPrefix(t, issue, "defer_until", "2099-01-15")
}

// TestProtocol_MetadataRoundTrip asserts that JSON metadata set via
// bd update --metadata survives in show --json output.
func TestProtocol_MetadataRoundTrip(t *testing.T) {
	w := newWorkspace(t)
	id := w.create("--title", "Metadata carrier", "--type", "task")
	w.run("update", id, "--metadata", `{"component":"auth","risk":"high"}`)

	issue := w.showJSON(id)

	md, exists := issue["metadata"]
	if !exists {
		t.Fatal("metadata field missing from show --json")
	}

	// Metadata may be a string or a parsed object depending on JSON serialization
	switch v := md.(type) {
	case map[string]any:
		if v["component"] != "auth" || v["risk"] != "high" {
			t.Errorf("metadata content mismatch: got %v", v)
		}
	case string:
		if !strings.Contains(v, "auth") || !strings.Contains(v, "high") {
			t.Errorf("metadata content mismatch: got %q", v)
		}
	default:
		t.Errorf("unexpected metadata type %T: %v", md, md)
	}
}

// TestProtocol_SpecIDRoundTrip asserts that spec_id set via bd update --spec-id
// survives in show --json output.
func TestProtocol_SpecIDRoundTrip(t *testing.T) {
	w := newWorkspace(t)
	id := w.create("--title", "Spec carrier", "--type", "task")
	w.run("update", id, "--spec-id", "RFC-007")

	issue := w.showJSON(id)

	specID, ok := issue["spec_id"].(string)
	if !ok || specID == "" {
		t.Fatal("spec_id field missing or empty from show --json")
	}
	if specID != "RFC-007" {
		t.Errorf("spec_id = %q, want %q", specID, "RFC-007")
	}
}

// TestProtocol_CloseReasonRoundTrip asserts that close_reason survives
// close → show --json.
func TestProtocol_CloseReasonRoundTrip(t *testing.T) {
	w := newWorkspace(t)
	id := w.create("--title", "Closeable", "--type", "bug", "--priority", "2")
	w.run("close", id, "--reason", "Fixed in commit abc123")

	issue := w.showJSON(id)

	reason, ok := issue["close_reason"].(string)
	if !ok || reason == "" {
		t.Fatal("close_reason missing or empty from show --json after bd close --reason")
	}
	if reason != "Fixed in commit abc123" {
		t.Errorf("close_reason = %q, want %q", reason, "Fixed in commit abc123")
	}
}
