package protocol

import (
	"strings"
	"testing"
)

// TestProtocol_UpdateRejectsEmptyTitle verifies that bd update --title ""
// exits non-zero rather than silently blanking the title.
func TestProtocol_UpdateRejectsEmptyTitle(t *testing.T) {
	w := newWorkspace(t)
	id := w.create("Test issue")

	out, code := w.runExpectError("update", id, "--title", "")
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(out, "title cannot be empty") {
		t.Errorf("expected 'title cannot be empty' in output, got: %s", out)
	}
}

// TestProtocol_UpdateRejectsWhitespaceTitle verifies that whitespace-only
// titles are rejected the same as empty titles.
func TestProtocol_UpdateRejectsWhitespaceTitle(t *testing.T) {
	w := newWorkspace(t)
	id := w.create("Test issue")

	out, code := w.runExpectError("update", id, "--title", "   ")
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(out, "title cannot be empty") {
		t.Errorf("expected 'title cannot be empty' in output, got: %s", out)
	}
}

// TestProtocol_LabelAddRejectsEmptyLabel verifies that bd label add with
// an empty label exits non-zero.
func TestProtocol_LabelAddRejectsEmptyLabel(t *testing.T) {
	w := newWorkspace(t)
	id := w.create("Test issue")

	out, code := w.runExpectError("label", "add", id, "")
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(out, "label cannot be empty") {
		t.Errorf("expected 'label cannot be empty' in output, got: %s", out)
	}
}

// TestProtocol_LabelAddAcceptsValidLabel verifies no regression: adding
// a normal label still works.
func TestProtocol_LabelAddAcceptsValidLabel(t *testing.T) {
	w := newWorkspace(t)
	id := w.create("Test issue")
	w.run("label", "add", id, "urgent")
}
