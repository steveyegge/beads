package spec

import "testing"

func TestTokenize(t *testing.T) {
	tokens := Tokenize("Implement OAuth flow for new login spec")
	if len(tokens) == 0 {
		t.Fatalf("expected tokens, got none")
	}
	for _, stop := range []string{"implement", "for", "new", "spec"} {
		for _, tok := range tokens {
			if tok == stop {
				t.Fatalf("stopword %q should be removed", stop)
			}
		}
	}
}

func TestSuggestSpecs(t *testing.T) {
	specs := []SpecRegistryEntry{
		{SpecID: "specs/auth/oauth.md", Title: "OAuth flow"},
		{SpecID: "specs/payments/credit_cards.md", Title: "Credit card processing"},
		{SpecID: "specs/auth/session.md", Title: "Session management"},
	}

	matches := SuggestSpecs("Implement OAuth flow with PKCE", specs, 3, 0.2)
	if len(matches) == 0 {
		t.Fatalf("expected matches, got none")
	}
	if matches[0].SpecID != "specs/auth/oauth.md" {
		t.Fatalf("expected top match oauth spec, got %s", matches[0].SpecID)
	}
}

func TestBestSpecMatchThreshold(t *testing.T) {
	specs := []SpecRegistryEntry{
		{SpecID: "specs/auth/oauth.md", Title: "OAuth flow"},
	}
	if _, ok := BestSpecMatch("Unrelated task", specs, 0.8); ok {
		t.Fatalf("expected no match above threshold")
	}
}
