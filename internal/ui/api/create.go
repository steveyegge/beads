package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

const issueCreationUnavailableDetails = "Issue creation requires an active Beads daemon connection."

// CreateClient captures the subset of rpc.Client needed for issue creation.
type CreateClient interface {
	Create(args *rpc.CreateArgs) (*rpc.Response, error)
}

type createRequest struct {
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	IssueType      string   `json:"issue_type,omitempty"`
	Priority       *int     `json:"priority,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	DiscoveredFrom string   `json:"discovered_from,omitempty"`
}

// NewCreateHandler returns an HTTP handler that proxies quick issue creation.
func NewCreateHandler(client CreateClient, publisher EventPublisher) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if client == nil {
			WriteServiceUnavailable(w, "issue creation unavailable", issueCreationUnavailableDetails)
			return
		}

		defer r.Body.Close() // nolint:errcheck

		var req createRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("decode payload: %v", err), http.StatusBadRequest)
			return
		}

		title := strings.TrimSpace(req.Title)
		if title == "" {
			http.Error(w, "title is required", http.StatusBadRequest)
			return
		}

		issueType := strings.TrimSpace(req.IssueType)
		if issueType == "" {
			issueType = string(types.TypeTask)
		}
		if !types.IssueType(issueType).IsValid() {
			http.Error(w, fmt.Sprintf("invalid issue_type %q", req.IssueType), http.StatusBadRequest)
			return
		}

		priority := 2
		if req.Priority != nil {
			priority = *req.Priority
		}
		if priority < 0 || priority > 4 {
			http.Error(w, fmt.Sprintf("invalid priority %d", priority), http.StatusBadRequest)
			return
		}

		labels := normalizeLabels(req.Labels)

		args := &rpc.CreateArgs{
			Title:       title,
			Description: strings.TrimSpace(req.Description),
			IssueType:   issueType,
			Priority:    priority,
			Labels:      labels,
		}

		if dep := strings.TrimSpace(req.DiscoveredFrom); dep != "" {
			args.Dependencies = []string{dep}
		}

		resp, err := client.Create(args)
		if err != nil {
			if isDaemonUnavailable(err) {
				WriteServiceUnavailable(w, "issue creation unavailable", issueCreationUnavailableDetails)
				return
			}
			if resp != nil && !resp.Success {
				message := strings.TrimSpace(resp.Error)
				if message == "" {
					message = fmt.Sprintf("create issue failed: %v", err)
				}
				status := statusFromResponse(resp, mapCreateError(message))
				http.Error(w, message, status)
				return
			}
			http.Error(w, fmt.Sprintf("create issue failed: %v", err), http.StatusBadGateway)
			return
		}
		if resp == nil {
			http.Error(w, "create issue: empty response", http.StatusBadGateway)
			return
		}
		if !resp.Success {
			status := statusFromResponse(resp, mapCreateError(resp.Error))
			http.Error(w, resp.Error, status)
			return
		}

		issue, err := decodeCreatedIssue(resp.Data)
		if err != nil {
			http.Error(w, fmt.Sprintf("decode issue: %v", err), http.StatusBadGateway)
			return
		}

		summary := IssueToSummary(issue)
		if len(summary.Labels) == 0 && len(labels) > 0 {
			summary.Labels = append([]string(nil), labels...)
		}

		if publisher != nil {
			publisher.Publish(IssueEvent{
				Type:  EventTypeCreated,
				Issue: summary,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		if summary.ID != "" {
			w.Header().Set("Location", fmt.Sprintf("/api/issues/%s", summary.ID))
		}
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"issue": summary,
		}); err != nil {
			http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
			return
		}
	})
}

func decodeCreatedIssue(data json.RawMessage) (*types.Issue, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty create response payload")
	}
	var issue types.Issue
	if err := json.Unmarshal(data, &issue); err == nil && issue.ID != "" {
		return &issue, nil
	}
	var wrapper struct {
		Issue *types.Issue `json:"issue"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil || wrapper.Issue == nil {
		return nil, fmt.Errorf("unexpected create payload: %w", err)
	}
	return wrapper.Issue, nil
}

func normalizeLabels(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, label := range raw {
		trimmed := strings.TrimSpace(label)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mapCreateError(err string) int {
	lower := strings.ToLower(strings.TrimSpace(err))
	switch {
	case strings.Contains(lower, "invalid"), strings.Contains(lower, "required"):
		return http.StatusBadRequest
	case strings.Contains(lower, "duplicate"), strings.Contains(lower, "exists"):
		return http.StatusConflict
	default:
		return http.StatusBadGateway
	}
}
