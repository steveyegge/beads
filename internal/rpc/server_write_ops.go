package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/steveyegge/beads/internal/formula"
	"github.com/steveyegge/beads/internal/routing"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/storage/sqlite"
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
	targetBeadsDir, targetPrefix, err := s.resolveTargetRig(req, args.TargetRig)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to resolve target rig: %v", err)}
	}

	// Check not moving to same rig
	sourcePrefix := routing.ExtractPrefix(args.IssueID)
	if sourcePrefix == targetPrefix {
		return Response{Success: false, Error: fmt.Sprintf("issue %s is already in rig %q", args.IssueID, args.TargetRig)}
	}

	// Open target storage (in single-DB mode, reuse server's own storage)
	var targetStore storage.Storage
	if s.isSingleDBMode() {
		targetStore = store // same database, different prefix
	} else {
		targetStore, err = factory.NewFromConfig(ctx, targetBeadsDir)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("failed to open target rig database: %v", err)}
		}
		defer targetStore.Close()
	}

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

	s.emitMutationFor(MutationUpdate, sourceIssue)

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
	targetBeadsDir, targetPrefix, err := s.resolveTargetRig(req, args.TargetRig)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("failed to resolve target rig: %v", err)}
	}

	sourcePrefix := routing.ExtractPrefix(args.IssueID)
	if sourcePrefix == targetPrefix {
		return Response{Success: false, Error: fmt.Sprintf("issue %s is already in rig %q", args.IssueID, args.TargetRig)}
	}

	// Open target storage (in single-DB mode, reuse server's own storage)
	var targetStore storage.Storage
	if s.isSingleDBMode() {
		targetStore = store // same database, different prefix
	} else {
		targetStore, err = factory.NewFromConfig(ctx, targetBeadsDir)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("failed to open target rig database: %v", err)}
		}
		defer targetStore.Close()
	}

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

	s.emitMutationFor(MutationUpdate, sourceIssue)

	result := RefileResult{
		SourceID: args.IssueID,
		TargetID: newIssue.ID,
		Closed:   closed,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleCook handles the cook RPC operation (gt-pozvwr.24.6).
// Loads formulas from DB (via Parser with storage backend), applies the full
// transformation pipeline, and returns a TemplateSubgraph or persists to DB.
func (s *Server) handleCook(req *Request) Response {
	var args CookArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid cook args: %v", err)}
	}

	if args.FormulaName == "" {
		return Response{Success: false, Error: "formula_name is required"}
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

	// Load and resolve formula using DB-backed parser (checks DB first, then filesystem)
	resolved, err := formula.LoadAndResolveWithStorage(args.FormulaName, nil, store)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("loading formula %q: %v", args.FormulaName, err)}
	}

	// Apply prefix to proto ID if specified
	protoID := resolved.Formula
	if args.Prefix != "" {
		protoID = args.Prefix + resolved.Formula
	}

	// Extract variables and bond points
	vars := formula.ExtractVariables(resolved)
	var bondPoints []string
	if resolved.Compose != nil {
		for _, bp := range resolved.Compose.BondPoints {
			bondPoints = append(bondPoints, bp.ID)
		}
	}

	// Dry-run mode: return preview info
	if args.DryRun {
		result := CookResult{
			ProtoID:    protoID,
			Variables:  vars,
			BondPoints: bondPoints,
			DryRun:     true,
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	// Determine runtime mode
	runtimeMode := args.Mode == "runtime" || len(args.Vars) > 0

	// Apply variable substitutions for runtime mode
	if runtimeMode {
		// Apply defaults from formula variable definitions
		for name, def := range resolved.Vars {
			if _, provided := args.Vars[name]; !provided && def.Default != "" {
				if args.Vars == nil {
					args.Vars = make(map[string]string)
				}
				args.Vars[name] = def.Default
			}
		}

		// Check for missing required variables
		var missingVars []string
		for name, def := range resolved.Vars {
			if _, ok := args.Vars[name]; !ok && def.Default == "" {
				missingVars = append(missingVars, name)
			}
		}
		if len(missingVars) > 0 {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("runtime mode requires all defined variables to have values; missing: %s", strings.Join(missingVars, ", ")),
			}
		}

		// Substitute variables in the formula
		formula.SubstituteFormulaVars(resolved, args.Vars)
	}

	// Persist mode: cook and store proto in database
	if args.Persist {
		return s.handleCookPersist(ctx, resolved, protoID, args.Force, vars, bondPoints, actor)
	}

	// Ephemeral mode (default): return TemplateSubgraph as JSON
	subgraph, err := formula.CookToSubgraphWithVars(resolved, protoID, resolved.Vars)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("cooking formula: %v", err)}
	}

	subgraphJSON, err := json.Marshal(subgraph)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("serializing subgraph: %v", err)}
	}

	result := CookResult{
		ProtoID:    protoID,
		Created:    len(subgraph.Issues),
		Variables:  vars,
		BondPoints: bondPoints,
		Subgraph:   subgraphJSON,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleCookPersist creates a proto bead from a resolved formula and stores it in the DB.
func (s *Server) handleCookPersist(ctx context.Context, resolved *formula.Formula, protoID string, force bool, vars, bondPoints []string, actor string) Response {
	store := s.storage

	// Check if proto already exists
	existingProto, err := store.GetIssue(ctx, protoID)
	if err == nil && existingProto != nil {
		if !force {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("proto %s already exists (use force to replace)", protoID),
			}
		}
		// Delete existing proto and its children
		if err := deleteProtoSubgraphFromDB(ctx, store, protoID); err != nil {
			return Response{
				Success: false,
				Error:   fmt.Sprintf("deleting existing proto: %v", err),
			}
		}
	}

	// Build the subgraph in memory
	subgraph, err := formula.CookToSubgraphWithVars(resolved, protoID, resolved.Vars)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("cooking formula: %v", err)}
	}

	// Extract labels from issues (DB stores labels separately)
	type labelEntry struct {
		issueID string
		label   string
	}
	var labels []labelEntry
	labels = append(labels, labelEntry{protoID, MoleculeLabel})
	for _, issue := range subgraph.Issues {
		for _, label := range issue.Labels {
			labels = append(labels, labelEntry{issue.ID, label})
		}
		issue.Labels = nil // DB stores labels separately
	}

	// Create all issues using batch with skip prefix validation (mol-* IDs
	// don't match the configured prefix).
	sqliteStore, ok := store.(*sqlite.SQLiteStorage)
	if !ok {
		return Response{Success: false, Error: "cook --persist requires SQLite storage"}
	}
	opts := sqlite.BatchCreateOptions{SkipPrefixValidation: true}
	if err := sqliteStore.CreateIssuesWithFullOptions(ctx, subgraph.Issues, actor, opts); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("creating issues: %v", err)}
	}

	// Add labels and dependencies in a transaction
	err = store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		for _, l := range labels {
			if err := tx.AddLabel(ctx, l.issueID, l.label, actor); err != nil {
				return fmt.Errorf("adding label %s to %s: %w", l.label, l.issueID, err)
			}
		}
		for _, dep := range subgraph.Dependencies {
			if err := tx.AddDependency(ctx, dep, actor); err != nil {
				return fmt.Errorf("creating dependency: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		// Best-effort cleanup: delete the issues we batch-created
		_ = store.RunInTransaction(ctx, func(tx storage.Transaction) error {
			for i := len(subgraph.Issues) - 1; i >= 0; i-- {
				_ = tx.DeleteIssue(ctx, subgraph.Issues[i].ID)
			}
			return nil
		})
		return Response{Success: false, Error: fmt.Sprintf("persisting labels/deps: %v", err)}
	}

	s.emitMutation(MutationCreate, protoID, resolved.Formula, "")

	result := CookResult{
		ProtoID:    protoID,
		Created:    len(subgraph.Issues),
		Variables:  vars,
		BondPoints: bondPoints,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// deleteProtoSubgraphFromDB deletes a proto and all its child issues from the database.
func deleteProtoSubgraphFromDB(ctx context.Context, store storage.Storage, protoID string) error {
	// Find all children by searching for issues with parent-child dependency
	children, err := store.GetDependents(ctx, protoID)
	if err != nil {
		return fmt.Errorf("finding proto children: %w", err)
	}

	// Recursively delete children first
	for _, child := range children {
		if err := deleteProtoSubgraphFromDB(ctx, store, child.ID); err != nil {
			return err
		}
	}

	// Delete the proto itself
	return store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		return tx.DeleteIssue(ctx, protoID)
	})
}

// handlePour handles the pour RPC operation (gt-pozvwr.24.7).
// Instantiates a proto (formula or DB proto) into persistent mol issues.
func (s *Server) handlePour(req *Request) Response {
	var args PourArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid pour args: %v", err)}
	}

	if args.ProtoID == "" {
		return Response{Success: false, Error: "proto_id is required"}
	}

	ctx, cancel := s.reqCtx(req)
	defer cancel()

	store := s.storage
	if store == nil {
		return Response{Success: false, Error: "storage not available"}
	}

	actor := s.reqActor(req)

	// Get configured prefix for mol IDs
	prefix := types.IDPrefixMol
	if p, err := store.GetConfig(ctx, "issue_prefix"); err == nil && p != "" {
		prefix = p
	}

	vars := args.Vars
	if vars == nil {
		vars = make(map[string]string)
	}

	// Try to resolve proto_id: first as DB issue, then as formula name
	var fSubgraph *formula.TemplateSubgraph
	var sSubgraph *ServerTemplateSubgraph

	// Try loading as existing DB proto
	id, err := s.resolvePartialID(ctx, args.ProtoID)
	if err == nil {
		issue, err := store.GetIssue(ctx, id)
		if err == nil && issue != nil && s.isProto(issue) {
			sSubgraph, err = s.loadTemplateSubgraph(ctx, id)
			if err != nil {
				return Response{Success: false, Error: fmt.Sprintf("loading proto subgraph: %v", err)}
			}
		}
	}

	// If not a DB proto, try as formula name (cook inline then pour)
	if sSubgraph == nil {
		cooked, err := s.cookFormulaFull(ctx, args.ProtoID, vars)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("'%s' not found as proto ID or formula: %v", args.ProtoID, err)}
		}
		fSubgraph = cooked
	}

	// Apply variable defaults and validate required vars (formula path only)
	if fSubgraph != nil {
		vars = formula.ApplyVariableDefaults(vars, fSubgraph.VarDefs)
		requiredVars := formula.ExtractRequiredSubgraphVariables(fSubgraph.Issues, fSubgraph.VarDefs)
		var missing []string
		for _, v := range requiredVars {
			if _, ok := vars[v]; !ok {
				missing = append(missing, v)
			}
		}
		if len(missing) > 0 {
			return Response{Success: false, Error: fmt.Sprintf("missing required variables: %s", strings.Join(missing, ", "))}
		}
	}

	if args.DryRun {
		issueCount := 0
		if fSubgraph != nil {
			issueCount = len(fSubgraph.Issues)
		} else {
			issueCount = len(sSubgraph.Issues)
		}
		result := PourResult{
			RootID:  args.ProtoID,
			Created: issueCount,
			Phase:   "liquid",
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	// Spawn as persistent mol (ephemeral=false)
	var rootID string
	var created int

	if fSubgraph != nil {
		// Formula path: use formula.SpawnMolecule for full variable handling
		spawnResult, err := formula.SpawnMolecule(ctx, store, fSubgraph, vars, args.Assignee, actor, false, prefix)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("pour failed: %v", err)}
		}
		rootID = spawnResult.NewEpicID
		created = spawnResult.Created
	} else {
		// DB proto path: use server-side spawnSubgraph
		spawnResult, err := s.spawnSubgraph(ctx, sSubgraph, vars, actor, false, "", "")
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("pour failed: %v", err)}
		}
		rootID = spawnResult.NewRootID
		created = spawnResult.Created

		// Set assignee on root if specified
		if args.Assignee != "" {
			_ = store.UpdateIssue(ctx, rootID, map[string]interface{}{
				"assignee": args.Assignee,
			}, actor)
		}
	}

	// Handle attachments
	attached := 0
	attachType := args.AttachType
	if attachType == "" {
		attachType = types.BondTypeSequential
	}
	for _, attachProtoID := range args.Attachments {
		aID, err := s.resolvePartialID(ctx, attachProtoID)
		if err != nil {
			continue
		}
		attachIssue, err := store.GetIssue(ctx, aID)
		if err != nil || attachIssue == nil {
			continue
		}
		attachSg, err := s.loadTemplateSubgraph(ctx, aID)
		if err != nil {
			continue
		}
		molIssue, err := store.GetIssue(ctx, rootID)
		if err != nil {
			continue
		}
		bondResult, err := s.bondProtoMol(ctx, attachSg, attachIssue, molIssue,
			attachType, vars, "", actor, false, true)
		if err != nil {
			continue
		}
		attached += bondResult.Spawned
	}

	s.emitMutation(MutationCreate, rootID, "", "")

	// Collect runbook refs from formula subgraph (od-dv0.6)
	var runbooks []string
	if fSubgraph != nil && len(fSubgraph.Runbooks) > 0 {
		runbooks = fSubgraph.Runbooks
	}

	result := PourResult{
		RootID:   rootID,
		Created:  created,
		Attached: attached,
		Phase:    "liquid",
		Runbooks: runbooks,
	}
	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// cookFormulaFull loads a formula by name, applies the full transformation pipeline,
