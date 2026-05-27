package db

import (
	"database/sql"
	"strings"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

func (s *testSuite) TestIssueUseCase_MintTopLevelID() {
	s.Run("HashModeMintsBdPrefixedID", s.useCaseMintHashMode)
	s.Run("CounterModeMintsSequentialID", s.useCaseMintCounterMode)
	s.Run("CounterModeUnavailableForWisps", s.useCaseMintWispIgnoresCounterMode)
	s.Run("IssuePrefixOverrideHonored", s.useCaseMintRespectsPrefixOverride)
	s.Run("IDPrefixSubprefixHonored", s.useCaseMintRespectsIDPrefix)
	s.Run("WispUsesWispPrefix", s.useCaseMintWispPrefix)
	s.Run("MissingConfigPrefixErrors", s.useCaseMintMissingPrefix)
}

func (s *testSuite) issueUseCase() domain.IssueUseCase {
	runner := s.Runner()
	return domain.NewIssueUseCase(
		NewIssueSQLRepository(runner),
		NewDependencySQLRepository(runner),
		NewLabelSQLRepository(runner),
		NewChildCounterSQLRepository(runner),
		NewCommentSQLRepository(runner),
		NewConfigSQLRepository(runner),
	)
}

// resetMintConfig seeds the two config keys the mint path reads, so each
// subtest sees a clean state. testify's suite SetupTest doesn't run between
// s.Run subtests within a single Test method, so we explicitly reset.
func (s *testSuite) resetMintConfig(prefix, idMode string) {
	r := NewConfigSQLRepository(s.Runner())
	s.Require().NoError(r.SetConfig(s.Ctx(), "issue_prefix", prefix))
	s.Require().NoError(r.SetConfig(s.Ctx(), "issue_id_mode", idMode))
}

func (s *testSuite) useCaseMintHashMode() {
	s.resetMintConfig("bd", "")
	uc := s.issueUseCase()

	res, err := uc.CreateIssue(s.Ctx(), domain.CreateIssueParams{
		Issue: &types.Issue{
			Title:     "fresh top-level",
			IssueType: types.TypeTask,
			Priority:  2,
		},
	}, "tester")
	s.Require().NoError(err)

	s.Require().NotEmpty(res.Issue.ID)
	s.True(strings.HasPrefix(res.Issue.ID, "bd-"), "expected bd- prefix, got %q", res.Issue.ID)
	suffix := strings.TrimPrefix(res.Issue.ID, "bd-")
	s.NotContains(suffix, ".", "hash IDs must not contain '.'")
	s.True(len(suffix) >= 3 && len(suffix) <= 8, "expected base36 hash 3..8 chars, got %q", suffix)
}

func (s *testSuite) useCaseMintCounterMode() {
	// Use a unique prefix so the counter row doesn't collide with earlier
	// subtests that exercise NextCounterID directly under a different name.
	s.resetMintConfig("ucCnt", "counter")
	uc := s.issueUseCase()

	first, err := uc.CreateIssue(s.Ctx(), domain.CreateIssueParams{
		Issue: &types.Issue{Title: "one", IssueType: types.TypeTask, Priority: 2},
	}, "tester")
	s.Require().NoError(err)
	s.Equal("ucCnt-1", first.Issue.ID)

	second, err := uc.CreateIssue(s.Ctx(), domain.CreateIssueParams{
		Issue: &types.Issue{Title: "two", IssueType: types.TypeTask, Priority: 2},
	}, "tester")
	s.Require().NoError(err)
	s.Equal("ucCnt-2", second.Issue.ID)
}

func (s *testSuite) useCaseMintWispIgnoresCounterMode() {
	// Even with counter mode enabled, wisps must hash-mint because there is
	// no wisp_counter table. This locks in the embedded contract.
	s.resetMintConfig("ucWcnt", "counter")
	uc := s.issueUseCase()

	res, err := uc.CreateWisp(s.Ctx(), domain.CreateIssueParams{
		Issue: &types.Issue{
			Title:     "wisp",
			IssueType: types.TypeTask,
			Priority:  2,
			Ephemeral: true,
		},
	}, "tester")
	s.Require().NoError(err)
	s.True(strings.HasPrefix(res.Issue.ID, "ucWcnt-wisp-"), "expected ucWcnt-wisp- prefix, got %q", res.Issue.ID)
}

func (s *testSuite) useCaseMintRespectsPrefixOverride() {
	s.resetMintConfig("bd", "")
	uc := s.issueUseCase()

	res, err := uc.CreateIssue(s.Ctx(), domain.CreateIssueParams{
		Issue: &types.Issue{
			Title:          "overridden",
			IssueType:      types.TypeTask,
			Priority:       2,
			PrefixOverride: "spec",
		},
	}, "tester")
	s.Require().NoError(err)
	s.True(strings.HasPrefix(res.Issue.ID, "spec-"), "expected override prefix, got %q", res.Issue.ID)
}

func (s *testSuite) useCaseMintRespectsIDPrefix() {
	s.resetMintConfig("bd", "")
	uc := s.issueUseCase()

	res, err := uc.CreateIssue(s.Ctx(), domain.CreateIssueParams{
		Issue: &types.Issue{
			Title:     "subprefixed",
			IssueType: types.TypeTask,
			Priority:  2,
			IDPrefix:  "exp",
		},
	}, "tester")
	s.Require().NoError(err)
	s.True(strings.HasPrefix(res.Issue.ID, "bd-exp-"), "expected bd-exp- subprefix, got %q", res.Issue.ID)
}

func (s *testSuite) useCaseMintWispPrefix() {
	s.resetMintConfig("bd", "")
	uc := s.issueUseCase()

	res, err := uc.CreateWisp(s.Ctx(), domain.CreateIssueParams{
		Issue: &types.Issue{
			Title:     "wispy",
			IssueType: types.TypeTask,
			Priority:  2,
			Ephemeral: true,
		},
	}, "tester")
	s.Require().NoError(err)
	s.True(strings.HasPrefix(res.Issue.ID, "bd-wisp-"), "expected bd-wisp- prefix, got %q", res.Issue.ID)
}

func (s *testSuite) useCaseMintMissingPrefix() {
	// Explicitly clear issue_prefix; prior subtests have seeded "bd"/"ucCnt"
	// etc. and that state persists across s.Run subtests.
	s.resetMintConfig("", "")
	uc := s.issueUseCase()
	_, err := uc.CreateIssue(s.Ctx(), domain.CreateIssueParams{
		Issue: &types.Issue{Title: "no prefix", IssueType: types.TypeTask, Priority: 2},
	}, "tester")
	s.Require().Error(err)
	s.Contains(err.Error(), "issue_prefix")
}

func (s *testSuite) TestIssueUseCase_ApplyGraph() {
	s.Run("ChildrenBeforeParentsSucceed", s.applyGraphChildrenBeforeParents)
	s.Run("ExplicitParentChildEdgeIsDeduped", s.applyGraphParentChildEdgeDedup)
	s.Run("DifferentTypeOverParentChildPairErrors", s.applyGraphDifferentTypeOverPair)
	s.Run("ReverseBlockingOverParentChildPairErrors", s.applyGraphReverseBlocking)
	s.Run("LiveCycleThroughExistingDepsErrors", s.applyGraphLiveCycle)
	s.Run("HealthyPlanRoundTrips", s.applyGraphHealthy)
	s.Run("WispGraphRoutesToWispTables", s.applyGraphWispRouting)
}

// TestIssueUseCase_MixedParentChildRouting covers maphew's P1 review on the
// child-counter and dependency target-column routing fixes:
//   - WispChildOfRegularParent: --ephemeral with --parent <regular issue>
//   - DepTargetClassification: wisp / regular / external targets each land
//     in their typed column on dep insert.
func (s *testSuite) TestIssueUseCase_MixedParentChildRouting() {
	s.Run("WispChildOfRegularParent", s.mixedWispChildOfRegularParent)
	s.Run("DepTargetClassification", s.mixedDepTargetClassification)
}

// mixedWispChildOfRegularParent verifies that creating a wisp child under a
// regular parent routes the child-counter row to child_counters (the parent's
// table), not wisp_child_counters. Pre-fix this called wisp_child_counters
// with parent_id = <regular issue id>, which either FK-failed or recorded the
// counter in the wrong table.
func (s *testSuite) mixedWispChildOfRegularParent() {
	s.resetMintConfig("mw", "")
	uc := s.issueUseCase()

	// Create a regular epic parent.
	pRes, err := uc.CreateIssue(s.Ctx(), domain.CreateIssueParams{
		Issue: &types.Issue{Title: "regular parent", IssueType: types.TypeEpic, Priority: 2},
	}, "tester")
	s.Require().NoError(err)
	parentID := pRes.Issue.ID

	// Now create a wisp child of the regular parent.
	cRes, err := uc.CreateWisp(s.Ctx(), domain.CreateIssueParams{
		Issue:    &types.Issue{Title: "wisp child", IssueType: types.TypeTask, Priority: 2, Ephemeral: true},
		ParentID: parentID,
	}, "tester")
	s.Require().NoError(err)
	childID := cRes.Issue.ID
	s.True(strings.HasPrefix(childID, parentID+"."), "child ID %q should start with %q.", childID, parentID)

	// Counter row must live in child_counters (parent's table), not
	// wisp_child_counters. Pre-fix the call routed by the child's useWisp.
	var regularCounter, wispCounter int
	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(),
		"SELECT COUNT(*) FROM child_counters WHERE parent_id = ?", parentID).Scan(&regularCounter))
	s.Equal(1, regularCounter, "regular parent's counter must land in child_counters")
	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(),
		"SELECT COUNT(*) FROM wisp_child_counters WHERE parent_id = ?", parentID).Scan(&wispCounter))
	s.Equal(0, wispCounter, "regular parent's counter must NOT land in wisp_child_counters")

	// The wisp child itself lives in wisps; the regular parent in issues.
	var wispChildExists, regularParentExists int
	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(),
		"SELECT COUNT(*) FROM wisps WHERE id = ?", childID).Scan(&wispChildExists))
	s.Equal(1, wispChildExists, "wisp child must live in wisps table")
	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(),
		"SELECT COUNT(*) FROM issues WHERE id = ?", parentID).Scan(&regularParentExists))
	s.Equal(1, regularParentExists, "regular parent must live in issues table")

	// Parent-child dep: written by domain.create() with useWisp=true →
	// wisp_dependencies. Target is a regular issue → depends_on_issue_id.
	deps := s.loadDepRows("wisp_dependencies", "mw-%")
	s.Require().Len(deps, 1)
	s.Equal(childID, deps[0].issueID)
	s.Equal(parentID, deps[0].dependsOnID)
	s.Equal(string(types.DepParentChild), deps[0].depType)
	s.Equal("depends_on_issue_id", deps[0].targetColumn(),
		"regular-issue target must use depends_on_issue_id, got %+v", deps[0])
}

