// Package rpc provides RPC server handlers for mol operations (gt-as9kdm).
// These handlers enable mol bond, squash, and burn to work in daemon mode.
package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/formula"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

// MoleculeLabel is the label used to identify molecule templates (protos)
const MoleculeLabel = "molecule"

// variablePattern matches {{variable}} placeholders
var serverVariablePattern = regexp.MustCompile(`\{\{([a-zA-Z_][a-zA-Z0-9_]*)\}\}`)

// ServerTemplateSubgraph holds a template and all its descendants for server-side operations
type ServerTemplateSubgraph struct {
	Root         *types.Issue
	Issues       []*types.Issue
	Dependencies []*types.Dependency
	IssueMap     map[string]*types.Issue
}

// handleMolBond handles the mol bond RPC operation
func (s *Server) handleMolBond(req *Request) Response {
	var args MolBondArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}

	ctx := s.reqCtx(req)
	actor := s.reqActor(req)

	// Validate bond type
	if args.BondType != types.BondTypeSequential && args.BondType != types.BondTypeParallel && args.BondType != types.BondTypeConditional {
		return Response{Success: false, Error: fmt.Sprintf("invalid bond type '%s', must be: sequential, parallel, or conditional", args.BondType)}
	}

	// Validate phase flags
	if args.Ephemeral && args.Pour {
		return Response{Success: false, Error: "cannot use both ephemeral and pour"}
	}

	// Dry-run just validates and returns preview
	if args.DryRun {
		return s.handleMolBondDryRun(ctx, &args, actor)
	}

	// Resolve both operands
	issueA, aIsProto, subgraphA, err := s.resolveOperand(ctx, args.IDa, args.Vars)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("resolving operand A: %v", err)}
	}

	issueB, bIsProto, subgraphB, err := s.resolveOperand(ctx, args.IDb, args.Vars)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("resolving operand B: %v", err)}
	}

	// Dispatch based on operand types
	var result *MolBondResult
	switch {
	case aIsProto && bIsProto:
		result, err = s.bondProtoProto(ctx, issueA, issueB, args.BondType, args.Title, actor)
	case aIsProto && !bIsProto:
		result, err = s.bondProtoMol(ctx, subgraphA, issueA, issueB, args.BondType, args.Vars, args.ChildRef, actor, args.Ephemeral, args.Pour)
	case !aIsProto && bIsProto:
		result, err = s.bondProtoMol(ctx, subgraphB, issueB, issueA, args.BondType, args.Vars, args.ChildRef, actor, args.Ephemeral, args.Pour)
	default:
		result, err = s.bondMolMol(ctx, issueA, issueB, args.BondType, actor)
	}

	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("bonding failed: %v", err)}
	}

	// Emit mutation event
	s.emitMutation(MutationBonded, result.ResultID, result.ResultType, "")

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// handleMolBondDryRun returns a preview of what bonding would do
func (s *Server) handleMolBondDryRun(ctx context.Context, args *MolBondArgs, actor string) Response {
	// Resolve operands for preview
	issueA, aIsProto, _, err := s.resolveOperand(ctx, args.IDa, args.Vars)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("resolving operand A: %v", err)}
	}

	issueB, bIsProto, _, err := s.resolveOperand(ctx, args.IDb, args.Vars)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("resolving operand B: %v", err)}
	}

	// Build preview result
	resultType := "compound_molecule"
	if aIsProto && bIsProto {
		resultType = "compound_proto"
	}

	result := &MolBondResult{
		ResultID:   fmt.Sprintf("(dry-run: %s + %s)", issueA.ID, issueB.ID),
		ResultType: resultType,
		BondType:   args.BondType,
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// resolveOperand resolves an operand to an issue, determines if it's a proto,
// and loads its subgraph if it's a proto
func (s *Server) resolveOperand(ctx context.Context, operand string, vars map[string]string) (*types.Issue, bool, *ServerTemplateSubgraph, error) {
	// First try to resolve as an issue ID
	id, err := s.resolvePartialID(ctx, operand)
	if err == nil {
		issue, err := s.storage.GetIssue(ctx, id)
		if err == nil && issue != nil {
			isProto := s.isProto(issue)
			var subgraph *ServerTemplateSubgraph
			if isProto {
				subgraph, err = s.loadTemplateSubgraph(ctx, id)
				if err != nil {
					return nil, false, nil, fmt.Errorf("loading proto subgraph: %w", err)
				}
			} else {
				// Wrap molecule as single-issue subgraph
				subgraph = &ServerTemplateSubgraph{
					Root:     issue,
					Issues:   []*types.Issue{issue},
					IssueMap: map[string]*types.Issue{issue.ID: issue},
				}
			}
			return issue, isProto, subgraph, nil
		}
	}

	// Try to resolve as a formula name
	if s.looksLikeFormulaName(operand) {
		subgraph, err := s.cookFormula(ctx, operand, vars)
		if err != nil {
			return nil, false, nil, fmt.Errorf("cooking formula '%s': %w", operand, err)
		}
		return subgraph.Root, true, subgraph, nil
	}

	return nil, false, nil, fmt.Errorf("'%s' not found (not an issue ID or formula name)", operand)
}

// isProto checks if an issue is a proto (has the molecule label)
func (s *Server) isProto(issue *types.Issue) bool {
	if issue == nil {
		return false
	}
	for _, label := range issue.Labels {
		if label == MoleculeLabel {
			return true
		}
	}
	return false
}

// looksLikeFormulaName checks if an operand looks like a formula name
func (s *Server) looksLikeFormulaName(operand string) bool {
	if strings.HasPrefix(operand, "mol-") {
		return true
	}
	if strings.Contains(operand, ".formula") {
		return true
	}
	if strings.Contains(operand, "/") || strings.Contains(operand, "\\") {
		return true
	}
	return false
}

// cookFormula cooks a formula to an in-memory subgraph
func (s *Server) cookFormula(ctx context.Context, formulaName string, vars map[string]string) (*ServerTemplateSubgraph, error) {
	parser := formula.NewParser()
	f, err := parser.LoadByName(formulaName)
	if err != nil {
		return nil, fmt.Errorf("loading formula: %w", err)
	}

	// Build the cooked subgraph in memory (not persisted to DB)
	subgraph := &ServerTemplateSubgraph{
		Issues:   make([]*types.Issue, 0),
		IssueMap: make(map[string]*types.Issue),
	}

	// Use formula name as title, description from formula
	rootTitle := f.Formula
	if f.Description != "" {
		rootTitle = f.Description
	}

	// Cook root step
	rootIssue := &types.Issue{
		ID:          fmt.Sprintf("cooked-%s", formulaName),
		Title:       s.substituteVariables(rootTitle, vars),
		Description: s.substituteVariables(f.Description, vars),
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeEpic,
		Labels:      []string{MoleculeLabel},
		IsTemplate:  true,
	}
	subgraph.Root = rootIssue
	subgraph.Issues = append(subgraph.Issues, rootIssue)
	subgraph.IssueMap[rootIssue.ID] = rootIssue

	// Cook child steps
	for i, step := range f.Steps {
		priority := 2
		if step.Priority != nil {
			priority = *step.Priority
		}
		issueType := types.TypeTask
		if step.Type != "" {
			issueType = types.IssueType(step.Type)
		}
		stepIssue := &types.Issue{
			ID:          fmt.Sprintf("cooked-%s.%d", formulaName, i+1),
			Title:       s.substituteVariables(step.Title, vars),
			Description: s.substituteVariables(step.Description, vars),
			Status:      types.StatusOpen,
			Priority:    priority,
			IssueType:   issueType,
		}
		subgraph.Issues = append(subgraph.Issues, stepIssue)
		subgraph.IssueMap[stepIssue.ID] = stepIssue

		// Add parent-child dependency
		subgraph.Dependencies = append(subgraph.Dependencies, &types.Dependency{
			IssueID:     stepIssue.ID,
			DependsOnID: rootIssue.ID,
			Type:        types.DepParentChild,
		})
	}

	return subgraph, nil
}

// loadTemplateSubgraph loads a proto subgraph from the database
func (s *Server) loadTemplateSubgraph(ctx context.Context, protoID string) (*ServerTemplateSubgraph, error) {
	root, err := s.storage.GetIssue(ctx, protoID)
	if err != nil {
		return nil, fmt.Errorf("getting root: %w", err)
	}

	subgraph := &ServerTemplateSubgraph{
		Root:     root,
		Issues:   []*types.Issue{root},
		IssueMap: map[string]*types.Issue{root.ID: root},
	}

	// Load all children (issues with parent-child dependency to this proto)
	depMetas, err := s.storage.GetDependentsWithMetadata(ctx, protoID)
	if err != nil {
		return nil, fmt.Errorf("getting dependents: %w", err)
	}

	for _, depMeta := range depMetas {
		if depMeta.DependencyType != types.DepParentChild {
			continue
		}
		child := &depMeta.Issue
		subgraph.Issues = append(subgraph.Issues, child)
		subgraph.IssueMap[child.ID] = child
		subgraph.Dependencies = append(subgraph.Dependencies, &types.Dependency{
			IssueID:     child.ID,
			DependsOnID: protoID,
			Type:        depMeta.DependencyType,
		})

		// Recursively load grandchildren
		if err := s.loadDescendants(ctx, subgraph, child.ID); err != nil {
			// Non-fatal, continue
		}
	}

	return subgraph, nil
}

// loadDescendants recursively loads descendants of an issue
func (s *Server) loadDescendants(ctx context.Context, subgraph *ServerTemplateSubgraph, parentID string) error {
	depMetas, err := s.storage.GetDependentsWithMetadata(ctx, parentID)
	if err != nil {
		return err
	}

	for _, depMeta := range depMetas {
		if depMeta.DependencyType != types.DepParentChild {
			continue
		}
		if _, exists := subgraph.IssueMap[depMeta.Issue.ID]; exists {
			continue // Already loaded
		}
		child := &depMeta.Issue
		subgraph.Issues = append(subgraph.Issues, child)
		subgraph.IssueMap[child.ID] = child
		subgraph.Dependencies = append(subgraph.Dependencies, &types.Dependency{
			IssueID:     child.ID,
			DependsOnID: parentID,
			Type:        depMeta.DependencyType,
		})

		// Recurse
		if err := s.loadDescendants(ctx, subgraph, child.ID); err != nil {
			// Non-fatal
		}
	}

	return nil
}

// substituteVariables replaces {{var}} placeholders with values
func (s *Server) substituteVariables(text string, vars map[string]string) string {
	if vars == nil || len(vars) == 0 {
		return text
	}
	return serverVariablePattern.ReplaceAllStringFunc(text, func(match string) string {
		// Extract variable name from {{name}}
		name := match[2 : len(match)-2]
		if val, ok := vars[name]; ok {
			return val
		}
		return match // Keep original if not found
	})
}

// bondProtoProto bonds two protos to create a compound proto
func (s *Server) bondProtoProto(ctx context.Context, protoA, protoB *types.Issue, bondType, customTitle, actor string) (*MolBondResult, error) {
	compoundTitle := fmt.Sprintf("Compound: %s + %s", protoA.Title, protoB.Title)
	if customTitle != "" {
		compoundTitle = customTitle
	}

	var compoundID string
	err := s.storage.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Create compound root issue
		compound := &types.Issue{
			Title:       compoundTitle,
			Description: fmt.Sprintf("Compound proto bonding %s and %s", protoA.ID, protoB.ID),
			Status:      types.StatusOpen,
			Priority:    minPriority(protoA.Priority, protoB.Priority),
			IssueType:   types.TypeEpic,
			BondedFrom: []types.BondRef{
				{SourceID: protoA.ID, BondType: bondType},
				{SourceID: protoB.ID, BondType: bondType},
			},
		}
		if err := tx.CreateIssue(ctx, compound, actor); err != nil {
			return fmt.Errorf("creating compound: %w", err)
		}
		compoundID = compound.ID

		// Add template label
		if err := tx.AddLabel(ctx, compoundID, MoleculeLabel, actor); err != nil {
			return fmt.Errorf("adding template label: %w", err)
		}

		// Add parent-child dependencies from compound to both proto roots
		depA := &types.Dependency{
			IssueID:     protoA.ID,
			DependsOnID: compoundID,
			Type:        types.DepParentChild,
		}
		if err := tx.AddDependency(ctx, depA, actor); err != nil {
			return fmt.Errorf("linking proto A: %w", err)
		}

		depB := &types.Dependency{
			IssueID:     protoB.ID,
			DependsOnID: compoundID,
			Type:        types.DepParentChild,
		}
		if err := tx.AddDependency(ctx, depB, actor); err != nil {
			return fmt.Errorf("linking proto B: %w", err)
		}

		// For sequential/conditional, add blocking dependency: B blocks on A
		if bondType == types.BondTypeSequential || bondType == types.BondTypeConditional {
			depType := types.DepBlocks
			if bondType == types.BondTypeConditional {
				depType = types.DepConditionalBlocks
			}
			seqDep := &types.Dependency{
				IssueID:     protoB.ID,
				DependsOnID: protoA.ID,
				Type:        depType,
			}
			if err := tx.AddDependency(ctx, seqDep, actor); err != nil {
				return fmt.Errorf("adding sequence dep: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &MolBondResult{
		ResultID:   compoundID,
		ResultType: "compound_proto",
		BondType:   bondType,
	}, nil
}

// bondProtoMol bonds a proto to an existing molecule by spawning the proto
func (s *Server) bondProtoMol(ctx context.Context, protoSubgraph *ServerTemplateSubgraph, proto, mol *types.Issue, bondType string, vars map[string]string, childRef string, actor string, ephemeralFlag, pourFlag bool) (*MolBondResult, error) {
	if protoSubgraph == nil {
		var err error
		protoSubgraph, err = s.loadTemplateSubgraph(ctx, proto.ID)
		if err != nil {
			return nil, fmt.Errorf("loading proto: %w", err)
		}
	}

	// Determine ephemeral flag
	makeEphemeral := mol.Ephemeral
	if ephemeralFlag {
		makeEphemeral = true
	} else if pourFlag {
		makeEphemeral = false
	}

	// Spawn the proto
	spawnResult, err := s.spawnSubgraph(ctx, protoSubgraph, vars, actor, makeEphemeral, mol.ID, childRef)
	if err != nil {
		return nil, fmt.Errorf("spawning proto: %w", err)
	}

	// Attach spawned molecule to existing molecule
	err = s.storage.RunInTransaction(ctx, func(tx storage.Transaction) error {
		var depType types.DependencyType
		switch bondType {
		case types.BondTypeSequential:
			depType = types.DepBlocks
		case types.BondTypeConditional:
			depType = types.DepConditionalBlocks
		default:
			depType = types.DepParentChild
		}
		dep := &types.Dependency{
			IssueID:     spawnResult.NewRootID,
			DependsOnID: mol.ID,
			Type:        depType,
		}
		return tx.AddDependency(ctx, dep, actor)
	})

	if err != nil {
		return nil, fmt.Errorf("attaching to molecule: %w", err)
	}

	return &MolBondResult{
		ResultID:   mol.ID,
		ResultType: "compound_molecule",
		BondType:   bondType,
		Spawned:    spawnResult.Created,
		IDMapping:  spawnResult.IDMapping,
	}, nil
}

// bondMolMol bonds two molecules together
func (s *Server) bondMolMol(ctx context.Context, molA, molB *types.Issue, bondType, actor string) (*MolBondResult, error) {
	err := s.storage.RunInTransaction(ctx, func(tx storage.Transaction) error {
		var depType types.DependencyType
		switch bondType {
		case types.BondTypeSequential:
			depType = types.DepBlocks
		case types.BondTypeConditional:
			depType = types.DepConditionalBlocks
		default:
			depType = types.DepParentChild
		}
		dep := &types.Dependency{
			IssueID:     molB.ID,
			DependsOnID: molA.ID,
			Type:        depType,
		}
		return tx.AddDependency(ctx, dep, actor)
	})

	if err != nil {
		return nil, fmt.Errorf("linking molecules: %w", err)
	}

	return &MolBondResult{
		ResultID:   molA.ID,
		ResultType: "compound_molecule",
		BondType:   bondType,
	}, nil
}

// SpawnResult holds the result of spawning a subgraph
type SpawnResult struct {
	NewRootID string
	IDMapping map[string]string
	Created   int
}

// spawnSubgraph spawns a subgraph, creating new issues with new IDs
func (s *Server) spawnSubgraph(ctx context.Context, subgraph *ServerTemplateSubgraph, vars map[string]string, actor string, ephemeral bool, parentID, childRef string) (*SpawnResult, error) {
	idMapping := make(map[string]string)
	created := 0
	var newRootID string

	err := s.storage.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// First pass: create all issues with new IDs
		for _, oldIssue := range subgraph.Issues {
			newIssue := &types.Issue{
				Title:       s.substituteVariables(oldIssue.Title, vars),
				Description: s.substituteVariables(oldIssue.Description, vars),
				Status:      types.StatusOpen,
				Priority:    oldIssue.Priority,
				IssueType:   oldIssue.IssueType,
				Assignee:    s.substituteVariables(oldIssue.Assignee, vars),
				Ephemeral:   ephemeral,
			}

			if err := tx.CreateIssue(ctx, newIssue, actor); err != nil {
				return fmt.Errorf("creating issue: %w", err)
			}

			idMapping[oldIssue.ID] = newIssue.ID
			created++

			if oldIssue.ID == subgraph.Root.ID {
				newRootID = newIssue.ID
			}

			// Copy labels
			for _, label := range oldIssue.Labels {
				if label == MoleculeLabel {
					continue // Don't copy template label to spawned instances
				}
				if err := tx.AddLabel(ctx, newIssue.ID, label, actor); err != nil {
					// Non-fatal
				}
			}
		}

		// Second pass: recreate dependencies with mapped IDs
		for _, dep := range subgraph.Dependencies {
			newFromID, ok1 := idMapping[dep.IssueID]
			newToID, ok2 := idMapping[dep.DependsOnID]
			if !ok1 || !ok2 {
				continue
			}
			newDep := &types.Dependency{
				IssueID:     newFromID,
				DependsOnID: newToID,
				Type:        dep.Type,
			}
			if err := tx.AddDependency(ctx, newDep, actor); err != nil {
				// Non-fatal
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &SpawnResult{
		NewRootID: newRootID,
		IDMapping: idMapping,
		Created:   created,
	}, nil
}

// resolvePartialID resolves a partial ID to a full ID using the standard utility
func (s *Server) resolvePartialID(ctx context.Context, partial string) (string, error) {
	return utils.ResolvePartialID(ctx, s.storage, partial)
}

// minPriority returns the higher priority (lower number)
func minPriority(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// handleMolSquash handles the mol squash RPC operation
func (s *Server) handleMolSquash(req *Request) Response {
	var args MolSquashArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}

	ctx := s.reqCtx(req)
	actor := s.reqActor(req)

	// Resolve molecule ID
	moleculeID, err := s.resolvePartialID(ctx, args.MoleculeID)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("resolving molecule ID: %v", err)}
	}

	// Load the molecule subgraph
	subgraph, err := s.loadTemplateSubgraph(ctx, moleculeID)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("loading molecule: %v", err)}
	}

	// Filter to only ephemeral children (exclude root)
	var wispChildren []*types.Issue
	for _, issue := range subgraph.Issues {
		if issue.ID == subgraph.Root.ID {
			continue // Skip root
		}
		if issue.Ephemeral {
			wispChildren = append(wispChildren, issue)
		}
	}

	// Dry-run: just return preview
	if args.DryRun {
		result := &MolSquashResult{
			MoleculeID:    moleculeID,
			SquashedCount: len(wispChildren),
			KeptChildren:  args.KeepChildren,
		}
		for _, child := range wispChildren {
			result.SquashedIDs = append(result.SquashedIDs, child.ID)
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	// No children to squash
	if len(wispChildren) == 0 {
		result := &MolSquashResult{
			MoleculeID:    moleculeID,
			SquashedCount: 0,
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	// Perform the squash
	result, err := s.squashMolecule(ctx, subgraph.Root, wispChildren, args.KeepChildren, args.Summary, actor)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("squash failed: %v", err)}
	}

	// Emit mutation event
	s.emitMutation(MutationSquashed, result.DigestID, "digest", "")

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// squashMolecule performs the squash operation
func (s *Server) squashMolecule(ctx context.Context, root *types.Issue, children []*types.Issue, keepChildren bool, summary string, actor string) (*MolSquashResult, error) {
	// Collect child IDs
	childIDs := make([]string, len(children))
	for i, c := range children {
		childIDs[i] = c.ID
	}

	// Generate digest content
	var digestContent string
	if summary != "" {
		digestContent = summary
	} else {
		digestContent = s.generateDigest(root, children)
	}

	result := &MolSquashResult{
		MoleculeID:    root.ID,
		SquashedIDs:   childIDs,
		SquashedCount: len(children),
		KeptChildren:  keepChildren,
	}

	// Create digest issue in transaction
	var digestID string
	err := s.storage.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Create digest issue (permanent, not ephemeral)
		now := time.Now()
		digestIssue := &types.Issue{
			Title:       fmt.Sprintf("Digest: %s", root.Title),
			Description: digestContent,
			Status:      types.StatusClosed,
			CloseReason: fmt.Sprintf("Squashed from %d wisps", len(children)),
			Priority:    root.Priority,
			IssueType:   types.TypeTask,
			Ephemeral:   false,
			ClosedAt:    &now,
		}
		if err := tx.CreateIssue(ctx, digestIssue, actor); err != nil {
			return fmt.Errorf("creating digest issue: %w", err)
		}
		digestID = digestIssue.ID

		// Link digest to root as parent-child
		dep := &types.Dependency{
			IssueID:     digestID,
			DependsOnID: root.ID,
			Type:        types.DepParentChild,
		}
		if err := tx.AddDependency(ctx, dep, actor); err != nil {
			return fmt.Errorf("linking digest to root: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	result.DigestID = digestID

	// Delete children if not keeping them
	if !keepChildren {
		deleted := 0
		for _, id := range childIDs {
			if err := s.storage.DeleteIssue(ctx, id); err != nil {
				continue // Non-fatal
			}
			deleted++
		}
		result.DeletedCount = deleted
	}

	return result, nil
}

// generateDigest creates a summary from the molecule execution
func (s *Server) generateDigest(root *types.Issue, children []*types.Issue) string {
	var sb strings.Builder

	sb.WriteString("## Molecule Execution Summary\n\n")
	sb.WriteString(fmt.Sprintf("**Molecule**: %s\n", root.Title))
	sb.WriteString(fmt.Sprintf("**Steps**: %d\n\n", len(children)))

	// Count completed vs other statuses
	completed := 0
	inProgress := 0
	for _, c := range children {
		switch c.Status {
		case types.StatusClosed:
			completed++
		case types.StatusInProgress:
			inProgress++
		}
	}
	sb.WriteString(fmt.Sprintf("**Completed**: %d/%d\n", completed, len(children)))
	if inProgress > 0 {
		sb.WriteString(fmt.Sprintf("**In Progress**: %d\n", inProgress))
	}
	sb.WriteString("\n---\n\n")

	// List each step with its outcome
	sb.WriteString("### Steps\n\n")
	for i, child := range children {
		status := string(child.Status)
		sb.WriteString(fmt.Sprintf("%d. **[%s]** %s\n", i+1, status, child.Title))
		if child.Description != "" {
			desc := child.Description
			if len(desc) > 200 {
				desc = desc[:200] + "..."
			}
			sb.WriteString(fmt.Sprintf("   %s\n", desc))
		}
		if child.CloseReason != "" {
			sb.WriteString(fmt.Sprintf("   *Outcome: %s*\n", child.CloseReason))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// handleMolBurn handles the mol burn RPC operation
func (s *Server) handleMolBurn(req *Request) Response {
	var args MolBurnArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}

	ctx := s.reqCtx(req)
	actor := s.reqActor(req)

	if len(args.MoleculeIDs) == 0 {
		return Response{Success: false, Error: "no molecule IDs provided"}
	}

	/// Dry-run: just return preview
	if args.DryRun {
		var allIDs []string
		var failedCount int
		for _, molID := range args.MoleculeIDs {
			resolvedID, err := s.resolvePartialID(ctx, molID)
			if err != nil {
				failedCount++
				continue
			}
			allIDs = append(allIDs, resolvedID)
		}
		result := &MolBurnResult{
			DeletedIDs:   allIDs,
			DeletedCount: len(allIDs),
			FailedCount:  failedCount,
		}
		data, _ := json.Marshal(result)
		return Response{Success: true, Data: data}
	}

	// Perform the burn
	result, err := s.burnMolecules(ctx, args.MoleculeIDs, args.Force, actor)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("burn failed: %v", err)}
	}

	// Emit mutation events for each deleted molecule
	for _, id := range result.DeletedIDs {
		s.emitMutation(MutationBurned, id, "burned", "")
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// burnMolecules deletes the specified molecules and their children
func (s *Server) burnMolecules(ctx context.Context, moleculeIDs []string, force bool, actor string) (*MolBurnResult, error) {
	result := &MolBurnResult{
		DeletedIDs: make([]string, 0),
	}

	for _, molID := range moleculeIDs {
		// Resolve molecule ID
		resolvedID, err := s.resolvePartialID(ctx, molID)
		if err != nil {
			result.FailedCount++
			continue
		}

		// Load subgraph to get all children
		subgraph, err := s.loadTemplateSubgraph(ctx, resolvedID)
		if err != nil {
			result.FailedCount++
			continue
		}

		// Delete all issues in subgraph (children first, then root)
		// Collect IDs in reverse order (children before parents)
		var toDelete []string
		for _, issue := range subgraph.Issues {
			if issue.ID != subgraph.Root.ID {
				toDelete = append(toDelete, issue.ID)
			}
		}
		toDelete = append(toDelete, subgraph.Root.ID)

		// Delete each issue
		for _, id := range toDelete {
			if err := s.storage.DeleteIssue(ctx, id); err != nil {
				continue // Non-fatal, continue with others
			}
			result.DeletedIDs = append(result.DeletedIDs, id)
			result.DeletedCount++
		}
	}

	return result, nil
}

// handleMolCurrent handles the mol current RPC operation (bd-fwsa)
func (s *Server) handleMolCurrent(req *Request) Response {
	var args MolCurrentArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}

	ctx := s.reqCtx(req)

	// Determine agent filter
	agent := args.Agent
	if agent == "" {
		agent = s.reqActor(req)
	}

	var molecules []*MolCurrentProgress

	if args.MoleculeID != "" {
		// Explicit molecule ID given
		moleculeID, err := s.resolvePartialID(ctx, args.MoleculeID)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("molecule '%s' not found", args.MoleculeID)}
		}

		progress, err := s.getMoleculeProgress(ctx, moleculeID, args.Limit, args.RangeStart, args.RangeEnd)
		if err != nil {
			return Response{Success: false, Error: fmt.Sprintf("loading molecule: %v", err)}
		}

		molecules = append(molecules, progress)
	} else {
		// Infer from in_progress issues
		molecules = s.findInProgressMolecules(ctx, agent)

		// Fallback: check for hooked issues with bonded molecules
		if len(molecules) == 0 {
			molecules = s.findHookedMolecules(ctx, agent)
		}
	}

	result := &MolCurrentResult{
		Molecules: molecules,
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// getMoleculeProgress loads a molecule and computes detailed progress
func (s *Server) getMoleculeProgress(ctx context.Context, moleculeID string, limit, rangeStart, rangeEnd int) (*MolCurrentProgress, error) {
	// Load the molecule root
	root, err := s.storage.GetIssue(ctx, moleculeID)
	if err != nil || root == nil {
		return nil, fmt.Errorf("molecule not found: %s", moleculeID)
	}

	progress := &MolCurrentProgress{
		MoleculeID:    root.ID,
		MoleculeTitle: root.Title,
		Assignee:      root.Assignee,
	}

	// Load all children (steps)
	depMetas, err := s.storage.GetDependentsWithMetadata(ctx, moleculeID)
	if err != nil {
		return nil, fmt.Errorf("getting steps: %w", err)
	}

	// Get ready issues for determining step readiness
	readyIssues, _ := s.storage.GetReadyWork(ctx, types.WorkFilter{IncludeMolSteps: true})
	readyIDs := make(map[string]bool)
	for _, issue := range readyIssues {
		readyIDs[issue.ID] = true
	}

	// Build step status list
	var steps []*MolCurrentStepStatus
	for _, depMeta := range depMetas {
		if depMeta.DependencyType != types.DepParentChild {
			continue
		}
		issue := &depMeta.Issue
		step := &MolCurrentStepStatus{
			IssueID:   issue.ID,
			Title:     issue.Title,
			IssueType: string(issue.IssueType),
			Priority:  issue.Priority,
		}

		switch issue.Status {
		case types.StatusClosed:
			step.Status = "done"
			progress.Completed++
		case types.StatusInProgress:
			step.Status = "current"
			step.IsCurrent = true
			progress.CurrentStep = step
		case types.StatusBlocked:
			step.Status = "blocked"
		default:
			if readyIDs[issue.ID] {
				step.Status = "ready"
				if progress.NextStep == nil {
					progress.NextStep = step
				}
			} else {
				step.Status = "pending"
			}
		}

		steps = append(steps, step)
	}

	progress.Total = len(steps)

	// Apply range/limit filtering
	if rangeStart > 0 && rangeEnd > 0 {
		startIdx := rangeStart - 1
		if startIdx >= len(steps) {
			steps = nil
		} else {
			endIdx := rangeEnd
			if endIdx > len(steps) {
				endIdx = len(steps)
			}
			steps = steps[startIdx:endIdx]
		}
	} else if limit > 0 && len(steps) > limit {
		steps = steps[:limit]
	}

	progress.Steps = steps

	// If no current step but there's a ready step, set it as next
	if progress.CurrentStep == nil && progress.NextStep == nil {
		for _, step := range steps {
			if step.Status == "ready" {
				progress.NextStep = step
				break
			}
		}
	}

	return progress, nil
}

// findInProgressMolecules finds molecules with in_progress steps for an agent
func (s *Server) findInProgressMolecules(ctx context.Context, agent string) []*MolCurrentProgress {
	// Query for in_progress issues
	status := types.StatusInProgress
	filter := types.IssueFilter{Status: &status}
	if agent != "" {
		filter.Assignee = &agent
	}
	inProgressIssues, err := s.storage.SearchIssues(ctx, "", filter)
	if err != nil || len(inProgressIssues) == 0 {
		return nil
	}

	// For each in_progress issue, find its parent molecule
	moleculeMap := make(map[string]*MolCurrentProgress)
	for _, issue := range inProgressIssues {
		moleculeID := s.findParentMolecule(ctx, issue.ID)
		if moleculeID == "" {
			continue
		}

		if _, exists := moleculeMap[moleculeID]; !exists {
			progress, err := s.getMoleculeProgress(ctx, moleculeID, 0, 0, 0)
			if err == nil {
				moleculeMap[moleculeID] = progress
			}
		}
	}

	// Convert to slice
	var molecules []*MolCurrentProgress
	for _, mol := range moleculeMap {
		molecules = append(molecules, mol)
	}

	return molecules
}

// findHookedMolecules finds molecules bonded to hooked issues for an agent
func (s *Server) findHookedMolecules(ctx context.Context, agent string) []*MolCurrentProgress {
	// Query for hooked issues assigned to the agent
	status := types.StatusHooked
	filter := types.IssueFilter{Status: &status}
	if agent != "" {
		filter.Assignee = &agent
	}
	hookedIssues, err := s.storage.SearchIssues(ctx, "", filter)
	if err != nil || len(hookedIssues) == 0 {
		return nil
	}

	// For each hooked issue, check for blocks dependencies on molecules
	moleculeMap := make(map[string]*MolCurrentProgress)
	for _, issue := range hookedIssues {
		deps, err := s.storage.GetDependencyRecords(ctx, issue.ID)
		if err != nil {
			continue
		}

		// Look for a blocks dependency pointing to a molecule
		for _, dep := range deps {
			if dep.Type != types.DepBlocks {
				continue
			}
			candidate, err := s.storage.GetIssue(ctx, dep.DependsOnID)
			if err != nil || candidate == nil {
				continue
			}

			// Check if candidate is a molecule
			isMolecule := candidate.IssueType == types.TypeEpic || s.isProto(candidate)
			if isMolecule {
				if _, exists := moleculeMap[candidate.ID]; !exists {
					progress, err := s.getMoleculeProgress(ctx, candidate.ID, 0, 0, 0)
					if err == nil {
						moleculeMap[candidate.ID] = progress
					}
				}
			}
		}
	}

	// Convert to slice
	var molecules []*MolCurrentProgress
	for _, mol := range moleculeMap {
		molecules = append(molecules, mol)
	}

	return molecules
}

// findParentMolecule walks up parent-child chain to find the root molecule
func (s *Server) findParentMolecule(ctx context.Context, issueID string) string {
	visited := make(map[string]bool)
	currentID := issueID

	for !visited[currentID] {
		visited[currentID] = true

		deps, err := s.storage.GetDependencyRecords(ctx, currentID)
		if err != nil {
			return ""
		}

		// Find parent-child dependency where current is the child
		var parentID string
		for _, dep := range deps {
			if dep.Type == types.DepParentChild && dep.IssueID == currentID {
				parentID = dep.DependsOnID
				break
			}
		}

		if parentID == "" {
			// No parent - check if current issue is a molecule root
			issue, err := s.storage.GetIssue(ctx, currentID)
			if err != nil || issue == nil {
				return ""
			}
			// Check if it's a proto (has template label) or epic
			if s.isProto(issue) || issue.IssueType == types.TypeEpic {
				return currentID
			}
			return ""
		}

		currentID = parentID
	}

	return ""
}

// handleMolReadyGated handles the mol ready --gated RPC operation (bd-2n56)
func (s *Server) handleMolReadyGated(req *Request) Response {
	var args MolReadyGatedArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return Response{Success: false, Error: fmt.Sprintf("invalid arguments: %v", err)}
	}

	ctx := s.reqCtx(req)

	// Find gate-ready molecules
	molecules, err := s.findGateReadyMolecules(ctx, args.Limit)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("finding gate-ready molecules: %v", err)}
	}

	result := &MolReadyGatedResult{
		Molecules: molecules,
		Count:     len(molecules),
	}

	data, _ := json.Marshal(result)
	return Response{Success: true, Data: data}
}

// findGateReadyMolecules finds molecules where a gate has closed and work can resume.
//
// Logic:
// 1. Find all closed gate beads
// 2. For each closed gate, find what step it was blocking
// 3. Check if that step is now ready (unblocked)
// 4. Find the parent molecule
// 5. Filter out molecules that are already hooked by someone
func (s *Server) findGateReadyMolecules(ctx context.Context, limit int) ([]*MolReadyGatedMolecule, error) {
	if limit == 0 {
		limit = 100
	}

	// Step 1: Find all closed gate beads
	gateType := types.IssueType("gate")
	closedStatus := types.StatusClosed
	gateFilter := types.IssueFilter{
		IssueType: &gateType,
		Status:    &closedStatus,
		Limit:     limit,
	}

	closedGates, err := s.storage.SearchIssues(ctx, "", gateFilter)
	if err != nil {
		return nil, fmt.Errorf("searching closed gates: %w", err)
	}

	if len(closedGates) == 0 {
		return []*MolReadyGatedMolecule{}, nil
	}

	// Step 2: Get ready work to check which steps are ready
	// IncludeMolSteps: true because we specifically need to see molecule steps here
	readyIssues, err := s.storage.GetReadyWork(ctx, types.WorkFilter{Limit: 500, IncludeMolSteps: true})
	if err != nil {
		return nil, fmt.Errorf("getting ready work: %w", err)
	}
	readyIDs := make(map[string]bool)
	for _, issue := range readyIssues {
		readyIDs[issue.ID] = true
	}

	// Step 3: Get hooked molecules to filter out
	hookedStatus := types.StatusHooked
	hookedFilter := types.IssueFilter{
		Status: &hookedStatus,
		Limit:  100,
	}
	hookedIssues, err := s.storage.SearchIssues(ctx, "", hookedFilter)
	if err != nil {
		// Non-fatal: just continue without filtering
		hookedIssues = nil
	}
	hookedMolecules := make(map[string]bool)
	for _, issue := range hookedIssues {
		// If the hooked issue is a molecule root, mark it
		hookedMolecules[issue.ID] = true
		// Also find parent molecule for hooked steps
		if parentMol := s.findParentMolecule(ctx, issue.ID); parentMol != "" {
			hookedMolecules[parentMol] = true
		}
	}

	// Step 4: For each closed gate, find issues that depend on it (were blocked)
	moleculeMap := make(map[string]*MolReadyGatedMolecule)

	for _, gate := range closedGates {
		// Find issues that depend on this gate (GetDependents returns issues where depends_on_id = gate.ID)
		dependents, err := s.storage.GetDependents(ctx, gate.ID)
		if err != nil {
			continue
		}

		for _, dependent := range dependents {
			// Check if the previously blocked step is now ready
			if !readyIDs[dependent.ID] {
				continue
			}

			// Find the parent molecule
			moleculeID := s.findParentMolecule(ctx, dependent.ID)
			if moleculeID == "" {
				continue
			}

			// Skip if already hooked
			if hookedMolecules[moleculeID] {
				continue
			}

			// Get molecule details
			moleculeIssue, err := s.storage.GetIssue(ctx, moleculeID)
			if err != nil || moleculeIssue == nil {
				continue
			}

			// Add to results (dedupe by molecule ID)
			if _, exists := moleculeMap[moleculeID]; !exists {
				moleculeMap[moleculeID] = &MolReadyGatedMolecule{
					MoleculeID:     moleculeID,
					MoleculeTitle:  moleculeIssue.Title,
					ClosedGateID:   gate.ID,
					ClosedGateType: gate.AwaitType,
					ReadyStepID:    dependent.ID,
					ReadyStepTitle: dependent.Title,
				}
			}
		}
	}

	// Convert to slice
	var molecules []*MolReadyGatedMolecule
	for _, mol := range moleculeMap {
		molecules = append(molecules, mol)
	}

	// Sort by molecule ID for consistent ordering
	for i := 0; i < len(molecules); i++ {
		for j := i + 1; j < len(molecules); j++ {
			if molecules[i].MoleculeID > molecules[j].MoleculeID {
				molecules[i], molecules[j] = molecules[j], molecules[i]
			}
		}
	}

	return molecules, nil
}
