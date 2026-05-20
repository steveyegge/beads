package domain

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

type OrphanHandling int

const (
	OrphanAllow OrphanHandling = iota
	OrphanReject
)

type InsertIssueOpts struct {
	SkipPrefixValidation bool
	OrphanHandling       OrphanHandling
	UseWispsTable        bool
}

type IssueTableOpts struct {
	UseWispsTable bool
}

type IssueSQLRepository interface {
	Insert(ctx context.Context, issue *types.Issue, actor string, opts InsertIssueOpts) error
	InsertBatch(ctx context.Context, issues []*types.Issue, actor string, opts InsertIssueOpts) error
	Update(ctx context.Context, id string, updates map[string]any, actor string, opts IssueTableOpts) error

	Get(ctx context.Context, id string, opts IssueTableOpts) (*types.Issue, error)
	GetByIDs(ctx context.Context, ids []string, opts IssueTableOpts) ([]*types.Issue, error)

	Search(ctx context.Context, filter types.IssueFilter, opts IssueTableOpts) ([]*types.Issue, error)
}

type CreateIssueParams struct {
	Issue                   *types.Issue
	ExplicitID              string
	ParentID                string
	Labels                  []string
	InheritLabelsFromParent bool
	Dependencies            []DependencySpec
	WaitsFor                *WaitsForSpec
	DiscoveredFromParent    string
	ForcePrefix             bool
	PrefixOverride          string
}

type DependencySpec struct {
	Type          types.DependencyType
	TargetID      string
	SwapDirection bool
	Metadata      string
}

type WaitsForSpec struct {
	SpawnerID string
	Gate      string
}

type CreateIssueResult struct {
	Issue            *types.Issue
	InheritedLabels  []string
	PostCreateWrites bool
}

type CreateIssuesOpts struct {
	OrphanHandling       OrphanHandling
	SkipPrefixValidation bool
}

type CreateIssuesResult struct {
	Issues []*types.Issue
}

type ListProjection struct {
	Labels           bool
	Dependencies     bool
	DependencyCounts bool
	Parent           bool
	CommentCounts    bool
	Comments         bool
}

type ListResult struct {
	Issues    []*types.IssueWithCounts
	Labels    map[string][]string
	BlockedBy map[string][]string
	Blocks    map[string][]string
	Parent    map[string]string
}

type GraphPlan struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

type GraphNode struct {
	Key               string
	Issue             *types.Issue
	ParentKey         string
	ParentID          string
	Assignee          string
	AssignAfterCreate bool
	MetadataRefs      map[string]string
	Labels            []string
}

type GraphEdge struct {
	FromKey string
	FromID  string
	ToKey   string
	ToID    string
	Type    types.DependencyType
}

type GraphApplyResult struct {
	IDs map[string]string
}

type IssueUseCase interface {
	GetIssue(ctx context.Context, id string) (*types.Issue, error)
	GetIssuesByIDs(ctx context.Context, ids []string) ([]*types.Issue, error)
	ListIssues(ctx context.Context, filter types.IssueFilter, proj ListProjection) (ListResult, error)
	CreateIssue(ctx context.Context, params CreateIssueParams, actor string) (CreateIssueResult, error)
	CreateIssues(ctx context.Context, params []CreateIssueParams, actor string, opts CreateIssuesOpts) (CreateIssuesResult, error)
	UpdateIssue(ctx context.Context, id string, updates map[string]any, actor string) error
	ApplyIssueGraph(ctx context.Context, plan GraphPlan, actor string) (GraphApplyResult, error)

	GetWisp(ctx context.Context, id string) (*types.Issue, error)
	GetWispsByIDs(ctx context.Context, ids []string) ([]*types.Issue, error)
	ListWisps(ctx context.Context, filter types.IssueFilter, proj ListProjection) (ListResult, error)
	CreateWisp(ctx context.Context, params CreateIssueParams, actor string) (CreateIssueResult, error)
	CreateWisps(ctx context.Context, params []CreateIssueParams, actor string, opts CreateIssuesOpts) (CreateIssuesResult, error)
	UpdateWisp(ctx context.Context, id string, updates map[string]any, actor string) error
	ApplyWispGraph(ctx context.Context, plan GraphPlan, actor string) (GraphApplyResult, error)
}

