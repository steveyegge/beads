package idgen

import (
	"testing"
)

func TestGenerateSlug(t *testing.T) {
	gen := NewSemanticIDGenerator()

	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"simple", "Fix login timeout", "fix_login_timeout"},
		{"with articles", "The API returns an error", "api_returns_error"},
		{"with prepositions", "Add support for dark mode", "add_support_dark_mode"},
		{"uppercase", "FIX THE BUG", "fix_bug"},
		{"numbers", "Fix issue 123", "fix_issue_123"},
		{"punctuation", "Fix: login (timeout)", "fix_login_timeout"},
		{"special chars", "Fix bug #42 - login", "fix_bug_42_login"},
		{"priority prefix", "URGENT: Fix login", "fix_login"},
		{"p0 prefix", "P0 Database crash", "database_crash"},
		{"empty", "", "untitled"},
		{"only stop words", "the a an", "the"}, // Falls back to first word
		{"numeric start", "123 fix", "n123_fix"},
		{"very long", "This is a very long title that should be truncated to fit within the maximum slug length limit", "very_long_title_should_truncated_fit"},
		{"hyphens to underscores", "fix-login-bug", "fix_login_bug"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gen.GenerateSlug(tt.title)
			if got != tt.want {
				t.Errorf("GenerateSlug(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestGenerateSemanticID(t *testing.T) {
	gen := NewSemanticIDGenerator()

	tests := []struct {
		name        string
		prefix      string
		issueType   string
		title       string
		existingIDs []string
		want        string
	}{
		{
			name:      "basic bug",
			prefix:    "gt",
			issueType: "bug",
			title:     "Fix login timeout",
			want:      "gt-bug-fix_login_timeout",
		},
		{
			name:      "task type",
			prefix:    "bd",
			issueType: "task",
			title:     "Implement caching",
			want:      "bd-tsk-implement_caching",
		},
		{
			name:      "feature type",
			prefix:    "gt",
			issueType: "feature",
			title:     "Add dark mode",
			want:      "gt-feat-add_dark_mode",
		},
		{
			name:      "epic type",
			prefix:    "hq",
			issueType: "epic",
			title:     "Semantic issue IDs",
			want:      "hq-epc-semantic_issue_ids",
		},
		{
			name:        "collision handling",
			prefix:      "gt",
			issueType:   "bug",
			title:       "Fix login timeout",
			existingIDs: []string{"gt-bug-fix_login_timeout"},
			want:        "gt-bug-fix_login_timeout_2",
		},
		{
			name:        "multiple collisions",
			prefix:      "gt",
			issueType:   "bug",
			title:       "Fix login timeout",
			existingIDs: []string{"gt-bug-fix_login_timeout", "gt-bug-fix_login_timeout_2"},
			want:        "gt-bug-fix_login_timeout_3",
		},
		{
			name:      "unknown type defaults to task",
			prefix:    "gt",
			issueType: "unknown",
			title:     "Something",
			want:      "gt-tsk-something",
		},
		{
			name:      "merge request",
			prefix:    "bd",
			issueType: "merge-request",
			title:     "Add readme",
			want:      "bd-mr-add_readme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gen.GenerateSemanticID(tt.prefix, tt.issueType, tt.title, tt.existingIDs)
			if got != tt.want {
				t.Errorf("GenerateSemanticID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateSemanticIDWithCallback(t *testing.T) {
	gen := NewSemanticIDGenerator()

	// Simulate a database with existing IDs
	existingIDs := map[string]bool{
		"gt-bug-fix_login": true,
	}
	exists := func(id string) bool {
		return existingIDs[id]
	}

	// First ID should collide, second should work
	id := gen.GenerateSemanticIDWithCallback("gt", "bug", "Fix login", exists)
	if id != "gt-bug-fix_login_2" {
		t.Errorf("Got %q, want gt-bug-fix_login_2", id)
	}

	// Non-colliding ID
	id = gen.GenerateSemanticIDWithCallback("gt", "task", "New feature", exists)
	if id != "gt-tsk-new_feature" {
		t.Errorf("Got %q, want gt-tsk-new_feature", id)
	}
}

func TestSlugLength(t *testing.T) {
	gen := NewSemanticIDGenerator()

	// Very long title
	longTitle := "This is an extremely long title that goes on and on and should definitely be truncated to fit within the maximum allowed slug length which is forty characters"
	slug := gen.GenerateSlug(longTitle)

	if len(slug) > 40 {
		t.Errorf("Slug length %d exceeds max 40: %q", len(slug), slug)
	}

	if len(slug) < 3 {
		t.Errorf("Slug length %d is below minimum 3: %q", len(slug), slug)
	}
}

func TestStopWordRemoval(t *testing.T) {
	gen := NewSemanticIDGenerator()

	// All stop words should produce fallback
	slug := gen.GenerateSlug("is are the a an")
	if slug == "" || len(slug) < 3 {
		t.Errorf("Slug from stop words should have fallback, got %q", slug)
	}
}

func TestTypeAbbreviations(t *testing.T) {
	gen := NewSemanticIDGenerator()

	types := []struct {
		issueType string
		abbrev    string
	}{
		{"bug", "bug"},
		{"task", "tsk"},
		{"feature", "feat"},
		{"epic", "epc"},
		{"merge-request", "mr"},
		{"wisp", "wsp"},
		{"molecule", "mol"},
		{"agent", "agt"},
		{"convoy", "cvy"},
		{"chore", "chr"},
		{"event", "evt"},
		{"message", "msg"},
		{"role", "rol"},
	}

	for _, tt := range types {
		t.Run(tt.issueType, func(t *testing.T) {
			id := gen.GenerateSemanticID("gt", tt.issueType, "Test", nil)
			expectedPrefix := "gt-" + tt.abbrev + "-"
			if !startsWith(id, expectedPrefix) {
				t.Errorf("ID %q should start with %q", id, expectedPrefix)
			}
		})
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func TestGenerateSlugWithRandom(t *testing.T) {
	gen := NewSemanticIDGenerator()

	tests := []struct {
		name            string
		prefix          string
		issueType       string
		title           string
		canonicalRandom string
		want            string
	}{
		{
			name:            "epic with random",
			prefix:          "gt",
			issueType:       "epic",
			title:           "Semantic Issue IDs",
			canonicalRandom: "zfyl8",
			want:            "gt-epc-semantic_issue_idszfyl8",
		},
		{
			name:            "bug with random",
			prefix:          "gt",
			issueType:       "bug",
			title:           "Fix login timeout",
			canonicalRandom: "3q6a9",
			want:            "gt-bug-fix_login_timeout3q6a9",
		},
		{
			name:            "task with random",
			prefix:          "bd",
			issueType:       "task",
			title:           "Add validation",
			canonicalRandom: "x7m2",
			want:            "bd-tsk-add_validationx7m2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gen.GenerateSlugWithRandom(tt.prefix, tt.issueType, tt.title, tt.canonicalRandom)
			if got != tt.want {
				t.Errorf("GenerateSlugWithRandom() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateChildSlug(t *testing.T) {
	gen := NewSemanticIDGenerator()

	tests := []struct {
		name       string
		parentSlug string
		childTitle string
		want       string
	}{
		{
			name:       "format spec child",
			parentSlug: "gt-epc-semantic_idszfyl8",
			childTitle: "Format specification",
			want:       "gt-epc-semantic_idszfyl8.format_specification",
		},
		{
			name:       "validation child",
			parentSlug: "gt-epc-semantic_idszfyl8",
			childTitle: "Validation preview",
			want:       "gt-epc-semantic_idszfyl8.validation_preview",
		},
		{
			name:       "grandchild",
			parentSlug: "gt-epc-semantic_idszfyl8.format_spec",
			childTitle: "Regex pattern",
			want:       "gt-epc-semantic_idszfyl8.format_spec.regex_pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gen.GenerateChildSlug(tt.parentSlug, tt.childTitle)
			if got != tt.want {
				t.Errorf("GenerateChildSlug() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractRandomFromID(t *testing.T) {
	tests := []struct {
		name        string
		canonicalID string
		want        string
	}{
		{"simple", "gt-zfyl8", "zfyl8"},
		{"with child", "gt-zfyl8.1", "zfyl8"},
		{"with grandchild", "gt-zfyl8.1.2", "zfyl8"},
		{"longer random", "bd-abc123", "abc123"},
		{"hq prefix", "hq-xyz99", "xyz99"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractRandomFromID(tt.canonicalID)
			if got != tt.want {
				t.Errorf("ExtractRandomFromID(%q) = %q, want %q", tt.canonicalID, got, tt.want)
			}
		})
	}
}
