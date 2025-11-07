package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	mdhtml "github.com/yuin/goldmark/renderer/html"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui/templates"
)

const issueDetailUnavailableDetails = "Issue detail requires an active Beads daemon connection."

// DetailClient captures the subset of rpc.Client needed for show operations.
type DetailClient interface {
	Show(args *rpc.ShowArgs) (*rpc.Response, error)
}

// MarkdownRenderer converts markdown text into safe HTML.
type MarkdownRenderer interface {
	Render(markdown string) (template.HTML, error)
}

type markdownRenderer struct {
	md     goldmark.Markdown
	policy *bluemonday.Policy
}

// NewMarkdownRenderer returns the shared markdown renderer for UI fragments.
func NewMarkdownRenderer() MarkdownRenderer {
	return &markdownRenderer{
		md: goldmark.New(
			goldmark.WithExtensions(extension.GFM, extension.Linkify, extension.Strikethrough),
			goldmark.WithRendererOptions(mdhtml.WithUnsafe()),
		),
		policy: bluemonday.UGCPolicy(),
	}
}

func (r *markdownRenderer) Render(markdown string) (template.HTML, error) {
	if strings.TrimSpace(markdown) == "" {
		return "", nil
	}
	var buf bytes.Buffer
	if err := r.md.Convert([]byte(markdown), &buf); err != nil {
		return "", err
	}
	sanitized := r.policy.SanitizeBytes(buf.Bytes())
	return template.HTML(sanitized), nil
}

// DependencySummary is a lightweight representation used by the UI.
type DependencySummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	IssueType string `json:"issue_type"`
	Priority  int    `json:"priority"`
}

// IssueDetail is the JSON payload returned to the UI.
type IssueDetail struct {
	ID                  string                         `json:"id"`
	Title               string                         `json:"title"`
	Status              string                         `json:"status"`
	StatusLabel         string                         `json:"status_label"`
	IssueType           string                         `json:"issue_type"`
	Priority            int                            `json:"priority"`
	Assignee            string                         `json:"assignee,omitempty"`
	Labels              []string                       `json:"labels,omitempty"`
	Description         string                         `json:"description,omitempty"`
	DescriptionHTML     string                         `json:"description_html,omitempty"`
	DesignHTML          string                         `json:"design_html,omitempty"`
	Notes               string                         `json:"notes,omitempty"`
	NotesHTML           string                         `json:"notes_html,omitempty"`
	AcceptanceHTML      string                         `json:"acceptance_html,omitempty"`
	UpdatedAt           string                         `json:"updated_at"`
	DependenciesSummary map[string][]DependencySummary `json:"-"`
}

type showPayload struct {
	*types.Issue
	Labels            []string            `json:"labels,omitempty"`
	Dependencies      []*types.Issue      `json:"dependencies,omitempty"`
	Dependents        []*types.Issue      `json:"dependents,omitempty"`
	DependencyRecords []*types.Dependency `json:"dependency_records,omitempty"`
}

func decodeShowPayload(data json.RawMessage) (*showPayload, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}

	var payload showPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if payload.Issue == nil {
		return nil, fmt.Errorf("missing issue in payload")
	}
	return &payload, nil
}

// NewDetailHandler returns the JSON API handler for issue detail.
func NewDetailHandler(client DetailClient, renderer MarkdownRenderer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := strings.TrimPrefix(r.URL.Path, "/api/issues/")
		id = strings.Trim(id, "/")
		if id == "" {
			http.NotFound(w, r)
			return
		}

		payload, status, err := fetchIssueDetail(client, id)
		if err != nil {
			if isDaemonUnavailable(err) {
				WriteServiceUnavailable(w, "issue detail unavailable", issueDetailUnavailableDetails)
				return
			}
			http.Error(w, err.Error(), status)
			return
		}

		detail, err := buildIssueDetail(payload, renderer)
		if err != nil {
			http.Error(w, fmt.Sprintf("render markdown: %v", err), http.StatusInternalServerError)
			return
		}

		response := map[string]any{
			"issue":        detail,
			"dependencies": detail.DependenciesSummary,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
		}
	})
}

// NewDetailFragmentHandler returns the HTML fragment handler for issue detail.
func NewDetailFragmentHandler(client DetailClient, renderer MarkdownRenderer, tmpl *template.Template) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}

		payload, status, err := fetchIssueDetail(client, id)
		if err != nil {
			if isDaemonUnavailable(err) {
				WriteServiceUnavailable(w, "issue detail unavailable", issueDetailUnavailableDetails)
				return
			}
			http.Error(w, err.Error(), status)
			return
		}

		detail, err := buildIssueDetail(payload, renderer)
		if err != nil {
			http.Error(w, fmt.Sprintf("render markdown: %v", err), http.StatusInternalServerError)
			return
		}

		type viewModel struct {
			Issue        IssueDetail
			Dependencies map[string][]DependencySummary
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, viewModel{
			Issue:        detail,
			Dependencies: detail.DependenciesSummary,
		}); err != nil {
			http.Error(w, fmt.Sprintf("render template: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write(buf.Bytes()); err != nil {
			http.Error(w, fmt.Sprintf("write response: %v", err), http.StatusInternalServerError)
			return
		}
	})
}