func NewIssueUseCase(
	issueRepo IssueSQLRepository,
	depRepo DependencySQLRepository,
	labelRepo LabelSQLRepository,
	counterRepo ChildCounterSQLRepository,
	commentRepo CommentSQLRepository,
	cfgRepo ConfigSQLRepository,
) IssueUseCase {
	return &issueUseCaseImpl{
		issueRepo:   issueRepo,
		depRepo:     depRepo,
		labelRepo:   labelRepo,
		counterRepo: counterRepo,
		commentRepo: commentRepo,
		cfgRepo:     cfgRepo,
	}
}

type issueUseCaseImpl struct {
	issueRepo   IssueSQLRepository
	depRepo     DependencySQLRepository
	labelRepo   LabelSQLRepository
	counterRepo ChildCounterSQLRepository
	commentRepo CommentSQLRepository
	cfgRepo     ConfigSQLRepository
}

var _ IssueUseCase = (*issueUseCaseImpl)(nil)

func (u *issueUseCaseImpl) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	return u.get(ctx, id, false)
}

func (u *issueUseCaseImpl) GetWisp(ctx context.Context, id string) (*types.Issue, error) {
	return u.get(ctx, id, true)
}

func (u *issueUseCaseImpl) get(ctx context.Context, id string, useWisp bool) (*types.Issue, error) {
	if id == "" {
		return nil, fmt.Errorf("get: id must not be empty")
	}
	issue, err := u.issueRepo.Get(ctx, id, IssueTableOpts{UseWispsTable: useWisp})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", id, err)
	}
	return issue, nil
}

func (u *issueUseCaseImpl) GetIssuesByIDs(ctx context.Context, ids []string) ([]*types.Issue, error) {
	return u.getByIDs(ctx, ids, false)
}

func (u *issueUseCaseImpl) GetWispsByIDs(ctx context.Context, ids []string) ([]*types.Issue, error) {
	return u.getByIDs(ctx, ids, true)
}

func (u *issueUseCaseImpl) getByIDs(ctx context.Context, ids []string, useWisp bool) ([]*types.Issue, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out, err := u.issueRepo.GetByIDs(ctx, ids, IssueTableOpts{UseWispsTable: useWisp})
	if err != nil {
		return nil, fmt.Errorf("getByIDs: %w", err)
	}
	return out, nil
}

func (u *issueUseCaseImpl) UpdateIssue(ctx context.Context, id string, updates map[string]any, actor string) error {
	return u.update(ctx, id, updates, actor, false)
}

func (u *issueUseCaseImpl) UpdateWisp(ctx context.Context, id string, updates map[string]any, actor string) error {
	return u.update(ctx, id, updates, actor, true)
}

func (u *issueUseCaseImpl) update(ctx context.Context, id string, updates map[string]any, actor string, useWisp bool) error {
	if id == "" {
		return fmt.Errorf("update: id must not be empty")
	}
	if len(updates) == 0 {
		return nil
	}
	return u.issueRepo.Update(ctx, id, updates, actor, IssueTableOpts{UseWispsTable: useWisp})
}

func (u *issueUseCaseImpl) ListIssues(ctx context.Context, filter types.IssueFilter, proj ListProjection) (ListResult, error) {
	return u.list(ctx, filter, proj, false)
}

func (u *issueUseCaseImpl) ListWisps(ctx context.Context, filter types.IssueFilter, proj ListProjection) (ListResult, error) {
	return u.list(ctx, filter, proj, true)
}

