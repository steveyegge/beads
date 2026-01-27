package notification

import (
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// hq-946577.21: Tests for email notification templates

func TestRenderEmail_Basic(t *testing.T) {
	timeout := time.Now().Add(24 * time.Hour)
	payload := &DecisionPayload{
		Type:   "decision_point",
		ID:     "gt-abc123.decision-1",
		Prompt: "Which caching strategy should we use?",
		Options: []types.DecisionOption{
			{ID: "a", Short: "Redis", Label: "Use Redis for caching"},
			{ID: "b", Short: "Memory", Label: "Use in-memory cache"},
		},
		Default:    "a",
		TimeoutAt:  &timeout,
		RespondURL: "https://beads.example.com/api/decisions/gt-abc123.decision-1/respond",
		ViewURL:    "https://beads.example.com/decisions/gt-abc123.decision-1",
		Source: &PayloadSource{
			Molecule: "gt-abc123",
			Step:     "cache-decision",
			Agent:    "beads/crew/test",
		},
	}

	result, err := RenderEmail(payload)
	if err != nil {
		t.Fatalf("RenderEmail failed: %v", err)
	}

	// Check subject
	if !strings.Contains(result.Subject, "[Decision Required]") {
		t.Errorf("Subject missing [Decision Required]: %q", result.Subject)
	}
	if !strings.Contains(result.Subject, "caching strategy") {
		t.Errorf("Subject missing prompt: %q", result.Subject)
	}

	// Check plain text contains key elements
	if !strings.Contains(result.PlainText, "gt-abc123") {
		t.Error("PlainText missing molecule ID")
	}
	if !strings.Contains(result.PlainText, "Which caching strategy") {
		t.Error("PlainText missing prompt")
	}
	if !strings.Contains(result.PlainText, "[a] Redis") {
		t.Error("PlainText missing option a")
	}
	if !strings.Contains(result.PlainText, "[b] Memory") {
		t.Error("PlainText missing option b")
	}
	if !strings.Contains(result.PlainText, "(default)") {
		t.Error("PlainText missing default marker")
	}
	if !strings.Contains(result.PlainText, "bd decision respond") {
		t.Error("PlainText missing CLI instructions")
	}

	// Check HTML contains key elements
	if !strings.Contains(result.HTML, "<!DOCTYPE html>") {
		t.Error("HTML missing doctype")
	}
	if !strings.Contains(result.HTML, "Decision Required") {
		t.Error("HTML missing header")
	}
	if !strings.Contains(result.HTML, "Which caching strategy") {
		t.Error("HTML missing prompt")
	}
	if !strings.Contains(result.HTML, "Use Redis for caching") {
		t.Error("HTML missing option label")
	}
	if !strings.Contains(result.HTML, "Respond Now") {
		t.Error("HTML missing CTA button")
	}
	if !strings.Contains(result.HTML, "beads/crew/test") {
		t.Error("HTML missing agent attribution")
	}
}

func TestRenderEmail_NoOptions(t *testing.T) {
	payload := &DecisionPayload{
		Type:   "decision_point",
		ID:     "gt-abc123.decision-1",
		Prompt: "Please provide your feedback",
		Source: &PayloadSource{
			Molecule: "gt-abc123",
		},
	}

	result, err := RenderEmail(payload)
	if err != nil {
		t.Fatalf("RenderEmail failed: %v", err)
	}

	// Should still render without options
	if !strings.Contains(result.PlainText, "Please provide your feedback") {
		t.Error("PlainText missing prompt")
	}
	// Should have fallback example in instructions
	if !strings.Contains(result.PlainText, `"yes"`) {
		t.Error("PlainText missing fallback example")
	}
}

func TestRenderEmail_LongSubject(t *testing.T) {
	payload := &DecisionPayload{
		Type:   "decision_point",
		ID:     "test-123",
		Prompt: "This is a very long prompt that should be truncated in the email subject line to prevent overly long subjects",
	}

	result, err := RenderEmail(payload)
	if err != nil {
		t.Fatalf("RenderEmail failed: %v", err)
	}

	// Subject should be truncated
	if len(result.Subject) > 100 {
		t.Errorf("Subject too long (%d chars): %q", len(result.Subject), result.Subject)
	}
	if !strings.HasSuffix(result.Subject, "...") {
		t.Errorf("Truncated subject should end with ...: %q", result.Subject)
	}
}

func TestRenderEmail_Timeout(t *testing.T) {
	// Note: Add extra time to avoid sub-second drift causing boundary issues
	tests := []struct {
		name        string
		timeout     time.Duration
		wantContain string
	}{
		{"hours", 25 * time.Hour, "1 days"},     // 25h ensures we stay above 24h threshold
		{"2 days", 49 * time.Hour, "2 days"},    // 49h ensures we stay above 48h threshold
		{"30 minutes", 30 * time.Minute, "minutes"},
		{"days and hours", 55 * time.Hour, "2 days, 6 hours"}, // 55h ensures we stay above 54h = 2d6h
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timeout := time.Now().Add(tt.timeout)
			payload := &DecisionPayload{
				Type:      "decision_point",
				ID:        "test-123",
				Prompt:    "Test?",
				TimeoutAt: &timeout,
			}

			result, err := RenderEmail(payload)
			if err != nil {
				t.Fatalf("RenderEmail failed: %v", err)
			}

			if !strings.Contains(result.PlainText, tt.wantContain) {
				t.Errorf("PlainText should contain %q for %v timeout", tt.wantContain, tt.timeout)
			}
		})
	}
}

