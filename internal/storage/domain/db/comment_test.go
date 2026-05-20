package db

import (
	"time"

	"github.com/google/uuid"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

func (s *testSuite) TestCommentSQLRepository() {
	s.Run("CountsByIssueIDs", func() {
		s.Run("EmptySliceReturnsEmptyMap", s.commentCountsEmpty)
		s.Run("AggregatesPerIssue", s.commentCountsAggregates)
		s.Run("MissingIssuesAbsentFromResult", s.commentCountsMissingAbsent)
	})
	s.Run("ListByIssueIDs", func() {
		s.Run("EmptySliceReturnsEmptyMap", s.commentListEmpty)
		s.Run("OrdersByCreatedAtAscending", s.commentListOrdered)
		s.Run("GroupsByIssueID", s.commentListGrouped)
		s.Run("FieldsRoundTrip", s.commentListRoundTrip)
		s.Run("MissingIssueAbsent", s.commentListMissingAbsent)
	})
}

func (s *testSuite) commentRepo() domain.CommentSQLRepository {
	return NewCommentSQLRepository(s.Runner())
}

func (s *testSuite) seedComment(issueID, author, text string, createdAt time.Time) string {
	id := uuid.Must(uuid.NewV7()).String()
	_, err := s.Runner().ExecContext(s.Ctx(), `
		INSERT INTO comments (id, issue_id, author, text, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, id, issueID, author, text, createdAt.UTC())
	s.Require().NoError(err)
	return id
}

func (s *testSuite) commentCountsEmpty() {
	out, err := s.commentRepo().CountsByIssueIDs(s.Ctx(), nil)
	s.Require().NoError(err)
	s.NotNil(out)
	s.Empty(out)
}

func (s *testSuite) commentCountsAggregates() {
	s.seedIssueRow("bd-cmt-cnt-1")
	s.seedIssueRow("bd-cmt-cnt-2")
	now := time.Now().UTC()
	s.seedComment("bd-cmt-cnt-1", "alice", "first", now)
	s.seedComment("bd-cmt-cnt-1", "alice", "second", now.Add(time.Second))
	s.seedComment("bd-cmt-cnt-1", "bob", "third", now.Add(2*time.Second))
	s.seedComment("bd-cmt-cnt-2", "alice", "only", now)

	out, err := s.commentRepo().CountsByIssueIDs(s.Ctx(), []string{"bd-cmt-cnt-1", "bd-cmt-cnt-2"})
	s.Require().NoError(err)
	s.Equal(3, out["bd-cmt-cnt-1"])
	s.Equal(1, out["bd-cmt-cnt-2"])
}

func (s *testSuite) commentCountsMissingAbsent() {
	s.seedIssueRow("bd-cmt-cnt-real")
	s.seedComment("bd-cmt-cnt-real", "alice", "x", time.Now().UTC())

	out, err := s.commentRepo().CountsByIssueIDs(s.Ctx(), []string{"bd-cmt-cnt-real", "bd-cmt-cnt-ghost"})
	s.Require().NoError(err)
	s.Equal(1, out["bd-cmt-cnt-real"])
	_, present := out["bd-cmt-cnt-ghost"]
	s.False(present, "issues with zero comments should not appear in the count map")
}

func (s *testSuite) commentListEmpty() {
	out, err := s.commentRepo().ListByIssueIDs(s.Ctx(), nil)
	s.Require().NoError(err)
	s.NotNil(out)
	s.Empty(out)
}

func (s *testSuite) commentListOrdered() {
	s.seedIssueRow("bd-cmt-ord")
	base := time.Now().UTC().Truncate(time.Second)
	s.seedComment("bd-cmt-ord", "a", "third", base.Add(2*time.Second))
	s.seedComment("bd-cmt-ord", "a", "first", base)
	s.seedComment("bd-cmt-ord", "a", "second", base.Add(time.Second))

	out, err := s.commentRepo().ListByIssueIDs(s.Ctx(), []string{"bd-cmt-ord"})
	s.Require().NoError(err)
	s.Require().Len(out["bd-cmt-ord"], 3)
	s.Equal("first", out["bd-cmt-ord"][0].Text)
	s.Equal("second", out["bd-cmt-ord"][1].Text)
	s.Equal("third", out["bd-cmt-ord"][2].Text)
}

func (s *testSuite) commentListGrouped() {
	s.seedIssueRow("bd-cmt-grp-1")
	s.seedIssueRow("bd-cmt-grp-2")
	now := time.Now().UTC()
	s.seedComment("bd-cmt-grp-1", "a", "one-a", now)
	s.seedComment("bd-cmt-grp-1", "b", "one-b", now.Add(time.Second))
	s.seedComment("bd-cmt-grp-2", "a", "two-a", now)

	out, err := s.commentRepo().ListByIssueIDs(s.Ctx(), []string{"bd-cmt-grp-1", "bd-cmt-grp-2"})
	s.Require().NoError(err)
	s.Len(out["bd-cmt-grp-1"], 2)
	s.Len(out["bd-cmt-grp-2"], 1)
	s.Equal("two-a", out["bd-cmt-grp-2"][0].Text)
}

func (s *testSuite) commentListRoundTrip() {
	s.seedIssueRow("bd-cmt-rt")
	created := time.Now().UTC().Truncate(time.Second)
	id := s.seedComment("bd-cmt-rt", "alice", "hello world", created)

	out, err := s.commentRepo().ListByIssueIDs(s.Ctx(), []string{"bd-cmt-rt"})
	s.Require().NoError(err)
	s.Require().Len(out["bd-cmt-rt"], 1)
	c := out["bd-cmt-rt"][0]
	s.Equal(id, c.ID)
	s.Equal("bd-cmt-rt", c.IssueID)
	s.Equal("alice", c.Author)
	s.Equal("hello world", c.Text)
	s.Equal(created.Unix(), c.CreatedAt.Unix())
}

func (s *testSuite) commentListMissingAbsent() {
	s.seedIssueRow("bd-cmt-real")
	s.seedComment("bd-cmt-real", "a", "x", time.Now().UTC())

	out, err := s.commentRepo().ListByIssueIDs(s.Ctx(), []string{"bd-cmt-real", "bd-cmt-ghost"})
	s.Require().NoError(err)
	s.Len(out["bd-cmt-real"], 1)
	_, present := out["bd-cmt-ghost"]
	s.False(present, "missing issue IDs should not appear in the result map")

	// Sanity-check: types.Comment is still a struct (catches accidental import drift)
	var c types.Comment
	_ = c.ID
}
