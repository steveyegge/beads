package slackbot

import (
	"strings"
	"testing"
)

func TestUrgencyToEmoji(t *testing.T) {
	tests := []struct {
		name    string
		urgency string
		want    string
	}{
		{name: "high", urgency: "high", want: ":red_circle:"},
		{name: "medium", urgency: "medium", want: ":large_yellow_circle:"},
		{name: "low", urgency: "low", want: ":large_green_circle:"},
		{name: "empty string", urgency: "", want: ":white_circle:"},
		{name: "unknown value", urgency: "unknown", want: ":white_circle:"},
		{name: "critical not recognized", urgency: "critical", want: ":white_circle:"},
		{name: "HIGH is case sensitive", urgency: "HIGH", want: ":white_circle:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := urgencyToEmoji(tt.urgency)
			if got != tt.want {
				t.Errorf("urgencyToEmoji(%q) = %q, want %q", tt.urgency, got, tt.want)
			}
		})
	}
}

func TestFormatResolver(t *testing.T) {
	tests := []struct {
		name       string
		resolverID string
		want       string
	}{
		{name: "empty string", resolverID: "", want: "unknown"},
		{name: "slack prefix", resolverID: "slack:U12345", want: "<@U12345>"},
		{name: "slack prefix with long ID", resolverID: "slack:U12345ABC", want: "<@U12345ABC>"},
		{name: "raw slack user ID starting with U", resolverID: "U12345ABC", want: "<@U12345ABC>"},
		{name: "raw slack user ID exactly 4 chars", resolverID: "U123", want: "<@U123>"},
		{name: "agent name passthrough", resolverID: "agent-name", want: "agent-name"},
		{name: "rpc-client passthrough", resolverID: "rpc-client", want: "rpc-client"},
		{name: "U too short for auto-detection", resolverID: "U", want: "U"},
		{name: "U with two chars", resolverID: "U12", want: "U12"},
		{name: "U with three chars exactly", resolverID: "U1A", want: "U1A"},
		{name: "contains colon but not slack prefix", resolverID: "other:U12345", want: "other:U12345"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatResolver(tt.resolverID)
			if got != tt.want {
				t.Errorf("formatResolver(%q) = %q, want %q", tt.resolverID, got, tt.want)
			}
		})
	}
}

func TestTruncateForSlack(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{name: "short string under limit", s: "hello", maxLen: 10, want: "hello"},
		{name: "exactly at limit", s: "hello", maxLen: 5, want: "hello"},
		{name: "over limit by one", s: "hello!", maxLen: 5, want: "he..."},
		{name: "well over limit", s: "this is a long string", maxLen: 10, want: "this is..."},
		{name: "maxLen of 3 yields just ellipsis", s: "abcdef", maxLen: 3, want: "..."},
		{name: "empty string", s: "", maxLen: 10, want: ""},
		{name: "single char under limit", s: "a", maxLen: 5, want: "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateForSlack(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateForSlack(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestExtractTypeFromContext(t *testing.T) {
	tests := []struct {
		name string
		ctx  string
		want string
	}{
		{name: "empty string", ctx: "", want: ""},
		{name: "not json", ctx: "not json", want: ""},
		{name: "valid _type field", ctx: `{"_type":"tradeoff"}`, want: "tradeoff"},
		{name: "no _type field", ctx: `{"foo":"bar"}`, want: ""},
		{name: "_type not a string (number)", ctx: `{"_type": 123}`, want: ""},
		{name: "_type not a string (bool)", ctx: `{"_type": true}`, want: ""},
		{name: "_type not a string (null)", ctx: `{"_type": null}`, want: ""},
		{name: "_type with other fields", ctx: `{"_type":"stuck","question":"help"}`, want: "stuck"},
		{name: "json array not object", ctx: `["a","b"]`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTypeFromContext(tt.ctx)
			if got != tt.want {
				t.Errorf("extractTypeFromContext(%q) = %q, want %q", tt.ctx, got, tt.want)
			}
		})
	}
}

