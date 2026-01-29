package validation

import (
	"testing"
)

func TestValidateSemanticID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		// Valid semantic IDs
		{"valid bug", "gt-bug-fix_login_timeout", false},
		{"valid task", "bd-tsk-implement_caching", false},
		{"valid epic", "hq-epc-semantic_issue_ids", false},
		{"valid feature", "gt-feat-add_dark_mode", false},
		{"valid merge request", "bd-mr-update_readme", false},
		{"valid with numbers", "gt-bug-fix_issue_123", false},
		{"valid with collision suffix", "gt-bug-fix_login_timeout_2", false},
		{"valid longer suffix", "gt-bug-fix_login_timeout_42", false},
		{"valid minimal slug", "gt-bug-abc", false},
		{"valid wisp", "gt-wsp-patrol_check", false},
		{"valid molecule", "gt-mol-deploy_pipeline", false},
		{"valid agent", "gt-agt-worker_alpha", false},
		{"valid convoy", "gt-cvy-batch_import", false},
		{"valid chore", "gt-chr-cleanup_temp", false},

		// Invalid semantic IDs
		{"empty", "", true},
		{"no hyphen", "gtbugfix", true},
		{"missing type", "gt-fix_login", true},
		{"missing slug", "gt-bug", true},
		{"invalid type abbrev", "gt-invalid-fix_login", true},
		{"uppercase in prefix", "GT-bug-fix_login", true},
		{"uppercase in type", "gt-BUG-fix_login", true},
		{"uppercase in slug", "gt-bug-Fix_Login", true},
		{"hyphen in slug", "gt-bug-fix-login", true},
		{"space in slug", "gt-bug-fix login", true},
		{"slug too short", "gt-bug-ab", true},
		{"slug starts with number", "gt-bug-123_fix", true},
		{"prefix too long", "gastownn-bug-fix", true},
		{"prefix too short", "g-bug-fix", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSemanticID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSemanticID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestParseSemanticID(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		wantPrefix string
		wantType   string
		wantSlug   string
		wantSuffix int
		wantErr    bool
	}{
		{
			name:       "basic bug",
			id:         "gt-bug-fix_login_timeout",
			wantPrefix: "gt",
			wantType:   "bug",
			wantSlug:   "fix_login_timeout",
			wantSuffix: 1,
		},
		{
			name:       "with collision suffix",
			id:         "gt-bug-fix_login_timeout_2",
			wantPrefix: "gt",
			wantType:   "bug",
			wantSlug:   "fix_login_timeout",
			wantSuffix: 2,
		},
		{
			name:       "task type",
			id:         "bd-tsk-implement_caching",
			wantPrefix: "bd",
			wantType:   "task",
			wantSlug:   "implement_caching",
			wantSuffix: 1,
		},
		{
			name:       "slug with numbers",
			id:         "gt-bug-fix_issue_123",
			wantPrefix: "gt",
			wantType:   "bug",
			wantSlug:   "fix_issue_123",
			wantSuffix: 1,
		},
		{
			name:    "invalid format",
			id:      "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSemanticID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSemanticID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			if result.Prefix != tt.wantPrefix {
				t.Errorf("Prefix = %q, want %q", result.Prefix, tt.wantPrefix)
			}
			if result.FullType != tt.wantType {
				t.Errorf("FullType = %q, want %q", result.FullType, tt.wantType)
			}
			if result.Slug != tt.wantSlug {
				t.Errorf("Slug = %q, want %q", result.Slug, tt.wantSlug)
			}
			if result.Suffix != tt.wantSuffix {
				t.Errorf("Suffix = %d, want %d", result.Suffix, tt.wantSuffix)
			}
		})
	}
}

func TestIsSemanticID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"gt-bug-fix_login_timeout", true},
		{"bd-tsk-implement_caching", true},
		{"gt-bug-fix_login_timeout_2", true},
		{"bd-abc123", false},       // legacy hash ID
		{"gt-x7q9z", false},        // legacy hash ID
		{"hq-cv-abc.1", false},     // legacy with child
		{"invalid", false},         // no hyphen
		{"", false},                // empty
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := IsSemanticID(tt.id); got != tt.want {
				t.Errorf("IsSemanticID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestIsLegacyID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"bd-abc123", true},
		{"gt-x7q9z", true},
		{"hq-abc", true},
		{"bd-3q6.9", true},
		{"hq-cv-abc123", true},
		{"gt-bug-fix_login_timeout", false}, // semantic, not legacy
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := IsLegacyID(tt.id); got != tt.want {
				t.Errorf("IsLegacyID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestValidateIssueID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		// Semantic IDs - valid
		{"semantic bug", "gt-bug-fix_login_timeout", false},
		{"semantic task", "bd-tsk-implement_caching", false},
		{"semantic with suffix", "gt-bug-fix_login_timeout_2", false},

		// Legacy IDs - valid (backward compatibility)
		{"legacy short", "bd-abc123", false},
		{"legacy with child", "bd-3q6.9", false},
		{"legacy compound prefix", "hq-cv-abc123", false},

		// Invalid IDs
		{"empty", "", true},
		{"no hyphen", "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIssueID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIssueID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		name    string
		slug    string
		wantErr bool
	}{
		{"valid", "fix_login_timeout", false},
		{"valid with numbers", "fix_issue_123", false},
		{"valid minimal", "abc", false},
		{"valid long", "this_is_a_very_long_slug_that_is_valid_yes", false}, // 46 chars max
		{"empty", "", true},
		{"too short", "ab", true},
		{"starts with number", "123_fix", true},
		{"has hyphen", "fix-login", true},
		{"has uppercase", "Fix_Login", true},
		{"has space", "fix login", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSlug(tt.slug)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSlug(%q) error = %v, wantErr %v", tt.slug, err, tt.wantErr)
			}
		})
	}
}

func TestIsReservedWord(t *testing.T) {
	tests := []struct {
		word string
		want bool
	}{
		{"new", true},
		{"list", true},
		{"create", true},
		{"all", true},
		{"fix_login", false},
		{"myslug", false},
	}

	for _, tt := range tests {
		t.Run(tt.word, func(t *testing.T) {
			if got := IsReservedWord(tt.word); got != tt.want {
				t.Errorf("IsReservedWord(%q) = %v, want %v", tt.word, got, tt.want)
			}
		})
	}
}

func TestSemanticIDTypeAbbreviations(t *testing.T) {
	// Verify all type abbreviations are 2-4 characters
	for typ, abbrev := range SemanticIDTypeAbbreviations {
		if len(abbrev) < 2 || len(abbrev) > 4 {
			t.Errorf("Type abbreviation for %q is %q (length %d), expected 2-4 chars", typ, abbrev, len(abbrev))
		}
	}

	// Verify reverse mapping is consistent
	for typ, abbrev := range SemanticIDTypeAbbreviations {
		if SemanticIDAbbreviationToType[abbrev] != typ {
			t.Errorf("Reverse mapping broken: %s -> %s -> %s", typ, abbrev, SemanticIDAbbreviationToType[abbrev])
		}
	}
}
