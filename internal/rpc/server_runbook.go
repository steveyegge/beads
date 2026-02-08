package rpc

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/runbook"
	"github.com/steveyegge/beads/internal/types"
)

// handleRunbookList handles the runbook_list RPC operation.
// Lists all runbook beads stored in the database.
func (s *Server) handleRunbookList(req *Request) Response {
	var args RunbookListArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("invalid runbook_list args: %v", err)}
		}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	// Search for all runbook-type issues
	filter := types.IssueFilter{
		IssueType: func() *types.IssueType { t := types.TypeRunbook; return &t }(),
	}
	issues, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to search runbooks: %v", err)}
	}

	var runbooks []RunbookSummary
	for _, issue := range issues {
		if issue.Status == types.StatusClosed {
			continue
		}

		rb, err := runbook.IssueToRunbook(issue)
		if err != nil {
			continue
		}

		runbooks = append(runbooks, RunbookSummary{
			Name:     rb.Name,
			Format:   rb.Format,
			Source:   "db",
			Jobs:     len(rb.Jobs),
			Commands: len(rb.Commands),
			Workers:  len(rb.Workers),
		})
	}

	result := RunbookListResult{
		Runbooks: runbooks,
		Count:    len(runbooks),
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleRunbookGet handles the runbook_get RPC operation.
// Gets a runbook by ID or name.
func (s *Server) handleRunbookGet(req *Request) Response {
	var args RunbookGetArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid runbook_get args: %v", err)}
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
			return Response{Success: false, Error: fmt.Sprintf("failed to get runbook %s: %v", args.ID, err)}
		}
		if issue == nil {
			return Response{Success: false, Error: fmt.Sprintf("runbook %s not found", args.ID)}
		}
		if issue.IssueType != types.TypeRunbook {
			return Response{Success: false, Error: fmt.Sprintf("issue %s is not a runbook (type: %s)", args.ID, issue.IssueType)}
		}
	} else {
		// Try by slug-based ID first
		idPrefix := ""
		if p, err := store.GetConfig(ctx, "issue_prefix"); err == nil && p != "" {
			idPrefix = p + "-"
		}
		slug := "runbook-" + strings.ToLower(strings.ReplaceAll(args.Name, " ", "-"))
		candidateID := idPrefix + slug
		candidate, err := store.GetIssue(ctx, candidateID)
		if err == nil && candidate != nil && candidate.IssueType == types.TypeRunbook {
			issue = candidate
		}

		// Fall back to title search
		if issue == nil {
			filter := types.IssueFilter{
				IssueType: func() *types.IssueType { t := types.TypeRunbook; return &t }(),
			}
			issues, err := store.SearchIssues(ctx, args.Name, filter)
			if err != nil {
				return Response{Success: false, Error: fmt.Sprintf("failed to search runbooks: %v", err)}
			}
			for _, candidate := range issues {
				if strings.EqualFold(candidate.Title, args.Name) && candidate.Status != types.StatusClosed {
					issue = candidate
					break
				}
			}
		}

		if issue == nil {
			return Response{Success: false, Error: fmt.Sprintf("runbook %q not found", args.Name)}
		}
	}

	result := RunbookGetResult{
		ID:      issue.ID,
		Name:    issue.Title,
		Content: issue.Metadata,
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleRunbookSave handles the runbook_save RPC operation.
// Creates or updates a runbook bead.
func (s *Server) handleRunbookSave(req *Request) Response {
	var args RunbookSaveArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid runbook_save args: %v", err)}
	}

	if len(args.Content) == 0 {
		return Response{Success: false, Error: "runbook content is required"}
	}

	// Parse and validate the runbook content
	var rb runbook.RunbookContent
	if err := json.Unmarshal(args.Content, &rb); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid runbook JSON: %v", err)}
	}
	if rb.Name == "" {
		return Response{Success: false, Error: "runbook name is required"}
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
		prefix, err := store.GetConfig(ctx, "issue_prefix")
		if err == nil && prefix != "" {
			idPrefix = prefix + "-"
		}
	}

	// Convert runbook to issue
	issue, labels, err := runbook.RunbookToIssue(&rb, idPrefix)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to convert runbook to issue: %v", err)}
	}

	if issue.Status == "" {
		issue.Status = types.StatusOpen
	}

	// Check if runbook already exists
	existing, _ := store.GetIssue(ctx, issue.ID)
	created := existing == nil

	if existing != nil {
		if !args.Force {
			return Response{Success: false, Error: fmt.Sprintf("runbook %q already exists as %s (use force to overwrite)", rb.Name, issue.ID)}
		}
		updates := map[string]interface{}{
			"title":       issue.Title,
			"description": issue.Description,
			"metadata":    issue.Metadata,
		}
		if err := store.UpdateIssue(ctx, existing.ID, updates, actor); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("failed to update runbook: %v", err)}
		}
	} else {
		if err := store.CreateIssue(ctx, issue, actor); err != nil {
			return Response{Success: false, Error: fmt.Sprintf("failed to create runbook: %v", err)}
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

	result := RunbookSaveResult{
		ID:      issue.ID,
		Name:    rb.Name,
		Created: created,
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}
