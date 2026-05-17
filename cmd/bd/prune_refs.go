package main

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// referencedSampleCap caps the number of referenced IDs surfaced in
// --dry-run / --json output. The full count is always reported via
// referenced_count; the sample is for operator-eyeballing.
const referencedSampleCap = 100

// buildReferencedSet scans every open / in_progress / blocked / deferred /
// pinned / hooked bead's description, notes, and comments for any literal
// occurrence of a candidate ID. Returns the set of IDs that ARE referenced
// (subset of candidateIDs), a sorted sample of up to 100 referenced IDs,
// and any storage error. Returns (nil, nil, nil) when candidateIDs is empty.
//
// The matcher is word-boundary anchored (\b) to avoid `be-08pl` matching
// inside `be-08plx`. Candidate IDs are regexp.QuoteMeta'd defensively even
// though bead IDs are [a-z0-9-]+.
//
// Filter is built with Statuses (OR list of all non-closed statuses) and
// NOT ExcludeStatus, because the PG backend silently drops ExcludeStatus.
// Using Statuses is safe on both backends.
func buildReferencedSet(
	ctx context.Context,
	store storage.Storage,
	candidateIDs map[string]bool,
) (refSet map[string]bool, sample []string, err error) {
	if len(candidateIDs) == 0 {
		return nil, nil, nil
	}

	ids := make([]string, 0, len(candidateIDs))
	for id := range candidateIDs {
		ids = append(ids, regexp.QuoteMeta(id))
	}
	sort.Strings(ids)
	pat := regexp.MustCompile(`\b(` + strings.Join(ids, "|") + `)\b`)

	notClosed := types.IssueFilter{
		Statuses: []types.Status{
			types.StatusOpen,
			types.StatusInProgress,
			types.StatusBlocked,
			types.StatusDeferred,
			types.StatusPinned,
			types.StatusHooked,
		},
	}
	openLike, err := store.SearchIssues(ctx, "", notClosed)
	if err != nil {
		return nil, nil, err
	}

	refSet = make(map[string]bool)
	for _, iss := range openLike {
		scanText(iss.Description, pat, refSet)
		scanText(iss.Notes, pat, refSet)
		comments, err := store.GetIssueComments(ctx, iss.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, c := range comments {
			scanText(c.Text, pat, refSet)
		}
	}

	if len(refSet) == 0 {
		return refSet, nil, nil
	}
	keys := make([]string, 0, len(refSet))
	for id := range refSet {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	if len(keys) > referencedSampleCap {
		keys = keys[:referencedSampleCap]
	}
	return refSet, keys, nil
}

// scanText finds every match of pat in text and adds matched substrings to out.
func scanText(text string, pat *regexp.Regexp, out map[string]bool) {
	if text == "" {
		return
	}
	for _, m := range pat.FindAllString(text, -1) {
		out[m] = true
	}
}
