package idgen

import (
	"testing"
	"time"
)

func TestGenerateHashIDMatchesJiraVector(t *testing.T) {
	timestamp := time.Date(2024, 1, 2, 3, 4, 5, 6*1_000_000, time.UTC)
	prefix := "bd"
	title := "Fix login"
	description := "Details"
	creator := "jira-import"

	tests := map[int]string{
		3: "bd-vju",
		4: "bd-8d8e",
		5: "bd-bi3tk",
		6: "bd-8bi3tk",
		7: "bd-r5sr6bm",
		8: "bd-8r5sr6bm",
	}

	for length, expected := range tests {
		got := GenerateHashID(prefix, title, description, creator, timestamp, length, 0)
		if got != expected {
			t.Fatalf("length %d: got %s, want %s", length, got, expected)
		}
	}
}
