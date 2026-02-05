package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/steveyegge/beads/internal/routing"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/types"
)

// handleRenamePrefix handles the rename-prefix RPC operation (bd-wj80).
func (s *Server) handleRenamePrefix(req *Request) Response {
	var args RenamePrefixArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("invalid rename-prefix args: %v", err),
		}
	}

	if args.NewPrefix == "" {
		return Response{Success: false, Error: "new_prefix is required"}
	}

	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	actor := req.Actor
	if actor == "" {
		actor = "daemon"
	}

	// Get current prefix from config
	oldPrefix, err := store.GetConfig(ctx, "issue_prefix")
	if err != nil || oldPrefix == "" {
		return Response{Success: false, Error: fmt.Sprintf("failed to get current prefix: %v", err)}
	}
	if !strings.HasSuffix(oldPrefix, "-") {
		oldPrefix += "-"
	}

	newPrefix := args.NewPrefix
	if !strings.HasSuffix(newPrefix, "-") {
		newPrefix += "-"
	}

	if oldPrefix == newPrefix {
		return Response{Success: false, Error: "new prefix is the same as current prefix"}
	}

	// Get all issues
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to list issues: %v", err)}
	}

	if args.DryRun {
		count := 0
		for _, issue := range issues {
			if strings.HasPrefix(issue.ID, oldPrefix) {
				count++
			}
		}
		result := RenamePrefixResult{
			OldPrefix:     oldPrefix,
			NewPrefix:     newPrefix,
			IssuesRenamed: count,
			DryRun:        true,
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	// Rename each issue
	renamed := 0
	for _, issue := range issues {
		if !strings.HasPrefix(issue.ID, oldPrefix) {
			continue
		}

		oldID := issue.ID
		newID := newPrefix + strings.TrimPrefix(oldID, oldPrefix)

		// Update text references in all fields
		renamePrefixInFields(issue, oldPrefix, newPrefix)
		issue.ID = newID

		if err := store.UpdateIssueID(ctx, oldID, newID, issue, actor); err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("failed to rename %s -> %s: %v (partial rename: %d issues renamed)", oldID, newID, err, renamed),
			}
		}
		renamed++
	}

	// Update dependency prefixes
	if err := store.RenameDependencyPrefix(ctx, oldPrefix, newPrefix); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to rename dependency prefix: %v (issues renamed: %d)", err, renamed),
		}
	}

	// Update counter prefix
	if err := store.RenameCounterPrefix(ctx, oldPrefix, newPrefix); err != nil {
		// Non-fatal for hash-based IDs
		_ = err
	}

	// Update config
	configPrefix := strings.TrimSuffix(newPrefix, "-")
	if err := store.SetConfig(ctx, "issue_prefix", configPrefix); err != nil {
		return Response{
			Success: false,
			Error:   fmt.Sprintf("failed to update prefix config: %v", err),
		}
	}

	result := RenamePrefixResult{
		OldPrefix:     oldPrefix,
		NewPrefix:     newPrefix,
		IssuesRenamed: renamed,
		DryRun:        false,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// renamePrefixInFields updates text references to the old prefix in all issue fields.
func renamePrefixInFields(issue *types.Issue, oldPrefix, newPrefix string) {
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(oldPrefix))
	issue.Title = pattern.ReplaceAllString(issue.Title, newPrefix)
	issue.Description = pattern.ReplaceAllString(issue.Description, newPrefix)
	issue.Design = pattern.ReplaceAllString(issue.Design, newPrefix)
	issue.AcceptanceCriteria = pattern.ReplaceAllString(issue.AcceptanceCriteria, newPrefix)
	issue.Notes = pattern.ReplaceAllString(issue.Notes, newPrefix)
}

