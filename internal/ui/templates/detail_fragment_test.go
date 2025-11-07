package templates_test

import (
	"bytes"
	"strings"
	"testing"

	uiapi "github.com/steveyegge/beads/internal/ui/api"
	"github.com/steveyegge/beads/internal/ui/templates"
)

func TestIssueDetailTemplateRendersMetadata(t *testing.T) {
	tmpl, err := templates.Parse("issue_detail.tmpl")
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	issue := uiapi.IssueDetail{
		ID:              "ui-101",
		Title:           "Detail Title",
		Status:          "open",
		StatusLabel:     "Ready",
		IssueType:       "feature",
		Priority:        2,
		Assignee:        "pat",
		Labels:          []string{"ui", "test"},
		DescriptionHTML: "<p>Body</p>",
		NotesHTML:       "<p>Notes</p>",
	}

	deps := map[string][]uiapi.DependencySummary{
		"blocks": {
			{ID: "bd-1", Title: "Upstream", Priority: 1, Status: "in_progress"},
		},
		"discovered_from": {
			{ID: "bd-2", Title: "Audit", Priority: 2, Status: "open"},
		},
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		Issue        uiapi.IssueDetail
		Dependencies map[string][]uiapi.DependencySummary
	}{Issue: issue, Dependencies: deps}); err != nil {
		t.Fatalf("execute template: %v", err)
	}

	html := buf.String()
	checks := []string{
		`data-testid="issue-detail-id" aria-hidden="true">ui-101`,
		`class="issue-detail__title" aria-label="Issue title" data-linebreak="slash-n">Detail Title`,
		`data-field="status"`,
		`data-status="open"`,
		`>Ready</span>`,
		`data-field="priority">P2`,
		`data-field="assignee">Assigned to pat`,
		`data-testid="label-editor"`,
		`data-role="label-chip"`,
		`data-role="remove-label"`,
		`data-testid="label-input"`,
		`data-testid="label-submit"`,
		`class="issue-detail__content" data-linebreak="slash-n"><p>Body</p>`,
		`data-section="notes"`,
		`data-dependency="blocks"`,
		`data-dependency="discovered-from"`,
		`data-testid="status-actions"`,
		`data-status-target="open"`,
		`data-status-target="in_progress"`,
		`data-status-target="closed"`,
	}
	for _, snippet := range checks {
		if !strings.Contains(html, snippet) {
			t.Fatalf("expected output to contain %q\nhtml=%s", snippet, html)
		}
	}
}

func TestIssueDetailTemplateShowsDescriptionPlaceholder(t *testing.T) {
	tmpl, err := templates.Parse("issue_detail.tmpl")
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	issue := uiapi.IssueDetail{
		ID:        "ui-202",
		Title:     "Placeholder Check",
		Status:    "open",
		IssueType: "task",
		Priority:  1,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		Issue        uiapi.IssueDetail
		Dependencies map[string][]uiapi.DependencySummary
	}{Issue: issue, Dependencies: map[string][]uiapi.DependencySummary{}}); err != nil {
		t.Fatalf("execute template: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, "No description provided yet.") {
		t.Fatalf("expected description placeholder, got %s", html)
	}
}
