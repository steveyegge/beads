package encode

import (
	"errors"
	"reflect"
	"testing"

	"github.com/steveyegge/beads/issueops"
)

// `bd list --title-contains X` folds onto the listing's `title` parameter: the
// store compiles TitleContains and TitleSearch to one predicate, so the fold
// is exact. A pair that differs other than in letter case still refuses,
// because the listing publishes one title parameter and dropping either half
// would widen the answer.
func TestFoldTitleContainsRidesTheTitleParameter(t *testing.T) {
	for _, tc := range []struct {
		name      string
		req       issueops.ListRequest
		wantTitle []string
		refused   bool
	}{
		{name: "title-contains alone", req: issueops.ListRequest{TitleContains: "Routed"}, wantTitle: []string{"Routed"}},
		{name: "equal pair sends one title", req: issueops.ListRequest{TitleSearch: "Routed", TitleContains: "Routed"}, wantTitle: []string{"Routed"}},
		// The store lowers both values with strings.ToLower, so a pair that
		// differs only in letter case is one predicate too.
		{name: "pair differing only in case sends one title", req: issueops.ListRequest{TitleSearch: "Routed", TitleContains: "rOUTED"}, wantTitle: []string{"Routed"}},
		{name: "title alone is untouched", req: issueops.ListRequest{TitleSearch: "Routed"}, wantTitle: []string{"Routed"}},
		{name: "neither sends no title", req: issueops.ListRequest{}},
		{name: "differing pair refuses", req: issueops.ListRequest{TitleSearch: "Routed", TitleContains: "task"}, refused: true},
		// strings.EqualFold matches "ſ" (long s) with "s", but the store lowers
		// them to different patterns, so this pair is two predicates.
		{name: "pair equal only under case folding refuses", req: issueops.ListRequest{TitleSearch: "ſtep", TitleContains: "step"}, refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			folded := FoldTitleContains(tc.req)
			params, err := ListParams(folded)
			if tc.refused {
				var refused *RefusedError
				if !errors.As(err, &refused) {
					t.Fatalf("ListParams(%+v) error = %v, want a *RefusedError", folded, err)
				}
				if refused.Row.ID != "E-ListRequest.TitleContains" {
					t.Fatalf("refused by row %q, want E-ListRequest.TitleContains", refused.Row.ID)
				}
				return
			}
			if err != nil {
				t.Fatalf("ListParams(%+v): %v", folded, err)
			}
			if got := params["title"]; !reflect.DeepEqual(got, tc.wantTitle) {
				t.Errorf("title = %q, want %q", got, tc.wantTitle)
			}
			if got, ok := params["title_contains"]; ok {
				t.Errorf("listIssues carries no title_contains parameter, but the encoder sent %q", got)
			}
		})
	}
}
