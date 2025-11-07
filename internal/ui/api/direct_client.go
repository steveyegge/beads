package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// NewDirectListClient returns a ListClient backed by a storage.Storage implementation.
func NewDirectListClient(store storage.Storage) ListClient {
	return &directListClient{
		store: store,
	}
}

// NewDirectDetailClient returns a DetailClient backed by storage.
func NewDirectDetailClient(store storage.Storage) DetailClient {
	return &directDetailClient{
		store: store,
	}
}

// NewDirectCreateClient returns a CreateClient backed by storage.
func NewDirectCreateClient(store storage.Storage) CreateClient {
	return &directCreateClient{
		store: store,
	}
}

// NewDirectUpdateClient returns an UpdateClient backed by storage.
func NewDirectUpdateClient(store storage.Storage) UpdateClient {
	return &directUpdateClient{
		store: store,
	}
}

// NewDirectDeleteClient returns a DeleteClient backed by storage.
func NewDirectDeleteClient(store storage.Storage) DeleteClient {
	if store == nil {
		return nil
	}
	return &directDeleteClient{
		store: store,
	}
}

// NewDirectBulkClient returns a BulkClient backed by storage.
func NewDirectBulkClient(store storage.Storage) BulkClient {
	return &directBulkClient{
		updater: &directUpdateClient{store: store},
	}
}

// NewDirectLabelClient returns a LabelClient backed by storage.
func NewDirectLabelClient(store storage.Storage) LabelClient {
	return &directLabelClient{
		store: store,
	}
}

type directListClient struct {
	store storage.Storage
}

func (c *directListClient) List(args *rpc.ListArgs) (*rpc.Response, error) {
	filter := convertListArgsToFilter(args)

	issues, err := c.store.SearchIssues(context.Background(), args.Query, filter)
	if err != nil {
		return &rpc.Response{Success: false, Error: err.Error()}, nil
	}

	data, err := json.Marshal(issues)
	if err != nil {
		return &rpc.Response{Success: false, Error: err.Error()}, nil
	}

	return &rpc.Response{
		Success: true,
		Data:    data,
	}, nil
}

type directDetailClient struct {
	store storage.Storage
}

func (c *directDetailClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	if args == nil || strings.TrimSpace(args.ID) == "" {
		return &rpc.Response{Success: false, Error: "invalid issue id"}, nil
	}

	ctx := context.Background()
	issue, err := c.store.GetIssue(ctx, args.ID)
	if err != nil {
		return &rpc.Response{Success: false, Error: err.Error()}, nil
	}
	if issue == nil {
		return &rpc.Response{
			Success:    false,
			Error:      fmt.Sprintf("issue %s not found", args.ID),
			StatusCode: http.StatusNotFound,
		}, nil
	}

	labels, _ := c.store.GetLabels(ctx, issue.ID)
	dependencies, _ := c.store.GetDependencies(ctx, issue.ID)
	dependents, _ := c.store.GetDependents(ctx, issue.ID)
	records, _ := c.store.GetDependencyRecords(ctx, issue.ID)

	payload := struct {
		*types.Issue
		Labels            []string            `json:"labels,omitempty"`
		Dependencies      []*types.Issue      `json:"dependencies,omitempty"`
		Dependents        []*types.Issue      `json:"dependents,omitempty"`
		DependencyRecords []*types.Dependency `json:"dependency_records,omitempty"`
	}{
		Issue:             issue,
		Labels:            labels,
		Dependencies:      dependencies,
		Dependents:        dependents,
		DependencyRecords: records,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return &rpc.Response{Success: false, Error: err.Error()}, nil
	}

	return &rpc.Response{
		Success: true,
		Data:    data,
	}, nil
}

type directUpdateClient struct {
	store storage.Storage
}

type directBulkClient struct {
	updater *directUpdateClient
}