func (u *issueUseCaseImpl) list(ctx context.Context, filter types.IssueFilter, proj ListProjection, useWisp bool) (ListResult, error) {
	issues, err := u.issueRepo.Search(ctx, filter, IssueTableOpts{UseWispsTable: useWisp})
	if err != nil {
		return ListResult{}, fmt.Errorf("list: search: %w", err)
	}

	out := ListResult{
		Issues:    make([]*types.IssueWithCounts, 0, len(issues)),
		Labels:    map[string][]string{},
		BlockedBy: map[string][]string{},
		Blocks:    map[string][]string{},
		Parent:    map[string]string{},
	}
	if len(issues) == 0 {
		return out, nil
	}

	ids := make([]string, len(issues))
	for i, issue := range issues {
		ids[i] = issue.ID
	}

	if proj.Labels {
		labels, err := u.labelRepo.ListByIssueIDs(ctx, ids, LabelOpts{UseWispsTable: useWisp})
		if err != nil {
			return ListResult{}, fmt.Errorf("list: labels: %w", err)
		}
		out.Labels = labels
		for _, issue := range issues {
			if l, ok := labels[issue.ID]; ok {
				issue.Labels = l
			}
		}
	}

	counts := make(map[string]*types.DependencyCounts, len(ids))
	for _, id := range ids {
		counts[id] = &types.DependencyCounts{}
	}

	needDeps := proj.Dependencies || proj.DependencyCounts || proj.Parent
	if needDeps {
		depBulk, err := u.depRepo.ListByIssueIDs(ctx, ids, DepListOpts{Direction: DepDirectionBoth, UseWispsTable: useWisp})
		if err != nil {
			return ListResult{}, fmt.Errorf("list: deps: %w", err)
		}
		for _, issue := range issues {
			outgoing := depBulk.Outgoing[issue.ID]
			if proj.Dependencies {
				issue.Dependencies = outgoing
			}
			for _, d := range outgoing {
				if d.Type == types.DepBlocks {
					counts[issue.ID].DependencyCount++
				}
				if d.Type == types.DepParentChild {
					out.Parent[issue.ID] = d.DependsOnID
				}
			}
			for _, d := range depBulk.Incoming[issue.ID] {
				if d.Type == types.DepBlocks {
					counts[issue.ID].DependentCount++
					out.BlockedBy[issue.ID] = append(out.BlockedBy[issue.ID], d.IssueID)
				}
			}
		}
		for from, deps := range depBulk.Outgoing {
			for _, d := range deps {
				if d.Type == types.DepBlocks {
					out.Blocks[from] = append(out.Blocks[from], d.DependsOnID)
				}
			}
		}
	} else if proj.DependencyCounts {
		c, err := u.depRepo.CountsByIssueIDs(ctx, ids, DepCountsOpts{UseWispsTable: useWisp})
		if err != nil {
			return ListResult{}, fmt.Errorf("list: dep counts: %w", err)
		}
		for id, v := range c {
			counts[id] = v
		}
	}

	var commentCounts map[string]int
	if proj.CommentCounts {
		commentCounts, err = u.commentRepo.CountsByIssueIDs(ctx, ids, CommentOpts{UseWispsTable: useWisp})
		if err != nil {
			return ListResult{}, fmt.Errorf("list: comment counts: %w", err)
		}
	}

	if proj.Comments {
		comments, err := u.commentRepo.ListByIssueIDs(ctx, ids, CommentOpts{UseWispsTable: useWisp})
		if err != nil {
			return ListResult{}, fmt.Errorf("list: comments: %w", err)
		}
		for _, issue := range issues {
			issue.Comments = comments[issue.ID]
		}
	}

	for _, issue := range issues {
		c := counts[issue.ID]
		if c == nil {
			c = &types.DependencyCounts{}
		}
		var parentPtr *string
		if p, ok := out.Parent[issue.ID]; ok {
			pp := p
			parentPtr = &pp
		}
		out.Issues = append(out.Issues, &types.IssueWithCounts{
			Issue:           issue,
			DependencyCount: c.DependencyCount,
			DependentCount:  c.DependentCount,
			CommentCount:    commentCounts[issue.ID],
			Parent:          parentPtr,
		})
	}

	return out, nil
}

func (u *issueUseCaseImpl) CreateIssue(ctx context.Context, params CreateIssueParams, actor string) (CreateIssueResult, error) {
	return u.create(ctx, params, actor, false)
}

func (u *issueUseCaseImpl) CreateWisp(ctx context.Context, params CreateIssueParams, actor string) (CreateIssueResult, error) {
	return u.create(ctx, params, actor, true)
}

