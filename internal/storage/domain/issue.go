package domain

import (
	"context"

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
	List(ctx context.Context, filter types.IssueFilter, proj ListProjection) (ListResult, error)
	CreateIssue(ctx context.Context, params CreateIssueParams, actor string) (CreateIssueResult, error)
	CreateIssues(ctx context.Context, params []CreateIssueParams, actor string, opts CreateIssuesOpts) (CreateIssuesResult, error)
	UpdateIssue(ctx context.Context, id string, updates map[string]any, actor string) error
	ApplyGraph(ctx context.Context, plan GraphPlan, actor string) (GraphApplyResult, error)
}

func NewIssueUseCase(
	issueRepo IssueSQLRepository,
	depRepo DependencySQLRepository,
	labelRepo LabelSQLRepository,
	counterRepo ChildCounterSQLRepository,
	commentRepo CommentSQLRepository,
	cfgRepo ConfigSQLRepository,
) IssueUseCase {
	return nil
}