func (c *directUpdateClient) Update(args *rpc.UpdateArgs) (*rpc.Response, error) {
	if args == nil || strings.TrimSpace(args.ID) == "" {
		return &rpc.Response{Success: false, Error: "invalid issue id"}, nil
	}

	updates := updatesFromRPCArgs(*args)
	ctx := context.Background()

	if len(updates) > 0 {
		if err := c.store.UpdateIssue(ctx, args.ID, updates, "ui"); err != nil {
			return &rpc.Response{Success: false, Error: err.Error()}, nil
		}
	}

	issue, err := c.store.GetIssue(ctx, args.ID)
	if err != nil {
		return &rpc.Response{Success: false, Error: err.Error()}, nil
	}
	if issue == nil {
		return &rpc.Response{Success: false, Error: "issue not found"}, nil
	}

	data, err := json.Marshal(issue)
	if err != nil {
		return nil, err
	}

	return &rpc.Response{
		Success: true,
		Data:    data,
	}, nil
}

type directLabelClient struct {
	store storage.Storage
}

type directCreateClient struct {
	store storage.Storage
}

func (c *directCreateClient) Create(args *rpc.CreateArgs) (*rpc.Response, error) {
	if args == nil {
		return &rpc.Response{Success: false, Error: "invalid create args"}, nil
	}

	title := strings.TrimSpace(args.Title)
	if title == "" {
		return &rpc.Response{Success: false, Error: "title is required"}, nil
	}

	issueType := strings.TrimSpace(args.IssueType)
	if issueType == "" {
		issueType = string(types.TypeTask)
	}

	labels := normalizeLabels(args.Labels)

	now := time.Now().UTC()

	issue := &types.Issue{
		ID:          strings.TrimSpace(args.ID),
		Title:       title,
		Description: strings.TrimSpace(args.Description),
		IssueType:   types.IssueType(issueType),
		Priority:    args.Priority,
		Status:      types.StatusOpen,
		Labels:      labels,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	ctx := context.Background()
	if err := c.store.CreateIssue(ctx, issue, "ui"); err != nil {
		return &rpc.Response{Success: false, Error: err.Error()}, nil
	}

	for _, dep := range args.Dependencies {
		target := strings.TrimSpace(dep)
		if target == "" {
			continue
		}
		dependency := &types.Dependency{
			IssueID:     issue.ID,
			DependsOnID: target,
			Type:        types.DepDiscoveredFrom,
		}
		if err := c.store.AddDependency(ctx, dependency, "ui"); err != nil {
			return &rpc.Response{Success: false, Error: err.Error()}, nil
		}
	}

	created, err := c.store.GetIssue(ctx, issue.ID)
	if err != nil {
		return &rpc.Response{Success: false, Error: err.Error()}, nil
	}
	if created == nil {
		created = issue
	}

	data, err := json.Marshal(created)
	if err != nil {
		return nil, err
	}

	return &rpc.Response{
		Success: true,
		Data:    data,
	}, nil
}

func (c *directLabelClient) AddLabel(args *rpc.LabelAddArgs) (*rpc.Response, error) {
	if args == nil || strings.TrimSpace(args.ID) == "" {
		return &rpc.Response{Success: false, Error: "invalid issue id"}, nil
	}

	label := strings.TrimSpace(args.Label)
	if label == "" {
		return &rpc.Response{Success: false, Error: "label is required"}, nil
	}

	if err := c.store.AddLabel(context.Background(), args.ID, label, "ui"); err != nil {
		return &rpc.Response{Success: false, Error: err.Error()}, nil
	}
	return &rpc.Response{Success: true}, nil
}

func (c *directLabelClient) RemoveLabel(args *rpc.LabelRemoveArgs) (*rpc.Response, error) {
	if args == nil || strings.TrimSpace(args.ID) == "" {
		return &rpc.Response{Success: false, Error: "invalid issue id"}, nil
	}

	label := strings.TrimSpace(args.Label)
	if label == "" {
		return &rpc.Response{Success: false, Error: "label is required"}, nil
	}

	if err := c.store.RemoveLabel(context.Background(), args.ID, label, "ui"); err != nil {
		return &rpc.Response{Success: false, Error: err.Error()}, nil
	}
	return &rpc.Response{Success: true}, nil
}

func updatesFromRPCArgs(a rpc.UpdateArgs) map[string]interface{} {
	u := map[string]interface{}{}
	if a.Title != nil {
		u["title"] = *a.Title
	}
	if a.Description != nil {
		u["description"] = *a.Description
	}
	if a.Status != nil {
		u["status"] = *a.Status
	}
	if a.Priority != nil {
		u["priority"] = *a.Priority
	}
	if a.Design != nil {
		u["design"] = *a.Design
	}
	if a.AcceptanceCriteria != nil {
		u["acceptance_criteria"] = *a.AcceptanceCriteria
	}
	if a.Notes != nil {
		u["notes"] = *a.Notes
	}
	if a.Assignee != nil {
		u["assignee"] = *a.Assignee
	}
	return u
}

func convertListArgsToFilter(args *rpc.ListArgs) types.IssueFilter {
	filter := types.IssueFilter{
		Limit:     args.Limit,
		Labels:    args.Labels,
		LabelsAny: args.LabelsAny,
		IDs:       args.IDs,
		IDPrefix:  strings.TrimSpace(args.IDPrefix),
	}

	if args.Priority != nil {
		filter.Priority = args.Priority
	}

	if args.Status != "" {
		status := types.Status(args.Status)
		filter.Status = &status
	}

	if args.IssueType != "" {
		issueType := types.IssueType(args.IssueType)
		filter.IssueType = &issueType
	}

	if args.Assignee != "" {
		filter.Assignee = &args.Assignee
	}

	if trimmed := strings.TrimSpace(args.Order); trimmed != "" {
		switch strings.ToLower(trimmed) {
		case closedQueueOrder, legacyClosedQueueOrder:
			filter.OrderClosed = true
		default:
			if options := types.ParseIssueSortOrder(trimmed); len(options) > 0 {
				filter.Sort = options
			}
		}
	}

	closedBefore := strings.TrimSpace(args.ClosedBefore)
	if closedBefore != "" {
		if ts, err := time.Parse(time.RFC3339Nano, closedBefore); err == nil {
			filter.ClosedBefore = &ts
			filter.ClosedBeforeID = strings.TrimSpace(args.ClosedBeforeID)
		}
	} else if cursor := strings.TrimSpace(args.Cursor); cursor != "" {
		if ts, id, err := parseClosedCursor(cursor); err == nil {
			filter.ClosedBefore = &ts
			filter.ClosedBeforeID = id
		}
	}

	return filter
}

func (c *directBulkClient) Batch(args *rpc.BatchArgs) (*rpc.Response, error) {
	results := make([]rpc.BatchResult, 0, len(args.Operations))

	if args == nil || len(args.Operations) == 0 {
		data, err := json.Marshal(rpc.BatchResponse{Results: results})
		if err != nil {
			return nil, err
		}
		return &rpc.Response{Success: true, Data: data}, nil
	}

	for _, op := range args.Operations {
		if strings.TrimSpace(op.Operation) == "" {
			results = append(results, rpc.BatchResult{
				Success: false,
				Error:   "missing operation",
			})
			break
		}
		if op.Operation != rpc.OpUpdate {
			results = append(results, rpc.BatchResult{
				Success: false,
				Error:   fmt.Sprintf("unsupported operation %q", op.Operation),
			})
			break
		}

		var update rpc.UpdateArgs
		if err := json.Unmarshal(op.Args, &update); err != nil {
			results = append(results, rpc.BatchResult{
				Success: false,
				Error:   fmt.Sprintf("decode update args: %v", err),
			})
			break
		}

		resp, err := c.updater.Update(&update)
		if err != nil {
			results = append(results, rpc.BatchResult{
				Success: false,
				Error:   err.Error(),
			})
			break
		}
		if resp == nil {
			results = append(results, rpc.BatchResult{
				Success: false,
				Error:   "empty update response",
			})
			break
		}

		results = append(results, rpc.BatchResult{
			Success: resp.Success,
			Data:    resp.Data,
			Error:   resp.Error,
		})

		if !resp.Success {
			break
		}
	}

	data, err := json.Marshal(rpc.BatchResponse{Results: results})
	if err != nil {
		return nil, err
	}
	return &rpc.Response{
		Success: true,
		Data:    data,
	}, nil
}

type directDeleteClient struct {
	store storage.Storage
}

func (c *directDeleteClient) DeleteIssue(ctx context.Context, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("issue id is required")
	}
	return c.store.DeleteIssue(ctx, trimmed)
}
