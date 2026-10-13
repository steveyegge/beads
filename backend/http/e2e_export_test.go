//go:build cgo

package bdhttp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type exportLine struct {
	Type         string   `json:"_type"`
	ID           string   `json:"id"`
	Ephemeral    bool     `json:"ephemeral"`
	Labels       []string `json:"labels"`
	CommentCount int      `json:"comment_count"`
	DepCount     int      `json:"dependency_count"`
	Dependencies []struct {
		IssueID     string `json:"issue_id"`
		DependsOnID string `json:"depends_on_id"`
		Type        string `json:"type"`
	} `json:"dependencies"`
	Comments []struct {
		Text string `json:"text"`
	} `json:"comments"`
}

func readExportLines(t *testing.T, path string) map[string]exportLine {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	out := map[string]exportLine{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec exportLine
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("parse export line %q: %v", line, err)
		}
		if rec.Type == "issue" {
			out[rec.ID] = rec
		}
	}
	return out
}

// TestE2E_ExportOverHTTP is S21. `bd export` read through the raw SearchIssues
// method and the bulk relation reads, which the http backend refuses, so gc's
// JSONL-export order failed on every remote scope. It now composes served role
// reads (issueops.ReadExport): the rows with their labels and counts, every
// row's dependency records and the comment bodies.
func TestE2E_ExportOverHTTP(t *testing.T) {
	bd := connectE2EWorkspace(t, "worker")

	blocked := createE2EIssue(t, bd, "blocked work")
	blocker := createE2EIssue(t, bd, "blocker")
	mustRun(t, bd, "dep", "add", blocked, blocker, "--json")
	mustRun(t, bd, "label", "add", blocked, "lane:x", "--json")
	mustRun(t, bd, "comments", "add", blocked, "an export comment")
	closed := createE2EIssue(t, bd, "closed work")
	mustRun(t, bd, "close", closed, "--json")
	wisp := firstJSONField(t, mustRun(t, bd, "create", "a wisp", "--ephemeral", "--json").stdout, "id")

	dir := t.TempDir()
	all := filepath.Join(dir, "all.jsonl")
	mustRun(t, bd, "export", "--all", "-o", all)
	got := readExportLines(t, all)
	for _, id := range []string{blocked, blocker, closed, wisp} {
		if _, ok := got[id]; !ok {
			t.Errorf("bd export --all is missing %s; got %d issues", id, len(got))
		}
	}
	rec := got[blocked]
	if len(rec.Dependencies) != 1 || rec.Dependencies[0].DependsOnID != blocker || rec.DepCount != 1 {
		t.Errorf("%s dependencies = %+v (count %d), want one edge onto %s", blocked, rec.Dependencies, rec.DepCount, blocker)
	}
	if len(rec.Labels) != 1 || rec.Labels[0] != "lane:x" {
		t.Errorf("%s labels = %v, want [lane:x]", blocked, rec.Labels)
	}
	if rec.CommentCount != 1 || len(rec.Comments) != 1 || rec.Comments[0].Text != "an export comment" {
		t.Errorf("%s comments = %+v (count %d), want the one comment", blocked, rec.Comments, rec.CommentCount)
	}
	if !got[wisp].Ephemeral {
		t.Errorf("%s exported without ephemeral: %+v", wisp, got[wisp])
	}

	// The default export keeps every status and drops the true wisp.
	def := filepath.Join(dir, "default.jsonl")
	mustRun(t, bd, "export", "-o", def)
	gotDef := readExportLines(t, def)
	if _, ok := gotDef[wisp]; ok {
		t.Errorf("bd export (default) exported the wisp %s", wisp)
	}
	for _, id := range []string{blocked, blocker, closed} {
		if _, ok := gotDef[id]; !ok {
			t.Errorf("bd export (default) is missing %s", id)
		}
	}
}