func fetchIssueDetail(client DetailClient, id string) (*showPayload, int, error) {
	resp, err := client.Show(&rpc.ShowArgs{ID: id})
	if err != nil {
		if isDaemonUnavailable(err) {
			return nil, http.StatusServiceUnavailable, err
		}
		status := statusFromResponse(resp, http.StatusBadGateway)
		if resp != nil && strings.TrimSpace(resp.Error) != "" {
			return nil, status, errors.New(resp.Error)
		}
		return nil, status, fmt.Errorf("show issue failed: %v", err)
	}
	if resp == nil {
		return nil, http.StatusNotFound, fmt.Errorf("issue %s not found", id)
	}
	if !resp.Success {
		if isNotFound(resp.Error) {
			return nil, http.StatusNotFound, fmt.Errorf("issue %s not found", id)
		}
		status := statusFromResponse(resp, http.StatusBadGateway)
		return nil, status, errors.New(resp.Error)
	}

	payload, err := decodeShowPayload(resp.Data)
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("decode issue: %v", err)
	}
	if payload == nil {
		return nil, http.StatusNotFound, fmt.Errorf("issue %s not found", id)
	}
	return payload, http.StatusOK, nil
}

func buildIssueDetail(payload *showPayload, renderer MarkdownRenderer) (IssueDetail, error) {
	issue := payload.Issue

	descriptionHTML, err := renderer.Render(issue.Description)
	if err != nil {
		return IssueDetail{}, err
	}
	designHTML, err := renderer.Render(issue.Design)
	if err != nil {
		return IssueDetail{}, err
	}
	notesHTML, err := renderer.Render(issue.Notes)
	if err != nil {
		return IssueDetail{}, err
	}
	acceptanceHTML, err := renderer.Render(issue.AcceptanceCriteria)
	if err != nil {
		return IssueDetail{}, err
	}

	depSummary := summarizeDependencies(payload)

	statusLabel := templates.StatusLabel(string(issue.Status))
	if strings.TrimSpace(statusLabel) == "" {
		statusLabel = string(issue.Status)
	}

	return IssueDetail{
		ID:                  issue.ID,
		Title:               issue.Title,
		Status:              string(issue.Status),
		StatusLabel:         statusLabel,
		IssueType:           string(issue.IssueType),
		Priority:            issue.Priority,
		Assignee:            issue.Assignee,
		Labels:              payload.Labels,
		Description:         issue.Description,
		DescriptionHTML:     string(descriptionHTML),
		DesignHTML:          string(designHTML),
		Notes:               issue.Notes,
		NotesHTML:           string(notesHTML),
		AcceptanceHTML:      string(acceptanceHTML),
		UpdatedAt:           issue.UpdatedAt.UTC().Format(time.RFC3339),
		DependenciesSummary: depSummary,
	}, nil
}

func summarizeDependencies(payload *showPayload) map[string][]DependencySummary {
	if len(payload.DependencyRecords) == 0 {
		return map[string][]DependencySummary{}
	}

	lookup := make(map[string]*types.Issue)
	for _, dep := range payload.Dependencies {
		if dep != nil {
			lookup[dep.ID] = dep
		}
	}

	summary := make(map[string][]DependencySummary)

	for _, rec := range payload.DependencyRecords {
		if rec == nil {
			continue
		}
		target := lookup[rec.DependsOnID]
		if target == nil {
			continue
		}
		item := DependencySummary{
			ID:        target.ID,
			Title:     target.Title,
			Status:    string(target.Status),
			IssueType: string(target.IssueType),
			Priority:  target.Priority,
		}

		switch rec.Type {
		case types.DepBlocks:
			summary["blocks"] = append(summary["blocks"], item)
		case types.DepDiscoveredFrom:
			summary["discovered_from"] = append(summary["discovered_from"], item)
		}
	}

	return summary
}

func isNotFound(err string) bool {
	err = strings.ToLower(err)
	if strings.Contains(err, "no rows") || strings.Contains(err, "not found") {
		return true
	}
	return false
}

// ResolveDetailPath returns the normalized path for API routing.
func ResolveDetailPath(p string) string {
	clean := path.Clean("/" + strings.TrimSpace(p))
	return strings.TrimPrefix(clean, "/")
}