func TestBuildTypeHeader(t *testing.T) {
	tests := []struct {
		name      string
		ctx       string
		wantEmoji string
		wantLabel string
	}{
		{name: "empty context", ctx: "", wantEmoji: "", wantLabel: ""},
		{name: "non-json context", ctx: "plain text", wantEmoji: "", wantLabel: ""},
		{name: "json without _type", ctx: `{"foo":"bar"}`, wantEmoji: "", wantLabel: ""},
		{name: "known type tradeoff", ctx: `{"_type":"tradeoff"}`, wantEmoji: "\u2696\ufe0f", wantLabel: "Tradeoff Decision"},
		{name: "known type stuck", ctx: `{"_type":"stuck"}`, wantEmoji: "\U0001f6a8", wantLabel: "Stuck - Need Help"},
		{name: "known type confirmation", ctx: `{"_type":"confirmation"}`, wantEmoji: "\u2705", wantLabel: "Confirmation"},
		{name: "known type checkpoint", ctx: `{"_type":"checkpoint"}`, wantEmoji: "\U0001f6a7", wantLabel: "Checkpoint"},
		{name: "known type assessment", ctx: `{"_type":"assessment"}`, wantEmoji: "\U0001f4ca", wantLabel: "Assessment"},
		{name: "known type ambiguity", ctx: `{"_type":"ambiguity"}`, wantEmoji: "\u2753", wantLabel: "Ambiguity Clarification"},
		{name: "unknown type uses default emoji and title-cased label", ctx: `{"_type":"mystery"}`, wantEmoji: "\U0001f4cb", wantLabel: "Mystery Decision"},
		{name: "unknown type starting with uppercase", ctx: `{"_type":"Zephyr"}`, wantEmoji: "\U0001f4cb", wantLabel: "Zephyr Decision"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEmoji, gotLabel := buildTypeHeader(tt.ctx)
			if gotEmoji != tt.wantEmoji {
				t.Errorf("buildTypeHeader(%q) emoji = %q, want %q", tt.ctx, gotEmoji, tt.wantEmoji)
			}
			if gotLabel != tt.wantLabel {
				t.Errorf("buildTypeHeader(%q) label = %q, want %q", tt.ctx, gotLabel, tt.wantLabel)
			}
		})
	}
}

func TestFormatContextForSlack(t *testing.T) {
	tests := []struct {
		name string
		ctx  string
		// For exact matches use want; for substring checks use wantContains/wantPrefix.
		want         string
		wantContains string
		wantPrefix   string
		wantEmpty    bool
	}{
		{
			name:  "empty string",
			ctx:   "",
			want:  "",
		},
		{
			name: "plain text not json",
			ctx:  "some plain text context",
			want: "some plain text context",
		},
		{
			name: "plain text long truncated",
			ctx:  strings.Repeat("x", 3000),
			want: strings.Repeat("x", 2897) + "...",
		},
		{
			name: "json with _value string field",
			ctx:  `{"_type":"tradeoff","_value":"important decision details"}`,
			want: "important decision details",
		},
		{
			name:       "json with _value non-string field (object)",
			ctx:        `{"_type":"tradeoff","_value":{"key":"val"}}`,
			wantPrefix: "```\n",
		},
		{
			name:      "json object stripped of all metadata leaves empty",
			ctx:       `{"_type":"tradeoff","_session_id":"abc","session_id":"def","referenced_beads":[],"successor_schemas":[]}`,
			wantEmpty: true,
		},
		{
			name:         "json object after stripping metadata keeps remaining fields",
			ctx:          `{"_type":"tradeoff","custom_field":"hello"}`,
			wantContains: "custom_field",
		},
		{
			name:         "json object formatted as code block",
			ctx:          `{"_type":"tradeoff","detail":"info"}`,
			wantPrefix:   "```\n",
			wantContains: "detail",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatContextForSlack(tt.ctx)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("formatContextForSlack(%q) = %q, want empty", tt.ctx, got)
				}
				return
			}
			if tt.want != "" {
				if got != tt.want {
					t.Errorf("formatContextForSlack(%q) = %q, want %q", tt.ctx, got, tt.want)
				}
			}
			if tt.wantPrefix != "" {
				if !strings.HasPrefix(got, tt.wantPrefix) {
					t.Errorf("formatContextForSlack(%q) = %q, want prefix %q", tt.ctx, got, tt.wantPrefix)
				}
			}
			if tt.wantContains != "" {
				if !strings.Contains(got, tt.wantContains) {
					t.Errorf("formatContextForSlack(%q) = %q, want to contain %q", tt.ctx, got, tt.wantContains)
				}
			}
		})
	}
}

func TestFormatContextForSlack_LongJSON(t *testing.T) {
	// Build a JSON object that will exceed the slack block limit when pretty-printed.
	longValue := strings.Repeat("a", 3000)
	ctx := `{"data":"` + longValue + `"}`
	got := formatContextForSlack(ctx)

	if len(got) > 2900 {
		t.Errorf("formatContextForSlack with long JSON: len=%d, want <= 2900", len(got))
	}
	if !strings.HasPrefix(got, "```\n") {
		t.Errorf("formatContextForSlack with long JSON should start with code block, got prefix %q", got[:10])
	}
	if !strings.HasSuffix(got, "\n```") {
		t.Errorf("formatContextForSlack with long JSON should end with code block, got suffix %q", got[len(got)-10:])
	}
}

func TestExtractAgentShortName(t *testing.T) {
	tests := []struct {
		name  string
		agent string
		want  string
	}{
		{name: "three-part path", agent: "gastown/polecats/furiosa", want: "furiosa"},
		{name: "single name", agent: "mayor", want: "mayor"},
		{name: "empty string", agent: "", want: ""},
		{name: "two-part path", agent: "a/b", want: "b"},
		{name: "trailing slash", agent: "a/b/", want: ""},
		{name: "deep nesting", agent: "a/b/c/d/e", want: "e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAgentShortName(tt.agent)
			if got != tt.want {
				t.Errorf("extractAgentShortName(%q) = %q, want %q", tt.agent, got, tt.want)
			}
		})
	}
}
