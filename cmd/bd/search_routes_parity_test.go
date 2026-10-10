//go:build cgo

package main

import (
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// The `bd search` route agreement. Every route (the direct store, the proxied
// server and a remote backend) builds ONE request (searchListRequest over
// issueops.SearchListRequest) and asks ONE library entry with it
// (issueops.Reader.List), so the same seed must answer the same flags the same
// way on all of them. A route that mapped a flag on its own, or reached a
// different read, would answer differently.
//
// The routes run in two lanes: TestEmbeddedSearchRoutesAgree (the direct
// route, embedded lane) and TestProxiedServerSearchRoutesAgree (the proxied
// and remote routes, proxied lane). Neither lane can run the other's
// workspace, so both hold their answers to the same expectation,
// searchRouteQueries' want, through checkSearchRoute. Agreeing with it on
// every route is agreeing with each other.

// searchRouteSeed is one row of the seed every route gets. Distinct priorities
// among the "routed" rows make a --limit cut the same rows on every route
// regardless of creation-time ties.
type searchRouteSeed struct {
	title string
	args  []string
	close bool
}

var searchRouteSeeds = []searchRouteSeed{
	{"Routed alpha task", []string{"--type", "task", "--priority", "0", "--assignee", "alice", "--label", "lane:a"}, false},
	{"Routed beta bug", []string{"--type", "bug", "--priority", "1", "--assignee", "bob", "--label", "lane:b"}, false},
	{"re-ROUTED gamma", []string{"--type", "feature", "--priority", "2", "--label", "lane:a"}, false},
	{"Routed and done", []string{"--type", "task", "--priority", "3"}, true},
	{"unrelated delta", []string{"--type", "task", "--priority", "4", "--label", "lane:a"}, false},
}

const (
	srAlpha = "Routed alpha task|open"
	srBeta  = "Routed beta bug|open"
	srGamma = "re-ROUTED gamma|open"
	srDone  = "Routed and done|closed"
)

// searchRouteQuery is one flag set. want is the seed's answer as title|status:
// in output order when ordered, otherwise as a set.
type searchRouteQuery struct {
	name    string
	args    []string
	ordered bool // the flags fix the output order
	wireCan bool // listIssues can carry every flag
	want    []string
}

var searchRouteQueries = []searchRouteQuery{
	{"text", []string{"routed"}, false, true, []string{srAlpha, srBeta, srGamma, srDone}},
	{"status open", []string{"routed", "--status", "open"}, false, true, []string{srAlpha, srBeta, srGamma}},
	{"status all", []string{"routed", "--status", "all"}, false, true, []string{srAlpha, srBeta, srGamma, srDone}},
	{"type", []string{"routed", "--type", "bug"}, false, true, []string{srBeta}},
	{"label", []string{"routed", "--label", "lane:a"}, false, true, []string{srAlpha, srGamma}},
	{"assignee", []string{"routed", "--assignee", "alice"}, false, true, []string{srAlpha}},
	// The store's default order is priority first, so the cut keeps P0 and P1.
	{"limit", []string{"routed", "--limit", "2"}, false, true, []string{srAlpha, srBeta}},
	// The title sort is case-insensitive (workapi.SortIssues).
	{"sort title", []string{"routed", "--sort", "title"}, true, true, []string{srGamma, srAlpha, srDone, srBeta}},
	{"sort priority reverse", []string{"routed", "--sort", "priority", "--reverse"}, true, true, []string{srDone, srGamma, srBeta, srAlpha}},
	{"query flag", []string{"--query", "gamma"}, false, true, []string{srGamma}},
	{"no match", []string{"nothing-matches-this"}, false, true, nil},
	{"priority-max", []string{"routed", "--priority-max", "1"}, false, false, []string{srAlpha, srBeta}},
}

type searchRouteRow struct{ ID, Title, Status string }

// seedSearchRoutes creates searchRouteSeeds through one route's create and
// close.
func seedSearchRoutes(create func(title string, args []string) string, closeIssue func(id string)) {
	for _, s := range searchRouteSeeds {
		id := create(s.title, s.args)
		if s.close {
			closeIssue(id)
		}
	}
}

// runSearchRoute runs `bd search --json args...` in dir and returns its
// stdout, stderr and exit code.
func runSearchRoute(t *testing.T, bd, dir string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bd, append([]string{"search", "--json"}, args...)...)
	cmd.Dir = dir
	cmd.Env = env
	stdout, stderr, err := runCommandBuffers(t, cmd)
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running bd search: %v", err)
		}
		code = ee.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

func decodeSearchRoute(t *testing.T, out string) []searchRouteRow {
	t.Helper()
	s := strings.TrimSpace(out)
	start := strings.Index(s, "[")
	if start < 0 {
		return nil
	}
	var rows []searchRouteRow
	if err := json.Unmarshal([]byte(s[start:]), &rows); err != nil {
		t.Fatalf("parse search JSON: %v\n%s", err, s)
	}
	return rows
}

// searchRouteComparable projects rows onto what two databases with the same
// seed share (title and status, not ids); an unordered answer is a sorted set.
func searchRouteComparable(rows []searchRouteRow, ordered bool) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Title+"|"+r.Status)
	}
	if !ordered {
		slices.Sort(out)
	}
	return out
}

// checkSearchRoute runs every query on one route and holds each answer to its
// want. A query listIssues cannot carry (wireCan false) must refuse on a
// remote route rather than answer without the flag. It returns the decoded
// answers by query name, for a route that is compared row for row with
// another one over the same database.
func checkSearchRoute(t *testing.T, route, bd, dir string, env []string, remote bool) map[string][]searchRouteRow {
	t.Helper()
	answers := map[string][]searchRouteRow{}
	for _, q := range searchRouteQueries {
		stdout, stderr, code := runSearchRoute(t, bd, dir, env, q.args...)
		if remote && !q.wireCan {
			if code == 0 {
				t.Errorf("%s: %s bd search %v succeeded; listIssues cannot carry it, so it must refuse", q.name, route, q.args)
			}
			continue
		}
		if code != 0 {
			t.Errorf("%s: %s bd search %v failed (exit %d): %s", q.name, route, q.args, code, stderr)
			continue
		}
		rows := decodeSearchRoute(t, stdout)
		answers[q.name] = rows
		want := slices.Clone(q.want)
		if !q.ordered {
			slices.Sort(want)
		}
		if got := searchRouteComparable(rows, q.ordered); !slices.Equal(got, want) {
			t.Errorf("%s: %s bd search %v answered %v, want %v", q.name, route, q.args, got, want)
		}
	}
	return answers
}
