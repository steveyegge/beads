package templates_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/ui/templates"
)

func TestBaseTemplateRendersRegions(t *testing.T) {
	tmpl, err := templates.Parse("index.html.tmpl")
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	data := templates.BasePageData{
		AppTitle:             "Beads UI",
		InitialFiltersJSON:   mustDefaultFiltersJSON(t),
		EventStreamURL:       "/events",
		StaticPrefix:         "/.assets",
		LiveUpdatesAvailable: true,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("execute template: %v", err)
	}

	html := buf.String()

	for _, region := range []string{
		`data-role="search-panel"`,
		`data-role="issue-list"`,
		`data-role="issue-detail"`,
	} {
		if !strings.Contains(html, region) {
			t.Fatalf("expected template output to contain %s", region)
		}
	}

	expectFragments := []string{
		`hx-get="/fragments/issues?status=open"`,
		`/.assets/vendor/htmx.min.js`,
		`/.assets/vendor/alpine.min.js`,
		`data-event-stream="/events"`,
	}

	for _, fragment := range expectFragments {
		if !strings.Contains(html, fragment) {
			t.Fatalf("expected template output to contain %s", fragment)
		}
	}

	if !strings.Contains(html, `data-role="live-update-warning"`) {
		t.Fatalf("expected live update banner element in output")
	}
	if !strings.Contains(html, `data-live-updates="on"`) {
		t.Fatalf("expected live updates data attribute to be set to on")
	}
	if strings.Contains(html, "ui-body--degraded") {
		t.Fatalf("did not expect degraded class when live updates are available")
	}
}
