package api

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui/templates"
)

// ListFragmentOption configures the list fragment handler.
type ListFragmentOption func(*listFragmentConfig)

type listFragmentConfig struct {
	// nowFunc returns the current time, overridable for tests.
	nowFunc func() time.Time
	// tmpl executes the fragment rendering; nil defaults to embedded template.
	tmpl *template.Template
}

// WithListFragmentClock overrides the clock used for relative time calculations.
func WithListFragmentClock(now func() time.Time) ListFragmentOption {
	return func(cfg *listFragmentConfig) {
		if now != nil {
			cfg.nowFunc = now
		}
	}
}

// WithListFragmentTemplate injects a parsed template for rendering.
func WithListFragmentTemplate(tmpl *template.Template) ListFragmentOption {
	return func(cfg *listFragmentConfig) {
		cfg.tmpl = tmpl
	}
}

// NewListFragmentHandler returns an HTTP handler that renders the issue list fragment.
func NewListFragmentHandler(client ListClient, opts ...ListFragmentOption) http.Handler {
	cfg := listFragmentConfig{
		nowFunc: time.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		queryValues := r.URL.Query()
		appendMode := strings.EqualFold(strings.TrimSpace(queryValues.Get("append")), "1")
		if client == nil {
			WriteServiceUnavailable(w, "issue list unavailable", "Issue list requires an active Beads daemon connection.")
			return
		}

		args := buildListArgs(r)

		pageSize := args.Limit
		statusClosed := strings.EqualFold(strings.TrimSpace(args.Status), string(types.StatusClosed))
		if statusClosed {
			pageSize = clampClosedLimit(pageSize)
			if pageSize == 0 {
				pageSize = ClosedQueueDefaultLimit
			}
			args.Limit = pageSize + 1
			args.Order = closedQueueOrder

			cursorParam := strings.TrimSpace(queryValues.Get("cursor"))
			if cursorParam != "" {
				closedBefore, cursorID, err := parseClosedCursor(cursorParam)
				if err != nil {
					http.Error(w, fmt.Sprintf("invalid cursor: %v", err), http.StatusBadRequest)
					return
				}
				args.Cursor = cursorParam
				args.ClosedBefore = closedBefore.UTC().Format(time.RFC3339Nano)
				args.ClosedBeforeID = cursorID
			}
		}

		resp, err := client.List(args)
		if err != nil {
			if isDaemonUnavailable(err) {
				WriteServiceUnavailable(w, "issue list unavailable", issueListUnavailableDetails)
				return
			}
			status := statusFromResponse(resp, http.StatusBadGateway)
			message := fmt.Sprintf("list issues failed: %v", err)
			if resp != nil && strings.TrimSpace(resp.Error) != "" {
				message = resp.Error
			}
			http.Error(w, message, status)
			return
		}
		if resp == nil {
			http.Error(w, "list issues returned nil response", http.StatusBadGateway)
			return
		}
		if !resp.Success {
			status := statusFromResponse(resp, http.StatusBadGateway)
			if status == http.StatusBadGateway {
				if resp.Error == "invalid priority" || resp.Error == "invalid status" {
					status = http.StatusBadRequest
				}
			}
			http.Error(w, resp.Error, status)
			return
		}

		var issues []*types.Issue
		if err := json.Unmarshal(resp.Data, &issues); err != nil {
			http.Error(w, fmt.Sprintf("decode issues: %v", err), http.StatusBadGateway)
			return
		}

		if statusClosed {
			sortClosedQueueIssues(issues)
		}

		selectedID := strings.TrimSpace(queryValues.Get("selected"))

		trimmedIssues := issues
		hasMore := false
		nextCursor := ""
		if statusClosed {
			if pageSize == 0 {
				pageSize = ClosedQueueDefaultLimit
			}
			trimmedIssues, hasMore, nextCursor = paginateClosedIssues(issues, pageSize)
		}

		items := buildListIssues(cfg.nowFunc, trimmedIssues)
		if len(items) > 0 {
			if selectedID == "" {
				selectedID = items[0].ID
			}
			for i := range items {
				items[i].Index = i
				items[i].Active = items[i].ID == selectedID
			}
		}

		filters := sanitizeRefreshFilters(queryValues)
		heading := deriveListHeading(args)
		emptyMessage := deriveEmptyMessage(heading, args)
		data := templates.ListFragmentData{
			SelectedIssueID: selectedID,
			Filters:         filters,
			Heading:         heading,
			EmptyMessage:    emptyMessage,
			Issues:          items,
			HasMore:         hasMore,
			NextCursor:      nextCursor,
		}
		data.RefreshURL = templates.BuildListRefreshURL(selectedID, filters)
		data.LoadMoreURL = data.RefreshURL

		if hasMore && nextCursor != "" {
			data.LoadMoreVals = map[string]any{
				"append": "1",
				"cursor": nextCursor,
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")

		if appendMode {
			appendVals := data.LoadMoreVals
			if appendVals != nil {
				clone := make(map[string]any, len(appendVals))
				for k, v := range appendVals {
					clone[k] = v
				}
				appendVals = clone
			}
			appendData := templates.ListAppendData{
				Issues:       items,
				HasMore:      hasMore,
				LoadMoreURL:  data.LoadMoreURL,
				LoadMoreVals: appendVals,
			}
			html, err := templates.RenderListAppend(appendData)
			if err != nil {
				http.Error(w, fmt.Sprintf("render append list: %v", err), http.StatusInternalServerError)
				return
			}
			if _, err := w.Write(html); err != nil {
				http.Error(w, fmt.Sprintf("write response: %v", err), http.StatusInternalServerError)
			}
			return
		}

		html, err := templates.ExecuteListTemplate(cfg.tmpl, data)
		if err != nil {
			http.Error(w, fmt.Sprintf("render list: %v", err), http.StatusInternalServerError)
			return
		}

		if _, err := w.Write(html); err != nil {
			http.Error(w, fmt.Sprintf("write response: %v", err), http.StatusInternalServerError)
			return
		}
	})
}

func buildListIssues(now func() time.Time, issues []*types.Issue) []templates.ListIssue {
	if now == nil {
		now = time.Now
	}
	current := now()
	items := make([]templates.ListIssue, 0, len(issues))

	for _, issue := range issues {
		if issue == nil {
			continue
		}
		updatedAt := issue.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = current
		}
		item := templates.ListIssue{
			ID:              issue.ID,
			Title:           issue.Title,
			Status:          string(issue.Status),
			IssueType:       string(issue.IssueType),
			IssueTypeLabel:  formatClassLabel(string(issue.IssueType)),
			IssueTypeClass:  sanitizeClass(string(issue.IssueType)),
			Priority:        issue.Priority,
			PriorityLabel:   formatPriorityLabel(issue.Priority),
			PriorityClass:   sanitizePriority(issue.Priority),
			UpdatedISO:      updatedAt.UTC().Format(time.RFC3339),
			UpdatedRelative: templates.RelativeTimeString(current, updatedAt),
			DetailURL:       buildDetailURL(issue.ID),
		}
		items = append(items, item)
	}
	return items
}

