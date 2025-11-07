package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

const bulkUpdatesUnavailableDetails = "Bulk changes require an active Beads daemon connection."

// BulkClient captures the subset of rpc.Client needed for batch updates.
type BulkClient interface {
	Batch(args *rpc.BatchArgs) (*rpc.Response, error)
}

type bulkRequest struct {
	IDs    []string   `json:"ids"`
	Action bulkAction `json:"action"`
}

type bulkAction struct {
	Status   string `json:"status,omitempty"`
	Priority *int   `json:"priority,omitempty"`
}

type bulkResult struct {
	Issue   IssueSummary `json:"issue,omitempty"`
	Success bool         `json:"success"`
	Error   string       `json:"error,omitempty"`
}

// NewBulkHandler returns an HTTP handler that applies batched updates to issues.
func NewBulkHandler(client BulkClient, publisher EventPublisher) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if client == nil {
			WriteServiceUnavailable(w, "bulk updates unavailable", bulkUpdatesUnavailableDetails)
			return
		}

		defer r.Body.Close() // nolint:errcheck

		var req bulkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("decode payload: %v", err), http.StatusBadRequest)
			return
		}

		ids := normalizeIDs(req.IDs)
		if len(ids) == 0 {
			http.Error(w, "ids are required", http.StatusBadRequest)
			return
		}

		action, err := normalizeAction(req.Action)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		ops, err := buildBatchOperations(ids, action)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp, err := client.Batch(&rpc.BatchArgs{Operations: ops})
		if err != nil {
			if isDaemonUnavailable(err) {
				WriteServiceUnavailable(w, "bulk updates unavailable", bulkUpdatesUnavailableDetails)
				return
			}
			status := statusFromResponse(resp, http.StatusBadGateway)
			message := fmt.Sprintf("bulk update failed: %v", err)
			if resp != nil && strings.TrimSpace(resp.Error) != "" {
				message = resp.Error
			}
			http.Error(w, message, status)
			return
		}
		if resp == nil || !resp.Success {
			status := statusFromResponse(resp, http.StatusBadGateway)
			message := "bulk update failed"
			if resp != nil && strings.TrimSpace(resp.Error) != "" {
				message = resp.Error
			}
			http.Error(w, message, status)
			return
		}

		var batch rpc.BatchResponse
		if err := json.Unmarshal(resp.Data, &batch); err != nil {
			http.Error(w, fmt.Sprintf("decode batch response: %v", err), http.StatusBadGateway)
			return
		}

		results := make([]bulkResult, len(ids))
		for i := range results {
			results[i] = bulkResult{Success: false, Error: "operation not executed"}
		}

		for idx, result := range batch.Results {
			if idx >= len(ids) {
				break
			}

			out := bulkResult{
				Success: result.Success,
				Error:   strings.TrimSpace(result.Error),
			}

			if result.Success {
				issue, err := decodeIssueFromResult(result.Data)
				if err != nil {
					out.Success = false
					out.Error = fmt.Sprintf("decode updated issue: %v", err)
				} else {
					summary := IssueToSummary(issue)
					out.Issue = summary
					out.Success = true
					out.Error = ""

					if publisher != nil {
						publisher.Publish(IssueEvent{
							Type:  EventTypeUpdated,
							Issue: summary,
						})
					}
				}
			} else if out.Error == "" {
				out.Error = "operation failed"
			}

			results[idx] = out
			if !result.Success {
				// Subsequent operations are skipped by daemon; stop processing.
				break
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"results": results}); err != nil {
			http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
			return
		}
	})
}

func normalizeIDs(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

type normalizedAction struct {
	Status   string
	Priority *int
}

func normalizeAction(action bulkAction) (normalizedAction, error) {
	status := strings.TrimSpace(action.Status)
	if status != "" {
		status = strings.ToLower(status)
	}

	var priority *int
	if action.Priority != nil {
		value := *action.Priority
		if value < 0 || value > 4 {
			return normalizedAction{}, fmt.Errorf("priority must be between 0 and 4")
		}
		priority = &value
	}

	if status == "" && priority == nil {
		return normalizedAction{}, fmt.Errorf("action must include status or priority")
	}

	return normalizedAction{
		Status:   status,
		Priority: priority,
	}, nil
}

func buildBatchOperations(ids []string, action normalizedAction) ([]rpc.BatchOperation, error) {
	ops := make([]rpc.BatchOperation, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		update := rpc.UpdateArgs{
			ID: id,
		}
		if action.Status != "" {
			status := action.Status
			update.Status = &status
		}
		if action.Priority != nil {
			value := *action.Priority
			update.Priority = &value
		}

		payload, err := json.Marshal(update)
		if err != nil {
			return nil, fmt.Errorf("encode update operation: %w", err)
		}

		ops = append(ops, rpc.BatchOperation{
			Operation: rpc.OpUpdate,
			Args:      payload,
		})
	}

	if len(ops) == 0 {
		return nil, fmt.Errorf("no valid issue ids provided")
	}

	return ops, nil
}

func decodeIssueFromResult(data json.RawMessage) (*types.Issue, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty result payload")
	}

	var issue types.Issue
	if err := json.Unmarshal(data, &issue); err == nil && issue.ID != "" {
		return &issue, nil
	}

	var wrapper struct {
		Issue *types.Issue `json:"issue"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.Issue != nil {
		return wrapper.Issue, nil
	}

	return nil, fmt.Errorf("unexpected issue payload")
}
