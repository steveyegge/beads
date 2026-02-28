package protocol

import "testing"

// TestProtocol_CommentRejectsEmptyText asserts that bd comments add rejects
// empty and whitespace-only comment text with a non-zero exit code.
func TestProtocol_CommentRejectsEmptyText(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t)
	id := w.create("--title", "Comment target", "--type", "task")

	t.Run("empty", func(t *testing.T) {
		_, err := w.tryRun("comments", "add", id, "")
		if err == nil {
			t.Error("bd comments add with empty text should fail but exited 0")
		}
	})

	t.Run("whitespace", func(t *testing.T) {
		_, err := w.tryRun("comments", "add", id, "   ")
		if err == nil {
			t.Error("bd comments add with whitespace-only text should fail but exited 0")
		}
	})
}