// and returns a cooked TemplateSubgraph. Shared by handleCook and handlePour.
func (s *Server) cookFormulaFull(ctx context.Context, formulaName string, conditionVars map[string]string) (*formula.TemplateSubgraph, error) {
	store := s.storage
	parser := formula.NewParserWithStorage(store)

	resolved, err := parser.LoadByName(formulaName)
	if err != nil {
		return nil, fmt.Errorf("loading formula: %w", err)
	}

	resolved, err = parser.Resolve(resolved)
	if err != nil {
		return nil, fmt.Errorf("resolving formula: %w", err)
	}

	// Full transformation pipeline
	if controlFlowSteps, err := formula.ApplyControlFlow(resolved.Steps, resolved.Compose); err == nil {
		resolved.Steps = controlFlowSteps
	} else {
		return nil, fmt.Errorf("applying control flow: %w", err)
	}

	if len(resolved.Advice) > 0 {
		resolved.Steps = formula.ApplyAdvice(resolved.Steps, resolved.Advice)
	}

	if inlineSteps, err := formula.ApplyInlineExpansions(resolved.Steps, parser); err == nil {
		resolved.Steps = inlineSteps
	} else {
		return nil, fmt.Errorf("applying inline expansions: %w", err)
	}

	if resolved.Compose != nil && (len(resolved.Compose.Expand) > 0 || len(resolved.Compose.Map) > 0) {
		if expandedSteps, err := formula.ApplyExpansions(resolved.Steps, resolved.Compose, parser); err == nil {
			resolved.Steps = expandedSteps
		} else {
			return nil, fmt.Errorf("applying expansions: %w", err)
		}
	}

	if resolved.Compose != nil {
		for _, aspectName := range resolved.Compose.Aspects {
			aspectFormula, err := parser.LoadByName(aspectName)
			if err != nil {
				return nil, fmt.Errorf("loading aspect %q: %w", aspectName, err)
			}
			if len(aspectFormula.Advice) > 0 {
				resolved.Steps = formula.ApplyAdvice(resolved.Steps, aspectFormula.Advice)
			}
		}
	}

	protoID := resolved.Formula
	return formula.CookToSubgraphWithVars(resolved, protoID, resolved.Vars)
}