// mixedDepTargetClassification covers maphew's P1 #2: dependency Insert must
// pick the right typed column based on the target's kind. We construct three
// edges from a single source issue to: a regular issue, a wisp, and an
// external ref. Each must land in its respective column.
func (s *testSuite) mixedDepTargetClassification() {
	s.resetMintConfig("dx", "")
	uc := s.issueUseCase()
	depRepo := NewDependencySQLRepository(s.Runner())

	// Source + regular issue target.
	src, err := uc.CreateIssue(s.Ctx(), domain.CreateIssueParams{
		Issue: &types.Issue{Title: "source", IssueType: types.TypeTask, Priority: 2},
	}, "tester")
	s.Require().NoError(err)
	regular, err := uc.CreateIssue(s.Ctx(), domain.CreateIssueParams{
		Issue: &types.Issue{Title: "regular target", IssueType: types.TypeTask, Priority: 2},
	}, "tester")
	s.Require().NoError(err)
	wisp, err := uc.CreateWisp(s.Ctx(), domain.CreateIssueParams{
		Issue: &types.Issue{Title: "wisp target", IssueType: types.TypeTask, Priority: 2, Ephemeral: true},
	}, "tester")
	s.Require().NoError(err)

	// Use "related" so the writes don't trip the epic/task blocking rules.
	s.Require().NoError(depRepo.Insert(s.Ctx(), &types.Dependency{
		IssueID: src.Issue.ID, DependsOnID: regular.Issue.ID, Type: types.DepRelated,
	}, "tester", domain.DepInsertOpts{}))
	s.Require().NoError(depRepo.Insert(s.Ctx(), &types.Dependency{
		IssueID: src.Issue.ID, DependsOnID: wisp.Issue.ID, Type: types.DepRelated,
	}, "tester", domain.DepInsertOpts{}))
	s.Require().NoError(depRepo.Insert(s.Ctx(), &types.Dependency{
		IssueID: src.Issue.ID, DependsOnID: "external:GH-42", Type: types.DepRelated,
	}, "tester", domain.DepInsertOpts{}))

	deps := s.loadDepRows("dependencies", "dx-%")
	s.Require().Len(deps, 3)

	byTarget := make(map[string]depRow, len(deps))
	for _, d := range deps {
		byTarget[d.dependsOnID] = d
	}
	s.Equal("depends_on_issue_id", byTarget[regular.Issue.ID].targetColumn(),
		"regular target must use depends_on_issue_id, got %+v", byTarget[regular.Issue.ID])
	s.Equal("depends_on_wisp_id", byTarget[wisp.Issue.ID].targetColumn(),
		"wisp target must use depends_on_wisp_id, got %+v", byTarget[wisp.Issue.ID])
	s.Equal("depends_on_external", byTarget["external:GH-42"].targetColumn(),
		"external: target must use depends_on_external, got %+v", byTarget["external:GH-42"])
}

