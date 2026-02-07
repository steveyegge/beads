package slackbot

import (
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// mockChannelCreator implements ChannelCreator for testing epic routing.
// ---------------------------------------------------------------------------

type mockChannelCreator struct {
	channels map[string]string // name -> id
}

func (m *mockChannelCreator) FindChannelByName(name string) (string, error) {
	if id, ok := m.channels[name]; ok {
		return id, nil
	}
	return "", fmt.Errorf("not found")
}

func (m *mockChannelCreator) CreateChannel(name string) (string, error) {
	id := "C" + strings.ToUpper(strings.ReplaceAll(name, "-", ""))
	m.channels[name] = id
	return id, nil
}

// ---------------------------------------------------------------------------
// 1. matchPattern
// ---------------------------------------------------------------------------

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern []string
		agent   []string
		want    bool
	}{
		{
			name:    "exact match single segment",
			pattern: []string{"mayor"},
			agent:   []string{"mayor"},
			want:    true,
		},
		{
			name:    "exact match multi segment",
			pattern: []string{"gastown", "polecats", "furiosa"},
			agent:   []string{"gastown", "polecats", "furiosa"},
			want:    true,
		},
		{
			name:    "wildcard in last segment",
			pattern: []string{"gastown", "polecats", "*"},
			agent:   []string{"gastown", "polecats", "furiosa"},
			want:    true,
		},
		{
			name:    "wildcard in first segment",
			pattern: []string{"*", "crew", "bob"},
			agent:   []string{"gastown", "crew", "bob"},
			want:    true,
		},
		{
			name:    "all wildcards",
			pattern: []string{"*", "*", "*"},
			agent:   []string{"a", "b", "c"},
			want:    true,
		},
		{
			name:    "length mismatch pattern longer",
			pattern: []string{"gastown", "polecats", "furiosa"},
			agent:   []string{"gastown", "polecats"},
			want:    false,
		},
		{
			name:    "length mismatch agent longer",
			pattern: []string{"gastown"},
			agent:   []string{"gastown", "polecats"},
			want:    false,
		},
		{
			name:    "mismatch in middle segment",
			pattern: []string{"gastown", "crew", "furiosa"},
			agent:   []string{"gastown", "polecats", "furiosa"},
			want:    false,
		},
		{
			name:    "empty slices match",
			pattern: []string{},
			agent:   []string{},
			want:    true,
		},
		{
			name:    "empty pattern non-empty agent",
			pattern: []string{},
			agent:   []string{"mayor"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchPattern(tt.pattern, tt.agent)
			if got != tt.want {
				t.Errorf("matchPattern(%v, %v) = %v, want %v",
					tt.pattern, tt.agent, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 2. countWildcards
// ---------------------------------------------------------------------------

func TestCountWildcards(t *testing.T) {
	tests := []struct {
		name     string
		segments []string
		want     int
	}{
		{"no wildcards", []string{"gastown", "polecats", "furiosa"}, 0},
		{"single wildcard", []string{"gastown", "*", "furiosa"}, 1},
		{"multiple wildcards", []string{"*", "*", "*"}, 3},
		{"mixed wildcards", []string{"*", "polecats", "*"}, 2},
		{"empty slice", []string{}, 0},
		{"star-like but not wildcard", []string{"gastown", "**", "furiosa"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countWildcards(tt.segments)
			if got != tt.want {
				t.Errorf("countWildcards(%v) = %d, want %d",
					tt.segments, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 3. patternLessThan
// ---------------------------------------------------------------------------

func TestPatternLessThan(t *testing.T) {
	tests := []struct {
		name string
		a, b compiledPattern
		want bool
	}{
		{
			name: "more segments wins",
			a:    compiledPattern{original: "a/b/c", segments: []string{"a", "b", "c"}},
			b:    compiledPattern{original: "a/b", segments: []string{"a", "b"}},
			want: true,
		},
		{
			name: "fewer segments loses",
			a:    compiledPattern{original: "a/b", segments: []string{"a", "b"}},
			b:    compiledPattern{original: "a/b/c", segments: []string{"a", "b", "c"}},
			want: false,
		},
		{
			name: "fewer wildcards wins when same segments",
			a:    compiledPattern{original: "a/b/c", segments: []string{"a", "b", "c"}},
			b:    compiledPattern{original: "a/*/c", segments: []string{"a", "*", "c"}},
			want: true,
		},
		{
			name: "more wildcards loses when same segments",
			a:    compiledPattern{original: "*/*/c", segments: []string{"*", "*", "c"}},
			b:    compiledPattern{original: "a/*/c", segments: []string{"a", "*", "c"}},
			want: false,
		},
		{
			name: "alphabetical tie-breaker a < b",
			a:    compiledPattern{original: "alpha/*", segments: []string{"alpha", "*"}},
			b:    compiledPattern{original: "beta/*", segments: []string{"beta", "*"}},
			want: true,
		},
		{
			name: "alphabetical tie-breaker a > b",
			a:    compiledPattern{original: "beta/*", segments: []string{"beta", "*"}},
			b:    compiledPattern{original: "alpha/*", segments: []string{"alpha", "*"}},
			want: false,
		},
		{
			name: "identical patterns are not less than",
			a:    compiledPattern{original: "a/b", segments: []string{"a", "b"}},
			b:    compiledPattern{original: "a/b", segments: []string{"a", "b"}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := patternLessThan(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("patternLessThan(%q, %q) = %v, want %v",
					tt.a.original, tt.b.original, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4. DeriveChannelSlugWithMaxLen
// ---------------------------------------------------------------------------

func TestDeriveChannelSlugWithMaxLen(t *testing.T) {
	tests := []struct {
		name   string
		title  string
		maxLen int
		want   string
	}{
		{"empty string", "", 30, ""},
		{"simple title", "My Epic", 30, "my-epic"},
		{"special characters", "Fix: Bug #42!", 30, "fix-bug-42"},
		{"unicode combining accent kept", "cafe\u0301 latte", 30, "cafe-latte"},
		{"consecutive specials collapse", "hello---world", 30, "hello-world"},
		{"leading trailing specials", "  --hello-- ", 30, "hello"},
		{
			"truncation at word boundary",
			"this is a really long title that should be truncated nicely",
			30,
			"this-is-a-really-long-title",
		},
		{
			"truncation preserves short slug",
			"short",
			30,
			"short",
		},
		{
			"truncation with maxLen 10 no word break when hyphen at midpoint",
			"alpha-bravo-charlie",
			10,
			"alpha-brav",
		},
		{
			"truncation with maxLen 11 word break past midpoint",
			"alpha-bravo-charlie",
			11,
			"alpha-bravo",
		},
		{
			"all special chars",
			"!@#$%^&*()",
			30,
			"",
		},
		{
			"numbers preserved",
			"Sprint 42 Review",
			30,
			"sprint-42-review",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveChannelSlugWithMaxLen(tt.title, tt.maxLen)
			if got != tt.want {
				t.Errorf("DeriveChannelSlugWithMaxLen(%q, %d) = %q, want %q",
					tt.title, tt.maxLen, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 5. DeriveChannelSlug delegates to WithMaxLen(title, 30)
// ---------------------------------------------------------------------------

func TestDeriveChannelSlug(t *testing.T) {
	title := "this is a really long title that should be truncated nicely"
	got := DeriveChannelSlug(title)
	want := DeriveChannelSlugWithMaxLen(title, 30)
	if got != want {
		t.Errorf("DeriveChannelSlug(%q) = %q, want %q (same as WithMaxLen 30)", title, got, want)
	}
}

// ---------------------------------------------------------------------------
// 6. sanitizeChannelName
// ---------------------------------------------------------------------------

func TestSanitizeChannelName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercase conversion", "BD-Decisions", "bd-decisions"},
		{"invalid chars replaced", "bd_decisions.test", "bd-decisions-test"},
		{"consecutive hyphens collapsed", "bd---decisions", "bd-decisions"},
		{"leading trailing hyphens trimmed", "-bd-decisions-", "bd-decisions"},
		{"already valid", "bd-decisions-gastown", "bd-decisions-gastown"},
		{
			"max 80 chars",
			strings.Repeat("abcdefghij", 10), // 100 chars
			strings.Repeat("abcdefghij", 8),   // 80 chars
		},
		{
			"truncation strips trailing hyphens",
			strings.Repeat("a-", 50), // 100 chars "a-a-a-..."
			strings.Repeat("a-", 40)[:79],
		},
		{"empty string", "", ""},
		{"spaces become hyphens", "bd decisions test", "bd-decisions-test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeChannelName(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeChannelName(%q) = %q, want %q", tt.in, got, tt.want)
			}
			// Slack invariants: only a-z, 0-9, hyphens; max 80
			if len(got) > 80 {
				t.Errorf("sanitizeChannelName(%q) produced %d chars, want <= 80", tt.in, len(got))
			}
			for _, r := range got {
				if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
					t.Errorf("sanitizeChannelName(%q) contains invalid rune %q", tt.in, string(r))
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 7. AgentToChannelName
// ---------------------------------------------------------------------------

func TestAgentToChannelName(t *testing.T) {
	tests := []struct {
		name   string
		agent  string
		prefix string
		want   string
	}{
		{
			"single segment agent",
			"mayor",
			"bd-decisions",
			"bd-decisions-mayor",
		},
		{
			"two segment agent uses both",
			"gastown/polecats",
			"bd-decisions",
			"bd-decisions-gastown-polecats",
		},
		{
			"three segment agent drops name",
			"gastown/polecats/furiosa",
			"bd-decisions",
			"bd-decisions-gastown-polecats",
		},
		{
			"empty prefix",
			"mayor",
			"",
			"mayor",
		},
		{
			"custom prefix",
			"citadel/warboys/nux",
			"slack",
			"slack-citadel-warboys",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AgentToChannelName(tt.agent, tt.prefix)
			if got != tt.want {
				t.Errorf("AgentToChannelName(%q, %q) = %q, want %q",
					tt.agent, tt.prefix, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 8. IsValidChannelMode
// ---------------------------------------------------------------------------

func TestIsValidChannelMode(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{"general", true},
		{"agent", true},
		{"epic", true},
		{"dm", true},
		{"General", false},
		{"AGENT", false},
		{"", false},
		{"thread", false},
		{"direct", false},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			got := IsValidChannelMode(tt.mode)
			if got != tt.want {
				t.Errorf("IsValidChannelMode(%q) = %v, want %v",
					tt.mode, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 9. normalizeAgent
// ---------------------------------------------------------------------------

func TestNormalizeAgent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no trailing slash", "gastown/polecats/furiosa", "gastown/polecats/furiosa"},
		{"single trailing slash", "mayor/", "mayor"},
		{"multiple trailing slashes", "mayor///", "mayor"},
		{"only slashes", "///", ""},
		{"empty string", "", ""},
		{"internal slashes preserved", "a/b/c", "a/b/c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeAgent(tt.input)
			if got != tt.want {
				t.Errorf("normalizeAgent(%q) = %q, want %q",
					tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 10. extractMetadataFromDescription
// ---------------------------------------------------------------------------

func TestExtractMetadataFromDescription(t *testing.T) {
	tests := []struct {
		name        string
		description string
		want        string
	}{
		{
			"metadata on single line",
			`metadata: {"enabled":true}`,
			`{"enabled":true}`,
		},
		{
			"metadata after other content",
			"Slack routing config\nVersion: 2\nmetadata: {\"channels\":{}}",
			`{"channels":{}}`,
		},
		{
			"no metadata line",
			"Just a description\nWith multiple lines",
			"",
		},
		{
			"empty description",
			"",
			"",
		},
		{
			"metadata with leading whitespace",
			"  metadata: {\"key\":\"val\"}",
			`{"key":"val"}`,
		},
		{
			"metadata prefix but no space-colon",
			"metadata_extra: stuff",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMetadataFromDescription(tt.description)
			if got != tt.want {
				t.Errorf("extractMetadataFromDescription(%q) = %q, want %q",
					tt.description, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helper: create a test router with a standard Config.
// ---------------------------------------------------------------------------

func testRouter() *Router {
	return NewRouter(&Config{
		Type:    "slack",
		Version: 1,
		Enabled: true,

		DefaultChannel: "C_DEFAULT",

		Channels: map[string]string{
			"gastown/polecats/*": "C_POLECATS",
			"gastown/crew/*":    "C_CREW",
			"*/crew/*":          "C_CREW_ALL",
			"citadel/*":         "C_CITADEL",
		},

		Overrides: map[string]string{
			"gastown/polecats/furiosa": "C_FURIOSA",
		},

		ChannelNames: map[string]string{
			"C_DEFAULT":  "#decisions",
			"C_FURIOSA":  "#furiosa-private",
			"C_POLECATS": "#polecats",
		},

		WebhookURL: "https://hooks.slack.com/default",
		ChannelWebhooks: map[string]string{
			"C_FURIOSA": "https://hooks.slack.com/furiosa",
		},
	})
}

// ---------------------------------------------------------------------------
// 11. Router.Resolve
// ---------------------------------------------------------------------------

func TestRouterResolve(t *testing.T) {
	r := testRouter()

	tests := []struct {
		name          string
		agent         string
		wantChannel   string
		wantMatchedBy string
		wantDefault   bool
		wantWebhook   string
	}{
		{
			"exact override wins over pattern",
			"gastown/polecats/furiosa",
			"C_FURIOSA",
			"(override)",
			false,
			"https://hooks.slack.com/furiosa",
		},
		{
			"pattern match three segments",
			"gastown/polecats/max",
			"C_POLECATS",
			"gastown/polecats/*",
			false,
			"https://hooks.slack.com/default",
		},
		{
			"wildcard first segment",
			"citadel/crew/nux",
			"C_CREW_ALL",
			"*/crew/*",
			false,
			"https://hooks.slack.com/default",
		},
		{
			"two-segment pattern match",
			"citadel/warboys",
			"C_CITADEL",
			"citadel/*",
			false,
			"https://hooks.slack.com/default",
		},
		{
			"no match falls to default",
			"bartertown/merchants/aunty",
			"C_DEFAULT",
			"(default)",
			true,
			"https://hooks.slack.com/default",
		},
		{
			"channel name resolved for override",
			"gastown/polecats/furiosa",
			"C_FURIOSA",
			"(override)",
			false,
			"https://hooks.slack.com/furiosa",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.Resolve(tt.agent)
			if result.ChannelID != tt.wantChannel {
				t.Errorf("Resolve(%q).ChannelID = %q, want %q",
					tt.agent, result.ChannelID, tt.wantChannel)
			}
			if result.MatchedBy != tt.wantMatchedBy {
				t.Errorf("Resolve(%q).MatchedBy = %q, want %q",
					tt.agent, result.MatchedBy, tt.wantMatchedBy)
			}
			if result.IsDefault != tt.wantDefault {
				t.Errorf("Resolve(%q).IsDefault = %v, want %v",
					tt.agent, result.IsDefault, tt.wantDefault)
			}
			if result.WebhookURL != tt.wantWebhook {
				t.Errorf("Resolve(%q).WebhookURL = %q, want %q",
					tt.agent, result.WebhookURL, tt.wantWebhook)
			}
		})
	}
}

func TestRouterResolve_ChannelName(t *testing.T) {
	r := testRouter()

	// Override has a known channel name.
	res := r.Resolve("gastown/polecats/furiosa")
	if res.ChannelName != "#furiosa-private" {
		t.Errorf("expected ChannelName #furiosa-private, got %q", res.ChannelName)
	}

	// Default has a known name too.
	res = r.Resolve("unknown/agent")
	if res.ChannelName != "#decisions" {
		t.Errorf("expected ChannelName #decisions for default, got %q", res.ChannelName)
	}
}

func TestRouterResolve_NilOverrides(t *testing.T) {
	r := NewRouter(&Config{
		DefaultChannel: "C_DEF",
		Channels:       map[string]string{"a/*": "C_A"},
		// Overrides intentionally nil
	})
	res := r.Resolve("a/b")
	if res.ChannelID != "C_A" {
		t.Errorf("expected C_A, got %q", res.ChannelID)
	}
}

// ---------------------------------------------------------------------------
// 12. Router.ResolveAll (deduplication)
// ---------------------------------------------------------------------------

func TestRouterResolveAll(t *testing.T) {
	r := testRouter()

	// Two agents that map to the same channel should be deduplicated.
	agents := []string{
		"gastown/polecats/max",
		"gastown/polecats/nux",
		"gastown/polecats/furiosa", // override -> different channel
		"bartertown/unknown",       // default
	}

	results := r.ResolveAll(agents)

	ids := make(map[string]bool)
	for _, res := range results {
		if ids[res.ChannelID] {
			t.Errorf("ResolveAll returned duplicate channel %q", res.ChannelID)
		}
		ids[res.ChannelID] = true
	}

	// Expect 3 unique: C_POLECATS, C_FURIOSA, C_DEFAULT
	if len(results) != 3 {
		t.Errorf("ResolveAll returned %d results, want 3 unique channels", len(results))
	}
}

func TestRouterResolveAll_Empty(t *testing.T) {
	r := testRouter()
	results := r.ResolveAll(nil)
	if len(results) != 0 {
		t.Errorf("ResolveAll(nil) returned %d results, want 0", len(results))
	}
}

// ---------------------------------------------------------------------------
// 13. HasOverride / GetOverride / AddOverride / RemoveOverride
// ---------------------------------------------------------------------------

func TestOverrideCRUD(t *testing.T) {
	r := NewRouter(&Config{DefaultChannel: "C_DEF"})

	// Initially no overrides.
	if r.HasOverride("agent/a") {
		t.Error("HasOverride should be false initially")
	}
	if got := r.GetOverride("agent/a"); got != "" {
		t.Errorf("GetOverride should be empty, got %q", got)
	}

	// Add an override.
	r.AddOverride("agent/a", "C_OVERRIDE")
	if !r.HasOverride("agent/a") {
		t.Error("HasOverride should be true after AddOverride")
	}
	if got := r.GetOverride("agent/a"); got != "C_OVERRIDE" {
		t.Errorf("GetOverride = %q, want C_OVERRIDE", got)
	}

	// Override takes precedence in Resolve.
	res := r.Resolve("agent/a")
	if res.ChannelID != "C_OVERRIDE" {
		t.Errorf("Resolve with override = %q, want C_OVERRIDE", res.ChannelID)
	}

	// Remove override returns previous value.
	prev := r.RemoveOverride("agent/a")
	if prev != "C_OVERRIDE" {
		t.Errorf("RemoveOverride returned %q, want C_OVERRIDE", prev)
	}
	if r.HasOverride("agent/a") {
		t.Error("HasOverride should be false after RemoveOverride")
	}

	// Remove on non-existent returns empty.
	prev = r.RemoveOverride("nonexistent")
	if prev != "" {
		t.Errorf("RemoveOverride(nonexistent) = %q, want empty", prev)
	}
}

func TestOverrideCRUD_NilOverridesMap(t *testing.T) {
	r := NewRouter(&Config{DefaultChannel: "C_DEF"})
	// Config.Overrides is nil by default; HasOverride / GetOverride should not panic.
	if r.HasOverride("x") {
		t.Error("HasOverride should be false with nil overrides map")
	}
	if got := r.GetOverride("x"); got != "" {
		t.Errorf("GetOverride should be empty with nil overrides map, got %q", got)
	}
	// RemoveOverride on nil map should not panic.
	prev := r.RemoveOverride("x")
	if prev != "" {
		t.Errorf("RemoveOverride on nil map = %q, want empty", prev)
	}
}

func TestAddOverrideWithName(t *testing.T) {
	r := NewRouter(&Config{DefaultChannel: "C_DEF"})
	r.AddOverrideWithName("agent/x", "C_X", "x-channel")

	if got := r.GetOverride("agent/x"); got != "C_X" {
		t.Errorf("GetOverride = %q, want C_X", got)
	}
	if got := r.channelName("C_X"); got != "x-channel" {
		t.Errorf("channelName = %q, want x-channel", got)
	}
}

// ---------------------------------------------------------------------------
// 14. GetAgentByChannel (reverse lookup)
// ---------------------------------------------------------------------------

func TestGetAgentByChannel(t *testing.T) {
	r := testRouter()

	// Known override mapping.
	agent := r.GetAgentByChannel("C_FURIOSA")
	if agent != "gastown/polecats/furiosa" {
		t.Errorf("GetAgentByChannel(C_FURIOSA) = %q, want gastown/polecats/furiosa", agent)
	}

	// Unknown channel returns empty.
	agent = r.GetAgentByChannel("C_UNKNOWN")
	if agent != "" {
		t.Errorf("GetAgentByChannel(C_UNKNOWN) = %q, want empty", agent)
	}

	// Nil overrides.
	r2 := NewRouter(&Config{DefaultChannel: "C_DEF"})
	agent = r2.GetAgentByChannel("C_FURIOSA")
	if agent != "" {
		t.Errorf("GetAgentByChannel with nil overrides = %q, want empty", agent)
	}
}

// ---------------------------------------------------------------------------
// 15. channelPrefix
// ---------------------------------------------------------------------------

func TestChannelPrefix(t *testing.T) {
	// Custom prefix.
	r := NewRouter(&Config{ChannelPrefix: "my-prefix"})
	if got := r.channelPrefix(); got != "my-prefix" {
		t.Errorf("channelPrefix() = %q, want my-prefix", got)
	}

	// Default when empty.
	r2 := NewRouter(&Config{})
	if got := r2.channelPrefix(); got != DefaultChannelPrefix {
		t.Errorf("channelPrefix() = %q, want %q", got, DefaultChannelPrefix)
	}
}

// ---------------------------------------------------------------------------
// 16. channelName
// ---------------------------------------------------------------------------

func TestChannelName(t *testing.T) {
	r := testRouter()

	// Known name.
	if got := r.channelName("C_DEFAULT"); got != "#decisions" {
		t.Errorf("channelName(C_DEFAULT) = %q, want #decisions", got)
	}

	// Unknown falls back to ID.
	if got := r.channelName("C_UNKNOWN"); got != "C_UNKNOWN" {
		t.Errorf("channelName(C_UNKNOWN) = %q, want C_UNKNOWN", got)
	}

	// Nil ChannelNames map.
	r2 := NewRouter(&Config{})
	if got := r2.channelName("C_X"); got != "C_X" {
		t.Errorf("channelName with nil map = %q, want C_X", got)
	}
}

// ---------------------------------------------------------------------------
// 17. ResolveForDecision
// ---------------------------------------------------------------------------

func TestResolveForDecision_ChannelHint(t *testing.T) {
	r := testRouter()

	result := r.ResolveForDecision("gastown/polecats/furiosa", nil, "C_HINT")
	if result.ChannelID != "C_HINT" {
		t.Errorf("channel hint: got %q, want C_HINT", result.ChannelID)
	}
	if result.MatchedBy != "(decision-hint)" {
		t.Errorf("MatchedBy = %q, want (decision-hint)", result.MatchedBy)
	}
}

func TestResolveForDecision_Override(t *testing.T) {
	r := testRouter()

	result := r.ResolveForDecision("gastown/polecats/furiosa", nil, "")
	if result.ChannelID != "C_FURIOSA" {
		t.Errorf("override: got %q, want C_FURIOSA", result.ChannelID)
	}
	if result.MatchedBy != "(override)" {
		t.Errorf("MatchedBy = %q, want (override)", result.MatchedBy)
	}
}

func TestResolveForDecision_PatternMatch(t *testing.T) {
	r := testRouter()

	result := r.ResolveForDecision("gastown/polecats/max", nil, "")
	if result.ChannelID != "C_POLECATS" {
		t.Errorf("pattern: got %q, want C_POLECATS", result.ChannelID)
	}
	if result.MatchedBy != "gastown/polecats/*" {
		t.Errorf("MatchedBy = %q, want gastown/polecats/*", result.MatchedBy)
	}
}

func TestResolveForDecision_Default(t *testing.T) {
	r := testRouter()

	result := r.ResolveForDecision("bartertown/unknown", nil, "")
	if result.ChannelID != "C_DEFAULT" {
		t.Errorf("default: got %q, want C_DEFAULT", result.ChannelID)
	}
	if !result.IsDefault {
		t.Error("expected IsDefault to be true")
	}
}

func TestResolveForDecision_EmptyAgent(t *testing.T) {
	r := testRouter()

	// Empty agent, no hint, no decision -> default.
	result := r.ResolveForDecision("", nil, "")
	if result.ChannelID != "C_DEFAULT" {
		t.Errorf("empty agent: got %q, want C_DEFAULT", result.ChannelID)
	}
	if !result.IsDefault {
		t.Error("expected IsDefault for empty agent")
	}
}

func TestResolveForDecision_EpicRouting_FindExisting(t *testing.T) {
	mock := &mockChannelCreator{
		channels: map[string]string{
			"bd-decisions-deploy-pipeline": "C_EXISTING",
		},
	}

	r := NewRouter(&Config{
		Enabled:        true,
		RoutingMode:    "epic",
		DefaultChannel: "C_DEF",
		DynamicChannels: true,
	})
	r.SetChannelCreator(mock)

	decision := &Decision{
		ParentBeadTitle: "Deploy Pipeline",
	}

	result := r.ResolveForDecision("", decision, "")
	if result.ChannelID != "C_EXISTING" {
		t.Errorf("epic find: got %q, want C_EXISTING", result.ChannelID)
	}
	if !strings.Contains(result.MatchedBy, "epic:") {
		t.Errorf("MatchedBy = %q, expected to contain 'epic:'", result.MatchedBy)
	}
}

func TestResolveForDecision_EpicRouting_CreateNew(t *testing.T) {
	mock := &mockChannelCreator{
		channels: map[string]string{},
	}

	r := NewRouter(&Config{
		Enabled:         true,
		RoutingMode:     "", // empty defaults to epic
		DefaultChannel:  "C_DEF",
		DynamicChannels: true,
	})
	r.SetChannelCreator(mock)

	decision := &Decision{
		ParentBeadTitle: "New Feature",
	}

	result := r.ResolveForDecision("", decision, "")
	if result.ChannelID == "C_DEF" {
		t.Error("expected epic channel to be created, got default")
	}
	if result.ChannelID == "" {
		t.Error("expected epic channel to be created, got empty")
	}
	// Verify the channel was created in mock.
	if _, ok := mock.channels["bd-decisions-new-feature"]; !ok {
		t.Error("expected channel 'bd-decisions-new-feature' to be created in mock")
	}
}

func TestResolveForDecision_EpicRouting_NoCreator(t *testing.T) {
	r := NewRouter(&Config{
		Enabled:         true,
		RoutingMode:     "epic",
		DefaultChannel:  "C_DEF",
		DynamicChannels: true,
		// No channel creator set
	})

	decision := &Decision{
		ParentBeadTitle: "Some Epic",
	}

	result := r.ResolveForDecision("", decision, "")
	// Without a ChannelCreator, should fall to default.
	if result.ChannelID != "C_DEF" {
		t.Errorf("no creator: got %q, want C_DEF", result.ChannelID)
	}
}

func TestResolveForDecision_EpicRouting_AgentMode(t *testing.T) {
	r := NewRouter(&Config{
		Enabled:         true,
		RoutingMode:     "agent", // not epic
		DefaultChannel:  "C_DEF",
		DynamicChannels: true,
	})

	decision := &Decision{
		ParentBeadTitle: "Some Epic",
	}

	// With routing_mode="agent", epic routing should not be attempted.
	result := r.ResolveForDecision("", decision, "")
	if result.ChannelID != "C_DEF" {
		t.Errorf("agent mode: got %q, want C_DEF (epic routing should be skipped)", result.ChannelID)
	}
}

func TestResolveForDecision_EpicRouting_NoDynamicChannels(t *testing.T) {
	mock := &mockChannelCreator{
		channels: map[string]string{},
	}

	r := NewRouter(&Config{
		Enabled:         true,
		RoutingMode:     "epic",
		DefaultChannel:  "C_DEF",
		DynamicChannels: false, // creation disabled
	})
	r.SetChannelCreator(mock)

	decision := &Decision{
		ParentBeadTitle: "Some Epic",
	}

	result := r.ResolveForDecision("", decision, "")
	// Channel doesn't exist and dynamic creation is off -> default.
	if result.ChannelID != "C_DEF" {
		t.Errorf("no dynamic: got %q, want C_DEF", result.ChannelID)
	}
}

// ---------------------------------------------------------------------------
// NewRouter with nil config
// ---------------------------------------------------------------------------

func TestNewRouterNilConfig(t *testing.T) {
	r := NewRouter(nil)
	if r.config == nil {
		t.Fatal("NewRouter(nil) should create a default config")
	}
	if r.config.Type != "slack" {
		t.Errorf("default config Type = %q, want slack", r.config.Type)
	}
	if r.config.Version != 1 {
		t.Errorf("default config Version = %d, want 1", r.config.Version)
	}
}

// ---------------------------------------------------------------------------
// Pattern specificity sorting (integration through Resolve)
// ---------------------------------------------------------------------------

func TestPatternSpecificitySorting(t *testing.T) {
	// "gastown/polecats/*" (3 segments, 1 wildcard) should beat
	// "*/crew/*" (3 segments, 2 wildcards) when both could match
	// gastown/crew/bob.
	r := NewRouter(&Config{
		DefaultChannel: "C_DEF",
		Channels: map[string]string{
			"gastown/crew/*": "C_GASTOWN_CREW",
			"*/crew/*":       "C_CREW_ALL",
		},
	})

	// gastown/crew/bob matches both; gastown/crew/* has fewer wildcards.
	res := r.Resolve("gastown/crew/bob")
	if res.ChannelID != "C_GASTOWN_CREW" {
		t.Errorf("specificity: got %q, want C_GASTOWN_CREW (fewer wildcards)", res.ChannelID)
	}
}

func TestPatternSegmentCountPriority(t *testing.T) {
	// 3-segment pattern should beat 2-segment pattern.
	r := NewRouter(&Config{
		DefaultChannel: "C_DEF",
		Channels: map[string]string{
			"gastown/*":         "C_2SEG",
			"gastown/crew/furiosa": "C_3SEG",
		},
	})

	res := r.Resolve("gastown/crew/furiosa")
	if res.ChannelID != "C_3SEG" {
		t.Errorf("segment count: got %q, want C_3SEG", res.ChannelID)
	}
}
