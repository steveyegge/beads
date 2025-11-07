package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

const issueListUnavailableDetails = "Issue list requires an active Beads daemon connection."

// ListClient captures the subset of rpc.Client needed for issue listing.
type ListClient interface {
	List(args *rpc.ListArgs) (*rpc.Response, error)
}

// IssueSummary represents the lightweight payload returned to the UI.
type IssueSummary struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	IssueType string   `json:"issue_type"`
	Priority  int      `json:"priority"`
	Assignee  string   `json:"assignee,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	UpdatedAt string   `json:"updated_at"`
}

// IssueToSummary transforms a full issue record into the lightweight summary used by the UI.
func IssueToSummary(issue *types.Issue) IssueSummary {
	if issue == nil {
		return IssueSummary{}
	}

	return IssueSummary{
		ID:        issue.ID,
		Title:     issue.Title,
		Status:    string(issue.Status),
		IssueType: string(issue.IssueType),
		Priority:  issue.Priority,
		Assignee:  issue.Assignee,
		Labels:    issue.Labels,
		UpdatedAt: issue.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// NewListHandler returns an HTTP handler that proxies list queries via the provided client.
func NewListHandler(client ListClient) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		args := buildListArgs(r)
		queryValues := r.URL.Query()
		queueID := strings.TrimSpace(queryValues.Get("queue"))

		pageSize := args.Limit
		if strings.EqualFold(queueID, "closed") {
			pageSize = clampClosedLimit(pageSize)
			if pageSize == 0 {
				pageSize = ClosedQueueDefaultLimit
			}
			effectiveLimit := pageSize + 1
			args.Limit = effectiveLimit
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

		if strings.EqualFold(queueID, "closed") {
			sortClosedQueueIssues(issues)
		}

		hasMore := false
		nextCursor := ""
		if strings.EqualFold(queueID, "closed") {
			if pageSize == 0 {
				pageSize = ClosedQueueDefaultLimit
			}
			trimmed, more, cursor := paginateClosedIssues(issues, pageSize)
			issues = trimmed
			hasMore = more
			nextCursor = cursor
		}

		summaries := make([]IssueSummary, 0, len(issues))
		for _, issue := range issues {
			if issue == nil {
				continue
			}
			summaries = append(summaries, IssueToSummary(issue))
		}

		payload := map[string]any{
			"issues":   summaries,
			"has_more": hasMore,
		}
		if nextCursor != "" {
			payload["next_cursor"] = nextCursor
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
		}
	})
}

func buildListArgs(r *http.Request) *rpc.ListArgs {
	q := r.URL.Query()

	args := &rpc.ListArgs{
		Query: strings.TrimSpace(q.Get("q")),
	}

	if limitStr := strings.TrimSpace(q.Get("limit")); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			args.Limit = limit
		}
	}

	if priorityStr := strings.TrimSpace(q.Get("priority")); priorityStr != "" {
		if priority, err := strconv.Atoi(priorityStr); err == nil {
			args.Priority = &priority
		}
	}

	if status := strings.TrimSpace(q.Get("status")); status != "" {
		args.Status = status
	}

	if issueType := strings.TrimSpace(q.Get("type")); issueType != "" {
		args.IssueType = issueType
	}

	if assignee := strings.TrimSpace(q.Get("assignee")); assignee != "" {
		args.Assignee = assignee
	}

	labelSet := make(map[string]struct{})

	addLabels := func(values []string, consumer func([]string)) {
		if len(values) == 0 {
			return
		}
		out := make([]string, 0, len(values))
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}
			if _, exists := labelSet[trimmed]; exists {
				continue
			}
			labelSet[trimmed] = struct{}{}
			out = append(out, trimmed)
		}
		if len(out) > 0 {
			consumer(out)
		}
	}

	addLabels(q["label"], func(labels []string) {
		args.Labels = append(args.Labels, labels...)
	})
	if rawLabels := q.Get("labels"); rawLabels != "" {
		addLabels(strings.Split(rawLabels, ","), func(labels []string) {
			args.Labels = append(args.Labels, labels...)
		})
	}
	addLabels(q["labels_any"], func(labels []string) {
		args.LabelsAny = append(args.LabelsAny, labels...)
	})
	if rawAny := q.Get("labels_any"); rawAny != "" {
		addLabels(strings.Split(rawAny, ","), func(labels []string) {
			args.LabelsAny = append(args.LabelsAny, labels...)
		})
	}

	if idsRaw := strings.TrimSpace(q.Get("ids")); idsRaw != "" {
		for _, id := range strings.Split(idsRaw, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				args.IDs = append(args.IDs, id)
			}
		}
	}

	if idPrefix := strings.TrimSpace(q.Get("id_prefix")); idPrefix != "" {
		args.IDPrefix = idPrefix
	} else if legacyPrefix := strings.TrimSpace(q.Get("prefix")); legacyPrefix != "" {
		args.IDPrefix = legacyPrefix
	}

	if queue := strings.TrimSpace(q.Get("queue")); queue != "" {
		applyQueueFilter(queue, args)
	}

	if cursor := strings.TrimSpace(q.Get("cursor")); cursor != "" {
		args.Cursor = cursor
	}
	if closedBefore := strings.TrimSpace(q.Get("closed_before")); closedBefore != "" {
		args.ClosedBefore = closedBefore
	}
	if closedBeforeID := strings.TrimSpace(q.Get("closed_before_id")); closedBeforeID != "" {
		args.ClosedBeforeID = closedBeforeID
	}
	order := strings.TrimSpace(q.Get("order"))
	if order == "" {
		primary := strings.TrimSpace(q.Get("sort"))
		secondary := strings.TrimSpace(q.Get("sort_secondary"))
		order = combineSortParameters(primary, secondary)
	}
	if order != "" {
		if parsed := types.ParseIssueSortOrder(order); len(parsed) > 0 {
			args.Order = types.EncodeIssueSortOrder(parsed)
		}
	}

	return args
}

func combineSortParameters(primary, secondary string) string {
	normalizedPrimary := normalizeSortToken(primary)
	normalizedSecondary := normalizeSortToken(secondary)

	if normalizedPrimary == "" && normalizedSecondary == "" {
		return ""
	}

	tokens := make([]string, 0, 2)
	if normalizedPrimary != "" {
		tokens = append(tokens, normalizedPrimary)
	}
	if normalizedSecondary != "" && normalizedSecondary != normalizedPrimary {
		tokens = append(tokens, normalizedSecondary)
	}
	return strings.Join(tokens, ",")
}

func normalizeSortToken(token string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" || token == "none" {
		return ""
	}
	return token
}

func applyQueueFilter(queue string, args *rpc.ListArgs) {
	switch strings.ToLower(queue) {
	case "ready":
		if args.Status == "" {
			args.Status = string(types.StatusOpen)
		}
	case "in_progress":
		if args.Status == "" {
			args.Status = string(types.StatusInProgress)
		}
	case "blocked":
		if args.Status == "" {
			args.Status = string(types.StatusBlocked)
		}
	case "recent":
		if args.Limit == 0 {
			args.Limit = 20
		}
	case "closed":
		if args.Status == "" {
			args.Status = string(types.StatusClosed)
		}
	}
}
