package static

import "testing"

func TestEmbeddedAssets(t *testing.T) {
	required := []string{
		"app.js",
		"delete_issue.js",
		"detail_editor.js",
		"event_stream.js",
		"htmx_focus.js",
		"labels.js",
		"multiselect.js",
		"navigation.js",
		"palette.js",
		"queue_counts.js",
		"quick_create.js",
		"saved_views.js",
		"shell_state.js",
		"shortcut_guard.js",
		"status_actions.js",
		"styles.css",
		"images/bead-1-white.svg",
		"images/bead-2-orange.svg",
		"images/bead-3-green.svg",
		"images/bead-4-blue.svg",
		"images/bead-5-black.svg",
	}

	for _, name := range required {
		if _, err := Files.Open(name); err != nil {
			t.Fatalf("missing embedded asset %s: %v", name, err)
		}
	}
}
