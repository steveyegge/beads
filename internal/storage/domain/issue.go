package domain

import (
	"context"

	"github.com/steveyegge/beads/internal/types"
)

// ===== Repository =====

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

// IssueTableOpts pivots Update/Get/GetByIDs/Search to the wisps partition.
// Issue and wisp rows live in independent tables with the same column shape,
// so the same SQL works against either — only the table name changes.
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

// ===== Use-case parameter and result types =====

// CreateIssueParams carries everything cmd/bd needs to land one issue plus its
// initial labels, dependencies, and parent-child wiring. The use case owns ID
// generation, prefix validation, parent label inheritance, and (where wired)
// audit events; the repository just writes rows.
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

// ListProjection turns hydration on/off for bd list. Each field maps to
// (at most) one bulk fetch; a field that's already implied by another (e.g.
// DependencyCounts when Dependencies is set) is free.
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

// GraphPlan is bd create --graph's parsed plan: a set of keyed nodes plus
// edges referencing those keys (or absolute IDs). ApplyGraph resolves keys
// to generated IDs as it inserts.
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

// ===== Use case =====

// IssueUseCase is the single entry point for all issue-shaped operations.
// It composes the issue, dependency, label, comment, child-counter, and
// config repositories so the CLI layer doesn't have to thread them
// individually.
type IssueUseCase interface {
	// Reads.
	GetIssue(ctx context.Context, id string) (*types.Issue, error)
	GetIssuesByIDs(ctx context.Context, ids []string) ([]*types.Issue, error)
	List(ctx context.Context, filter types.IssueFilter, proj ListProjection) (ListResult, error)

	// Writes.
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