// handleMove handles the move RPC operation (bd-wj80).
func (s *Server) handleMove(req *Request) Response {
	var args MoveArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid move args: %v", err)}
	}

	if args.IssueID == "" {
		return Response{Success: false, Error: "issue_id is required"}
	}
	if args.TargetRig == "" {
		return Response{Success: false, Error: "target_rig is required"}
	}

	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	actor := req.Actor
	if actor == "" {
		actor = "daemon"
	}

	// Get source issue
	sourceIssue, err := store.GetIssue(ctx, args.IssueID)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to get issue %s: %v", args.IssueID, err)}
	}
	if sourceIssue == nil {
		return Response{Success: false, Error: fmt.Sprintf("issue %s not found", args.IssueID)}
	}

	// Resolve target rig
	targetBeadsDir, targetPrefix, err := resolveTargetRig(req, args.TargetRig)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to resolve target rig: %v", err)}
	}

	// Check not moving to same rig
	sourcePrefix := routing.ExtractPrefix(args.IssueID)
	if sourcePrefix == targetPrefix {
		return Response{Success: false, Error: fmt.Sprintf("issue %s is already in rig %q", args.IssueID, args.TargetRig)}
	}

	// Open target storage
	targetStore, err := factory.NewFromConfig(ctx, targetBeadsDir)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to open target rig database: %v", err)}
	}
	defer targetStore.Close()

	// Create new issue in target
	newIssue := copyIssueForMove(sourceIssue, args.IssueID, actor)
	if err := targetStore.CreateIssue(ctx, newIssue, actor); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to create issue in target rig: %v", err)}
	}

	// Copy labels
	copyLabels(ctx, store, targetStore, args.IssueID, newIssue.ID, actor)

	// Remap dependencies
	depsRemapped := 0
	if !args.SkipDeps {
		depsRemapped = remapMoveDependencies(ctx, store, args.IssueID, newIssue.ID, args.TargetRig, actor)
	}

	// Close source
	closed := false
	if !args.KeepOpen {
		closeReason := fmt.Sprintf("Moved to %s", newIssue.ID)
		if err := store.CloseIssue(ctx, args.IssueID, closeReason, actor, ""); err == nil {
			closed = true
		}
	}

	s.emitMutation(MutationUpdate, args.IssueID, sourceIssue.Title, sourceIssue.Assignee)

	result := MoveResult{
		SourceID:     args.IssueID,
		TargetID:     newIssue.ID,
		Closed:       closed,
		DepsRemapped: depsRemapped,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleRefile handles the refile RPC operation (bd-wj80).
func (s *Server) handleRefile(req *Request) Response {
	var args RefileArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid refile args: %v", err)}
	}

	if args.IssueID == "" {
		return Response{Success: false, Error: "issue_id is required"}
	}
	if args.TargetRig == "" {
		return Response{Success: false, Error: "target_rig is required"}
	}

	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	actor := req.Actor
	if actor == "" {
		actor = "daemon"
	}

	// Get source issue
	sourceIssue, err := store.GetIssue(ctx, args.IssueID)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to get issue %s: %v", args.IssueID, err)}
	}
	if sourceIssue == nil {
		return Response{Success: false, Error: fmt.Sprintf("issue %s not found", args.IssueID)}
	}

	// Resolve target rig
	targetBeadsDir, targetPrefix, err := resolveTargetRig(req, args.TargetRig)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to resolve target rig: %v", err)}
	}

	sourcePrefix := routing.ExtractPrefix(args.IssueID)
	if sourcePrefix == targetPrefix {
		return Response{Success: false, Error: fmt.Sprintf("issue %s is already in rig %q", args.IssueID, args.TargetRig)}
	}

	// Open target storage
	targetStore, err := factory.NewFromConfig(ctx, targetBeadsDir)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to open target rig database: %v", err)}
	}
	defer targetStore.Close()

	// Create new issue in target (simpler than move — no dependency remapping)
	newIssue := &types.Issue{
		Title:              sourceIssue.Title,
		Description:        sourceIssue.Description + fmt.Sprintf("\n\n(Refiled from %s)", args.IssueID),
		Design:             sourceIssue.Design,
		AcceptanceCriteria: sourceIssue.AcceptanceCriteria,
		Notes:              sourceIssue.Notes,
		Status:             types.StatusOpen,
		Priority:           sourceIssue.Priority,
		IssueType:          sourceIssue.IssueType,
		Assignee:           sourceIssue.Assignee,
		ExternalRef:        sourceIssue.ExternalRef,
		EstimatedMinutes:   sourceIssue.EstimatedMinutes,
		CreatedBy:          actor,
	}
	if err := targetStore.CreateIssue(ctx, newIssue, actor); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to create issue in target rig: %v", err)}
	}

	// Copy labels
	copyLabels(ctx, store, targetStore, args.IssueID, newIssue.ID, actor)

	// Close source
	closed := false
	if !args.KeepOpen {
		closeReason := fmt.Sprintf("Refiled to %s", newIssue.ID)
		if err := store.CloseIssue(ctx, args.IssueID, closeReason, actor, ""); err == nil {
			closed = true
		}
	}

	s.emitMutation(MutationUpdate, args.IssueID, sourceIssue.Title, sourceIssue.Assignee)

	result := RefileResult{
		SourceID: args.IssueID,
		TargetID: newIssue.ID,
		Closed:   closed,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleCook handles the cook RPC operation (bd-wj80).
// Cook --persist requires the daemon to have local filesystem access to formula files.
func (s *Server) handleCook(req *Request) Response {
	var args CookArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid cook args: %v", err)}
	}

	if args.FormulaName == "" {
		return Response{Success: false, Error: "formula_name is required"}
	}

	// Cook --persist requires local daemon with filesystem access
	// Remote daemons don't have access to formula files
	return Response{
		Success: false,
		Error:   "cook is not yet supported via daemon RPC; use direct mode (unset BD_DAEMON_HOST)",
	}
}