// depRow models a single row from the dependencies / wisp_dependencies table
// for direct verification inside tests.
type depRow struct {
	issueID      string
	dependsOnID  string
	depType      string
	depsOnIssue  sql.NullString
	depsOnWisp   sql.NullString
	depsOnExtern sql.NullString
}

// targetColumn returns the typed column the row was written into. Used by
// wisp-routing tests to assert deps land in depends_on_wisp_id when the
// target is a wisp, not depends_on_issue_id (regression for maphew's review).
func (r depRow) targetColumn() string {
	switch {
	case r.depsOnIssue.Valid:
		return "depends_on_issue_id"
	case r.depsOnWisp.Valid:
		return "depends_on_wisp_id"
	case r.depsOnExtern.Valid:
		return "depends_on_external"
	default:
		return ""
	}
}

func (s *testSuite) loadDepRows(table, prefixLike string) []depRow {
	rows, err := s.Runner().QueryContext(s.Ctx(),
		//nolint:gosec // G201: table is one of two hardcoded constants for tests
		"SELECT issue_id, COALESCE(depends_on_issue_id, depends_on_wisp_id, depends_on_external) AS depends_on_id, type, depends_on_issue_id, depends_on_wisp_id, depends_on_external FROM "+table+" WHERE issue_id LIKE ? OR depends_on_issue_id LIKE ? OR depends_on_wisp_id LIKE ? OR depends_on_external LIKE ? ORDER BY issue_id, depends_on_id, type",
		prefixLike, prefixLike, prefixLike, prefixLike,
	)
	s.Require().NoError(err)
	defer rows.Close()
	var out []depRow
	for rows.Next() {
		var r depRow
		s.Require().NoError(rows.Scan(&r.issueID, &r.dependsOnID, &r.depType, &r.depsOnIssue, &r.depsOnWisp, &r.depsOnExtern))
		out = append(out, r)
	}
	s.Require().NoError(rows.Err())
	return out
}