func (u *issueUseCaseImpl) create(ctx context.Context, params CreateIssueParams, actor string, useWisp bool) (CreateIssueResult, error) {
	if params.Issue == nil {
		return CreateIssueResult{}, fmt.Errorf("create: Issue must not be nil")
	}
	issue := params.Issue

	if issue.Status == "" {
		issue.Status = types.StatusOpen
	}

	switch {
	case params.ExplicitID != "":
		issue.ID = params.ExplicitID
	case params.ParentID != "":
		childID, err := u.counterRepo.NextChildID(ctx, params.ParentID, ChildCounterOpts{UseWispsTable: useWisp})
		if err != nil {
			return CreateIssueResult{}, fmt.Errorf("create: next child ID for %s: %w", params.ParentID, err)
		}
		issue.ID = childID
	case issue.ID == "":
		return CreateIssueResult{}, fmt.Errorf("create: top-level ID generation is not yet implemented; provide ExplicitID or ParentID")
	}

	if params.DiscoveredFromParent != "" {
		if parent, err := u.GetIssue(ctx, params.DiscoveredFromParent); err == nil && parent.SourceRepo != "" {
			issue.SourceRepo = parent.SourceRepo
		}
	}

	insertOpts := InsertIssueOpts{
		SkipPrefixValidation: !params.ForcePrefix,
		UseWispsTable:        useWisp,
	}
	if err := u.issueRepo.Insert(ctx, issue, actor, insertOpts); err != nil {
		return CreateIssueResult{}, fmt.Errorf("create: insert: %w", err)
	}

	result := CreateIssueResult{Issue: issue}

	if params.ParentID != "" {
		pcDep := &types.Dependency{
			IssueID:     issue.ID,
			DependsOnID: params.ParentID,
			Type:        types.DepParentChild,
		}
		if err := u.depRepo.Insert(ctx, pcDep, actor, DepInsertOpts{UseWispsTable: useWisp}); err != nil {
			return result, fmt.Errorf("create: add parent-child dep: %w", err)
		}
		result.PostCreateWrites = true
	}

	if params.InheritLabelsFromParent && params.ParentID != "" {
		parentLabels, err := u.labelRepo.List(ctx, params.ParentID, LabelOpts{UseWispsTable: useWisp})
		if err == nil {
			existing := make(map[string]bool, len(params.Labels))
			for _, l := range params.Labels {
				existing[l] = true
			}
			for _, l := range parentLabels {
				if !existing[l] {
					result.InheritedLabels = append(result.InheritedLabels, l)
				}
			}
		}
	}

	for _, label := range params.Labels {
		if err := u.labelRepo.Insert(ctx, issue.ID, label, actor, LabelOpts{UseWispsTable: useWisp}); err != nil {
			return result, fmt.Errorf("create: add label %s: %w", label, err)
		}
		result.PostCreateWrites = true
	}
	for _, label := range result.InheritedLabels {
		if err := u.labelRepo.Insert(ctx, issue.ID, label, actor, LabelOpts{UseWispsTable: useWisp}); err != nil {
			return result, fmt.Errorf("create: add inherited label %s: %w", label, err)
		}
		result.PostCreateWrites = true
	}

	for _, spec := range params.Dependencies {
		dep := &types.Dependency{
			IssueID:     issue.ID,
			DependsOnID: spec.TargetID,
			Type:        spec.Type,
			Metadata:    spec.Metadata,
		}
		if spec.SwapDirection {
			dep.IssueID, dep.DependsOnID = dep.DependsOnID, dep.IssueID
		}
		if err := u.depRepo.Insert(ctx, dep, actor, DepInsertOpts{UseWispsTable: useWisp}); err != nil {
			return result, fmt.Errorf("create: add dep %s -> %s: %w", dep.IssueID, dep.DependsOnID, err)
		}
		result.PostCreateWrites = true
	}

	if params.WaitsFor != nil {
		gate := params.WaitsFor.Gate
		if gate == "" {
			gate = types.WaitsForAllChildren
		}
		metaJSON, err := json.Marshal(types.WaitsForMeta{Gate: gate})
		if err != nil {
			return result, fmt.Errorf("create: marshal waits-for meta: %w", err)
		}
		dep := &types.Dependency{
			IssueID:     issue.ID,
			DependsOnID: params.WaitsFor.SpawnerID,
			Type:        types.DepWaitsFor,
			Metadata:    string(metaJSON),
		}
		if err := u.depRepo.Insert(ctx, dep, actor, DepInsertOpts{UseWispsTable: useWisp}); err != nil {
			return result, fmt.Errorf("create: add waits-for: %w", err)
		}
		result.PostCreateWrites = true
	}

	return result, nil
}

