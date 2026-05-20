package domain

import (
	"context"

	"github.com/steveyegge/beads/internal/types"
)

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

type CreateIssueUseCase interface {
	CreateIssue(ctx context.Context, params CreateIssueParams, actor string) (CreateIssueResult, error)
	CreateIssues(ctx context.Context, params []CreateIssueParams, actor string, opts CreateIssuesOpts) (CreateIssuesResult, error)
}

func NewCreateIssueUseCase(
	issueRepo IssueSQLRepository,
	depRepo DependencySQLRepository,
	labelRepo LabelSQLRepository,
	counterRepo ChildCounterSQLRepository,
	cfgRepo ConfigSQLRepository,
) CreateIssueUseCase {
	return nil
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

type GraphApplyUseCase interface {
	Apply(ctx context.Context, plan GraphPlan, actor string) (GraphApplyResult, error)
}

func NewGraphApplyUseCase(
	createUC CreateIssueUseCase,
	issueRepo IssueSQLRepository,
	depRepo DependencySQLRepository,
	labelRepo LabelSQLRepository,
) GraphApplyUseCase {
	return nil
}
