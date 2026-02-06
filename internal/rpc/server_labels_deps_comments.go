package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// lookupIssueMeta fetches title and assignee for mutation events.
// Returns empty strings on error (acceptable for non-critical mutation metadata).
func (s *Server) lookupIssueMeta(ctx context.Context, issueID string) (title, assignee string) {
	if s.storage == nil {
		return "", ""
	}
	issue, err := s.storage.GetIssue(ctx, issueID)
	if err != nil || issue == nil {
		return "", ""
	}
	return issue.Title, issue.Assignee
}

// isChildOf returns true if childID is a hierarchical child of parentID.
// For example, "bd-abc.1" is a child of "bd-abc", and "bd-abc.1.2" is a child of "bd-abc.1".
func isChildOf(childID, parentID string) bool {
	_, actualParentID, depth := types.ParseHierarchicalID(childID)
	if depth == 0 {
		return false // Not a hierarchical ID
	}
	if actualParentID == parentID {
		return true
	}
	return strings.HasPrefix(childID, parentID+".")
}

func (s *Server) handleDepAdd(req *Request) Response {
	var depArgs DepAddArgs
	if err := json.Unmarshal(req.Args, &depArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid dep add args: %v", err),
		}
	}

	// Check for child->parent dependency anti-pattern
	// This creates a deadlock: child can't start (parent open), parent can't close (children not done)
	if isChildOf(depArgs.FromID, depArgs.ToID) {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("cannot add dependency: %s is already a child of %s (children inherit dependency via hierarchy)", depArgs.FromID, depArgs.ToID),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	dep := &types.Dependency{
		IssueID:     depArgs.FromID,
		DependsOnID: depArgs.ToID,
		Type:        types.DependencyType(depArgs.DepType),
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	if err := store.AddDependency(ctx, dep, s.reqActor(req)); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to add dependency: %v", err),
		}
	}

	// Emit mutation event for event-driven daemon
	title, assignee := s.lookupIssueMeta(ctx, depArgs.FromID)
	s.emitMutation(MutationUpdate, depArgs.FromID, title, assignee)

	result := map[string]interface{}{
		"status":        "added",
		"issue_id":      depArgs.FromID,
		"depends_on_id": depArgs.ToID,
		"type":          depArgs.DepType,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// Generic handler for simple store operations with standard error handling
func (s *Server) handleSimpleStoreOp(req *Request, argsPtr interface{}, argDesc string,
	opFunc func(context.Context, storage.Storage, string) error, issueID string,
	responseData func() map[string]interface{}) Response {
	if err := json.Unmarshal(req.Args, argsPtr); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid %s args: %v", argDesc, err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	if err := opFunc(ctx, store, s.reqActor(req)); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to %s: %v", argDesc, err),
		}
	}

	// Emit mutation event for event-driven daemon
	title, assignee := s.lookupIssueMeta(ctx, issueID)
	s.emitMutation(MutationUpdate, issueID, title, assignee)

	if responseData != nil {
		data, _ := json.Marshal(responseData())
		return Response{Success: true, Data: data}
	}
	return Response{Success: true}
}

func (s *Server) handleDepRemove(req *Request) Response {
	var depArgs DepRemoveArgs
	return s.handleSimpleStoreOp(req, &depArgs, "dep remove",
		func(ctx context.Context, store storage.Storage, actor string) error {
			return store.RemoveDependency(ctx, depArgs.FromID, depArgs.ToID, actor)
		},
		depArgs.FromID,
		func() map[string]interface{} {
			return map[string]interface{}{
				"status":        "removed",
				"issue_id":      depArgs.FromID,
				"depends_on_id": depArgs.ToID,
			}
		},
	)
}