func deriveListHeading(args *rpc.ListArgs) string {
	defaultHeading := "Filtered issues"
	if args == nil {
		return defaultHeading
	}
	if trimmed := strings.TrimSpace(args.Status); trimmed != "" {
		label := strings.TrimSpace(templates.StatusLabel(trimmed))
		if label != "" {
			return fmt.Sprintf("%s issues", label)
		}
	}
	if query := strings.TrimSpace(args.Query); query != "" {
		return fmt.Sprintf("Issues matching %q", query)
	}
	if len(args.Labels) > 0 {
		return "Issues matching all labels"
	}
	if len(args.LabelsAny) > 0 {
		return "Issues matching any label"
	}
	if trimmed := strings.TrimSpace(args.Assignee); trimmed != "" {
		return fmt.Sprintf("Issues assigned to %s", trimmed)
	}
	return defaultHeading
}

func deriveEmptyMessage(heading string, args *rpc.ListArgs) string {
	base := "No issues match the current filters."
	if args == nil {
		return base
	}
	if query := strings.TrimSpace(args.Query); query != "" {
		return fmt.Sprintf("No issues matched %q.", query)
	}
	if trimmed := strings.TrimSpace(args.Status); trimmed != "" {
		label := strings.TrimSpace(templates.StatusLabel(trimmed))
		if label != "" {
			return fmt.Sprintf("No %s issues found.", strings.ToLower(label))
		}
	}
	if len(args.Labels) > 0 || len(args.LabelsAny) > 0 {
		return "No issues matched the selected labels."
	}
	if trimmed := strings.TrimSpace(args.Assignee); trimmed != "" {
		return fmt.Sprintf("No issues assigned to %s.", trimmed)
	}
	return base
}

func sanitizeClass(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "_", "-")
	if value == "" {
		return "unknown"
	}
	return value
}

func formatClassLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		parts[i] = strings.ToUpper(lower[:1]) + lower[1:]
	}
	return strings.Join(parts, " ")
}

func sanitizePriority(priority int) string {
	if priority < 0 || priority > 4 {
		return "p?"
	}
	return fmt.Sprintf("p%d", priority)
}

func formatPriorityLabel(priority int) string {
	if priority < 0 || priority > 4 {
		return "P?"
	}
	return fmt.Sprintf("P%d", priority)
}

func buildDetailURL(id string) string {
	if strings.TrimSpace(id) == "" {
		return "/fragments/issue"
	}
	return "/fragments/issue?id=" + url.QueryEscape(id)
}

func sanitizeRefreshFilters(values url.Values) url.Values {
	if len(values) == 0 {
		return nil
	}
	out := url.Values{}
	for key, vals := range values {
		if key == "" {
			continue
		}
		lower := strings.ToLower(key)
		switch lower {
		case "queue", "queue_label", "selected", "cursor", "next_cursor", "append", "closed_before", "closed_before_id":
			continue
		}
		if strings.HasPrefix(lower, "_") || strings.HasPrefix(lower, "hx-") {
			continue
		}
		for _, val := range vals {
			out.Add(key, val)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