// --- helpers ---

// isSingleDBMode returns true when the daemon is running in single-DB mode
// (K8s deployment with shared Dolt database). In this mode, all rigs share
// one database and are distinguished by prefix, not separate .beads directories.
func (s *Server) isSingleDBMode() bool {
	return os.Getenv("BEADS_DOLT_SERVER_MODE") == "1"
}

// resolveRigToPrefix queries rig beads (type=rig) from the server's own storage
// to resolve a rig name to its prefix. Used in single-DB mode where all rigs
// share one database and there is no filesystem-based routes.jsonl.
//
// Rig beads have: type=rig, title=<rig-name>, label prefix:<X>.
// Matches by title (rig name) or by prefix label value.
func (s *Server) resolveRigToPrefix(targetRig string) (string, error) {
	store := s.storage
	if store == nil {
		return "", fmt.Errorf("storage not available")
	}

	ctx := context.Background()
	rigType := types.IssueType("rig")
	openStatus := types.StatusOpen
	rigBeads, err := store.SearchIssues(ctx, "", types.IssueFilter{
		IssueType: &rigType,
		Status:    &openStatus,
	})
	if err != nil {
		return "", fmt.Errorf("failed to query rig beads: %v", err)
	}

	for _, rig := range rigBeads {
		// Check title match (rig name)
		if strings.EqualFold(rig.Title, targetRig) {
			return extractPrefixLabel(ctx, store, rig.ID)
		}
		// Check if targetRig matches the prefix value itself
		labels, _ := store.GetLabels(ctx, rig.ID)
		for _, label := range labels {
			if strings.HasPrefix(label, "prefix:") {
				prefix := strings.TrimPrefix(label, "prefix:")
				if strings.EqualFold(prefix, targetRig) {
					return prefix, nil
				}
			}
		}
	}

	return "", fmt.Errorf("rig %q not found in rig beads", targetRig)
}