// handleDepAddBidirectional adds a bidirectional relation atomically in a single transaction.
// This is used for relates_to links where both directions must be added together.
func (s *Server) handleDepAddBidirectional(req *Request) Response {
	var args DepAddBidirectionalArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid bidirectional dep add args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	actor := s.reqActor(req)

	// Check for self-reference
	if args.ID1 == args.ID2 {
		return Response{
			Success: false,
			Error:   "cannot add bidirectional relation: id1 and id2 must be different",
		}
	}

	// Add both directions atomically in a transaction
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Add id1 -> id2
		dep1 := &types.Dependency{
			IssueID:     args.ID1,
			DependsOnID: args.ID2,
			Type:        types.DependencyType(args.DepType),
		}
		if err := tx.AddDependency(ctx, dep1, actor); err != nil {
			return fmt.Errorf("adding %s -> %s: %w", args.ID1, args.ID2, err)
		}

		// Add id2 -> id1
		dep2 := &types.Dependency{
			IssueID:     args.ID2,
			DependsOnID: args.ID1,
			Type:        types.DependencyType(args.DepType),
		}
		if err := tx.AddDependency(ctx, dep2, actor); err != nil {
			return fmt.Errorf("adding %s -> %s: %w", args.ID2, args.ID1, err)
		}

		return nil
	})

	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to add bidirectional relation: %v", err),
		}
	}

	// Emit mutation events for both issues
	title1, assignee1 := s.lookupIssueMeta(ctx, args.ID1)
	title2, assignee2 := s.lookupIssueMeta(ctx, args.ID2)
	s.emitMutation(MutationUpdate, args.ID1, title1, assignee1)
	s.emitMutation(MutationUpdate, args.ID2, title2, assignee2)

	result := map[string]interface{}{
		"status":  "added",
		"id1":     args.ID1,
		"id2":     args.ID2,
		"type":    args.DepType,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleDepRemoveBidirectional removes a bidirectional relation atomically in a single transaction.
// This is used for relates_to links where both directions must be removed together.
func (s *Server) handleDepRemoveBidirectional(req *Request) Response {
	var args DepRemoveBidirectionalArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid bidirectional dep remove args: %v", err),
		}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	actor := s.reqActor(req)

	// Remove both directions atomically in a transaction
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Remove id1 -> id2
		if err := tx.RemoveDependency(ctx, args.ID1, args.ID2, actor); err != nil {
			return fmt.Errorf("removing %s -> %s: %w", args.ID1, args.ID2, err)
		}

		// Remove id2 -> id1
		if err := tx.RemoveDependency(ctx, args.ID2, args.ID1, actor); err != nil {
			return fmt.Errorf("removing %s -> %s: %w", args.ID2, args.ID1, err)
		}

		return nil
	})

	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to remove bidirectional relation: %v", err),
		}
	}

	// Emit mutation events for both issues
	title1, assignee1 := s.lookupIssueMeta(ctx, args.ID1)
	title2, assignee2 := s.lookupIssueMeta(ctx, args.ID2)
	s.emitMutation(MutationUpdate, args.ID1, title1, assignee1)
	s.emitMutation(MutationUpdate, args.ID2, title2, assignee2)

	result := map[string]interface{}{
		"status": "removed",
		"id1":    args.ID1,
		"id2":    args.ID2,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

func (s *Server) handleLabelAdd(req *Request) Response {
	var labelArgs LabelAddArgs
	resp := s.handleSimpleStoreOp(req, &labelArgs, "label add", func(ctx context.Context, store storage.Storage, actor string) error {
		return store.AddLabel(ctx, labelArgs.ID, labelArgs.Label, actor)
	}, labelArgs.ID, nil)
	if resp.Success && s.labelCache != nil {
		s.labelCache.AddLabel(labelArgs.ID, labelArgs.Label)
	}
	return resp
}

func (s *Server) handleLabelRemove(req *Request) Response {
	var labelArgs LabelRemoveArgs
	resp := s.handleSimpleStoreOp(req, &labelArgs, "label remove", func(ctx context.Context, store storage.Storage, actor string) error {
		return store.RemoveLabel(ctx, labelArgs.ID, labelArgs.Label, actor)
	}, labelArgs.ID, nil)
	if resp.Success && s.labelCache != nil {
		s.labelCache.RemoveLabel(labelArgs.ID, labelArgs.Label)
	}
	return resp
}

// handleBatchAddLabels adds multiple labels to an issue in a single atomic transaction.
// This is more efficient than making multiple AddLabel calls and ensures atomicity.
func (s *Server) handleBatchAddLabels(req *Request) Response {
	var args BatchAddLabelsArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid batch_add_labels args: %v", err),
		}
	}

	// Validate required fields
	if args.IssueID == "" {
		return Response{Success: false, Error: "issue_id is required"}
	}
	if len(args.Labels) == 0 {
		// Nothing to do, return success with 0 labels added
		result := &BatchAddLabelsResult{
			IssueID:     args.IssueID,
			LabelsAdded: 0,
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	actor := s.reqActor(req)

	// Resolve partial ID to full ID
	fullID, err := s.resolvePartialID(ctx, args.IssueID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("resolving issue ID: %v", err),
		}
	}

	// Get existing labels to check for duplicates
	existingLabels, err := store.GetLabels(ctx, fullID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("getting existing labels: %v", err),
		}
	}

	// Build set of existing labels for fast lookup
	existingSet := make(map[string]bool)
	for _, label := range existingLabels {
		existingSet[label] = true
	}

	// Filter out labels that already exist
	var labelsToAdd []string
	for _, label := range args.Labels {
		if !existingSet[label] {
			labelsToAdd = append(labelsToAdd, label)
		}
	}

	// If all labels already exist, return early
	if len(labelsToAdd) == 0 {
		result := &BatchAddLabelsResult{
			IssueID:     fullID,
			LabelsAdded: 0,
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	// Add all labels in a single transaction
	err = store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		for _, label := range labelsToAdd {
			if err := tx.AddLabel(ctx, fullID, label, actor); err != nil {
				return fmt.Errorf("adding label %q: %w", label, err)
			}
		}
		return nil
	})

	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("batch_add_labels transaction failed: %v", err),
		}
	}

	// Emit mutation event for event-driven daemon
	title, assignee := s.lookupIssueMeta(ctx, fullID)
	s.emitMutation(MutationUpdate, fullID, title, assignee)

	// Invalidate label cache
	if s.labelCache != nil {
		s.labelCache.InvalidateIssue(fullID)
	}

	result := &BatchAddLabelsResult{
		IssueID:     fullID,
		LabelsAdded: len(labelsToAdd),
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

func (s *Server) handleCommentList(req *Request) Response {
	var commentArgs CommentListArgs
	if err := json.Unmarshal(req.Args, &commentArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid comment list args: %v", err),
		}
	}

	store := s.storage

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	comments, err := store.GetIssueComments(ctx, commentArgs.ID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to list comments: %v", err),
		}
	}

	data, _ := json.Marshal(comments)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleCommentAdd(req *Request) Response {
	var commentArgs CommentAddArgs
	if err := json.Unmarshal(req.Args, &commentArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid comment add args: %v", err),
		}
	}

	store := s.storage

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	comment, err := store.AddIssueComment(ctx, commentArgs.ID, commentArgs.Author, commentArgs.Text)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to add comment: %v", err),
		}
	}

	// Emit mutation event for event-driven daemon
	title, assignee := s.lookupIssueMeta(ctx, commentArgs.ID)
	s.emitMutation(MutationComment, commentArgs.ID, title, assignee)

	data, _ := json.Marshal(comment)
	return Response{
		Success: true,
		Data:    data,
	}
}

