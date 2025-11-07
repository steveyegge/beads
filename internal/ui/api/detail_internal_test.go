package api

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

type detailResponder struct {
	resp *rpc.Response
	err  error
}

func (d *detailResponder) Show(*rpc.ShowArgs) (*rpc.Response, error) {
	return d.resp, d.err
}

func TestFetchIssueDetailHandlesDaemonError(t *testing.T) {
	client := &detailResponder{
		resp: &rpc.Response{
			Success:    false,
			Error:      "daemon unavailable",
			StatusCode: 503,
		},
		err: errors.New("connection refused"),
	}

	payload, status, err := fetchIssueDetail(client, "ui-1")
	if payload != nil {
		t.Fatalf("expected nil payload on error")
	}
	if status != 503 {
		t.Fatalf("expected status 503, got %d", status)
	}
	if err == nil || err.Error() != "daemon unavailable" {
		t.Fatalf("expected propagated error message, got %v", err)
	}
}

func TestFetchIssueDetailHandlesMissingIssue(t *testing.T) {
	client := &detailResponder{
		resp: &rpc.Response{
			Success: false,
			Error:   "issue not found",
		},
	}

	payload, status, err := fetchIssueDetail(client, "ui-2")
	if payload != nil {
		t.Fatalf("expected nil payload for missing issue")
	}
	if status != 404 {
		t.Fatalf("expected 404, got %d", status)
	}
	if err == nil || err.Error() == "" {
		t.Fatalf("expected error message for missing issue")
	}
}

func TestFetchIssueDetailDecodeFailures(t *testing.T) {
	client := &detailResponder{
		resp: &rpc.Response{
			Success: true,
			Data:    []byte(`{"unexpected":true}`),
		},
	}

	payload, status, err := fetchIssueDetail(client, "ui-3")
	if payload != nil {
		t.Fatalf("expected nil payload when decode fails")
	}
	if status != 502 {
		t.Fatalf("expected 502, got %d", status)
	}
	if err == nil || err.Error() == "" {
		t.Fatalf("expected decode error")
	}
}

