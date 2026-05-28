package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

func (s *testSuite) TestIssueSQLRepository() {
	s.Run("Insert", func() {
		s.Run("RoundTripWithGet", s.issueInsertRoundTrip)
		s.Run("RequiresExplicitID", s.issueInsertRequiresID)
		s.Run("IdempotentOnDuplicateKey", s.issueInsertIdempotent)
		s.Run("RecordsCreatedEvent", s.issueInsertRecordsEvent)
		s.Run("RoutesToWispsTable", s.issueInsertWispRouting)
		s.Run("ComputesContentHashWhenMissing", s.issueInsertComputesHash)
	})
	s.Run("InsertBatch", func() {
		s.Run("AllIssuesInserted", s.issueInsertBatchAll)
		s.Run("StopsOnFirstError", s.issueInsertBatchStopsOnError)
	})
	s.Run("Update", func() {
		s.Run("UpdatesAllowedFields", s.issueUpdateAllowedFields)
		s.Run("RejectsUnknownFields", s.issueUpdateRejectsUnknownFields)
		s.Run("MissingIDReturnsErrNoRows", s.issueUpdateMissingID)
		s.Run("EmptyUpdatesIsNoop", s.issueUpdateEmpty)
		s.Run("NormalizesStatusType", s.issueUpdateStatusType)
		s.Run("NormalizesTimestampToUTC", s.issueUpdateNormalizesTimestamp)
	})
	s.Run("Get", func() {
		s.Run("MissingIDReturnsErrNoRows", s.issueGetMissing)
		s.Run("EmptyIDReturnsError", s.issueGetEmptyID)
	})
	s.Run("GetByIDs", func() {
		s.Run("EmptySliceReturnsNil", s.issueGetByIDsEmpty)
		s.Run("ReturnsOnlyExistingRows", s.issueGetByIDsPartial)
	})
	s.Run("Search", func() {
		s.Run("NoFilterReturnsAll", s.issueSearchAll)
		s.Run("FilterByStatus", s.issueSearchByStatus)
		s.Run("FilterByIssueType", s.issueSearchByIssueType)
		s.Run("FilterByIDPrefix", s.issueSearchByIDPrefix)
		s.Run("FilterByIDs", s.issueSearchByIDs)
		s.Run("LimitRespected", s.issueSearchLimit)
	})
	s.Run("Wisp", func() {
		s.Run("InsertRoutesToWispsTable", s.issueWispInsertRouting)
		s.Run("GetReadsFromWispsTable", s.issueWispGet)
		s.Run("UpdateWritesToWispsTable", s.issueWispUpdate)
		s.Run("SearchReadsFromWispsTable", s.issueWispSearch)
		s.Run("CrossRoutedLookupsAreEmpty", s.issueWispIsolated)
	})
	s.Run("Exists", func() {
		s.Run("MissingReturnsFalse", s.issueExistsMissing)
		s.Run("PresentReturnsTrue", s.issueExistsPresent)
		s.Run("EmptyIDReturnsError", s.issueExistsEmptyID)
		s.Run("RoutedToWisps", s.issueExistsWispRouting)
	})
	s.Run("CountForPrefix", func() {
		s.Run("EmptyTableReturnsZero", s.issueCountForPrefixEmpty)
		s.Run("CountsMatching", s.issueCountForPrefixMatches)
		s.Run("ExcludesChildIDs", s.issueCountForPrefixExcludesChildren)
		s.Run("RoutedToWisps", s.issueCountForPrefixWispRouting)
		s.Run("EmptyPrefixReturnsError", s.issueCountForPrefixEmptyPrefix)
	})
	s.Run("NextCounterID", func() {
		s.Run("FreshDBInsertsAtOne", s.issueNextCounterIDFresh)
		s.Run("MonotonicIncrement", s.issueNextCounterIDIncrement)
		s.Run("SeedsFromMaxExisting", s.issueNextCounterIDSeedsFromMax)
		s.Run("IgnoresChildIDsWhenSeeding", s.issueNextCounterIDSeedSkipsChildren)
		s.Run("EmptyPrefixReturnsError", s.issueNextCounterIDEmptyPrefix)
	})
}

