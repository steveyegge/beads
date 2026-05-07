package postgres

import "testing"

// TestPgGlobToLikePattern pins the glob -> SQL LIKE conversion used by the
// --label-pattern filter on Postgres. Mirrors the dolt-side test in
// internal/storage/issueops/filters_test.go.
func TestPgGlobToLikePattern(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "trailing star", in: "tech-*", want: "tech-%"},
		{name: "surrounding stars", in: "*foo*", want: "%foo%"},
		{name: "question mark", in: "v?", want: "v_"},
		{name: "literal percent", in: "5%", want: "5|%"},
		{name: "literal underscore", in: "snake_case", want: "snake|_case"},
		{name: "literal pipe", in: "a|b", want: "a||b"},
		{name: "no metachars", in: "needs-pm", want: "needs-pm"},
		{name: "empty", in: "", want: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := pgGlobToLikePattern(tc.in)
			if got != tc.want {
				t.Errorf("pgGlobToLikePattern(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
