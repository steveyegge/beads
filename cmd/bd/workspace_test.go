package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func setMtime(t *testing.T, path string, ts time.Time) {
	t.Helper()
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatalf("chtimes failed: %v", err)
	}
}

func TestWorkspaceScanDetectsUpdatedNotes(t *testing.T) {
	tmpDir := t.TempDir()
	workspacePath = filepath.Join(tmpDir, "workspace")
	hubPath = filepath.Join(tmpDir, "workspace-hub")

	src := filepath.Join(workspacePath, "proj-a", "specs", "alpha.md")
	writeFile(t, src, "# Alpha\n")

	note := filepath.Join(hubPath, "inbox", "proj-a_alpha.md")
	writeFile(t, note, "# Alpha\n")
	setMtime(t, note, time.Now().Add(-2*time.Hour))

	result := scanWorkspace()
	var got *WorkspaceScanItem
	for i := range result.Items {
		if result.Items[i].Path == src {
			got = &result.Items[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("expected item for %s", src)
	}
	if got.Status != "updated" {
		t.Fatalf("expected status updated, got %s", got.Status)
	}
}

func TestWorkspaceScanRoutesBuckets(t *testing.T) {
	tmpDir := t.TempDir()
	workspacePath = filepath.Join(tmpDir, "workspace")
	hubPath = filepath.Join(tmpDir, "workspace-hub")

	spec := filepath.Join(workspacePath, "proj-a", "specs", "alpha.md")
	doc := filepath.Join(workspacePath, "proj-b", "docs", "guide.md")
	writeFile(t, spec, "# Alpha\n")
	writeFile(t, doc, "# Guide\n")

	result := scanWorkspace()
	var specItem, docItem *WorkspaceScanItem
	for i := range result.Items {
		item := &result.Items[i]
		if item.Path == spec {
			specItem = item
		} else if item.Path == doc {
			docItem = item
		}
	}
	if specItem == nil || docItem == nil {
		t.Fatalf("expected both spec and doc items")
	}
	if specItem.Decision != "inbox" {
		t.Fatalf("expected spec decision inbox, got %s", specItem.Decision)
	}
	if docItem.Decision != "triage" {
		t.Fatalf("expected doc decision triage, got %s", docItem.Decision)
	}
	if !strings.Contains(specItem.HubNotePath, string(filepath.Separator)+"inbox"+string(filepath.Separator)) {
		t.Fatalf("expected spec note in inbox, got %s", specItem.HubNotePath)
	}
	if !strings.Contains(docItem.HubNotePath, string(filepath.Separator)+"triage"+string(filepath.Separator)) {
		t.Fatalf("expected doc note in triage, got %s", docItem.HubNotePath)
	}
}

func TestWorkspaceScanWritesReport(t *testing.T) {
	tmpDir := t.TempDir()
	workspacePath = filepath.Join(tmpDir, "workspace")
	hubPath = filepath.Join(tmpDir, "workspace-hub")

	src := filepath.Join(workspacePath, "proj-a", "specs", "alpha.md")
	writeFile(t, src, "# Alpha\n")

	result := scanWorkspace()
	reportPath, err := writeScanReport(result)
	if err != nil {
		t.Fatalf("writeScanReport failed: %v", err)
	}
	if reportPath == "" {
		t.Fatalf("expected report path")
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected report file: %v", err)
	}
}

func TestWorkspaceScanCreateBeadsFlag(t *testing.T) {
	tmpDir := t.TempDir()
	workspacePath = filepath.Join(tmpDir, "workspace")
	hubPath = filepath.Join(tmpDir, "workspace-hub")

	src := filepath.Join(workspacePath, "proj-a", "specs", "alpha.md")
	writeFile(t, src, "# Alpha\n")

	result := scanWorkspace()
	called := 0
	origCreate := createBeadFn
	createBeadFn = func(item WorkspaceScanItem) (string, error) {
		called++
		if item.Path != src {
			t.Fatalf("unexpected item path: %s", item.Path)
		}
		return "bd-test", nil
	}
	defer func() { createBeadFn = origCreate }()

	_, err := applyHubNotes(result, true)
	if err != nil {
		t.Fatalf("applyHubNotes failed: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected createBeadFn called once, got %d", called)
	}

	// Note should include bead id and be routed to active
	notePath := filepath.Join(hubPath, "active", "proj-a_alpha.md")
	content, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("expected active note: %v", err)
	}
	if !strings.Contains(string(content), "bd-test") {
		t.Fatalf("expected note to include bead id")
	}
}

func TestValidateJSONApplyRequiresYes(t *testing.T) {
	if err := validateJSONApply(true, true, false); err == nil {
		t.Fatalf("expected error when --json and --apply without --yes")
	}
	if err := validateJSONApply(true, true, true); err != nil {
		t.Fatalf("unexpected error when --json and --apply with --yes: %v", err)
	}
	if err := validateJSONApply(true, false, false); err != nil {
		t.Fatalf("unexpected error when --json without --apply: %v", err)
	}
}