func (s *testSuite) issueRepo() domain.IssueSQLRepository {
	return NewIssueSQLRepository(s.Runner())
}

func newTestIssue(id, title string) *types.Issue {
	return &types.Issue{
		ID:        id,
		Title:     title,
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
	}
}

func (s *testSuite) issueInsertRoundTrip() {
	r := s.issueRepo()
	in := newTestIssue("bd-test-1", "round trip")
	in.Description = "desc body"
	in.Assignee = "alice"
	in.Labels = []string{"ignored-in-this-impl"}
	mins := 45
	in.EstimatedMinutes = &mins

	s.Require().NoError(r.Insert(s.Ctx(), in, "tester", domain.InsertIssueOpts{}))

	out, err := r.Get(s.Ctx(), "bd-test-1", domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Equal("bd-test-1", out.ID)
	s.Equal("round trip", out.Title)
	s.Equal("desc body", out.Description)
	s.Equal("alice", out.Assignee)
	s.Equal(types.StatusOpen, out.Status)
	s.Equal(2, out.Priority)
	s.Equal(types.TypeTask, out.IssueType)
	s.Require().NotNil(out.EstimatedMinutes)
	s.Equal(45, *out.EstimatedMinutes)
}

func (s *testSuite) issueInsertRequiresID() {
	r := s.issueRepo()
	err := r.Insert(s.Ctx(), newTestIssue("", "no id"), "tester", domain.InsertIssueOpts{})
	s.Require().Error(err)
	s.Contains(err.Error(), "explicit ID required")
}

func (s *testSuite) issueInsertIdempotent() {
	r := s.issueRepo()
	in := newTestIssue("bd-test-dup", "v1")
	s.Require().NoError(r.Insert(s.Ctx(), in, "tester", domain.InsertIssueOpts{}))

	in.Title = "v2"
	in.Description = "added on second pass"
	s.Require().NoError(r.Insert(s.Ctx(), in, "tester", domain.InsertIssueOpts{}))

	out, err := r.Get(s.Ctx(), "bd-test-dup", domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Equal("v2", out.Title)
	s.Equal("added on second pass", out.Description)
}

func (s *testSuite) issueInsertRecordsEvent() {
	r := s.issueRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-test-evt", "event check"), "tester", domain.InsertIssueOpts{}))

	var count int
	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(),
		"SELECT COUNT(*) FROM events WHERE issue_id = ? AND event_type = ?",
		"bd-test-evt", string(types.EventCreated),
	).Scan(&count))
	s.Equal(1, count, "expected exactly one created event")
}

func (s *testSuite) issueInsertWispRouting() {
	r := s.issueRepo()
	wisp := newTestIssue("bd-test-wisp", "wisp issue")
	wisp.Ephemeral = true
	s.Require().NoError(r.Insert(s.Ctx(), wisp, "tester", domain.InsertIssueOpts{UseWispsTable: true}))

	var count int
	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(), "SELECT COUNT(*) FROM wisps WHERE id = ?", "bd-test-wisp").Scan(&count))
	s.Equal(1, count, "expected row in wisps table")

	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(), "SELECT COUNT(*) FROM issues WHERE id = ?", "bd-test-wisp").Scan(&count))
	s.Equal(0, count, "expected no row in issues table")

	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(),
		"SELECT COUNT(*) FROM wisp_events WHERE issue_id = ?",
		"bd-test-wisp",
	).Scan(&count))
	s.Equal(1, count, "expected created event in wisp_events")
}

