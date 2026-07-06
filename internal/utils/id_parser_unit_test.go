package utils

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestPartialIDSearchPartUsesLastHyphenSuffix(t *testing.T) {
	got, ok := partialIDSearchPart("hacker-news-ko4")
	if !ok {
		t.Fatal("partialIDSearchPart returned ok=false")
	}
	if got != "ko4" {
		t.Fatalf("search part = %q, want %q", got, "ko4")
	}
}

func TestPartialIDSearchPartKeepsPlainAndHierarchicalIDs(t *testing.T) {
	tests := []string{"abc123", "abc123.1", "bd-abc123.1"}
	for _, input := range tests {
		got, ok := partialIDSearchPart(input)
		if !ok {
			t.Fatalf("partialIDSearchPart(%q) returned ok=false", input)
		}
		want := input
		if input == "bd-abc123.1" {
			want = "abc123.1"
		}
		if got != want {
			t.Fatalf("partialIDSearchPart(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPartialIDSearchPartRejectsInvalidSearchText(t *testing.T) {
	for _, input := range []string{"", "bd abc", "bd:abc", "bd/abc"} {
		if got, ok := partialIDSearchPart(input); ok {
			t.Fatalf("partialIDSearchPart(%q) = %q, true; want false", input, got)
		}
	}
}

// --- partial-resolution notice -------------------------------------------------

func TestShouldNotifyPartialResolution(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		resolved   string
		quiet      bool
		noNotify   string
		wantNotify bool
	}{
		{"non-exact match notifies", "bd-x.11", "bd-x.11.1", false, "", true},
		{"identical resolution is silent", "bd-x.11", "bd-x.11", false, "", false},
		{"empty resolution is silent", "bd-x.11", "", false, "", false},
		{"quiet suppresses", "bd-x.11", "bd-x.11.1", true, "", false},
		{"env opt-out suppresses", "bd-x.11", "bd-x.11.1", false, "1", false},
		{"quiet and env together suppress", "bd-x.11", "bd-x.11.1", true, "1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldNotifyPartialResolution(tt.input, tt.resolved, tt.quiet, tt.noNotify)
			if got != tt.wantNotify {
				t.Fatalf("shouldNotifyPartialResolution(%q, %q, quiet=%v, env=%q) = %v, want %v",
					tt.input, tt.resolved, tt.quiet, tt.noNotify, got, tt.wantNotify)
			}
		})
	}
}

func TestEmitPartialResolutionNoticeNamesBothIDs(t *testing.T) {
	out := captureStderr(t, func() {
		emitPartialResolutionNotice("bd-x.11", "bd-x.11.1")
	})
	for _, want := range []string{"bd-x.11", "bd-x.11.1", "not an exact issue ID", "BD_NO_PARTIAL_ID_NOTICE"} {
		if !strings.Contains(out, want) {
			t.Fatalf("notice %q does not contain %q", out, want)
		}
	}
}

// fakeResolverStore is a minimal PartialIDResolverStore over an in-memory ID
// list. It avoids the live-Dolt dependency in id_parser_test.go, which skips
// when no test server is running and so cannot pin this behaviour in CI.
type fakeResolverStore struct {
	ids     []string
	prefix  string
	allowed string // allowed_prefixes config, for cross-prefix cases
}

func (f *fakeResolverStore) SearchIssues(_ context.Context, _ string, filter types.IssueFilter) ([]*types.Issue, error) {
	var out []*types.Issue
	for _, want := range filter.IDs {
		for _, id := range f.ids {
			if id == want {
				out = append(out, &types.Issue{ID: id})
			}
		}
	}
	return out, nil
}

// SearchIssueIDs mimics the storage layer's `id LIKE %query%` filtering.
func (f *fakeResolverStore) SearchIssueIDs(_ context.Context, query string, _ types.IssueFilter) ([]string, error) {
	var out []string
	for _, id := range f.ids {
		if query == "" || strings.Contains(id, query) {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeResolverStore) GetConfig(_ context.Context, key string) (string, error) {
	switch key {
	case "issue_prefix":
		return f.prefix, nil
	case "allowed_prefixes":
		return f.allowed, nil
	}
	return "", nil
}

// A renamed parent leaves its child carrying the old prefix, so the vacated
// parent ID resolves to that child by leading-prefix abbreviation. The caller
// must be told it did not get the ID it named.
func TestResolvePartialIDNotifiesWhenParentIDResolvesToItsChild(t *testing.T) {
	store := &fakeResolverStore{ids: []string{"bd-x.11.1"}, prefix: "bd"}

	var resolved string
	var err error
	out := captureStderr(t, func() {
		resolved, err = ResolvePartialID(context.Background(), store, "bd-x.11")
	})
	if err != nil {
		t.Fatalf("ResolvePartialID returned error: %v", err)
	}
	if resolved != "bd-x.11.1" {
		t.Fatalf("resolved = %q, want %q", resolved, "bd-x.11.1")
	}
	if !strings.Contains(out, "bd-x.11.1") || !strings.Contains(out, "not an exact issue ID") {
		t.Fatalf("expected a partial-resolution notice on stderr, got %q", out)
	}
}

// Reaching the in-loop exact-hash return needs a CROSS-PREFIX input: both
// fast paths (raw input, then prefix-normalized input) miss, because the
// issue's real prefix is an allowed one that is not the configured default, so
// resolution falls through to the search loop and matches on hash alone. That
// return is a second silent exit, and an emission planted there survives any
// test that only exercises the fast paths.
func TestResolvePartialIDSilentOnCrossPrefixExactHashMatch(t *testing.T) {
	store := &fakeResolverStore{ids: []string{"hq-a3f8e9"}, prefix: "bd", allowed: "hq"}

	var resolved string
	var err error
	out := captureStderr(t, func() {
		resolved, err = ResolvePartialID(context.Background(), store, "a3f8e9")
	})
	if err != nil {
		t.Fatalf("ResolvePartialID returned error: %v", err)
	}
	if resolved != "hq-a3f8e9" {
		t.Fatalf("resolved = %q, want %q", resolved, "hq-a3f8e9")
	}
	if out != "" {
		t.Fatalf("expected no notice for an exact hash match, got %q", out)
	}
}

// A prefix-less exact input is still silent, via the normalized fast path.
func TestResolvePartialIDSilentOnPrefixlessExactMatch(t *testing.T) {
	store := &fakeResolverStore{ids: []string{"bd-a3f8e9"}, prefix: "bd"}

	var resolved string
	var err error
	out := captureStderr(t, func() {
		resolved, err = ResolvePartialID(context.Background(), store, "a3f8e9")
	})
	if err != nil {
		t.Fatalf("ResolvePartialID returned error: %v", err)
	}
	if resolved != "bd-a3f8e9" {
		t.Fatalf("resolved = %q, want %q", resolved, "bd-a3f8e9")
	}
	if out != "" {
		t.Fatalf("expected no notice for an exact match, got %q", out)
	}
}

// The documented abbreviation path notifies too — this is not limited to the
// parent/child shape, and the CHANGELOG says so.
func TestResolvePartialIDNotifiesOnOrdinaryAbbreviation(t *testing.T) {
	store := &fakeResolverStore{ids: []string{"bd-a3f8e9"}, prefix: "bd"}

	out := captureStderr(t, func() {
		if _, err := ResolvePartialID(context.Background(), store, "a3f8"); err != nil {
			t.Errorf("ResolvePartialID returned error: %v", err)
		}
	})
	if !strings.Contains(out, "bd-a3f8e9") {
		t.Fatalf("expected a notice naming the resolved ID, got %q", out)
	}
}

func TestResolvePartialIDSilentOnExactMatch(t *testing.T) {
	store := &fakeResolverStore{ids: []string{"bd-x.11", "bd-x.11.1"}, prefix: "bd"}

	var resolved string
	var err error
	out := captureStderr(t, func() {
		resolved, err = ResolvePartialID(context.Background(), store, "bd-x.11")
	})
	if err != nil {
		t.Fatalf("ResolvePartialID returned error: %v", err)
	}
	if resolved != "bd-x.11" {
		t.Fatalf("resolved = %q, want %q", resolved, "bd-x.11")
	}
	if out != "" {
		t.Fatalf("expected no notice for an exact match, got %q", out)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	// Restore even if fn panics, so one failure cannot swallow every later
	// test's stderr. Both pipe ends are closed before returning.
	defer func() {
		os.Stderr = orig
		_ = r.Close()
	}()
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		_, _ = io.Copy(&sb, r)
		done <- sb.String()
	}()
	fn()
	_ = w.Close()
	return <-done
}

func TestHashSegmentPrefixMatch(t *testing.T) {
	tests := []struct {
		issueHash string
		hashPart  string
		want      bool
	}{
		// Direct prefix: plain hash starts with query
		{"oqfe9", "oqf", true},
		// Exact match
		{"oqf", "oqf", true},
		// The gcy-g4o repro: wisp compound hash must NOT match
		{"wisp-goqfo", "oqf", false},
		// Wisp last-segment match (t3st matches wisp-t3st)
		{"wisp-t3st", "t3st", true},
		// Partial prefix of last segment
		{"wisp-t3st", "t3", true},
		// No match
		{"abc123", "xyz", false},
	}
	for _, tt := range tests {
		got := hashSegmentPrefixMatch(tt.issueHash, tt.hashPart)
		if got != tt.want {
			t.Errorf("hashSegmentPrefixMatch(%q, %q) = %v; want %v", tt.issueHash, tt.hashPart, got, tt.want)
		}
	}
}

// TestHashSegmentPrefixMatchPlainHashPrefixOnly pins the intentional
// behavior change from strings.Contains to prefix-only matching: a plain
// (non-wisp, no-dash) hash matches a prefix fragment but no longer matches
// a tail or middle fragment. Previously "f8e9" (tail of "a3f8e9") would
// match via substring; that broadening was the source of the gcy-g4o
// wrong-bead resolution and is deliberately removed.
func TestHashSegmentPrefixMatchPlainHashPrefixOnly(t *testing.T) {
	tests := []struct {
		issueHash string
		hashPart  string
		want      bool
	}{
		// Prefix fragments of a plain hash still match
		{"a3f8e9", "a3", true},
		{"a3f8e9", "a3f8", true},
		// Tail fragment no longer matches (old Contains behavior)
		{"a3f8e9", "f8e9", false},
		// Middle fragment no longer matches
		{"a3f8e9", "3f8", false},
	}
	for _, tt := range tests {
		got := hashSegmentPrefixMatch(tt.issueHash, tt.hashPart)
		if got != tt.want {
			t.Errorf("hashSegmentPrefixMatch(%q, %q) = %v; want %v", tt.issueHash, tt.hashPart, got, tt.want)
		}
	}
}