func (s *Server) handleBatch(req *Request) Response {
	var batchArgs BatchArgs
	if err := json.Unmarshal(req.Args, &batchArgs); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid batch args: %v", err),
		}
	}

	results := make([]BatchResult, 0, len(batchArgs.Operations))

	for _, op := range batchArgs.Operations {
		subReq := &Request{
			Operation:     op.Operation,
			Args:          op.Args,
			Actor:         req.Actor,
			RequestID:     req.RequestID,
			Cwd:           req.Cwd,           // Pass through context
			ClientVersion: req.ClientVersion, // Pass through version for compatibility checks
		}

		resp := s.handleRequest(subReq)

		results = append(results, BatchResult(resp))

		if !resp.Success {
			break
		}
	}

	batchResp := BatchResponse{Results: results}
	data, _ := json.Marshal(batchResp)

	return Response{
		Success: true,
		Data:    data,
	}
}

// handleSetState handles the set_state RPC operation.
// This atomically creates an event bead and updates labels in a single transaction.
func (s *Server) handleSetState(req *Request) Response {
	var args SetStateArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid set_state args: %v", err),
		}
	}

	// Validate required fields
	if args.IssueID == "" {
		return Response{Success: false, Error: "issue_id is required"}
	}
	if args.Dimension == "" {
		return Response{Success: false, Error: "dimension is required"}
	}
	if args.NewValue == "" {
		return Response{Success: false, Error: "new_value is required"}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	actor := s.reqActor(req)

	// Resolve partial ID to full ID
	fullID, err := s.resolvePartialID(ctx, args.IssueID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("resolving issue ID: %v", err),
		}
	}

	// Get current labels to find existing dimension value
	labels, err := store.GetLabels(ctx, fullID)
	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("getting labels: %v", err),
		}
	}

	// Find existing label for this dimension
	prefix := args.Dimension + ":"
	var oldLabel string
	var oldValue *string
	for _, label := range labels {
		if strings.HasPrefix(label, prefix) {
			oldLabel = label
			v := strings.TrimPrefix(label, prefix)
			oldValue = &v
			break
		}
	}

	newLabel := args.Dimension + ":" + args.NewValue

	// Check if no change needed
	if oldLabel == newLabel {
		result := &SetStateResult{
			IssueID:   fullID,
			Dimension: args.Dimension,
			OldValue:  oldValue,
			NewValue:  args.NewValue,
			Changed:   false,
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	// Build event description
	var eventDesc string
	if oldValue != nil {
		eventDesc = fmt.Sprintf("Changed %s from %s to %s", args.Dimension, *oldValue, args.NewValue)
	} else {
		eventDesc = fmt.Sprintf("Set %s to %s", args.Dimension, args.NewValue)
	}
	if args.Reason != "" {
		eventDesc += "\n\nReason: " + args.Reason
	}

	eventTitle := fmt.Sprintf("State change: %s -> %s", args.Dimension, args.NewValue)

	var eventID string

	// Execute all operations in a single transaction
	err = store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// 1. Create event bead recording the state change
		event := &types.Issue{
			Title:       eventTitle,
			Description: eventDesc,
			Status:      types.StatusClosed, // Events are immediately closed
			Priority:    4,                  // Low priority for events
			IssueType:   types.TypeEvent,
			CreatedBy:   actor,
		}
		if err := tx.CreateIssue(ctx, event, actor); err != nil {
			return fmt.Errorf("creating event: %w", err)
		}
		eventID = event.ID

		// 2. Add parent-child dependency to link event to issue
		dep := &types.Dependency{
			IssueID:     eventID,
			DependsOnID: fullID,
			Type:        types.DepParentChild,
		}
		if err := tx.AddDependency(ctx, dep, actor); err != nil {
			return fmt.Errorf("adding parent-child dependency: %w", err)
		}

		// 3. Remove old label if exists
		if oldLabel != "" {
			if err := tx.RemoveLabel(ctx, fullID, oldLabel, actor); err != nil {
				return fmt.Errorf("removing old label: %w", err)
			}
		}

		// 4. Add new label
		if err := tx.AddLabel(ctx, fullID, newLabel, actor); err != nil {
			return fmt.Errorf("adding new label: %w", err)
		}

		return nil
	})

	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("set_state transaction failed: %v", err),
		}
	}

	// Emit mutation event for event-driven daemon
	title, assignee := s.lookupIssueMeta(ctx, fullID)
	s.emitMutation(MutationUpdate, fullID, title, assignee)

	// Invalidate label cache (label was renamed)
	if s.labelCache != nil {
		s.labelCache.InvalidateIssue(fullID)
	}

	result := &SetStateResult{
		IssueID:   fullID,
		Dimension: args.Dimension,
		OldValue:  oldValue,
		NewValue:  args.NewValue,
		EventID:   eventID,
		Changed:   true,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleBatchAddDependencies adds multiple dependencies atomically in a single transaction.
// This is more efficient than making multiple AddDependency calls and ensures atomicity.
func (s *Server) handleBatchAddDependencies(req *Request) Response {
	var args BatchAddDependenciesArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid batch_add_dependencies args: %v", err),
		}
	}

	// Validate input
	if len(args.Dependencies) == 0 {
		// Nothing to do, return success with 0 added
		result := &BatchAddDependenciesResult{
			Added: 0,
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	actor := s.reqActor(req)

	var addedCount int
	var errors []string

	// Add all dependencies in a single transaction
	err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		for _, dep := range args.Dependencies {
			// Check for child->parent dependency anti-pattern
			if isChildOf(dep.FromID, dep.ToID) {
				errors = append(errors, fmt.Sprintf("skipped %s->%s: child cannot depend on parent (hierarchy already implies dependency)", dep.FromID, dep.ToID))
				continue
			}

			dependency := &types.Dependency{
				IssueID:     dep.FromID,
				DependsOnID: dep.ToID,
				Type:        types.DependencyType(dep.Type),
			}

			if err := tx.AddDependency(ctx, dependency, actor); err != nil {
				errors = append(errors, fmt.Sprintf("failed to add %s->%s: %v", dep.FromID, dep.ToID, err))
				continue
			}
			addedCount++
		}
		return nil
	})

	if err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("batch_add_dependencies transaction failed: %v", err),
		}
	}

	// Emit mutation events for all affected issues
	affectedIssues := make(map[string]bool)
	for _, dep := range args.Dependencies {
		affectedIssues[dep.FromID] = true
	}
	for issueID := range affectedIssues {
		title, assignee := s.lookupIssueMeta(ctx, issueID)
		s.emitMutation(MutationUpdate, issueID, title, assignee)
	}

	result := &BatchAddDependenciesResult{
		Added:  addedCount,
		Errors: errors,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleBatchQueryWorkers queries worker assignments for multiple issues at once.
// This is more efficient than making multiple GetIssue calls.
func (s *Server) handleBatchQueryWorkers(req *Request) Response {
	var args BatchQueryWorkersArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid batch_query_workers args: %v", err),
		}
	}

	// Handle empty input
	if len(args.IssueIDs) == 0 {
		result := &BatchQueryWorkersResult{
			Workers: make(map[string]*WorkerInfo),
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	store := s.storage
	if store == nil {
		return Response{
			Success: false,
			Error:   "storage not available (global daemon deprecated - use local daemon instead with 'bd daemon' in your project)",
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()
	workers := make(map[string]*WorkerInfo)

	// Query each issue efficiently
	for _, issueID := range args.IssueIDs {
		// Resolve partial ID to full ID
		fullID, err := s.resolvePartialID(ctx, issueID)
		if err != nil {
			// Issue not found or error - skip with nil entry
			workers[issueID] = nil
			continue
		}

		issue, err := store.GetIssue(ctx, fullID)
		if err != nil || issue == nil {
			workers[issueID] = nil
			continue
		}

		workers[issueID] = &WorkerInfo{
			IssueID:  issue.ID,
			Assignee: issue.Assignee,
			Owner:    issue.Owner,
			Status:   string(issue.Status),
		}
	}

	result := &BatchQueryWorkersResult{
		Workers: workers,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}
