package api_test

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	uiapi "github.com/steveyegge/beads/internal/ui/api"
)

type stubDetailClient struct {
	t        testing.TB
	called   int
	lastArgs *rpc.ShowArgs
	resp     *rpc.Response
	err      error
}

func (s *stubDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	s.called++
	s.lastArgs = args
	return s.resp, s.err
}

type staticMarkdownRenderer struct {
	html template.HTML
	err  error
}

func (s *staticMarkdownRenderer) Render(string) (template.HTML, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.html, nil
}

func TestDetailHandlerSuccess(t *testing.T) {
	now := time.Date(2025, 10, 22, 19, 0, 0, 0, time.UTC)
	issue := &types.Issue{
		ID:          "ui-issue-1",
		Title:       "Seed Issue",
		Status:      types.StatusOpen,
		IssueType:   types.TypeFeature,
		Priority:    2,
		Description: "First line\n\nSecond paragraph",
		Design:      "Design **bold**",
		Notes:       "_Note_",
		UpdatedAt:   now,
	}

	details := struct {
		*types.Issue
		Labels            []string            `json:"labels,omitempty"`
		Dependencies      []*types.Issue      `json:"dependencies,omitempty"`
		Dependents        []*types.Issue      `json:"dependents,omitempty"`
		DependencyRecords []*types.Dependency `json:"dependency_records,omitempty"`
	}{
		Issue: issue,
		Labels: []string{
			"critical",
		},
		Dependencies: []*types.Issue{
			{ID: "ui-issue-2", Title: "Blocked", Status: types.StatusInProgress, Priority: 1},
		},
		Dependents: []*types.Issue{
			{ID: "ui-issue-3", Title: "Dependent", Status: types.StatusOpen, Priority: 3},
		},
		DependencyRecords: []*types.Dependency{
			{IssueID: "ui-issue-1", DependsOnID: "ui-issue-2", Type: types.DepBlocks},
		},
	}

	data, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("marshal details: %v", err)
	}

	client := &stubDetailClient{
		resp: &rpc.Response{Success: true, Data: data},
		t:    t,
	}

	renderer := uiapi.NewMarkdownRenderer()
	handler := uiapi.NewDetailHandler(client, renderer)

	req := httptest.NewRequest(http.MethodGet, "/api/issues/ui-issue-1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if client.called != 1 || client.lastArgs == nil || client.lastArgs.ID != "ui-issue-1" {
		t.Fatalf("expected client to be called with ui-issue-1, got %+v", client.lastArgs)
	}

	var payload struct {
		Issue struct {
			ID              string `json:"id"`
			DescriptionHTML string `json:"description_html"`
		} `json:"issue"`
		Dependencies map[string][]uiapi.DependencySummary `json:"dependencies"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Issue.ID != "ui-issue-1" {
		t.Fatalf("unexpected issue id %q", payload.Issue.ID)
	}
	if !strings.Contains(payload.Issue.DescriptionHTML, "<p>First line</p>") {
		t.Fatalf("description html missing paragraph: %s", payload.Issue.DescriptionHTML)
	}
	if len(payload.Dependencies["blocks"]) != 1 || payload.Dependencies["blocks"][0].ID != "ui-issue-2" {
		t.Fatalf("unexpected dependency summary: %+v", payload.Dependencies)
	}
}

func TestDetailHandlerNotFound(t *testing.T) {
	client := &stubDetailClient{
		resp: &rpc.Response{
			Success: false,
			Error:   "failed to get issue: sql: no rows in result set",
		},
	}
	renderer := uiapi.NewMarkdownRenderer()

	handler := uiapi.NewDetailHandler(client, renderer)

	req := httptest.NewRequest(http.MethodGet, "/api/issues/ui-missing", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestDetailFragmentHandlerRendersTemplate(t *testing.T) {
	issue := &types.Issue{
		ID:        "ui-issue-1",
		Title:     "Seed Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		UpdatedAt: time.Now().UTC(),
	}
	details := struct {
		*types.Issue
		Labels []string `json:"labels,omitempty"`
	}{
		Issue: issue,
		Labels: []string{
			"critical",
		},
	}
	data, _ := json.Marshal(details)

	client := &stubDetailClient{
		resp: &rpc.Response{Success: true, Data: data},
	}
	renderer := uiapi.NewMarkdownRenderer()

	tmpl := template.Must(template.New("issue").Parse("<section data-testid=\"issue-detail\"><h1>{{.Issue.Title}}</h1></section>"))
	handler := uiapi.NewDetailFragmentHandler(client, renderer, tmpl)

	req := httptest.NewRequest(http.MethodGet, "/fragments/issue?id=ui-issue-1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "data-testid=\"issue-detail\"") {
		t.Fatalf("expected fragment markup, got %s", body)
	}
}

func TestResolveDetailPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "empty", path: "", want: ""},
		{name: "already clean", path: "api/issues/ui-7", want: "api/issues/ui-7"},
		{name: "leading slash", path: "/api/issues/ui-7", want: "api/issues/ui-7"},
		{name: "whitespace and trailing slash", path: "  /api/issues/ui-7/details/  ", want: "api/issues/ui-7/details"},
		{name: "relative segments", path: "api/issues/../issues/ui-9", want: "api/issues/ui-9"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := uiapi.ResolveDetailPath(tc.path); got != tc.want {
				t.Fatalf("ResolveDetailPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestDetailHandlerRejectsNonGet(t *testing.T) {
	handler := uiapi.NewDetailHandler(&stubDetailClient{}, uiapi.NewMarkdownRenderer())

	req := httptest.NewRequest(http.MethodPost, "/api/issues/ui-1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestDetailHandlerMissingID(t *testing.T) {
	client := &stubDetailClient{
		resp: &rpc.Response{Success: false, Error: "not found"},
	}
	handler := uiapi.NewDetailHandler(client, uiapi.NewMarkdownRenderer())

	req := httptest.NewRequest(http.MethodGet, "/api/issues/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDetailFragmentHandlerRejectsNonGet(t *testing.T) {
	handler := uiapi.NewDetailFragmentHandler(&stubDetailClient{}, uiapi.NewMarkdownRenderer(), template.Must(template.New("noop").Parse("")))

	req := httptest.NewRequest(http.MethodPost, "/fragments/issue", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestDetailFragmentHandlerRequiresID(t *testing.T) {
	handler := uiapi.NewDetailFragmentHandler(&stubDetailClient{}, uiapi.NewMarkdownRenderer(), template.Must(template.New("noop").Parse("")))

	req := httptest.NewRequest(http.MethodGet, "/fragments/issue", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when id missing, got %d", rec.Code)
	}
}

func TestDetailFragmentHandlerMarkdownFailure(t *testing.T) {
	issue := &types.Issue{
		ID:                 "ui-issue-9",
		Title:              "Markdown failure",
		Status:             types.StatusOpen,
		Description:        "desc",
		Design:             "design",
		Notes:              "notes",
		AcceptanceCriteria: "accept",
		UpdatedAt:          time.Now().UTC(),
	}
	payload := struct {
		*types.Issue
	}{
		Issue: issue,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	client := &stubDetailClient{
		resp: &rpc.Response{Success: true, Data: data},
	}
	renderer := &staticMarkdownRenderer{err: errors.New("markdown boom")}
	tmpl := template.Must(template.New("issue").Parse("<div>{{.Issue.Title}}</div>"))
	handler := uiapi.NewDetailFragmentHandler(client, renderer, tmpl)

	req := httptest.NewRequest(http.MethodGet, "/fragments/issue?id=ui-issue-9", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "render markdown") {
		t.Fatalf("expected markdown error message, got %s", body)
	}
}

func TestDetailFragmentHandlerTemplateFailure(t *testing.T) {
	issue := &types.Issue{
		ID:        "ui-issue-10",
		Title:     "Template failure",
		Status:    types.StatusOpen,
		UpdatedAt: time.Now().UTC(),
	}
	payload := struct {
		*types.Issue
	}{
		Issue: issue,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	client := &stubDetailClient{
		resp: &rpc.Response{Success: true, Data: data},
	}
	renderer := &staticMarkdownRenderer{html: template.HTML("<p>ok</p>")}

	tmpl := template.Must(template.New("issue").Funcs(template.FuncMap{
		"explode": func() (string, error) {
			return "", errors.New("template boom")
		},
	}).Parse("{{explode}}"))

	handler := uiapi.NewDetailFragmentHandler(client, renderer, tmpl)

	req := httptest.NewRequest(http.MethodGet, "/fragments/issue?id=ui-issue-10", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "render template") {
		t.Fatalf("expected template error message, got %s", body)
	}
}