// extractPrefixLabel reads the prefix:<X> label from a rig bead.
func extractPrefixLabel(ctx context.Context, store storage.Storage, issueID string) (string, error) {
	labels, err := store.GetLabels(ctx, issueID)
	if err != nil {
		return "", fmt.Errorf("failed to get labels for rig %s: %v", issueID, err)
	}
	for _, label := range labels {
		if strings.HasPrefix(label, "prefix:") {
			return strings.TrimPrefix(label, "prefix:"), nil
		}
	}
	return "", fmt.Errorf("rig bead %s has no prefix: label", issueID)
}

// resolveTargetRig resolves a target rig name/prefix to a beads directory and prefix.
// In single-DB mode (K8s), queries rig beads from the shared database.
// In classical mode, walks the filesystem to find routes.jsonl.
func (s *Server) resolveTargetRig(req *Request, targetRig string) (string, string, error) {
	// Single-DB mode: resolve from rig beads, return same dbPath with resolved prefix
	if s.isSingleDBMode() {
		prefix, err := s.resolveRigToPrefix(targetRig)
		if err != nil {
			return "", "", err
		}
		// In single-DB mode, the beads dir is the server's own dbPath parent
		// (all rigs share the same database)
		return s.dbPath, prefix, nil
	}

	// Classical mode: filesystem-based resolution via routes.jsonl
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
