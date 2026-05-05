//go:build cgo

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/types"
)

func TestEmbeddedPromptCapture(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt prompt tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, beadsDir, _ := bdInit(t, bd, "--prefix", "pc")

	parent := bdCreate(t, bd, dir, "Parent work")

	cmd := exec.Command(
		bd,
		"prompt",
		"capture",
		"--json",
		"--title", "Capture raw user request",
		"--summary", "Build prompt capture",
		"--session", "sess-123",
		"--source-tool", "codex",
		"--parent", parent.ID,
		"--label", "product",
		"--stdin",
	)
	cmd.Dir = dir
	cmd.Env = bdEnv(dir)
	cmd.Stdin = strings.NewReader("please remember this exact user prompt")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bd prompt capture failed: %v\n%s", err, out)
	}

	issue := parseIssueJSON(t, out)
	if issue.ID == "" {
		t.Fatal("expected issue ID")
	}
	if issue.Title != "Capture raw user request" {
		t.Errorf("title: got %q", issue.Title)
	}
	if issue.Description != "please remember this exact user prompt" {
		t.Errorf("description: got %q", issue.Description)
	}
	if issue.IssueType != types.TypeTask {
		t.Errorf("type: got %q, want %q", issue.IssueType, types.TypeTask)
	}

	labels := map[string]bool{}
	for _, label := range issue.Labels {
		labels[label] = true
	}
	for _, want := range []string{"prompt", "user-request", "product"} {
		if !labels[want] {
			t.Errorf("missing label %q in %#v", want, issue.Labels)
		}
	}

	shown := bdShow(t, bd, dir, issue.ID)
	var metadata map[string]string
	if err := json.Unmarshal(shown.Metadata, &metadata); err != nil {
		t.Fatalf("metadata json: %v\n%s", err, shown.Metadata)
	}
	if metadata["kind"] != "prompt" {
		t.Errorf("metadata kind: got %q", metadata["kind"])
	}
	if metadata["source"] != "user_prompt" {
		t.Errorf("metadata source: got %q", metadata["source"])
	}
	if metadata["session_id"] != "sess-123" {
		t.Errorf("metadata session_id: got %q", metadata["session_id"])
	}
	if metadata["source_tool"] != "codex" {
		t.Errorf("metadata source_tool: got %q", metadata["source_tool"])
	}
	if metadata["summary"] != "Build prompt capture" {
		t.Errorf("metadata summary: got %q", metadata["summary"])
	}
	if metadata["cwd"] != dir {
		t.Errorf("metadata cwd: got %q, want %q", metadata["cwd"], dir)
	}

	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		t.Fatalf("load metadata: %v", err)
	}
	database := "beads"
	if cfg != nil && cfg.DoltDatabase != "" {
		database = cfg.DoltDatabase
	}
	assertDepExistsWithType(t, filepath.Join(beadsDir), database, issue.ID, parent.ID, string(types.DepParentChild))
}
