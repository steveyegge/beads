package validation

import "testing"

func TestIsTestIssueTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  bool
	}{
		{name: "test prefix", title: "test-foo", want: true},
		{name: "benchmark prefix", title: "benchmark_case", want: true},
		{name: "sample prefix", title: "sample item", want: true},
		{name: "tmp prefix", title: "tmp-file", want: true},
		{name: "debug prefix", title: "debug run", want: true},
		{name: "dummy prefix", title: "dummy-data", want: true},
		{name: "trim and case normalize", title: "  TeSt quick check", want: true},
		{name: "normal feature title", title: "Implement dependency export", want: false},
		{name: "contains test later", title: "Feature for test users", want: false},
		{name: "empty", title: "", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsTestIssueTitle(tc.title)
			if got != tc.want {
				t.Fatalf("IsTestIssueTitle(%q) = %v, want %v", tc.title, got, tc.want)
			}
		})
	}
}
