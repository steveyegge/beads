package rpc

import (
	"encoding/json"
	"fmt"

	"github.com/steveyegge/beads/internal/eventbus"
	"github.com/steveyegge/beads/internal/formula"
	"github.com/steveyegge/beads/internal/types"
)

// handleFormulaList handles the formula_list RPC operation (gt-pozvwr.24.9).
// Lists all formula beads stored in the database.
func (s *Server) handleFormulaList(req *Request) Response {
	var args FormulaListArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid formula_list args: %v", err)}
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	// Search for all formula-type issues
	filter := types.IssueFilter{
		IssueType: func() *types.IssueType { t := types.TypeFormula; return &t }(),
	}
	issues, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to search formulas: %v", err)}
	}

	var formulas []FormulaSummary
	for _, issue := range issues {
		// Skip closed/tombstoned formulas
		if issue.Status == types.StatusClosed {
			continue
		}

		// Deserialize to get formula metadata
		f, err := formula.IssueToFormula(issue)
		if err != nil {
			// Skip malformed entries but don't fail the whole list
			continue
		}

		// Apply type filter
		if args.Type != "" && string(f.Type) != args.Type {
			continue
		}

		// Apply phase filter
		if args.Phase != "" && f.Phase != args.Phase {
			continue
		}

		formulas = append(formulas, FormulaSummary{
			ID:          issue.ID,
			Name:        f.Formula,
			Description: f.Description,
			Type:        string(f.Type),
			Phase:       f.Phase,
			Version:     f.Version,
			Source:       f.Source,
		})

		if args.Limit > 0 && len(formulas) >= args.Limit {
			break
		}
	}

	result := FormulaListResult{
		Formulas: formulas,
		Count:    len(formulas),
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleFormulaGet handles the formula_get RPC operation (gt-pozvwr.24.9).
// Gets a formula by ID or name.
func (s *Server) handleFormulaGet(req *Request) Response {
	var args FormulaGetArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid formula_get args: %v", err)}
	}

	if args.ID == "" && args.Name == "" {
		return Response{Success: false, Error: "either id or name is required"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	var issue *types.Issue

	if args.ID != "" {
		// Direct lookup by ID
		var err error
		issue, err = store.GetIssue(ctx, args.ID)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("failed to get formula %s: %v", args.ID, err)}
		}
		if issue == nil {
			return Response{Success: false, Error: fmt.Sprintf("formula %s not found", args.ID)}
		}
		if issue.IssueType != types.TypeFormula {
			return Response{Success: false, Error: fmt.Sprintf("issue %s is not a formula (type: %s)", args.ID, issue.IssueType)}
		}
	} else {
		// Search by name (title match against formula-type issues)
		filter := types.IssueFilter{
			IssueType: func() *types.IssueType { t := types.TypeFormula; return &t }(),
		}
		issues, err := store.SearchIssues(ctx, args.Name, filter)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("failed to search formulas: %v", err)}
		}

		// Find exact title match
		for _, candidate := range issues {
			if candidate.Title == args.Name && candidate.Status != types.StatusClosed {
				issue = candidate
				break
			}
		}
		if issue == nil {
			return Response{Success: false, Error: fmt.Sprintf("formula %q not found", args.Name)}
		}
	}

	// Verify it can be deserialized
	f, err := formula.IssueToFormula(issue)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to deserialize formula: %v", err)}
	}

	result := FormulaGetResult{
		ID:      issue.ID,
		Name:    f.Formula,
		Formula: issue.Metadata,
		Source:  f.Source,
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleFormulaSave handles the formula_save RPC operation (gt-pozvwr.24.9).
// Creates or updates a formula bead.
func (s *Server) handleFormulaSave(req *Request) Response {
	var args FormulaSaveArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid formula_save args: %v", err)}
	}

	if len(args.Formula) == 0 {
		return Response{Success: false, Error: "formula content is required"}
	}

	// Parse and validate the formula
	var f formula.Formula
	if err := json.Unmarshal(args.Formula, &f); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid formula JSON: %v", err)}
	}
	if err := f.Validate(); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("formula validation failed: %v", err)}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	actor := s.reqActor(req)

	// Determine ID prefix
	idPrefix := args.IDPrefix
	if idPrefix == "" {
		// Get the configured prefix from the store
		prefix, err := store.GetConfig(ctx, "issue_prefix")
		if err == nil && prefix != "" {
			idPrefix = prefix + "-"
		}
	}

	// Convert formula to issue
	issue, labels, err := formula.FormulaToIssue(&f, idPrefix)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to convert formula to issue: %v", err)}
	}

	// Ensure status is set (FormulaToIssue doesn't set it)
	if issue.Status == "" {
		issue.Status = types.StatusOpen
	}

	// Check if formula already exists
	existing, _ := store.GetIssue(ctx, issue.ID)
	created := existing == nil

	if existing != nil {
		if !args.Force {
			return Response{Success: false, Error: fmt.Sprintf("formula %q already exists as %s (use force to overwrite)", f.Formula, issue.ID)}
		}
		// Update existing formula (only allowed update fields)
		updates := map[string]interface{}{
			"title":       issue.Title,
			"description": issue.Description,
			"metadata":    issue.Metadata,
		}
		if err := store.UpdateIssue(ctx, existing.ID, updates, actor); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("failed to update formula: %v", err)}
		}
	} else {
		// Create new formula
		if err := store.CreateIssue(ctx, issue, actor); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("failed to create formula: %v", err)}
		}
	}

	// Add labels
	for _, label := range labels {
		_ = store.AddLabel(ctx, issue.ID, label, actor)
	}

	if created {
		s.emitMutationFor(MutationCreate, issue)
	} else {
		s.emitMutationFor(MutationUpdate, issue)
	}

	s.emitConfigEvent(eventbus.EventFormulaSaved, eventbus.ConfigEventPayload{
		Name:    f.Formula,
		IssueID: issue.ID,
		Created: created,
		Actor:   actor,
	})

	result := FormulaSaveResult{
		ID:      issue.ID,
		Name:    f.Formula,
		Created: created,
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleFormulaDelete handles the formula_delete RPC operation (gt-pozvwr.24.9).
// Soft-deletes (tombstones) a formula bead by closing it.
func (s *Server) handleFormulaDelete(req *Request) Response {
	var args FormulaDeleteArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid formula_delete args: %v", err)}
	}

	if args.ID == "" && args.Name == "" {
		return Response{Success: false, Error: "either id or name is required"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	actor := s.reqActor(req)

	var issue *types.Issue

	if args.ID != "" {
		var err error
		issue, err = store.GetIssue(ctx, args.ID)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("failed to get formula %s: %v", args.ID, err)}
		}
		if issue == nil {
			return Response{Success: false, Error: fmt.Sprintf("formula %s not found", args.ID)}
		}
		if issue.IssueType != types.TypeFormula {
			return Response{Success: false, Error: fmt.Sprintf("issue %s is not a formula (type: %s)", args.ID, issue.IssueType)}
		}
	} else {
		// Search by name
		filter := types.IssueFilter{
			IssueType: func() *types.IssueType { t := types.TypeFormula; return &t }(),
		}
		issues, err := store.SearchIssues(ctx, args.Name, filter)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("failed to search formulas: %v", err)}
		}

		for _, candidate := range issues {
			if candidate.Title == args.Name && candidate.Status != types.StatusClosed {
				issue = candidate
				break
			}
		}
		if issue == nil {
			return Response{Success: false, Error: fmt.Sprintf("formula %q not found", args.Name)}
		}
	}

	if issue.Status == types.StatusClosed {
		return Response{Success: false, Error: fmt.Sprintf("formula %s is already deleted", issue.ID)}
	}

	// Soft-delete by closing
	reason := args.Reason
	if reason == "" {
		reason = "formula deleted"
	}
	if err := store.CloseIssue(ctx, issue.ID, reason, actor, ""); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to delete formula: %v", err)}
	}

	s.emitMutationFor(MutationDelete, issue)

	s.emitConfigEvent(eventbus.EventFormulaDeleted, eventbus.ConfigEventPayload{
		Name:    issue.Title,
		IssueID: issue.ID,
		Actor:   actor,
	})

	result := FormulaDeleteResult{
		ID:   issue.ID,
		Name: issue.Title,
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}
