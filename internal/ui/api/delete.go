package api

import (
	"context"
	"net/http"
	"path"
	"strings"
)

const issueDeletionUnavailableDetails = "Issue deletion requires an active Beads daemon connection."

// DeleteClient captures the subset of storage operations needed for issue deletion.
type DeleteClient interface {
	DeleteIssue(ctx context.Context, id string) error
}

// DeleteHandlerOption configures optional behaviour for the delete handler.
type DeleteHandlerOption func(*deleteHandlerOptions)

type deleteHandlerOptions struct {
	publisher EventPublisher
}

// WithDeleteEventPublisher forwards successful deletions to the provided publisher.
func WithDeleteEventPublisher(publisher EventPublisher) DeleteHandlerOption {
	return func(opts *deleteHandlerOptions) {
		opts.publisher = publisher
	}
}

// NewDeleteHandler returns an HTTP handler that removes an issue after confirmation.
func NewDeleteHandler(client DeleteClient, opts ...DeleteHandlerOption) http.Handler {
	var config deleteHandlerOptions
	for _, opt := range opts {
		opt(&config)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if client == nil {
			WriteServiceUnavailable(
				w,
				"issue deletion unavailable",
				issueDeletionUnavailableDetails,
			)
			return
		}

		id, ok := parseDeletePath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		confirm := strings.TrimSpace(r.URL.Query().Get("confirm"))
		if confirm == "" {
			http.Error(w, "confirmation is required", http.StatusBadRequest)
			return
		}
		if !strings.EqualFold(confirm, id) {
			http.Error(w, "confirmation does not match issue id", http.StatusBadRequest)
			return
		}

		if err := client.DeleteIssue(r.Context(), id); err != nil {
			if isDaemonUnavailable(err) {
				WriteServiceUnavailable(
					w,
					"issue deletion unavailable",
					issueDeletionUnavailableDetails,
				)
				return
			}
			status := mapDeleteError(err)
			http.Error(w, err.Error(), status)
			return
		}

		if config.publisher != nil {
			config.publisher.Publish(IssueEvent{
				Type:  EventTypeDeleted,
				Issue: IssueSummary{ID: id},
			})
		}

		w.WriteHeader(http.StatusNoContent)
	})
}

func parseDeletePath(rawPath string) (string, bool) {
	clean := path.Clean(rawPath)
	if !strings.HasPrefix(clean, "/api/issues/") {
		return "", false
	}

	id := strings.TrimPrefix(clean, "/api/issues/")
	id = strings.TrimSuffix(id, "/")
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func mapDeleteError(err error) int {
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "not found"):
		return http.StatusNotFound
	case strings.Contains(lower, "context canceled"),
		strings.Contains(lower, "context cancelled"),
		strings.Contains(lower, "deadline exceeded"):
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}