func TestRenderEmail_OverdueTimeout(t *testing.T) {
	timeout := time.Now().Add(-1 * time.Hour) // 1 hour ago
	payload := &DecisionPayload{
		Type:      "decision_point",
		ID:        "test-123",
		Prompt:    "Test?",
		TimeoutAt: &timeout,
	}

	result, err := RenderEmail(payload)
	if err != nil {
		t.Fatalf("RenderEmail failed: %v", err)
	}

	if !strings.Contains(result.PlainText, "OVERDUE") {
		t.Error("PlainText should show OVERDUE for past timeout")
	}
}

func TestTruncateSubject(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"hello world test", 12, "hello wor..."}, // idx=5 is not > maxLen/2=6, so no word break
		{"nospaces", 5, "no..."},
		{"exactly ten", 11, "exactly ten"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncateSubject(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateSubject(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		want     string
	}{
		{30 * time.Minute, "30 minutes"},
		{1 * time.Hour, "1 hours"},
		{24 * time.Hour, "1 days"},
		{25 * time.Hour, "1 days, 1 hours"},
		{48 * time.Hour, "2 days"},
		{72 * time.Hour, "3 days"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatDuration(tt.duration)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, got, tt.want)
			}
		})
	}
}

func TestFormatOptionsCompact(t *testing.T) {
	payload := &DecisionPayload{
		Options: []types.DecisionOption{
			{ID: "a", Short: "Redis", Label: "Use Redis"},
			{ID: "b", Short: "Memory", Label: "In-memory"},
		},
	}

	got := FormatOptionsCompact(payload)
	want := "a) Redis  b) Memory"

	if got != want {
		t.Errorf("FormatOptionsCompact() = %q, want %q", got, want)
	}
}

func TestFormatOptionsCompact_NoShort(t *testing.T) {
	payload := &DecisionPayload{
		Options: []types.DecisionOption{
			{ID: "yes", Label: "Yes, proceed"},
			{ID: "no", Label: "No, abort"},
		},
	}

	got := FormatOptionsCompact(payload)
	want := "yes) Yes, proceed  no) No, abort"

	if got != want {
		t.Errorf("FormatOptionsCompact() = %q, want %q", got, want)
	}
}

func TestFormatOptionsCompact_Empty(t *testing.T) {
	payload := &DecisionPayload{}

	got := FormatOptionsCompact(payload)
	if got != "" {
		t.Errorf("FormatOptionsCompact() = %q, want empty", got)
	}
}

func TestRenderEmail_HTMLEscaping(t *testing.T) {
	// Test that HTML special characters are escaped
	payload := &DecisionPayload{
		Type:   "decision_point",
		ID:     "test-123",
		Prompt: "Use <script>alert('xss')</script> or safe?",
		Options: []types.DecisionOption{
			{ID: "a", Label: "Option with <tags>"},
		},
	}

	result, err := RenderEmail(payload)
	if err != nil {
		t.Fatalf("RenderEmail failed: %v", err)
	}

	// HTML should escape the script tag
	if strings.Contains(result.HTML, "<script>") {
		t.Error("HTML contains unescaped script tag")
	}
	if !strings.Contains(result.HTML, "&lt;script&gt;") {
		t.Error("HTML should contain escaped script tag")
	}
}
