package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	toon "github.com/toon-format/toon-go"
)

// Uses captureStdout from init_test.go (signature: func(t, func() error) string)

func withBDEnv(t *testing.T, value string, fn func()) {
	t.Helper()
	orig := os.Getenv("BD_OUTPUT_FORMAT")
	if value == "" {
		os.Unsetenv("BD_OUTPUT_FORMAT")
	} else {
		os.Setenv("BD_OUTPUT_FORMAT", value)
	}
	defer func() {
		if orig == "" {
			os.Unsetenv("BD_OUTPUT_FORMAT")
		} else {
			os.Setenv("BD_OUTPUT_FORMAT", orig)
		}
	}()
	fn()
}

// --- resolveOutputFormat tests ---

func TestResolveOutputFormat(t *testing.T) {
	t.Run("default is json", func(t *testing.T) {
		withBDEnv(t, "", func() {
			if got := resolveOutputFormat(); got != "json" {
				t.Errorf("got %q, want \"json\"", got)
			}
		})
	})

	t.Run("toon when env set", func(t *testing.T) {
		withBDEnv(t, "toon", func() {
			if got := resolveOutputFormat(); got != "toon" {
				t.Errorf("got %q, want \"toon\"", got)
			}
		})
	})

	t.Run("case insensitive", func(t *testing.T) {
		withBDEnv(t, "TOON", func() {
			if got := resolveOutputFormat(); got != "toon" {
				t.Errorf("got %q, want \"toon\"", got)
			}
		})
	})

	t.Run("unknown value defaults to json", func(t *testing.T) {
		withBDEnv(t, "yaml", func() {
			if got := resolveOutputFormat(); got != "json" {
				t.Errorf("got %q, want \"json\"", got)
			}
		})
	})
}

// --- outputJSON tests ---

func TestOutputJSON_Default(t *testing.T) {
	withBDEnv(t, "", func() {
		data := map[string]interface{}{
			"id":    "bd-1",
			"title": "Test Issue",
			"count": 42,
		}
		got := captureStdout(t, func() error {
			outputJSON(data)
			return nil
		})

		var result map[string]interface{}
		if err := json.Unmarshal([]byte(got), &result); err != nil {
			t.Fatalf("not valid JSON: %v\nGot: %s", err, got)
		}
		if result["id"] != "bd-1" {
			t.Errorf("got id %v, want \"bd-1\"", result["id"])
		}
		if result["count"] != float64(42) {
			t.Errorf("got count %v, want 42", result["count"])
		}
	})
}

func TestOutputJSON_Array(t *testing.T) {
	withBDEnv(t, "", func() {
		data := []map[string]string{
			{"id": "bd-1", "title": "First"},
			{"id": "bd-2", "title": "Second"},
		}
		got := captureStdout(t, func() error {
			outputJSON(data)
			return nil
		})

		var result []map[string]string
		if err := json.Unmarshal([]byte(got), &result); err != nil {
			t.Fatalf("not valid JSON array: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 items, got %d", len(result))
		}
	})
}

func TestOutputJSON_TOONMode(t *testing.T) {
	withBDEnv(t, "toon", func() {
		data := []map[string]string{
			{"id": "bd-1", "title": "First"},
			{"id": "bd-2", "title": "Second"},
		}
		got := captureStdout(t, func() error {
			outputJSON(data)
			return nil
		})

		// TOON output should not be valid JSON for arrays
		var discard interface{}
		if err := json.Unmarshal([]byte(got), &discard); err == nil {
			t.Error("TOON output should not be valid JSON for arrays of maps")
		}
		if strings.TrimSpace(got) == "" {
			t.Error("expected non-empty output")
		}
	})
}

func TestOutputJSON_TOONPreservesData(t *testing.T) {
	withBDEnv(t, "toon", func() {
		data := map[string]interface{}{
			"id":    "bd-abc",
			"title": "Round trip test",
		}
		got := captureStdout(t, func() error {
			outputJSON(data)
			return nil
		})
		if !strings.Contains(got, "bd-abc") {
			t.Errorf("TOON output should contain the issue ID, got:\n%s", got)
		}
		if !strings.Contains(got, "Round trip test") {
			t.Errorf("TOON output should contain the title, got:\n%s", got)
		}
	})
}

func TestOutputJSON_Nil(t *testing.T) {
	withBDEnv(t, "", func() {
		got := captureStdout(t, func() error {
			outputJSON(nil)
			return nil
		})
		if strings.TrimSpace(got) != "null" {
			t.Errorf("expected \"null\", got: %q", strings.TrimSpace(got))
		}
	})
}

// --- outputJSONRaw tests ---

func TestOutputJSONRaw_IgnoresEnv(t *testing.T) {
	withBDEnv(t, "toon", func() {
		data := map[string]string{"key": "value"}
		got := captureStdout(t, func() error {
			outputJSONRaw(data)
			return nil
		})
		var parsed map[string]string
		if err := json.Unmarshal([]byte(got), &parsed); err != nil {
			t.Fatalf("outputJSONRaw should always produce JSON: %v", err)
		}
		if parsed["key"] != "value" {
			t.Errorf("got %q, want \"value\"", parsed["key"])
		}
	})
}

// --- Size comparison test ---

