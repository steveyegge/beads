package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

const statusUpdatesUnavailableDetails = "Status changes require an active Beads daemon connection."

// UpdateClient captures the subset of rpc.Client needed for issue updates.
type UpdateClient interface {
	Update(args *rpc.UpdateArgs) (*rpc.Response, error)
}

type statusRequest struct {
	Status string `json:"status"`
}

type StatusHandlerOption func(*statusHandlerOptions)

type statusHandlerOptions struct {
	publisher EventPublisher
}

type detailUpdateRequest struct {
	Description *string `json:"description,omitempty"`
	Notes       *string `json:"notes,omitempty"`
}

// WithStatusEventPublisher wires an event publisher that receives updates on successful mutations.
func WithStatusEventPublisher(publisher EventPublisher) StatusHandlerOption {
	return func(opts *statusHandlerOptions) {
		opts.publisher = publisher
	}
}

// NewStatusHandler returns an HTTP handler that updates an issue's status.
func NewStatusHandler(client UpdateClient, opts ...StatusHandlerOption) http.Handler {
	var config statusHandlerOptions
	for _, opt := range opts {
		opt(&config)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if client == nil {
			WriteServiceUnavailable(w, "status updates unavailable", statusUpdatesUnavailableDetails)
			return
		}

		id, ok := parseStatusPath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		defer r.Body.Close()

		var payload statusRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, fmt.Sprintf("decode payload: %v", err), http.StatusBadRequest)
			return
		}

		status := strings.TrimSpace(payload.Status)
		if status == "" {
			http.Error(w, "status is required", http.StatusBadRequest)
			return
		}

		statusCopy := status
		resp, err := client.Update(&rpc.UpdateArgs{
			ID:     id,
			Status: &statusCopy,
		})
		if err != nil {
			if isDaemonUnavailable(err) {
				WriteServiceUnavailable(w, "status updates unavailable", statusUpdatesUnavailableDetails)
				return
			}
			status := statusFromResponse(resp, http.StatusBadGateway)
			message := fmt.Sprintf("update failed: %v", err)
			if resp != nil && strings.TrimSpace(resp.Error) != "" {
				message = resp.Error
			}
			http.Error(w, message, status)
			return
		}
		if resp == nil {
			http.Error(w, "empty update response", http.StatusBadGateway)
			return
		}
		if !resp.Success {
			status := statusFromResponse(resp, mapUpdateError(resp.Error))
			http.Error(w, resp.Error, status)
			return
		}
		if len(resp.Data) == 0 {
			http.Error(w, "update succeeded without payload", http.StatusBadGateway)
			return
		}

		var issue types.Issue
		if err := json.Unmarshal(resp.Data, &issue); err != nil {
			http.Error(w, fmt.Sprintf("decode updated issue: %v", err), http.StatusBadGateway)
			return
		}

		summary := IssueToSummary(&issue)

		if config.publisher != nil {
			config.publisher.Publish(IssueEvent{
				Type:  EventTypeUpdated,
				Issue: summary,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"issue": summary,
		}); err != nil {
			http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
			return
		}
	})
}

func newDetailUpdateHandler(client UpdateClient, publisher EventPublisher) http.Handler {
	if client == nil {
		return nil
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id, ok := parseIssueBasePath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		defer r.Body.Close()

		var payload detailUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, fmt.Sprintf("decode payload: %v", err), http.StatusBadRequest)
			return
		}

		updatedFields := make([]string, 0, 2)
		args := rpc.UpdateArgs{ID: id}
		if payload.Description != nil {
			args.Description = payload.Description
			updatedFields = append(updatedFields, "description")
		}
		if payload.Notes != nil {
			args.Notes = payload.Notes
			updatedFields = append(updatedFields, "notes")
		}
		if len(updatedFields) == 0 {
			http.Error(w, "no fields to update", http.StatusBadRequest)
			return
		}

		resp, err := client.Update(&args)
		if err != nil {
			if isDaemonUnavailable(err) {
				WriteServiceUnavailable(w, "status updates unavailable", statusUpdatesUnavailableDetails)
				return
			}
			status := statusFromResponse(resp, http.StatusBadGateway)
			message := fmt.Sprintf("update failed: %v", err)
			if resp != nil && strings.TrimSpace(resp.Error) != "" {
				message = resp.Error
			}
			http.Error(w, message, status)
			return
		}
		if resp == nil {
			http.Error(w, "empty update response", http.StatusBadGateway)
			return
		}
		if !resp.Success {
			status := statusFromResponse(resp, mapUpdateError(resp.Error))
			http.Error(w, resp.Error, status)
			return
		}
		if len(resp.Data) == 0 {
			http.Error(w, "update succeeded without payload", http.StatusBadGateway)
			return
		}

		var issue types.Issue
		if err := json.Unmarshal(resp.Data, &issue); err != nil {
			http.Error(w, fmt.Sprintf("decode updated issue: %v", err), http.StatusBadGateway)
			return
		}

		summary := IssueToSummary(&issue)
		if publisher != nil {
			publisher.Publish(IssueEvent{
				Type:  EventTypeUpdated,
				Issue: summary,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"issue":          summary,
			"updated_fields": updatedFields,
		}); err != nil {
			http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
			return
		}
	})
}

