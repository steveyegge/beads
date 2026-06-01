package db

import (
	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

func (s *testSuite) TestIssueSearchAcrossIssuesAndWisps() {
	s.Run("MergesIssuesAndWisps", s.searchAcrossMergesTables)
	s.Run("SkipWispsReturnsOnlyIssues", s.searchAcrossSkipWisps)
	s.Run("EphemeralTrueReturnsOnlyWisps", s.searchAcrossEphemeralTrueOnlyWisps)
	s.Run("FilterByStatusAppliesAcrossTables", s.searchAcrossStatusFilter)
	s.Run("FilterByLabelAppliesAcrossTables", s.searchAcrossLabelFilter)
	s.Run("LabelHydrationOnResults", s.searchAcrossLabelHydration)
	s.Run("LimitRespectedPerTable", s.searchAcrossLimitRespected)
	s.Run("CollisionAcrossTablesIsError", s.searchAcrossCollisionError)
	s.Run("SkipLabelsLeavesLabelsNil", s.searchAcrossSkipLabels)
}

func (s *testSuite) searchAcrossMergesTables() {
	r := s.issueRepo()
	perm := newTestIssue("bd-srx-merge-perm", "perm one")
	s.Require().NoError(r.Insert(s.Ctx(), perm, "tester", domain.InsertIssueOpts{}))

	w := newTestIssue("bd-srx-merge-wisp", "wisp one")
	w.Ephemeral = true
	s.Require().NoError(r.Insert(s.Ctx(), w, "tester", domain.InsertIssueOpts{UseWispsTable: true}))

	out, err := r.SearchAcrossIssuesAndWisps(s.Ctx(), "", types.IssueFilter{IDPrefix: "bd-srx-merge-"})
	s.Require().NoError(err)
	gotIDs := idsFrom(out)
	s.Contains(gotIDs, "bd-srx-merge-perm")
	s.Contains(gotIDs, "bd-srx-merge-wisp")
}

func (s *testSuite) searchAcrossSkipWisps() {
	r := s.issueRepo()
	perm := newTestIssue("bd-srx-skip-perm", "perm")
	s.Require().NoError(r.Insert(s.Ctx(), perm, "tester", domain.InsertIssueOpts{}))

	w := newTestIssue("bd-srx-skip-wisp", "wisp")
	w.Ephemeral = true
	s.Require().NoError(r.Insert(s.Ctx(), w, "tester", domain.InsertIssueOpts{UseWispsTable: true}))

	out, err := r.SearchAcrossIssuesAndWisps(s.Ctx(), "",
		types.IssueFilter{IDPrefix: "bd-srx-skip-", SkipWisps: true})
	s.Require().NoError(err)
	gotIDs := idsFrom(out)
	s.Contains(gotIDs, "bd-srx-skip-perm")
	s.NotContains(gotIDs, "bd-srx-skip-wisp", "SkipWisps must exclude wisps from results")
}

func (s *testSuite) searchAcrossEphemeralTrueOnlyWisps() {
	r := s.issueRepo()
	perm := newTestIssue("bd-srx-eph-perm", "perm")
	s.Require().NoError(r.Insert(s.Ctx(), perm, "tester", domain.InsertIssueOpts{}))

	w := newTestIssue("bd-srx-eph-wisp", "wisp")
	w.Ephemeral = true
	s.Require().NoError(r.Insert(s.Ctx(), w, "tester", domain.InsertIssueOpts{UseWispsTable: true}))

	ephTrue := true
	out, err := r.SearchAcrossIssuesAndWisps(s.Ctx(), "",
		types.IssueFilter{IDPrefix: "bd-srx-eph-", Ephemeral: &ephTrue})
	s.Require().NoError(err)
	gotIDs := idsFrom(out)
	s.Contains(gotIDs, "bd-srx-eph-wisp")
	s.NotContains(gotIDs, "bd-srx-eph-perm", "Ephemeral=true must route to wisps table only")
}

func (s *testSuite) searchAcrossStatusFilter() {
	r := s.issueRepo()
	openIssue := newTestIssue("bd-srx-st-open", "open")
	s.Require().NoError(r.Insert(s.Ctx(), openIssue, "tester", domain.InsertIssueOpts{}))

	closedIssue := newTestIssue("bd-srx-st-closed", "closed")
	closedIssue.Status = types.StatusClosed
	s.Require().NoError(r.Insert(s.Ctx(), closedIssue, "tester", domain.InsertIssueOpts{}))

	closedWisp := newTestIssue("bd-srx-st-wclosed", "wisp closed")
	closedWisp.Ephemeral = true
	closedWisp.Status = types.StatusClosed
	s.Require().NoError(r.Insert(s.Ctx(), closedWisp, "tester", domain.InsertIssueOpts{UseWispsTable: true}))

	closed := types.StatusClosed
	out, err := r.SearchAcrossIssuesAndWisps(s.Ctx(), "",
		types.IssueFilter{IDPrefix: "bd-srx-st-", Status: &closed})
	s.Require().NoError(err)
	gotIDs := idsFrom(out)
	s.ElementsMatch([]string{"bd-srx-st-closed", "bd-srx-st-wclosed"}, gotIDs)
}

func (s *testSuite) searchAcrossLabelFilter() {
	r := s.issueRepo()
	labelRepo := NewLabelSQLRepository(s.Runner())

	hot := newTestIssue("bd-srx-lbl-hot", "tagged hot")
	s.Require().NoError(r.Insert(s.Ctx(), hot, "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(labelRepo.Insert(s.Ctx(), "bd-srx-lbl-hot", "hot", "tester", domain.LabelOpts{}))

	cold := newTestIssue("bd-srx-lbl-cold", "tagged cold")
	s.Require().NoError(r.Insert(s.Ctx(), cold, "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(labelRepo.Insert(s.Ctx(), "bd-srx-lbl-cold", "cold", "tester", domain.LabelOpts{}))

	wispHot := newTestIssue("bd-srx-lbl-whot", "wisp hot")
	wispHot.Ephemeral = true
	s.Require().NoError(r.Insert(s.Ctx(), wispHot, "tester", domain.InsertIssueOpts{UseWispsTable: true}))
	s.Require().NoError(labelRepo.Insert(s.Ctx(), "bd-srx-lbl-whot", "hot", "tester", domain.LabelOpts{UseWispsTable: true}))

	out, err := r.SearchAcrossIssuesAndWisps(s.Ctx(), "",
		types.IssueFilter{IDPrefix: "bd-srx-lbl-", Labels: []string{"hot"}})
	s.Require().NoError(err)
	gotIDs := idsFrom(out)
	s.ElementsMatch([]string{"bd-srx-lbl-hot", "bd-srx-lbl-whot"}, gotIDs)
}

func (s *testSuite) searchAcrossLabelHydration() {
	r := s.issueRepo()
	labelRepo := NewLabelSQLRepository(s.Runner())

	issue := newTestIssue("bd-srx-hyd-1", "labeled")
	s.Require().NoError(r.Insert(s.Ctx(), issue, "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(labelRepo.Insert(s.Ctx(), "bd-srx-hyd-1", "alpha", "tester", domain.LabelOpts{}))
	s.Require().NoError(labelRepo.Insert(s.Ctx(), "bd-srx-hyd-1", "beta", "tester", domain.LabelOpts{}))

	out, err := r.SearchAcrossIssuesAndWisps(s.Ctx(), "",
		types.IssueFilter{IDPrefix: "bd-srx-hyd-"})
	s.Require().NoError(err)
	s.Require().Len(out, 1)
	s.ElementsMatch([]string{"alpha", "beta"}, out[0].Labels)
}

func (s *testSuite) searchAcrossLimitRespected() {
	r := s.issueRepo()
	for i := 0; i < 5; i++ {
		s.Require().NoError(r.Insert(s.Ctx(),
			newTestIssue("bd-srx-lim-"+string(rune('a'+i)), "x"),
			"tester", domain.InsertIssueOpts{}))
	}
	out, err := r.SearchAcrossIssuesAndWisps(s.Ctx(), "",
		types.IssueFilter{IDPrefix: "bd-srx-lim-", Limit: 3, SkipWisps: true})
	s.Require().NoError(err)
	s.Len(out, 3)
}

func (s *testSuite) searchAcrossCollisionError() {
	r := s.issueRepo()
	const id = "bd-srx-collision-1"
	s.Require().NoError(r.Insert(s.Ctx(), newTestIssue(id, "perm"), "tester", domain.InsertIssueOpts{}))

	w := newTestIssue(id, "wisp")
	w.Ephemeral = true
	s.Require().NoError(r.Insert(s.Ctx(), w, "tester", domain.InsertIssueOpts{UseWispsTable: true}))

	_, err := r.SearchAcrossIssuesAndWisps(s.Ctx(), "",
		types.IssueFilter{IDPrefix: "bd-srx-collision-"})
	s.Require().Error(err)
	s.Contains(err.Error(), "exists in both issues and wisps")
}

func (s *testSuite) searchAcrossSkipLabels() {
	r := s.issueRepo()
	labelRepo := NewLabelSQLRepository(s.Runner())

	issue := newTestIssue("bd-srx-nolbl-1", "labeled")
	s.Require().NoError(r.Insert(s.Ctx(), issue, "tester", domain.InsertIssueOpts{}))
	s.Require().NoError(labelRepo.Insert(s.Ctx(), "bd-srx-nolbl-1", "gamma", "tester", domain.LabelOpts{}))

	out, err := r.SearchAcrossIssuesAndWisps(s.Ctx(), "",
		types.IssueFilter{IDPrefix: "bd-srx-nolbl-", SkipLabels: true, SkipWisps: true})
	s.Require().NoError(err)
	s.Require().Len(out, 1)
	s.Empty(out[0].Labels, "SkipLabels must leave Labels nil/empty")
}

func idsFrom(issues []*types.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, issue := range issues {
		out = append(out, issue.ID)
	}
	return out
}