type benchIssue struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Type        string   `json:"type"`
	Priority    int      `json:"priority"`
	Assignee    string   `json:"assignee"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	Labels      []string `json:"labels"`
	Description string   `json:"description"`
}

func makeIssue(i int) benchIssue {
	statuses := []string{"open", "in_progress", "closed", "blocked"}
	types := []string{"task", "bug", "feature", "epic"}
	return benchIssue{
		ID:        fmt.Sprintf("bd-%04d", i),
		Title:     fmt.Sprintf("Implement feature %d: add support for advanced config options", i),
		Status:    statuses[i%len(statuses)],
		Type:      types[i%len(types)],
		Priority:  i % 5,
		Assignee:  "polecat-1",
		CreatedAt: "2026-02-10T08:00:00Z",
		UpdatedAt: "2026-02-13T12:00:00Z",
		Labels:    []string{"enhancement", "v2"},
		Description: fmt.Sprintf("Task %d requires changes to the config parser, "+
			"the validation layer, and the CLI flags. Acceptance criteria: all tests pass, "+
			"new tests cover added functionality, docs updated.", i),
	}
}

func makeIssues(n int) []benchIssue {
	out := make([]benchIssue, n)
	for i := range out {
		out[i] = makeIssue(i)
	}
	return out
}

func TestTOONSizeSavings(t *testing.T) {
	cases := []struct {
		name string
		data interface{}
	}{
		{"issues_10", makeIssues(10)},
		{"issues_50", makeIssues(50)},
		{"issues_100", makeIssues(100)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jsonPretty, err := json.MarshalIndent(tc.data, "", "  ")
			if err != nil {
				t.Fatalf("json marshal: %v", err)
			}

			// bd's round-trip path: json.Marshal -> json.Unmarshal -> toon.Marshal
			jsonBytes, _ := json.Marshal(tc.data)
			var generic interface{}
			json.Unmarshal(jsonBytes, &generic)
			toonBytes, err := toon.Marshal(generic)
			if err != nil {
				t.Fatalf("toon marshal: %v", err)
			}

			jsonSize := len(jsonPretty)
			toonSize := len(toonBytes)
			saving := float64(jsonSize-toonSize) / float64(jsonSize) * 100

			t.Logf("JSON: %6d bytes | TOON: %6d bytes | saving: %.1f%%", jsonSize, toonSize, saving)

			if toonSize >= jsonSize {
				t.Errorf("TOON (%d) should be smaller than JSON (%d)", toonSize, jsonSize)
			}
		})
	}
}

// --- Benchmarks ---

func BenchmarkOutputJSON_Issues10(b *testing.B) {
	data := makeIssues(10)

	b.Run("json", func(b *testing.B) {
		os.Unsetenv("BD_OUTPUT_FORMAT")
		old := os.Stdout
		os.Stdout, _ = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		defer func() { os.Stdout = old }()
		for b.Loop() {
			outputJSON(data)
		}
	})

	b.Run("toon_roundtrip", func(b *testing.B) {
		os.Setenv("BD_OUTPUT_FORMAT", "toon")
		defer os.Unsetenv("BD_OUTPUT_FORMAT")
		old := os.Stdout
		os.Stdout, _ = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		defer func() { os.Stdout = old }()
		for b.Loop() {
			outputJSON(data)
		}
	})
}

func BenchmarkOutputJSON_Issues50(b *testing.B) {
	data := makeIssues(50)

	b.Run("json", func(b *testing.B) {
		os.Unsetenv("BD_OUTPUT_FORMAT")
		old := os.Stdout
		os.Stdout, _ = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		defer func() { os.Stdout = old }()
		for b.Loop() {
			outputJSON(data)
		}
	})

	b.Run("toon_roundtrip", func(b *testing.B) {
		os.Setenv("BD_OUTPUT_FORMAT", "toon")
		defer os.Unsetenv("BD_OUTPUT_FORMAT")
		old := os.Stdout
		os.Stdout, _ = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		defer func() { os.Stdout = old }()
		for b.Loop() {
			outputJSON(data)
		}
	})
}

// Marshal-only benchmark to isolate serialization cost
func BenchmarkMarshalOnly_Issues50(b *testing.B) {
	data := makeIssues(50)

	b.Run("json_pretty", func(b *testing.B) {
		for b.Loop() {
			json.MarshalIndent(data, "", "  ")
		}
	})

	b.Run("toon_roundtrip", func(b *testing.B) {
		for b.Loop() {
			jsonBytes, _ := json.Marshal(data)
			var generic interface{}
			json.Unmarshal(jsonBytes, &generic)
			toon.Marshal(generic)
		}
	})
}

// Size benchmark reporting bytes/op
func BenchmarkSize_Issues50(b *testing.B) {
	data := makeIssues(50)

	b.Run("json", func(b *testing.B) {
		var totalBytes int64
		for b.Loop() {
			out, _ := json.MarshalIndent(data, "", "  ")
			totalBytes += int64(len(out))
		}
		b.ReportMetric(float64(totalBytes)/float64(b.N), "bytes/op")
	})

	b.Run("toon_roundtrip", func(b *testing.B) {
		var totalBytes int64
		for b.Loop() {
			jsonBytes, _ := json.Marshal(data)
			var generic interface{}
			json.Unmarshal(jsonBytes, &generic)
			out, _ := toon.Marshal(generic)
			totalBytes += int64(len(out))
		}
		b.ReportMetric(float64(totalBytes)/float64(b.N), "bytes/op")
	})
}