type issueHandlerOptions struct {
	labelAdder     LabelAdder
	labelRemover   LabelRemover
	deleteClient   DeleteClient
	eventPublisher EventPublisher
}

// IssueHandlerOption configures optional behavior for the issue handler multiplexer.
type IssueHandlerOption func(*issueHandlerOptions)

// WithLabelClient wires a label client for add/remove operations.
func WithLabelClient(client LabelClient) IssueHandlerOption {
	return func(opts *issueHandlerOptions) {
		if client != nil {
			opts.labelAdder = client
			opts.labelRemover = client
		}
	}
}

// WithEventPublisher forwards successful mutations to the provided publisher.
func WithEventPublisher(publisher EventPublisher) IssueHandlerOption {
	return func(opts *issueHandlerOptions) {
		opts.eventPublisher = publisher
	}
}

// WithLabelHandlers wires distinct label add/remove handlers.
func WithLabelHandlers(adder LabelAdder, remover LabelRemover) IssueHandlerOption {
	return func(opts *issueHandlerOptions) {
		opts.labelAdder = adder
		opts.labelRemover = remover
	}
}

// WithDeleteClient wires a delete client for permanent issue removal.
func WithDeleteClient(client DeleteClient) IssueHandlerOption {
	return func(opts *issueHandlerOptions) {
		opts.deleteClient = client
	}
}

// NewIssueHandler returns a multiplexer that routes issue API requests to detail, status, or label handlers.
func NewIssueHandler(detailClient DetailClient, renderer MarkdownRenderer, updateClient UpdateClient, opts ...IssueHandlerOption) http.Handler {
	var config issueHandlerOptions
	for _, opt := range opts {
		opt(&config)
	}

	detailHandler := NewDetailHandler(detailClient, renderer)
	statusHandler := NewStatusHandler(updateClient, WithStatusEventPublisher(config.eventPublisher))
	updateHandler := newDetailUpdateHandler(updateClient, config.eventPublisher)
	var labelHandler http.Handler
	if config.labelAdder != nil || config.labelRemover != nil {
		opts := []LabelHandlerOption{}
		if config.eventPublisher != nil {
			opts = append(opts, WithLabelEventPublisher(config.eventPublisher))
		}
		labelHandler = NewLabelHandler(detailClient, config.labelAdder, config.labelRemover, opts...)
	}
	var deleteHandler http.Handler
	if config.deleteClient != nil {
		deleteOpts := []DeleteHandlerOption{}
		if config.eventPublisher != nil {
			deleteOpts = append(deleteOpts, WithDeleteEventPublisher(config.eventPublisher))
		}
		deleteHandler = NewDeleteHandler(config.deleteClient, deleteOpts...)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if labelHandler != nil {
			if _, ok := parseLabelPath(r.URL.Path); ok {
				switch r.Method {
				case http.MethodPost, http.MethodDelete:
					labelHandler.ServeHTTP(w, r)
				default:
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				}
				return
			}
		}
		if _, ok := parseStatusPath(r.URL.Path); ok {
			statusHandler.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodDelete {
			if deleteHandler != nil {
				deleteHandler.ServeHTTP(w, r)
			} else {
				WriteServiceUnavailable(
					w,
					"issue deletion unavailable",
					"Issue deletion requires an active Beads daemon connection.",
				)
			}
			return
		}
		if r.Method == http.MethodPatch {
			if updateHandler != nil {
				updateHandler.ServeHTTP(w, r)
			} else {
				WriteServiceUnavailable(
					w,
					"issue updates unavailable",
					"Issue updates require an active Beads daemon connection.",
				)
			}
			return
		}
		detailHandler.ServeHTTP(w, r)
	})
}

func parseIssueBasePath(rawPath string) (string, bool) {
	clean := path.Clean(rawPath)
	if !strings.HasPrefix(clean, "/api/issues/") {
		return "", false
	}

	target := strings.TrimPrefix(clean, "/api/issues/")
	target = strings.TrimSuffix(target, "/")
	if target == "" {
		return "", false
	}
	if strings.Contains(target, "/") {
		return "", false
	}

	id := strings.TrimSpace(target)
	if id == "" {
		return "", false
	}
	return id, true
}

func parseStatusPath(rawPath string) (string, bool) {
	clean := path.Clean(rawPath)
	if !strings.HasPrefix(clean, "/api/issues/") {
		return "", false
	}

	target := strings.TrimPrefix(clean, "/api/issues/")
	if !strings.HasSuffix(target, "/status") {
		return "", false
	}

	id := strings.TrimSuffix(target, "/status")
	id = strings.TrimSuffix(id, "/")
	if strings.TrimSpace(id) == "" {
		return "", false
	}
	return id, true
}

func mapUpdateError(err string) int {
	lower := strings.ToLower(err)

	switch {
	case isNotFound(lower):
		return http.StatusNotFound
	case strings.Contains(lower, "invalid status"),
		strings.Contains(lower, "invalid priority"),
		strings.Contains(lower, "invalid field"),
		strings.Contains(lower, "invalid update"):
		return http.StatusBadRequest
	case strings.Contains(lower, "conflict"),
		strings.Contains(lower, "out of date"),
		strings.Contains(lower, "stale"):
		return http.StatusConflict
	default:
		return http.StatusBadGateway
	}
}