func newGraphNode(key, title string) domain.GraphNode {
	return domain.GraphNode{
		Key: key,
		Issue: &types.Issue{
			Title:     title,
			IssueType: types.TypeTask,
			Priority:  2,
		},
	}
}

func (s *testSuite) applyGraphChildrenBeforeParents() {
	s.resetMintConfig("gA", "")
	uc := s.issueUseCase()

	// Child appears first; parent reference must still resolve.
	child := newGraphNode("child", "child node")
	child.ParentKey = "parent"
	parent := newGraphNode("parent", "parent node")

	res, err := uc.ApplyIssueGraph(s.Ctx(), domain.GraphPlan{
		Nodes: []domain.GraphNode{child, parent},
	}, "tester")
	s.Require().NoError(err)
	s.Require().Len(res.IDs, 2)

	childID := res.IDs["child"]
	parentID := res.IDs["parent"]
	s.True(strings.HasPrefix(childID, "gA-"), "child should mint a top-level gA- ID, got %q", childID)
	s.True(strings.HasPrefix(parentID, "gA-"), "parent should mint a top-level gA- ID, got %q", parentID)
	s.NotContains(childID, ".", "graph child should not get a counter-style ID")

	deps := s.loadDepRows("dependencies", "gA-%")
	s.Require().Len(deps, 1, "expected one parent-child dep, got %d: %+v", len(deps), deps)
	s.Equal(childID, deps[0].issueID)
	s.Equal(parentID, deps[0].dependsOnID)
	s.Equal(string(types.DepParentChild), deps[0].depType)
}