// handlePour handles the pour RPC operation (bd-wj80).
// Pour requires formula resolution and database writes.
func (s *Server) handlePour(req *Request) Response {
	var args PourArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid pour args: %v", err)}
	}

	if args.ProtoID == "" {
		return Response{Success: false, Error: "proto_id is required"}
	}

	// Pour requires formula resolution from the filesystem + complex subgraph cloning
	// that is deeply embedded in cmd/bd/. Not yet available server-side.
	return Response{
		Success: false,
		Error:   "pour is not yet supported via daemon RPC; use direct mode (unset BD_DAEMON_HOST)",
	}
}

// --- helpers ---

// resolveTargetRig resolves a target rig name/prefix to a beads directory and prefix.
func resolveTargetRig(req *Request, targetRig string) (string, string, error) {
	// Use the request's cwd to find the town beads directory
	cwd := req.Cwd
	if cwd == "" {
		cwd = "."
	}
	townBeadsDir, err := findTownBeadsDir(cwd)
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve target rig: %v", err)
	}
	return routing.ResolveBeadsDirForRig(targetRig, townBeadsDir)
}

// findTownBeadsDir walks up from startDir looking for a .beads directory with routes.jsonl.
func findTownBeadsDir(startDir string) (string, error) {
	dir := startDir
	for {
		beadsDir := filepath.Join(dir, ".beads")
		routesFile := filepath.Join(beadsDir, routing.RoutesFileName)
		if _, err := os.Stat(routesFile); err == nil {
			return beadsDir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no town .beads directory with %s found above %s", routing.RoutesFileName, startDir)
}

// copyIssueForMove creates a new issue struct for a move operation.
func copyIssueForMove(source *types.Issue, sourceID, actor string) *types.Issue {
	return &types.Issue{
		Title:              source.Title,
		Description:        source.Description + fmt.Sprintf("\n\n(Moved from %s)", sourceID),
		Design:             source.Design,
		AcceptanceCriteria: source.AcceptanceCriteria,
		Notes:              source.Notes,
		Status:             types.StatusOpen,
		Priority:           source.Priority,
		IssueType:          source.IssueType,
		Assignee:           source.Assignee,
		ExternalRef:        source.ExternalRef,
		EstimatedMinutes:   source.EstimatedMinutes,
		SourceRepo:         source.SourceRepo,
		Ephemeral:          source.Ephemeral,
		MolType:            source.MolType,
		RoleType:           source.RoleType,
		Rig:                source.Rig,
		DueAt:              source.DueAt,
		DeferUntil:         source.DeferUntil,
		CreatedBy:          actor,
	}
}

// copyLabels copies labels from source to target issue across stores.
func copyLabels(ctx context.Context, sourceStore, targetStore storage.Storage, sourceID, targetID, actor string) {
	labels, err := sourceStore.GetLabels(ctx, sourceID)
	if err != nil || len(labels) == 0 {
		return
	}
	for _, label := range labels {
		_ = targetStore.AddLabel(ctx, targetID, label, actor)
	}
}

// remapMoveDependencies remaps dependencies for a moved issue.
// Returns the number of dependencies remapped.
func remapMoveDependencies(ctx context.Context, store storage.Storage, oldID, newID, targetRig, actor string) int {
	remapped := 0

	// Get issues that depend on the moved issue
	dependents, err := store.GetDependents(ctx, oldID)
	if err != nil {
		return 0
	}

	for _, dep := range dependents {
		// Remove old dependency
		if err := store.RemoveDependency(ctx, dep.ID, oldID, actor); err != nil {
			continue
		}
		// Add external ref dependency to new location
		externalRef := fmt.Sprintf("external:%s:%s", targetRig, newID)
		newDep := &types.Dependency{
			IssueID:     dep.ID,
			DependsOnID: externalRef,
			Type:        "blocks",
		}
		if err := store.AddDependency(ctx, newDep, actor); err != nil {
			continue
		}
		remapped++
	}

	return remapped
}
