package db

import (
	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

func (s *testSuite) TestDependencySQLRepository() {
	s.Run("Insert", func() {
		s.Run("RoundTripVisibleViaList", s.depInsertRoundTrip)
		s.Run("RejectsSelfDependency", s.depInsertSelfDep)
		s.Run("RejectsEmptyIDs", s.depInsertEmptyIDs)
		s.Run("SameTypeIsIdempotentMetadataRefresh", s.depInsertIdempotentSameType)
		s.Run("DifferentTypeIsRejected", s.depInsertConflictingType)
		s.Run("MissingTargetIssueFailsFK", s.depInsertFKViolation)
		s.Run("ThreadIDPersists", s.depInsertThreadID)
	})
	s.Run("HasCycle", func() {
		s.Run("StraightLineIsAcyclic", s.depCycleAcyclic)
		s.Run("DirectBackEdgeDetected", s.depCycleDirectBackEdge)
		s.Run("BackEdgeDetected", s.depCycleBackEdge)
		s.Run("NonBlockingEdgesIgnored", s.depCycleIgnoresNonBlocking)
	})
	s.Run("ListByIssueIDs", func() {
		s.Run("EmptySliceReturnsEmptyMaps", s.depListEmpty)
		s.Run("OutgoingOnly", s.depListOutgoing)
		s.Run("IncomingOnly", s.depListIncoming)
		s.Run("BothDirections", s.depListBoth)
		s.Run("TypeFilterApplied", s.depListTypeFilter)
	})
	s.Run("CountsByIssueIDs", func() {
		s.Run("EmptySliceReturnsEmptyMap", s.depCountsEmpty)
		s.Run("CountsBlockingEdgesOnly", s.depCountsBlocksOnly)
		s.Run("ZeroCountsPresentInMap", s.depCountsZeroPresent)
	})
}

func (s *testSuite) depRepo() domain.DependencySQLRepository {
	return NewDependencySQLRepository(s.Runner())
}

func newDep(issueID, dependsOnID string, t types.DependencyType) *types.Dependency {
	return &types.Dependency{
		IssueID:     issueID,
		DependsOnID: dependsOnID,
		Type:        t,
	}
}

func (s *testSuite) depInsertRoundTrip() {
	s.seedIssueRow("bd-dep-a")
	s.seedIssueRow("bd-dep-b")
	r := s.depRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-dep-a", "bd-dep-b", types.DepBlocks), "tester"))

	out, err := r.ListByIssueIDs(s.Ctx(), []string{"bd-dep-a"}, domain.DepListOpts{Direction: domain.DepDirectionOut})
	s.Require().NoError(err)
	s.Require().Len(out.Outgoing["bd-dep-a"], 1)
	s.Equal("bd-dep-b", out.Outgoing["bd-dep-a"][0].DependsOnID)
	s.Equal(types.DepBlocks, out.Outgoing["bd-dep-a"][0].Type)
}

func (s *testSuite) depInsertSelfDep() {
	s.seedIssueRow("bd-dep-self")
	err := s.depRepo().Insert(s.Ctx(), newDep("bd-dep-self", "bd-dep-self", types.DepBlocks), "tester")
	s.Require().Error(err)
	s.Contains(err.Error(), "cannot depend on itself")
}

func (s *testSuite) depInsertEmptyIDs() {
	r := s.depRepo()
	s.Require().Error(r.Insert(s.Ctx(), newDep("", "bd-x", types.DepBlocks), "tester"))
	s.Require().Error(r.Insert(s.Ctx(), newDep("bd-x", "", types.DepBlocks), "tester"))
}

func (s *testSuite) depInsertIdempotentSameType() {
	s.seedIssueRow("bd-dep-idem-1")
	s.seedIssueRow("bd-dep-idem-2")
	r := s.depRepo()

	dep := newDep("bd-dep-idem-1", "bd-dep-idem-2", types.DepBlocks)
	dep.Metadata = `{"v":1}`
	s.Require().NoError(r.Insert(s.Ctx(), dep, "tester"))

	// Re-add same edge, new metadata. Should refresh, not error.
	dep.Metadata = `{"v":2}`
	s.Require().NoError(r.Insert(s.Ctx(), dep, "tester"))

	out, err := r.ListByIssueIDs(s.Ctx(), []string{"bd-dep-idem-1"}, domain.DepListOpts{Direction: domain.DepDirectionOut})
	s.Require().NoError(err)
	s.Require().Len(out.Outgoing["bd-dep-idem-1"], 1, "duplicate insert should still result in exactly one row")
	s.Equal(`{"v":2}`, out.Outgoing["bd-dep-idem-1"][0].Metadata)
}

func (s *testSuite) depInsertConflictingType() {
	s.seedIssueRow("bd-dep-conf-1")
	s.seedIssueRow("bd-dep-conf-2")
	r := s.depRepo()

	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-dep-conf-1", "bd-dep-conf-2", types.DepBlocks), "tester"))
	err := r.Insert(s.Ctx(), newDep("bd-dep-conf-1", "bd-dep-conf-2", types.DepRelated), "tester")
	s.Require().Error(err)
	s.Contains(err.Error(), "already exists with type")
}

func (s *testSuite) depInsertFKViolation() {
	s.seedIssueRow("bd-dep-src")
	err := s.depRepo().Insert(s.Ctx(), newDep("bd-dep-src", "bd-dep-no-such-target", types.DepBlocks), "tester")
	s.Require().Error(err, "missing target should fail fk_dep_issue_target")
}

func (s *testSuite) depInsertThreadID() {
	s.seedIssueRow("bd-dep-th-1")
	s.seedIssueRow("bd-dep-th-2")
	r := s.depRepo()

	dep := newDep("bd-dep-th-1", "bd-dep-th-2", types.DepRepliesTo)
	dep.ThreadID = "thread-xyz"
	s.Require().NoError(r.Insert(s.Ctx(), dep, "tester"))

	out, err := r.ListByIssueIDs(s.Ctx(), []string{"bd-dep-th-1"}, domain.DepListOpts{Direction: domain.DepDirectionOut})
	s.Require().NoError(err)
	s.Require().Len(out.Outgoing["bd-dep-th-1"], 1)
	s.Equal("thread-xyz", out.Outgoing["bd-dep-th-1"][0].ThreadID)
}

func (s *testSuite) depCycleAcyclic() {
	s.seedIssueRow("bd-cy-a")
	s.seedIssueRow("bd-cy-b")
	s.seedIssueRow("bd-cy-c")
	r := s.depRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-cy-a", "bd-cy-b", types.DepBlocks), "tester"))
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-cy-b", "bd-cy-c", types.DepBlocks), "tester"))

	// Adding a -> c is fine.
	cycle, err := r.HasCycle(s.Ctx(), "bd-cy-a", "bd-cy-c")
	s.Require().NoError(err)
	s.False(cycle)
}

func (s *testSuite) depCycleDirectBackEdge() {
	// Direct (one-hop) back-edge: a blocks b already; adding b -> a closes
	// a 2-cycle. Exercises the indexed point-lookup fast path before the CTE.
	s.seedIssueRow("bd-cy-dir-a")
	s.seedIssueRow("bd-cy-dir-b")
	r := s.depRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-cy-dir-a", "bd-cy-dir-b", types.DepBlocks), "tester"))

	cycle, err := r.HasCycle(s.Ctx(), "bd-cy-dir-b", "bd-cy-dir-a")
	s.Require().NoError(err)
	s.True(cycle, "direct back-edge should detect cycle via fast path")
}

func (s *testSuite) depCycleBackEdge() {
	s.seedIssueRow("bd-cy-back-a")
	s.seedIssueRow("bd-cy-back-b")
	s.seedIssueRow("bd-cy-back-c")
	r := s.depRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-cy-back-a", "bd-cy-back-b", types.DepBlocks), "tester"))
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-cy-back-b", "bd-cy-back-c", types.DepBlocks), "tester"))

	// Adding c -> a would close the cycle a -> b -> c -> a.
	cycle, err := r.HasCycle(s.Ctx(), "bd-cy-back-c", "bd-cy-back-a")
	s.Require().NoError(err)
	s.True(cycle, "expected back-edge to close a cycle")
}

func (s *testSuite) depCycleIgnoresNonBlocking() {
	s.seedIssueRow("bd-cy-rel-a")
	s.seedIssueRow("bd-cy-rel-b")
	r := s.depRepo()
	// related-only edge — not a blocking type, must not contribute to cycle search.
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-cy-rel-a", "bd-cy-rel-b", types.DepRelated), "tester"))

	cycle, err := r.HasCycle(s.Ctx(), "bd-cy-rel-b", "bd-cy-rel-a")
	s.Require().NoError(err)
	s.False(cycle)
}

func (s *testSuite) depListEmpty() {
	out, err := s.depRepo().ListByIssueIDs(s.Ctx(), nil, domain.DepListOpts{})
	s.Require().NoError(err)
	s.NotNil(out.Outgoing)
	s.NotNil(out.Incoming)
	s.Empty(out.Outgoing)
	s.Empty(out.Incoming)
}

func (s *testSuite) depListOutgoing() {
	s.seedIssueRow("bd-lst-out-1")
	s.seedIssueRow("bd-lst-out-2")
	s.seedIssueRow("bd-lst-out-3")
	r := s.depRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-lst-out-1", "bd-lst-out-2", types.DepBlocks), "tester"))
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-lst-out-1", "bd-lst-out-3", types.DepBlocks), "tester"))

	out, err := r.ListByIssueIDs(s.Ctx(), []string{"bd-lst-out-1"}, domain.DepListOpts{Direction: domain.DepDirectionOut})
	s.Require().NoError(err)
	s.Require().Len(out.Outgoing["bd-lst-out-1"], 2)
	s.Empty(out.Incoming, "outgoing-only request should leave Incoming empty")
}

func (s *testSuite) depListIncoming() {
	s.seedIssueRow("bd-lst-in-1")
	s.seedIssueRow("bd-lst-in-2")
	s.seedIssueRow("bd-lst-in-3")
	r := s.depRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-lst-in-2", "bd-lst-in-1", types.DepBlocks), "tester"))
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-lst-in-3", "bd-lst-in-1", types.DepBlocks), "tester"))

	out, err := r.ListByIssueIDs(s.Ctx(), []string{"bd-lst-in-1"}, domain.DepListOpts{Direction: domain.DepDirectionIn})
	s.Require().NoError(err)
	s.Require().Len(out.Incoming["bd-lst-in-1"], 2)
	s.Empty(out.Outgoing)
}

func (s *testSuite) depListBoth() {
	s.seedIssueRow("bd-lst-bo-mid")
	s.seedIssueRow("bd-lst-bo-up")
	s.seedIssueRow("bd-lst-bo-down")
	r := s.depRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-lst-bo-up", "bd-lst-bo-mid", types.DepBlocks), "tester"))
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-lst-bo-mid", "bd-lst-bo-down", types.DepBlocks), "tester"))

	out, err := r.ListByIssueIDs(s.Ctx(), []string{"bd-lst-bo-mid"}, domain.DepListOpts{Direction: domain.DepDirectionBoth})
	s.Require().NoError(err)
	s.Len(out.Outgoing["bd-lst-bo-mid"], 1, "mid -> down should be outgoing")
	s.Equal("bd-lst-bo-down", out.Outgoing["bd-lst-bo-mid"][0].DependsOnID)
	s.Len(out.Incoming["bd-lst-bo-mid"], 1, "up -> mid should be incoming")
	s.Equal("bd-lst-bo-up", out.Incoming["bd-lst-bo-mid"][0].IssueID)
}

func (s *testSuite) depListTypeFilter() {
	s.seedIssueRow("bd-lst-typ-a")
	s.seedIssueRow("bd-lst-typ-b")
	s.seedIssueRow("bd-lst-typ-c")
	r := s.depRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-lst-typ-a", "bd-lst-typ-b", types.DepBlocks), "tester"))
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-lst-typ-a", "bd-lst-typ-c", types.DepRelated), "tester"))

	out, err := r.ListByIssueIDs(s.Ctx(), []string{"bd-lst-typ-a"}, domain.DepListOpts{
		Direction: domain.DepDirectionOut,
		Types:     []types.DependencyType{types.DepBlocks},
	})
	s.Require().NoError(err)
	s.Require().Len(out.Outgoing["bd-lst-typ-a"], 1)
	s.Equal("bd-lst-typ-b", out.Outgoing["bd-lst-typ-a"][0].DependsOnID)
}

func (s *testSuite) depCountsEmpty() {
	out, err := s.depRepo().CountsByIssueIDs(s.Ctx(), nil)
	s.Require().NoError(err)
	s.NotNil(out)
	s.Empty(out)
}

func (s *testSuite) depCountsBlocksOnly() {
	s.seedIssueRow("bd-cnt-mid")
	s.seedIssueRow("bd-cnt-out-1")
	s.seedIssueRow("bd-cnt-out-2")
	s.seedIssueRow("bd-cnt-in-1")
	r := s.depRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-cnt-mid", "bd-cnt-out-1", types.DepBlocks), "tester"))
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-cnt-mid", "bd-cnt-out-2", types.DepBlocks), "tester"))
	// Non-blocking outgoing — must not be counted.
	s.seedIssueRow("bd-cnt-rel-tgt")
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-cnt-mid", "bd-cnt-rel-tgt", types.DepRelated), "tester"))
	// Incoming blocking edge.
	s.Require().NoError(r.Insert(s.Ctx(), newDep("bd-cnt-in-1", "bd-cnt-mid", types.DepBlocks), "tester"))

	out, err := r.CountsByIssueIDs(s.Ctx(), []string{"bd-cnt-mid"})
	s.Require().NoError(err)
	s.Require().NotNil(out["bd-cnt-mid"])
	s.Equal(2, out["bd-cnt-mid"].DependencyCount, "outgoing blocks only")
	s.Equal(1, out["bd-cnt-mid"].DependentCount, "incoming blocks only")
}

func (s *testSuite) depCountsZeroPresent() {
	s.seedIssueRow("bd-cnt-zero")
	out, err := s.depRepo().CountsByIssueIDs(s.Ctx(), []string{"bd-cnt-zero"})
	s.Require().NoError(err)
	s.Require().NotNil(out["bd-cnt-zero"], "issues with zero deps should still appear with zero counts")
	s.Equal(0, out["bd-cnt-zero"].DependencyCount)
	s.Equal(0, out["bd-cnt-zero"].DependentCount)
}
