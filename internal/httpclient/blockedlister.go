package httpclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/steveyegge/beads/internal/httpapi/apigen"
	"github.com/steveyegge/beads/internal/httpclient/wire"
	storageops "github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/issueops"
)

// httpBlockedLister is issueops.BlockedLister over listBlockedIssues
// (GET /v0/beads/blocked).
//
// It dials through Store.dispatch, so blocked.list is preflighted: a server
// that does not advertise it is refused BEFORE the dial with
// *wire.CapabilityError naming the token (it unwraps to
// *storage.ErrUnsupported{Capability: "blocked.list"}). It never composes the
// listing client-side from other reads: the set, its narrowing and its order
// are the server's role's, in one transaction.
//
// The labels are sent normalized (storageops.BlockedFilter, the rule the
// server's role applies again), so a blank or repeated value never reaches the
// wire. The listing is a GET, so dispatch replays it on a busy answer.
type httpBlockedLister struct{ store *Store }

var _ issueops.BlockedLister = (*httpBlockedLister)(nil)

func (l *httpBlockedLister) ListBlocked(ctx context.Context, req issueops.BlockedRequest) (issueops.BlockedResult, error) {
	var body apigen.BlockedPage
	if err := l.store.dispatch(ctx, wire.Request{
		Op:     wire.OpListBlockedIssues,
		Method: http.MethodGet,
		Path:   wire.PathBlocked,
		Query:  blockedParams(req),
	}, &body); err != nil {
		return issueops.BlockedResult{}, err
	}
	items := make([]*types.BlockedIssue, 0, len(body.Items))
	for i := range body.Items {
		items = append(items, &body.Items[i])
	}
	return storageops.BlockedResultOf(items), nil
}

// blockedParams encodes the request as the operation's query: one parameter
// per `bd blocked` flag, labels repeated, nothing sent for an empty field.
func blockedParams(req issueops.BlockedRequest) url.Values {
	filter := storageops.BlockedFilter(req)
	params := url.Values{}
	if filter.ParentID != nil {
		params.Set("parent", *filter.ParentID)
	}
	for _, label := range filter.Labels {
		params.Add("label", label)
	}
	for _, label := range filter.LabelsAny {
		params.Add("label_any", label)
	}
	for _, label := range filter.ExcludeLabels {
		params.Add("exclude_label", label)
	}
	return params
}

// GetBlockedIssues is the legacy blocked read, answered by the role over the
// wire, so a caller of the raw door gets the same listing `bd blocked` does.
//
// The role has no page and no row cap, and that is restated here rather than
// dropped in silence: a filter carrying a Limit, an Offset or a MaxRows is
// applied to the listing the server returned, exactly as the external-
// dependency decorator applies them to the local one (finishBlockedIssues).
// Every other WorkFilter field the legacy door never read stays unread.
func (s *Store) GetBlockedIssues(ctx context.Context, filter types.WorkFilter) ([]*types.BlockedIssue, error) {
	req := issueops.BlockedRequest{
		Labels:        filter.Labels,
		LabelsAny:     filter.LabelsAny,
		ExcludeLabels: filter.ExcludeLabels,
	}
	if filter.ParentID != nil {
		req.ParentID = *filter.ParentID
	}
	result, err := (&httpBlockedLister{store: s}).ListBlocked(ctx, req)
	if err != nil {
		return nil, err
	}
	items := result.Items
	if filter.Offset > 0 {
		if filter.Offset >= len(items) {
			items = nil
		} else {
			items = items[filter.Offset:]
		}
	}
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	if err := storageops.EnforceMaxRowsCap(len(items), filter.MaxRows, filter.MaxRowsSource); err != nil {
		return nil, err
	}
	return items, nil
}