func (s *testSuite) applyGraphParentChildEdgeDedup() {
	s.resetMintConfig("gB", "")
	uc := s.issueUseCase()

	child := newGraphNode("child", "child")
	child.ParentKey = "parent"
	parent := newGraphNode("parent", "parent")

	// Explicit parent-child edge over the same pair the node already
	// implies — applyGraph should silently skip it and the parent-child
	// pass should insert exactly one row.
	res, err := uc.ApplyIssueGraph(s.Ctx(), domain.GraphPlan{
		Nodes: []domain.GraphNode{parent, child},
		Edges: []domain.GraphEdge{{
			FromKey: "child",
			ToKey:   "parent",
			Type:    types.DepParentChild,
		}},
	}, "tester")
	s.Require().NoError(err)

	deps := s.loadDepRows("dependencies", "gB-%")
	s.Require().Len(deps, 1, "explicit parent-child edge duplicating an implicit pair must collapse to one row")
	s.Equal(string(types.DepParentChild), deps[0].depType)
	s.Equal(res.IDs["child"], deps[0].issueID)
	s.Equal(res.IDs["parent"], deps[0].dependsOnID)
}

func (s *testSuite) applyGraphDifferentTypeOverPair() {
	s.resetMintConfig("gC", "")
	uc := s.issueUseCase()

	child := newGraphNode("child", "child")
	child.ParentKey = "parent"
	parent := newGraphNode("parent", "parent")

	// Edge in the same direction as the parent-child pair but with a
	// different dep type — must error before any deps are written.
	_, err := uc.ApplyIssueGraph(s.Ctx(), domain.GraphPlan{
		Nodes: []domain.GraphNode{parent, child},
		Edges: []domain.GraphEdge{{
			FromKey: "child",
			ToKey:   "parent",
			Type:    types.DepBlocks,
		}},
	}, "tester")
	s.Require().Error(err)
	s.Contains(err.Error(), "duplicates a parent-child relationship")

	deps := s.loadDepRows("dependencies", "gC-%")
	s.Empty(deps, "no deps should be written when the plan is rejected before pass 2")
}

func (s *testSuite) applyGraphReverseBlocking() {
	s.resetMintConfig("gD", "")
	uc := s.issueUseCase()

	child := newGraphNode("child", "child")
	child.ParentKey = "parent"
	parent := newGraphNode("parent", "parent")

	// Reverse direction (parent blocks child) is forbidden because it would
	// create a cycle once the parent-child link is inserted.
	_, err := uc.ApplyIssueGraph(s.Ctx(), domain.GraphPlan{
		Nodes: []domain.GraphNode{parent, child},
		Edges: []domain.GraphEdge{{
			FromKey: "parent",
			ToKey:   "child",
			Type:    types.DepBlocks,
		}},
	}, "tester")
	s.Require().Error(err)
	s.Contains(err.Error(), "creates a blocking reverse")

	deps := s.loadDepRows("dependencies", "gD-%")
	s.Empty(deps)
}

func (s *testSuite) applyGraphLiveCycle() {
	s.resetMintConfig("gE", "")
	uc := s.issueUseCase()

	// Seed two existing issues with an existing blocks dep: X blocks Y.
	issueRepo := NewIssueSQLRepository(s.Runner())
	x := newTestIssue("gE-existing-x", "existing X")
	y := newTestIssue("gE-existing-y", "existing Y")
	s.Require().NoError(issueRepo.Insert(s.Ctx(), x, "seeder", domain.InsertIssueOpts{}))
	s.Require().NoError(issueRepo.Insert(s.Ctx(), y, "seeder", domain.InsertIssueOpts{}))

	depRepo := NewDependencySQLRepository(s.Runner())
	s.Require().NoError(depRepo.Insert(s.Ctx(), &types.Dependency{
		IssueID:     "gE-existing-x",
		DependsOnID: "gE-existing-y",
		Type:        types.DepBlocks,
	}, "seeder", domain.DepInsertOpts{}))

	// Plan creates parent + child with planned edges that, combined with
	// the existing X→Y dep, form a path parent → X → Y → child. The
	// parent-child link about to be inserted would then close a cycle.
	parent := newGraphNode("parent", "new parent")
	child := newGraphNode("child", "new child")
	child.ParentKey = "parent"

	_, err := uc.ApplyIssueGraph(s.Ctx(), domain.GraphPlan{
		Nodes: []domain.GraphNode{parent, child},
		Edges: []domain.GraphEdge{
			{FromKey: "parent", ToID: "gE-existing-x", Type: types.DepBlocks},
			{FromID: "gE-existing-y", ToKey: "child", Type: types.DepBlocks},
		},
	}, "tester")
	s.Require().Error(err)
	s.Contains(err.Error(), "planned blocking dependencies create a path from parent")

	// No parent-child dep should have landed.
	deps := s.loadDepRows("dependencies", "gE-%")
	for _, d := range deps {
		s.NotEqual(string(types.DepParentChild), d.depType, "parent-child dep must not be written when live cycle detected: %+v", d)
	}
}