func (u *issueUseCaseImpl) CreateIssues(ctx context.Context, params []CreateIssueParams, actor string, opts CreateIssuesOpts) (CreateIssuesResult, error) {
	return u.createMany(ctx, params, actor, opts, false)
}

func (u *issueUseCaseImpl) CreateWisps(ctx context.Context, params []CreateIssueParams, actor string, opts CreateIssuesOpts) (CreateIssuesResult, error) {
	return u.createMany(ctx, params, actor, opts, true)
}

func (u *issueUseCaseImpl) createMany(ctx context.Context, params []CreateIssueParams, actor string, _ CreateIssuesOpts, useWisp bool) (CreateIssuesResult, error) {
	result := CreateIssuesResult{}
	for i := range params {
		r, err := u.create(ctx, params[i], actor, useWisp)
		if err != nil {
			return result, fmt.Errorf("createMany[%d]: %w", i, err)
		}
		result.Issues = append(result.Issues, r.Issue)
	}
	return result, nil
}

func (u *issueUseCaseImpl) ApplyIssueGraph(ctx context.Context, plan GraphPlan, actor string) (GraphApplyResult, error) {
	return u.applyGraph(ctx, plan, actor, false)
}

func (u *issueUseCaseImpl) ApplyWispGraph(ctx context.Context, plan GraphPlan, actor string) (GraphApplyResult, error) {
	return u.applyGraph(ctx, plan, actor, true)
}

func (u *issueUseCaseImpl) applyGraph(ctx context.Context, plan GraphPlan, actor string, useWisp bool) (GraphApplyResult, error) {
	keyToID := make(map[string]string, len(plan.Nodes))

	for i, node := range plan.Nodes {
		if node.Issue == nil {
			return GraphApplyResult{}, fmt.Errorf("applyGraph: node %d (key=%q) has nil Issue", i, node.Key)
		}

		parentID := node.ParentID
		if node.ParentKey != "" {
			id, ok := keyToID[node.ParentKey]
			if !ok {
				return GraphApplyResult{}, fmt.Errorf("applyGraph: node %q references undefined parent_key %q (parents must precede children in plan order)", node.Key, node.ParentKey)
			}
			parentID = id
		}

		deferredAssignee := ""
		if node.AssignAfterCreate {
			deferredAssignee = node.Assignee
			node.Issue.Assignee = ""
		} else if node.Assignee != "" {
			node.Issue.Assignee = node.Assignee
		}

		params := CreateIssueParams{
			Issue:    node.Issue,
			ParentID: parentID,
			Labels:   node.Labels,
		}
		r, err := u.create(ctx, params, actor, useWisp)
		if err != nil {
			return GraphApplyResult{}, fmt.Errorf("applyGraph: node %q: %w", node.Key, err)
		}
		keyToID[node.Key] = r.Issue.ID

		if deferredAssignee != "" {
			if err := u.issueRepo.Update(ctx, r.Issue.ID, map[string]any{"assignee": deferredAssignee}, actor, IssueTableOpts{UseWispsTable: useWisp}); err != nil {
				return GraphApplyResult{}, fmt.Errorf("applyGraph: node %q: defer assignee: %w", node.Key, err)
			}
		}
	}

	for i, edge := range plan.Edges {
		fromID := edge.FromID
		if edge.FromKey != "" {
			id, ok := keyToID[edge.FromKey]
			if !ok {
				return GraphApplyResult{}, fmt.Errorf("applyGraph: edge %d references undefined from_key %q", i, edge.FromKey)
			}
			fromID = id
		}
		toID := edge.ToID
		if edge.ToKey != "" {
			id, ok := keyToID[edge.ToKey]
			if !ok {
				return GraphApplyResult{}, fmt.Errorf("applyGraph: edge %d references undefined to_key %q", i, edge.ToKey)
			}
			toID = id
		}
		depType := edge.Type
		if depType == "" {
			depType = types.DepBlocks
		}
		dep := &types.Dependency{
			IssueID:     fromID,
			DependsOnID: toID,
			Type:        depType,
		}
		if err := u.depRepo.Insert(ctx, dep, actor, DepInsertOpts{UseWispsTable: useWisp}); err != nil {
			return GraphApplyResult{}, fmt.Errorf("applyGraph: edge %d (%s -> %s): %w", i, fromID, toID, err)
		}
	}

	return GraphApplyResult{IDs: keyToID}, nil
}
