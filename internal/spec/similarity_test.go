package spec

import "testing"

func TestFindDuplicates(t *testing.T) {
	specs := []SpecRegistryEntry{
		{SpecID: "specs/auth.md", Title: "Authentication Flow Design", Summary: "oauth login flow"},
		{SpecID: "specs/auth-v2.md", Title: "Auth Flow Design", Summary: "oauth login"},
		{SpecID: "specs/other.md", Title: "Billing Plan", Summary: "payments and invoices"},
	}

	results := FindDuplicates(specs, 0.4)
	if len(results) == 0 {
		t.Fatalf("expected duplicates, got none")
	}
	if results[0].SpecA != "specs/auth.md" || results[0].SpecB != "specs/auth-v2.md" {
		t.Fatalf("unexpected pair: %s %s", results[0].SpecA, results[0].SpecB)
	}
}

func TestResolveCanonical(t *testing.T) {
	tests := []struct {
		name       string
		specA      string
		specB      string
		wantKeep   string
		wantDelete string
		wantSkip   bool
	}{
		{"active vs reference", "specs/active/a.md", "specs/reference/a.md", "specs/active/a.md", "specs/reference/a.md", false},
		{"active vs archive", "specs/active/a.md", "specs/archive/a.md", "specs/archive/a.md", "specs/active/a.md", false},
		{"archive vs reference", "specs/archive/a.md", "specs/reference/a.md", "specs/archive/a.md", "specs/reference/a.md", false},
		{"root vs archive", "specs/FOO.md", "specs/archive/FOO.md", "specs/archive/FOO.md", "specs/FOO.md", false},
		{"root vs active", "specs/FOO.md", "specs/active/FOO.md", "specs/active/FOO.md", "specs/FOO.md", false},
		{"root vs reference", "specs/FOO.md", "specs/reference/FOO.md", "specs/reference/FOO.md", "specs/FOO.md", false},
		{"ideas vs active", "specs/ideas/a.md", "specs/active/a.md", "specs/active/a.md", "specs/ideas/a.md", false},
		{"ideas vs archive", "specs/ideas/a.md", "specs/archive/a.md", "specs/archive/a.md", "specs/ideas/a.md", false},
		{"ideas vs reference", "specs/ideas/a.md", "specs/reference/a.md", "specs/reference/a.md", "specs/ideas/a.md", false},
		{"same dir skip active", "specs/active/a.md", "specs/active/b.md", "", "", true},
		{"same dir skip ideas", "specs/ideas/DRAFT.md", "specs/ideas/POLISHED.md", "", "", true},
		{"same dir skip root", "specs/A.md", "specs/B.md", "", "", true},
		{"unknown dirs skip", "docs/a.md", "notes/b.md", "", "", true},
		{"one unknown skip", "docs/a.md", "specs/active/b.md", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pair := DuplicatePair{SpecA: tt.specA, SpecB: tt.specB, Similarity: 1.0}
			keep, del, skip := ResolveCanonical(pair)
			if skip != tt.wantSkip {
				t.Errorf("skip: got %v, want %v", skip, tt.wantSkip)
			}
			if !skip {
				if keep != tt.wantKeep {
					t.Errorf("keep: got %q, want %q", keep, tt.wantKeep)
				}
				if del != tt.wantDelete {
					t.Errorf("delete: got %q, want %q", del, tt.wantDelete)
				}
			}
		})
	}
}

func TestCanonicalDir(t *testing.T) {
	tests := []struct {
		specID string
		want   string
	}{
		{"specs/active/foo.md", "active"},
		{"specs/archive/foo.md", "archive"},
		{"specs/reference/foo.md", "reference"},
		{"specs/ideas/foo.md", "ideas"},
		{"specs/FOO.md", "root"},
		{"docs/bar.md", "unknown"},
		{"deeply/nested/active/spec.md", "active"},
	}
	for _, tt := range tests {
		t.Run(tt.specID, func(t *testing.T) {
			got := canonicalDir(tt.specID)
			if got != tt.want {
				t.Errorf("canonicalDir(%q) = %q, want %q", tt.specID, got, tt.want)
			}
		})
	}
}