func (s *testSuite) applyGraphHealthy() {
	s.resetMintConfig("gF", "")
	uc := s.issueUseCase()

	parent := newGraphNode("p", "parent")
	child := newGraphNode("c", "child")
	child.ParentKey = "p"
	sibling := newGraphNode("s", "sibling")

	res, err := uc.ApplyIssueGraph(s.Ctx(), domain.GraphPlan{
		Nodes: []domain.GraphNode{parent, child, sibling},
		Edges: []domain.GraphEdge{{
			FromKey: "c",
			ToKey:   "s",
			Type:    types.DepRelated,
		}},
	}, "tester")
	s.Require().NoError(err)
	s.Len(res.IDs, 3)

	deps := s.loadDepRows("dependencies", "gF-%")
	s.Require().Len(deps, 2)
	var pcSeen, relSeen bool
	for _, d := range deps {
		switch d.depType {
		case string(types.DepParentChild):
			pcSeen = true
			s.Equal(res.IDs["c"], d.issueID)
			s.Equal(res.IDs["p"], d.dependsOnID)
		case string(types.DepRelated):
			relSeen = true
			s.Equal(res.IDs["c"], d.issueID)
			s.Equal(res.IDs["s"], d.dependsOnID)
		}
	}
	s.True(pcSeen, "expected parent-child dep")
	s.True(relSeen, "expected related dep")
}

func (s *testSuite) applyGraphWispRouting() {
	s.resetMintConfig("gG", "")
	uc := s.issueUseCase()

	parent := newGraphNode("p", "parent wisp")
	parent.Issue.Ephemeral = true
	child := newGraphNode("c", "child wisp")
	child.Issue.Ephemeral = true
	child.ParentKey = "p"

	res, err := uc.ApplyWispGraph(s.Ctx(), domain.GraphPlan{
		Nodes: []domain.GraphNode{child, parent},
	}, "tester")
	s.Require().NoError(err)
	s.Require().Len(res.IDs, 2)
	s.True(strings.HasPrefix(res.IDs["p"], "gG-wisp-"), "wisp parent should carry the -wisp suffix, got %q", res.IDs["p"])
	s.True(strings.HasPrefix(res.IDs["c"], "gG-wisp-"), "wisp child should carry the -wisp suffix, got %q", res.IDs["c"])

	// Verify no rows landed in the regular tables.
	regular := s.loadDepRows("dependencies", "gG-%")
	s.Empty(regular, "no deps should appear in dependencies table for a wisp graph")
	regularIssues := 0
	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(), "SELECT COUNT(*) FROM issues WHERE id LIKE ?", "gG-%").Scan(&regularIssues))
	s.Equal(0, regularIssues, "wisp graph issues must not land in `issues`")

	// And the wisp tables have the expected shape.
	wispDeps := s.loadDepRows("wisp_dependencies", "gG-%")
	s.Require().Len(wispDeps, 1)
	s.Equal(string(types.DepParentChild), wispDeps[0].depType)
	s.Equal(res.IDs["c"], wispDeps[0].issueID)
	s.Equal(res.IDs["p"], wispDeps[0].dependsOnID)
	// Wisp→wisp edge must land in depends_on_wisp_id, not depends_on_issue_id.
	// Maphew's P1 review: prior code unconditionally wrote depends_on_issue_id,
	// which would break FK/cascade/rename and wisp-aware dep filters.
	s.Equal("depends_on_wisp_id", wispDeps[0].targetColumn(),
		"wisp-target dep must use depends_on_wisp_id, got %+v", wispDeps[0])
}
