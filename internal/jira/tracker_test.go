package jira

import (
	"testing"

	"github.com/steveyegge/beads/internal/tracker"
)

func TestRegistered(t *testing.T) {
	factory := tracker.Get("jira")
	if factory == nil {
		t.Fatal("jira tracker not registered")
	}
	tr := factory()
	if tr.Name() != "jira" {
		t.Errorf("Name() = %q, want %q", tr.Name(), "jira")
	}
	if tr.DisplayName() != "Jira" {
		t.Errorf("DisplayName() = %q, want %q", tr.DisplayName(), "Jira")
	}
	if tr.ConfigPrefix() != "jira" {
		t.Errorf("ConfigPrefix() = %q, want %q", tr.ConfigPrefix(), "jira")
	}
}

func TestIsExternalRef(t *testing.T) {
	tr := &Tracker{jiraURL: "https://company.atlassian.net"}
	tests := []struct {
		ref  string
		want bool
	}{
		{"https://company.atlassian.net/browse/PROJ-123", true},
		{"https://company.atlassian.net/browse/TEAM-1", true},
		{"https://other.atlassian.net/browse/PROJ-123", false},
		{"https://linear.app/team/issue/PROJ-123", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tr.IsExternalRef(tt.ref); got != tt.want {
			t.Errorf("IsExternalRef(%q) = %v, want %v", tt.ref, got, tt.want)
		}
	}
}

func TestExtractIdentifier(t *testing.T) {
	tr := &Tracker{}
	tests := []struct {
		ref  string
		want string
	}{
		{"https://company.atlassian.net/browse/PROJ-123", "PROJ-123"},
		{"https://company.atlassian.net/browse/TEAM-1", "TEAM-1"},
		{"not-a-url", ""},
	}
	for _, tt := range tests {
		if got := tr.ExtractIdentifier(tt.ref); got != tt.want {
			t.Errorf("ExtractIdentifier(%q) = %q, want %q", tt.ref, got, tt.want)
		}
	}
}

func TestBuildExternalRef(t *testing.T) {
	tr := &Tracker{jiraURL: "https://company.atlassian.net"}
	ti := &tracker.TrackerIssue{Identifier: "PROJ-123"}
	ref := tr.BuildExternalRef(ti)
	want := "https://company.atlassian.net/browse/PROJ-123"
	if ref != want {
		t.Errorf("BuildExternalRef() = %q, want %q", ref, want)
	}
}

func TestStubMethodsReturnErrNotImplemented(t *testing.T) {
	tr := &Tracker{}
	if _, err := tr.FetchIssues(nil, tracker.FetchOptions{}); err != ErrNotImplemented {
		t.Errorf("FetchIssues error = %v, want ErrNotImplemented", err)
	}
	if _, err := tr.FetchIssue(nil, "X-1"); err != ErrNotImplemented {
		t.Errorf("FetchIssue error = %v, want ErrNotImplemented", err)
	}
	if _, err := tr.CreateIssue(nil, nil); err != ErrNotImplemented {
		t.Errorf("CreateIssue error = %v, want ErrNotImplemented", err)
	}
	if _, err := tr.UpdateIssue(nil, "", nil); err != ErrNotImplemented {
		t.Errorf("UpdateIssue error = %v, want ErrNotImplemented", err)
	}
}
