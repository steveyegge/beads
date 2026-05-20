package db

import (
	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

func (s *testSuite) TestLabelSQLRepository() {
	s.Run("Insert", func() {
		s.Run("RoundTripWithList", s.labelInsertRoundTrip)
		s.Run("IdempotentDuplicate", s.labelInsertIdempotent)
		s.Run("RecordsLabelAddedEvent", s.labelInsertRecordsEvent)
		s.Run("RejectsEmptyIssueID", s.labelInsertEmptyIssueID)
		s.Run("RejectsEmptyLabel", s.labelInsertEmptyLabel)
		s.Run("MissingIssueIDFailsFK", s.labelInsertFKViolation)
	})
	s.Run("List", func() {
		s.Run("OrdersByLabelAlpha", s.labelListAlphaOrder)
		s.Run("UnknownIssueReturnsEmpty", s.labelListUnknown)
	})
	s.Run("ListByIssueIDs", func() {
		s.Run("EmptySliceReturnsEmptyMap", s.labelBulkEmpty)
		s.Run("MultipleIssuesGroupedByID", s.labelBulkGrouped)
		s.Run("MissingIDsAreAbsent", s.labelBulkMissingAbsent)
	})
}

func (s *testSuite) labelRepo() domain.LabelSQLRepository {
	return NewLabelSQLRepository(s.Runner())
}

func (s *testSuite) labelInsertRoundTrip() {
	s.seedIssueRow("bd-lbl-1")
	r := s.labelRepo()
	s.Require().NoError(r.Insert(s.Ctx(), "bd-lbl-1", "tech-debt", "tester"))

	out, err := r.List(s.Ctx(), "bd-lbl-1")
	s.Require().NoError(err)
	s.Equal([]string{"tech-debt"}, out)
}

func (s *testSuite) labelInsertIdempotent() {
	s.seedIssueRow("bd-lbl-dup")
	r := s.labelRepo()
	s.Require().NoError(r.Insert(s.Ctx(), "bd-lbl-dup", "needs-review", "tester"))
	s.Require().NoError(r.Insert(s.Ctx(), "bd-lbl-dup", "needs-review", "tester"))

	out, err := r.List(s.Ctx(), "bd-lbl-dup")
	s.Require().NoError(err)
	s.Equal([]string{"needs-review"}, out, "duplicate label add should be a no-op on the labels table")

	var count int
	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(),
		"SELECT COUNT(*) FROM events WHERE issue_id = ? AND event_type = ?",
		"bd-lbl-dup", string(types.EventLabelAdded),
	).Scan(&count))
	s.Equal(2, count)
}

func (s *testSuite) labelInsertRecordsEvent() {
	s.seedIssueRow("bd-lbl-evt")
	r := s.labelRepo()
	s.Require().NoError(r.Insert(s.Ctx(), "bd-lbl-evt", "perf", "alice"))

	var actor, newValue string
	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(),
		"SELECT actor, new_value FROM events WHERE issue_id = ? AND event_type = ?",
		"bd-lbl-evt", string(types.EventLabelAdded),
	).Scan(&actor, &newValue))
	s.Equal("alice", actor)
	s.Equal("perf", newValue, "event new_value should carry the label name")
}

func (s *testSuite) labelInsertEmptyIssueID() {
	err := s.labelRepo().Insert(s.Ctx(), "", "x", "tester")
	s.Require().Error(err)
}

func (s *testSuite) labelInsertEmptyLabel() {
	err := s.labelRepo().Insert(s.Ctx(), "bd-lbl-x", "", "tester")
	s.Require().Error(err)
}

func (s *testSuite) labelInsertFKViolation() {
	err := s.labelRepo().Insert(s.Ctx(), "bd-no-such-issue", "x", "tester")
	s.Require().Error(err, "expected FK violation when issue_id does not exist")
}

func (s *testSuite) labelListAlphaOrder() {
	s.seedIssueRow("bd-lbl-ord")
	r := s.labelRepo()
	s.Require().NoError(r.Insert(s.Ctx(), "bd-lbl-ord", "zeta", "tester"))
	s.Require().NoError(r.Insert(s.Ctx(), "bd-lbl-ord", "alpha", "tester"))
	s.Require().NoError(r.Insert(s.Ctx(), "bd-lbl-ord", "mu", "tester"))

	out, err := r.List(s.Ctx(), "bd-lbl-ord")
	s.Require().NoError(err)
	s.Equal([]string{"alpha", "mu", "zeta"}, out)
}

func (s *testSuite) labelListUnknown() {
	out, err := s.labelRepo().List(s.Ctx(), "bd-no-labels-here")
	s.Require().NoError(err)
	s.Empty(out)
}

func (s *testSuite) labelBulkEmpty() {
	out, err := s.labelRepo().ListByIssueIDs(s.Ctx(), nil)
	s.Require().NoError(err)
	s.NotNil(out, "ListByIssueIDs should return a non-nil empty map")
	s.Empty(out)
}

func (s *testSuite) labelBulkGrouped() {
	s.seedIssueRow("bd-lbl-bulk-1")
	s.seedIssueRow("bd-lbl-bulk-2")
	r := s.labelRepo()
	s.Require().NoError(r.Insert(s.Ctx(), "bd-lbl-bulk-1", "a", "tester"))
	s.Require().NoError(r.Insert(s.Ctx(), "bd-lbl-bulk-1", "b", "tester"))
	s.Require().NoError(r.Insert(s.Ctx(), "bd-lbl-bulk-2", "c", "tester"))

	out, err := r.ListByIssueIDs(s.Ctx(), []string{"bd-lbl-bulk-1", "bd-lbl-bulk-2"})
	s.Require().NoError(err)
	s.Equal([]string{"a", "b"}, out["bd-lbl-bulk-1"])
	s.Equal([]string{"c"}, out["bd-lbl-bulk-2"])
}

func (s *testSuite) labelBulkMissingAbsent() {
	s.seedIssueRow("bd-lbl-present")
	r := s.labelRepo()
	s.Require().NoError(r.Insert(s.Ctx(), "bd-lbl-present", "x", "tester"))

	out, err := r.ListByIssueIDs(s.Ctx(), []string{"bd-lbl-present", "bd-lbl-missing"})
	s.Require().NoError(err)
	s.Equal([]string{"x"}, out["bd-lbl-present"])
	_, present := out["bd-lbl-missing"]
	s.False(present, "missing issue IDs should not appear in the result map")
}
