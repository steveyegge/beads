package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/steveyegge/beads/internal/rpc"
)

const labelChangeUnavailableDetails = "Label changes require an active Beads daemon connection."

// LabelAdder represents the subset of rpc.Client needed for adding labels.
type LabelAdder interface {
	AddLabel(args *rpc.LabelAddArgs) (*rpc.Response, error)
}

// LabelRemover represents the subset of rpc.Client needed for removing labels.
type LabelRemover interface {
	RemoveLabel(args *rpc.LabelRemoveArgs) (*rpc.Response, error)
}

// LabelClient combines add/remove capabilities.
type LabelClient interface {
	LabelAdder
	LabelRemover
}

type labelRequest struct {
	Label string `json:"label"`
}

type labelHandlerOptions struct {
	publisher EventPublisher
}

// LabelHandlerOption configures optional label handler behaviour.
type LabelHandlerOption func(*labelHandlerOptions)

// WithLabelEventPublisher forwards label mutations to the provided event publisher.
func WithLabelEventPublisher(publisher EventPublisher) LabelHandlerOption {
	return func(opts *labelHandlerOptions) {
		if publisher != nil {
			opts.publisher = publisher
		}
	}
}

// NewLabelHandler returns an HTTP handler that mutates labels on an issue.
func NewLabelHandler(detail DetailClient, adder LabelAdder, remover LabelRemover, opts ...LabelHandlerOption) http.Handler {
	var config labelHandlerOptions
	for _, opt := range opts {
		opt(&config)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseLabelPath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		switch r.Method {
		case http.MethodPost:
			if adder == nil {
				WriteServiceUnavailable(w, "label addition unavailable", labelChangeUnavailableDetails)
				return
			}
			handleLabelMutation(w, r, id, detail, config.publisher, func(label string) (*rpc.Response, error) {
				return adder.AddLabel(&rpc.LabelAddArgs{
					ID:    id,
					Label: label,
				})
			})
		case http.MethodDelete:
			if remover == nil {
				WriteServiceUnavailable(w, "label removal unavailable", labelChangeUnavailableDetails)
				return
			}
			handleLabelMutation(w, r, id, detail, config.publisher, func(label string) (*rpc.Response, error) {
				return remover.RemoveLabel(&rpc.LabelRemoveArgs{
					ID:    id,
					Label: label,
				})
			})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func handleLabelMutation(
	w http.ResponseWriter,
	r *http.Request,
	id string,
	detail DetailClient,
	publisher EventPublisher,
	invoke func(label string) (*rpc.Response, error),
) {
	defer r.Body.Close() // nolint:errcheck

	var payload labelRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf("decode payload: %v", err), http.StatusBadRequest)
		return
	}

	label := strings.TrimSpace(payload.Label)
	if label == "" {
		http.Error(w, "label is required", http.StatusBadRequest)
		return
	}

	resp, err := invoke(label)
	if err != nil {
		if isDaemonUnavailable(err) {
			WriteServiceUnavailable(w, "label change unavailable", labelChangeUnavailableDetails)
			return
		}
		status := statusFromResponse(resp, http.StatusBadGateway)
		message := fmt.Sprintf("label mutation failed: %v", err)
		if resp != nil && strings.TrimSpace(resp.Error) != "" {
			message = resp.Error
		}
		http.Error(w, message, status)
		return
	}
	if resp == nil {
		http.Error(w, "empty label response", http.StatusBadGateway)
		return
	}
	if !resp.Success {
		status := statusFromResponse(resp, mapLabelError(resp.Error))
		http.Error(w, resp.Error, status)
		return
	}

	if detail == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	show, status, err := fetchIssueDetail(detail, id)
	if err != nil {
		if isDaemonUnavailable(err) {
			WriteServiceUnavailable(w, "issue detail unavailable", issueDetailUnavailableDetails)
			return
		}
		http.Error(w, err.Error(), status)
		return
	}

	summary := IssueToSummary(show.Issue)
	summary.Labels = append([]string(nil), show.Labels...)

	if publisher != nil {
		publisher.Publish(IssueEvent{
			Type:  EventTypeUpdated,
			Issue: summary,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"labels": show.Labels,
		"issue":  summary,
	}); err != nil {
		http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

func parseLabelPath(rawPath string) (string, bool) {
	clean := path.Clean(rawPath)
	if !strings.HasPrefix(clean, "/api/issues/") {
		return "", false
	}

	if !strings.HasSuffix(clean, "/labels") {
		return "", false
	}

	id := strings.TrimSuffix(clean, "/labels")
	id = strings.TrimSuffix(id, "/")
	id = strings.TrimPrefix(id, "/api/issues/")
	if strings.TrimSpace(id) == "" {
		return "", false
	}
	return id, true
}

func mapLabelError(err string) int {
	lower := strings.ToLower(err)
	switch {
	case isNotFound(lower):
		return http.StatusNotFound
	case strings.Contains(lower, "already exists"),
		strings.Contains(lower, "duplicate"),
		strings.Contains(lower, "conflict"):
		return http.StatusConflict
	case strings.Contains(lower, "invalid"),
		strings.Contains(lower, "missing"),
		strings.Contains(lower, "empty"):
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}