func (s *testSuite) issueInsertComputesHash() {
	r := s.issueRepo()
	in := newTestIssue("bd-test-hash", "hash check")
	s.Require().Empty(in.ContentHash)
	s.Require().NoError(r.Insert(s.Ctx(), in, "tester", domain.InsertIssueOpts{}))
	s.Require().NotEmpty(in.ContentHash, "Insert should populate ContentHash before writing")

	out, err := r.Get(s.Ctx(), "bd-test-hash", domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Equal(in.ContentHash, out.ContentHash)
}

func (s *testSuite) issueInsertBatchAll() {
	r := s.issueRepo()
	batch := []*types.Issue{
		newTestIssue("bd-batch-1", "one"),
		newTestIssue("bd-batch-2", "two"),
		newTestIssue("bd-batch-3", "three"),
	}
	s.Require().NoError(r.InsertBatch(s.Ctx(), batch, "tester", domain.InsertIssueOpts{}))

	got, err := r.GetByIDs(s.Ctx(), []string{"bd-batch-1", "bd-batch-2", "bd-batch-3"}, domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Len(got, 3)
}

func (s *testSuite) issueInsertBatchStopsOnError() {
	r := s.issueRepo()
	batch := []*types.Issue{
		newTestIssue("bd-stop-1", "ok"),
		newTestIssue("", "bad — missing id"),
		newTestIssue("bd-stop-3", "never reached"),
	}
	err := r.InsertBatch(s.Ctx(), batch, "tester", domain.InsertIssueOpts{})
	s.Require().Error(err)

	got, err := r.GetByIDs(s.Ctx(), []string{"bd-stop-1", "bd-stop-3"}, domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Len(got, 1, "first issue should be persisted, third should not")
	s.Equal("bd-stop-1", got[0].ID)
}

func (s *testSuite) issueUpdateAllowedFields() {
	r := s.issueRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-upd-1", "before"), "tester", domain.InsertIssueOpts{}))

	updates := map[string]any{
		"title":       "after",
		"priority":    0,
		"description": "new desc",
		"assignee":    "bob",
	}
	s.Require().NoError(r.Update(s.Ctx(), "bd-upd-1", updates, "tester", domain.IssueTableOpts{}))

	out, err := r.Get(s.Ctx(), "bd-upd-1", domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Equal("after", out.Title)
	s.Equal(0, out.Priority)
	s.Equal("new desc", out.Description)
	s.Equal("bob", out.Assignee)
}

func (s *testSuite) issueUpdateRejectsUnknownFields() {
	r := s.issueRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-upd-bad", "x"), "tester", domain.InsertIssueOpts{}))

	err := r.Update(s.Ctx(), "bd-upd-bad", map[string]any{"id": "rename-attempt"}, "tester", domain.IssueTableOpts{})
	s.Require().Error(err)
	s.Contains(err.Error(), "not allowed")
}

func (s *testSuite) issueUpdateMissingID() {
	r := s.issueRepo()
	err := r.Update(s.Ctx(), "bd-does-not-exist", map[string]any{"title": "x"}, "tester", domain.IssueTableOpts{})
	s.Require().Error(err)
	s.True(errors.Is(err, sql.ErrNoRows), "expected sql.ErrNoRows, got %v", err)
}

func (s *testSuite) issueUpdateEmpty() {
	r := s.issueRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-upd-empty", "x"), "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(r.Update(s.Ctx(), "bd-upd-empty", nil, "tester", domain.IssueTableOpts{}))
}

func (s *testSuite) issueUpdateStatusType() {
	r := s.issueRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-upd-status", "x"), "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(r.Update(s.Ctx(), "bd-upd-status", map[string]any{
		"status":     types.StatusInProgress,
		"issue_type": types.TypeBug,
	}, "tester", domain.IssueTableOpts{}))

	out, err := r.Get(s.Ctx(), "bd-upd-status", domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Equal(types.StatusInProgress, out.Status)
	s.Equal(types.TypeBug, out.IssueType)
}

func (s *testSuite) issueUpdateNormalizesTimestamp() {
	r := s.issueRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-upd-tz", "tz"), "tester", domain.InsertIssueOpts{}))

	tz, err := time.LoadLocation("America/Los_Angeles")
	s.Require().NoError(err)
	due := time.Date(2030, 6, 15, 10, 0, 0, 0, tz)

	s.Require().NoError(r.Update(s.Ctx(), "bd-upd-tz", map[string]any{"due_at": due}, "tester", domain.IssueTableOpts{}))

	out, err := r.Get(s.Ctx(), "bd-upd-tz", domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Require().NotNil(out.DueAt)
	s.Equal(due.UTC().Unix(), out.DueAt.Unix(), "due_at should round-trip via UTC")
}

func (s *testSuite) issueGetMissing() {
	_, err := s.issueRepo().Get(s.Ctx(), "bd-no-such-id", domain.IssueTableOpts{})
	s.Require().Error(err)
	s.True(errors.Is(err, sql.ErrNoRows), "expected sql.ErrNoRows, got %v", err)
}

func (s *testSuite) issueGetEmptyID() {
	_, err := s.issueRepo().Get(s.Ctx(), "", domain.IssueTableOpts{})
	s.Require().Error(err)
}

func (s *testSuite) issueGetByIDsEmpty() {
	out, err := s.issueRepo().GetByIDs(s.Ctx(), nil, domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Nil(out)
}

func (s *testSuite) issueGetByIDsPartial() {
	r := s.issueRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-pres-1", "a"), "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-pres-2", "b"), "tester", domain.InsertIssueOpts{}))

	out, err := r.GetByIDs(s.Ctx(), []string{"bd-pres-1", "bd-pres-2", "bd-missing"}, domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Len(out, 2)

	ids := map[string]bool{}
	for _, i := range out {
		ids[i.ID] = true
	}
	s.True(ids["bd-pres-1"])
	s.True(ids["bd-pres-2"])
}

func (s *testSuite) issueSearchAll() {
	r := s.issueRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-srch-1", "a"), "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-srch-2", "b"), "tester", domain.InsertIssueOpts{}))

	out, err := r.Search(s.Ctx(), types.IssueFilter{}, domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.GreaterOrEqual(len(out), 2)
}

func (s *testSuite) issueSearchByStatus() {
	r := s.issueRepo()

	open1 := newTestIssue("bd-stat-open", "open one")
	s.Require().NoError(r.Insert(s.Ctx(), open1, "tester", domain.InsertIssueOpts{}))

	closed := newTestIssue("bd-stat-closed", "closed one")
	closed.Status = types.StatusClosed
	s.Require().NoError(r.Insert(s.Ctx(), closed, "tester", domain.InsertIssueOpts{}))

	// Scope by ID prefix — earlier subtests share the DB state and may have
	// created closed rows.
	closedStatus := types.StatusClosed
	out, err := r.Search(s.Ctx(), types.IssueFilter{Status: &closedStatus, IDPrefix: "bd-stat-"}, domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Len(out, 1)
	s.Equal("bd-stat-closed", out[0].ID)
}

func (s *testSuite) issueSearchByIssueType() {
	r := s.issueRepo()

	bug := newTestIssue("bd-type-bug", "bug")
	bug.IssueType = types.TypeBug
	s.Require().NoError(r.Insert(s.Ctx(), bug, "tester", domain.InsertIssueOpts{}))

	task := newTestIssue("bd-type-task", "task")
	s.Require().NoError(r.Insert(s.Ctx(), task, "tester", domain.InsertIssueOpts{}))

	// Scope by ID prefix to isolate from earlier subtests.
	bugType := types.TypeBug
	out, err := r.Search(s.Ctx(), types.IssueFilter{IssueType: &bugType, IDPrefix: "bd-type-"}, domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Len(out, 1)
	s.Equal("bd-type-bug", out[0].ID)
}

func (s *testSuite) issueSearchByIDPrefix() {
	r := s.issueRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-pfx-a", "a"), "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-pfx-b", "b"), "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("other-1", "other"), "tester", domain.InsertIssueOpts{}))

	out, err := r.Search(s.Ctx(), types.IssueFilter{IDPrefix: "bd-pfx-"}, domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Len(out, 2)
}

func (s *testSuite) issueSearchByIDs() {
	r := s.issueRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-ids-1", "a"), "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-ids-2", "b"), "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-ids-3", "c"), "tester", domain.InsertIssueOpts{}))

	out, err := r.Search(s.Ctx(), types.IssueFilter{IDs: []string{"bd-ids-1", "bd-ids-3"}}, domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Len(out, 2)
}

func (s *testSuite) issueSearchLimit() {
	r := s.issueRepo()
	for i := 0; i < 5; i++ {
		s.Require().NoError(r.Insert(s.Ctx(), newTestIssue(fmt.Sprintf("bd-lim-%d", i), "x"), "tester", domain.InsertIssueOpts{}))
	}
	out, err := r.Search(s.Ctx(), types.IssueFilter{Limit: 3, IDPrefix: "bd-lim-"}, domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Len(out, 3)
}

func (s *testSuite) issueWispInsertRouting() {
	r := s.issueRepo()
	wisp := newTestIssue("bd-iss-wisp-1", "wisp issue")
	wisp.Ephemeral = true
	s.Require().NoError(r.Insert(s.Ctx(), wisp, "tester", domain.InsertIssueOpts{UseWispsTable: true}))

	var wispCount, permCount int
	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(),
		"SELECT COUNT(*) FROM wisps WHERE id = ?", "bd-iss-wisp-1").Scan(&wispCount))
	s.Equal(1, wispCount)
	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(),
		"SELECT COUNT(*) FROM issues WHERE id = ?", "bd-iss-wisp-1").Scan(&permCount))
	s.Equal(0, permCount)
}

func (s *testSuite) issueWispGet() {
	r := s.issueRepo()
	in := newTestIssue("bd-iss-wisp-get", "wisp get")
	in.Ephemeral = true
	s.Require().NoError(r.Insert(s.Ctx(), in, "tester", domain.InsertIssueOpts{UseWispsTable: true}))

	out, err := r.Get(s.Ctx(), "bd-iss-wisp-get", domain.IssueTableOpts{UseWispsTable: true})
	s.Require().NoError(err)
	s.Equal("bd-iss-wisp-get", out.ID)
	s.Equal("wisp get", out.Title)
}

func (s *testSuite) issueWispUpdate() {
	r := s.issueRepo()
	in := newTestIssue("bd-iss-wisp-upd", "before")
	in.Ephemeral = true
	s.Require().NoError(r.Insert(s.Ctx(), in, "tester", domain.InsertIssueOpts{UseWispsTable: true}))

	s.Require().NoError(r.Update(s.Ctx(), "bd-iss-wisp-upd",
		map[string]any{"title": "after"}, "tester",
		domain.IssueTableOpts{UseWispsTable: true},
	))

	out, err := r.Get(s.Ctx(), "bd-iss-wisp-upd", domain.IssueTableOpts{UseWispsTable: true})
	s.Require().NoError(err)
	s.Equal("after", out.Title)

	// The update event should land in wisp_events, not events.
	var wispEvtCount, permEvtCount int
	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(),
		"SELECT COUNT(*) FROM wisp_events WHERE issue_id = ? AND event_type = ?",
		"bd-iss-wisp-upd", string(types.EventUpdated)).Scan(&wispEvtCount))
	s.Equal(1, wispEvtCount)
	s.Require().NoError(s.Runner().QueryRowContext(s.Ctx(),
		"SELECT COUNT(*) FROM events WHERE issue_id = ? AND event_type = ?",
		"bd-iss-wisp-upd", string(types.EventUpdated)).Scan(&permEvtCount))
	s.Equal(0, permEvtCount)
}

func (s *testSuite) issueWispSearch() {
	r := s.issueRepo()
	for i := 0; i < 3; i++ {
		w := newTestIssue(fmt.Sprintf("bd-iss-wsrch-%d", i), "x")
		w.Ephemeral = true
		s.Require().NoError(r.Insert(s.Ctx(), w, "tester", domain.InsertIssueOpts{UseWispsTable: true}))
	}
	out, err := r.Search(s.Ctx(),
		types.IssueFilter{IDPrefix: "bd-iss-wsrch-"},
		domain.IssueTableOpts{UseWispsTable: true},
	)
	s.Require().NoError(err)
	s.Len(out, 3)
}

func (s *testSuite) issueWispIsolated() {
	r := s.issueRepo()
	perm := newTestIssue("bd-iss-iso-perm", "perm")
	s.Require().NoError(r.Insert(s.Ctx(), perm, "tester", domain.InsertIssueOpts{}))
	w := newTestIssue("bd-iss-iso-wisp", "wisp")
	w.Ephemeral = true
	s.Require().NoError(r.Insert(s.Ctx(), w, "tester", domain.InsertIssueOpts{UseWispsTable: true}))

	// Cross-routed Get should miss in each direction.
	_, err := r.Get(s.Ctx(), "bd-iss-iso-perm", domain.IssueTableOpts{UseWispsTable: true})
	s.Require().Error(err, "permanent issue should not be visible via wisp Get")
	_, err = r.Get(s.Ctx(), "bd-iss-iso-wisp", domain.IssueTableOpts{})
	s.Require().Error(err, "wisp issue should not be visible via permanent Get")

	// GetByIDs across the wrong table returns empty.
	got, err := r.GetByIDs(s.Ctx(), []string{"bd-iss-iso-perm"}, domain.IssueTableOpts{UseWispsTable: true})
	s.Require().NoError(err)
	s.Empty(got)
}

func (s *testSuite) issueExistsMissing() {
	r := s.issueRepo()
	got, err := r.Exists(s.Ctx(), "bd-not-there", domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.False(got)
}

func (s *testSuite) issueExistsPresent() {
	r := s.issueRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("bd-exists-yes", "present"), "tester", domain.InsertIssueOpts{}))
	got, err := r.Exists(s.Ctx(), "bd-exists-yes", domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.True(got)
}

func (s *testSuite) issueExistsEmptyID() {
	r := s.issueRepo()
	_, err := r.Exists(s.Ctx(), "", domain.IssueTableOpts{})
	s.Require().Error(err)
	s.Contains(err.Error(), "id must not be empty")
}

func (s *testSuite) issueExistsWispRouting() {
	r := s.issueRepo()
	w := newTestIssue("bd-exists-wisp", "wisp")
	w.Ephemeral = true
	s.Require().NoError(r.Insert(s.Ctx(), w, "tester", domain.InsertIssueOpts{UseWispsTable: true}))

	got, err := r.Exists(s.Ctx(), "bd-exists-wisp", domain.IssueTableOpts{UseWispsTable: true})
	s.Require().NoError(err)
	s.True(got, "should find wisp via wisps table")

	got, err = r.Exists(s.Ctx(), "bd-exists-wisp", domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.False(got, "should not find wisp via issues table")
}

func (s *testSuite) issueCountForPrefixEmpty() {
	// Use a fresh prefix that no prior test inserts under. The suite shares
	// state across s.Run subtests within a single TestXxx method, so we
	// can't rely on "bd" being empty here.
	r := s.issueRepo()
	got, err := r.CountForPrefix(s.Ctx(), "cfpEmpty", domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Equal(0, got)
}

func (s *testSuite) issueCountForPrefixMatches() {
	r := s.issueRepo()
	for _, id := range []string{"cfpMat-c1", "cfpMat-c2", "cfpMat-c3"} {
		s.Require().NoError(r.Insert(s.Ctx(), newTestIssue(id, id), "tester", domain.InsertIssueOpts{}))
	}
	// Decoy with a different prefix should not be counted.
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("cfpMatX-c1", "decoy"), "tester", domain.InsertIssueOpts{}))

	got, err := r.CountForPrefix(s.Ctx(), "cfpMat", domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Equal(3, got)
}

func (s *testSuite) issueCountForPrefixExcludesChildren() {
	r := s.issueRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("cfpChld-parent", "parent"), "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("cfpChld-parent.1", "child 1"), "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("cfpChld-parent.2", "child 2"), "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("cfpChld-sibling", "sibling"), "tester", domain.InsertIssueOpts{}))

	got, err := r.CountForPrefix(s.Ctx(), "cfpChld", domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Equal(2, got, "child IDs containing '.' must not be counted")
}

func (s *testSuite) issueCountForPrefixWispRouting() {
	r := s.issueRepo()
	w := newTestIssue("cfpWisp-c1", "wisp count")
	w.Ephemeral = true
	s.Require().NoError(r.Insert(s.Ctx(), w, "tester", domain.InsertIssueOpts{UseWispsTable: true}))

	got, err := r.CountForPrefix(s.Ctx(), "cfpWisp", domain.IssueTableOpts{UseWispsTable: true})
	s.Require().NoError(err)
	s.Equal(1, got)
	got, err = r.CountForPrefix(s.Ctx(), "cfpWisp", domain.IssueTableOpts{})
	s.Require().NoError(err)
	s.Equal(0, got, "issues table should not see wisp rows")
}

func (s *testSuite) issueCountForPrefixEmptyPrefix() {
	r := s.issueRepo()
	_, err := r.CountForPrefix(s.Ctx(), "", domain.IssueTableOpts{})
	s.Require().Error(err)
	s.Contains(err.Error(), "prefix must not be empty")
}

func (s *testSuite) issueNextCounterIDFresh() {
	// Unique prefix per subtest because subtests share state within a single
	// TestXxx method (testify suite quirk).
	r := s.issueRepo()
	got, err := r.NextCounterID(s.Ctx(), "ctrFresh")
	s.Require().NoError(err)
	s.Equal(1, got, "first counter call on a fresh prefix should yield 1")
}

func (s *testSuite) issueNextCounterIDIncrement() {
	r := s.issueRepo()
	a, err := r.NextCounterID(s.Ctx(), "ctrInc")
	s.Require().NoError(err)
	b, err := r.NextCounterID(s.Ctx(), "ctrInc")
	s.Require().NoError(err)
	c, err := r.NextCounterID(s.Ctx(), "ctrInc")
	s.Require().NoError(err)
	s.Equal(a+1, b)
	s.Equal(b+1, c)

	// Sanity: another prefix is independent.
	other, err := r.NextCounterID(s.Ctx(), "ctrIncAlt")
	s.Require().NoError(err)
	s.Equal(1, other)
}

func (s *testSuite) issueNextCounterIDSeedsFromMax() {
	r := s.issueRepo()
	// Pre-seed two issues with numeric suffixes; no issue_counter row exists.
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("ctrSeed-7", "seven"), "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("ctrSeed-12", "twelve"), "tester", domain.InsertIssueOpts{}))

	got, err := r.NextCounterID(s.Ctx(), "ctrSeed")
	s.Require().NoError(err)
	s.Equal(13, got, "should seed from max(7,12)=12 and return 13")
}

func (s *testSuite) issueNextCounterIDSeedSkipsChildren() {
	r := s.issueRepo()
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("ctrSkip-3", "three"), "tester", domain.InsertIssueOpts{}))
	// A child of ctrSkip-3 with a numeric child suffix — must be skipped during seed.
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue("ctrSkip-3.99", "child"), "tester", domain.InsertIssueOpts{}))

	got, err := r.NextCounterID(s.Ctx(), "ctrSkip")
	s.Require().NoError(err)
	s.Equal(4, got, "must ignore child IDs when seeding from max")
}

func (s *testSuite) issueNextCounterIDEmptyPrefix() {
	r := s.issueRepo()
	_, err := r.NextCounterID(s.Ctx(), "")
	s.Require().Error(err)
	s.Contains(err.Error(), "prefix must not be empty")
}