func TestFetchIssueDetailWrapsIssuePayload(t *testing.T) {
	raw, _ := json.Marshal(struct {
		*types.Issue
	}{
		Issue: &types.Issue{
			ID:     "ui-10",
			Title:  "Wrapped",
			Status: types.StatusOpen,
		},
	})

	client := &detailResponder{
		resp: &rpc.Response{
			Success: true,
			Data:    raw,
		},
	}

	payload, status, err := fetchIssueDetail(client, "ui-10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != 200 {
		t.Fatalf("expected 200, got %d", status)
	}
	if payload == nil || payload.Issue == nil || payload.Issue.Title != "Wrapped" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestFetchIssueDetailHandlesNilResponse(t *testing.T) {
	client := &detailResponder{}
	payload, status, err := fetchIssueDetail(client, "ui-11")
	if payload != nil || status != http.StatusNotFound || err == nil {
		t.Fatalf("expected not found when response nil, got payload=%+v status=%d err=%v", payload, status, err)
	}
}

func TestDecodeShowPayloadValidation(t *testing.T) {
	payload, err := decodeShowPayload(nil)
	if err != nil || payload != nil {
		t.Fatalf("expected nil payload for empty data")
	}

	raw, _ := json.Marshal(map[string]any{"labels": []string{"ui"}})
	if _, err := decodeShowPayload(raw); err == nil {
		t.Fatalf("expected error when issue missing")
	}
}

type renderResult struct {
	html template.HTML
	err  error
}

type sequenceRenderer struct {
	results []renderResult
	calls   []string
	index   int
}

func (r *sequenceRenderer) Render(markdown string) (template.HTML, error) {
	r.calls = append(r.calls, markdown)
	if r.index >= len(r.results) {
		r.index++
		return "", nil
	}
	res := r.results[r.index]
	r.index++
	return res.html, res.err
}

func detailPayload() *showPayload {
	now := time.Date(2025, 10, 31, 0, 0, 0, 0, time.UTC)
	return &showPayload{
		Issue: &types.Issue{
			ID:                 "ui-200",
			Title:              "Full detail",
			Description:        "## Description",
			Design:             "design spec",
			Notes:              "notes go here",
			AcceptanceCriteria: "acceptance text",
			Status:             types.StatusOpen,
			IssueType:          types.TypeFeature,
			Priority:           1,
			Assignee:           "casey",
			UpdatedAt:          now,
		},
		Labels: []string{"ui", "detail"},
		Dependencies: []*types.Issue{
			{ID: "dep-1", Title: "Blocking", Status: types.StatusBlocked, IssueType: types.TypeBug, Priority: 0},
			nil,
			{ID: "dep-2", Title: "Discovery", Status: types.StatusOpen, IssueType: types.TypeTask, Priority: 2},
		},
		DependencyRecords: []*types.Dependency{
			{IssueID: "ui-200", DependsOnID: "dep-1", Type: types.DepBlocks},
			nil,
			{IssueID: "ui-200", DependsOnID: "dep-2", Type: types.DepDiscoveredFrom},
			{IssueID: "ui-200", DependsOnID: "unknown", Type: types.DepBlocks},
		},
	}
}

func TestBuildIssueDetailMarkdownFailures(t *testing.T) {
	t.Parallel()

	fieldOrder := []string{"description", "design", "notes", "acceptance"}

	for idx, field := range fieldOrder {
		idx := idx
		field := field
		t.Run(field+" error", func(t *testing.T) {
			payload := detailPayload()
			renderer := &sequenceRenderer{}

			for i := range fieldOrder {
				if i == idx {
					renderer.results = append(renderer.results, renderResult{
						err: errors.New("render-" + field),
					})
					continue
				}
				renderer.results = append(renderer.results, renderResult{
					html: template.HTML("<p>" + fieldOrder[i] + "</p>"),
				})
			}

			_, err := buildIssueDetail(payload, renderer)
			if err == nil {
				t.Fatalf("expected error when %s renderer fails", field)
			}
			if !strings.Contains(err.Error(), "render-"+field) {
				t.Fatalf("expected error mentioning %s renderer, got %v", field, err)
			}
			if len(renderer.calls) != idx+1 {
				t.Fatalf("expected %d render calls, got %d", idx+1, len(renderer.calls))
			}
		})
	}
}

func TestBuildIssueDetailSuccess(t *testing.T) {
	payload := detailPayload()
	renderer := &sequenceRenderer{
		results: []renderResult{
			{html: template.HTML("<p>description</p>")},
			{html: template.HTML("<p>design</p>")},
			{html: template.HTML("<p>notes</p>")},
			{html: template.HTML("<p>acceptance</p>")},
		},
	}

	detail, err := buildIssueDetail(payload, renderer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if detail.ID != "ui-200" || detail.Title != "Full detail" {
		t.Fatalf("unexpected issue metadata: %+v", detail)
	}
	if detail.DescriptionHTML != "<p>description</p>" {
		t.Fatalf("unexpected description html: %q", detail.DescriptionHTML)
	}
	if detail.DesignHTML != "<p>design</p>" || detail.NotesHTML != "<p>notes</p>" || detail.AcceptanceHTML != "<p>acceptance</p>" {
		t.Fatalf("unexpected rendered html fields: %+v", detail)
	}
	if detail.StatusLabel != "Ready" {
		t.Fatalf("expected status label Ready, got %q", detail.StatusLabel)
	}
	if detail.UpdatedAt != payload.Issue.UpdatedAt.UTC().Format(time.RFC3339) {
		t.Fatalf("unexpected updated timestamp: %s", detail.UpdatedAt)
	}
	if len(detail.DependenciesSummary["blocks"]) != 1 || detail.DependenciesSummary["blocks"][0].ID != "dep-1" {
		t.Fatalf("unexpected blocks summary: %+v", detail.DependenciesSummary["blocks"])
	}
	if len(detail.DependenciesSummary["discovered_from"]) != 1 || detail.DependenciesSummary["discovered_from"][0].ID != "dep-2" {
		t.Fatalf("unexpected discovered_from summary: %+v", detail.DependenciesSummary["discovered_from"])
	}
	if _, exists := detail.DependenciesSummary["unknown"]; exists {
		t.Fatalf("unexpected dependency summary entries: %+v", detail.DependenciesSummary)
	}
	if len(renderer.calls) != 4 {
		t.Fatalf("expected 4 render calls, got %d", len(renderer.calls))
	}
}

func TestSummarizeDependenciesEmpty(t *testing.T) {
	payload := &showPayload{}
	summary := summarizeDependencies(payload)
	if summary == nil {
		t.Fatalf("expected empty map, got nil")
	}
	if len(summary) != 0 {
		t.Fatalf("expected empty summary for nil dependency records, got %+v", summary)
	}

	payload.DependencyRecords = []*types.Dependency{}
	summary = summarizeDependencies(payload)
	if len(summary) != 0 {
		t.Fatalf("expected empty summary for empty dependency records, got %+v", summary)
	}
}
