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
