package encode

import "github.com/steveyegge/beads/issueops"

// FoldTitleContains rewrites ListRequest.TitleContains onto TitleSearch when
// the pair compiles to one clause, so `bd list --title-contains X` rides the
// listing's existing `title` parameter (behind issues.list.title) instead of
// refusing.
//
// The fold is exact, not an approximation: the store compiles both members to
// the identical predicate, `LOWER(title) LIKE '%' || lower(v) || '%'`
// (internal/storage/sqlbuild), and workapi.BuildListFilter copies each straight
// through. So when TitleSearch is empty, or byte-equal to TitleContains, the
// request with TitleContains moved onto TitleSearch selects exactly the same
// rows.
//
// When both are set and differ the request is returned unchanged: the listing
// publishes one title parameter, and the encoder refuses the leftover
// TitleContains (E-ListRequest.TitleContains) rather than drop either half.
//
// The request is taken and returned by value; the caller's copy is never
// mutated.
func FoldTitleContains(req issueops.ListRequest) issueops.ListRequest {
	if req.TitleContains == "" {
		return req
	}
	if req.TitleSearch == "" || req.TitleSearch == req.TitleContains {
		req.TitleSearch = req.TitleContains
		req.TitleContains = ""
	}
	return req
}
