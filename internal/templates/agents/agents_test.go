package agents

import (
	"strings"
	"testing"
)

func TestEmbeddedDefault(t *testing.T) {
	content := EmbeddedDefault()

	if content == "" {
		t.Fatal("EmbeddedDefault() returned empty string")
	}

	required := []string{
		"# Agent Instructions",
		"## Quick Reference",
		"bd onboard",
		"BEGIN BEADS INTEGRATION",
		"END BEADS INTEGRATION",
		"## Landing the Plane",
		"git push",
	}
	for _, want := range required {
		if !strings.Contains(content, want) {
			t.Errorf("EmbeddedDefault() missing %q", want)
		}
	}
}

func TestEmbeddedBeadsSection(t *testing.T) {
	section := EmbeddedBeadsSection()

	if section == "" {
		t.Fatal("EmbeddedBeadsSection() returned empty string")
	}

	if !strings.HasPrefix(section, "<!-- BEGIN BEADS INTEGRATION -->") {
		t.Error("beads section should start with begin marker")
	}

	trimmed := strings.TrimSpace(section)
	if !strings.HasSuffix(trimmed, "<!-- END BEADS INTEGRATION -->") {
		t.Error("beads section should end with end marker")
	}

	required := []string{
		"bd create",
		"bd update",
		"bd close",
		"bd ready",
		"discovered-from",
	}
	for _, want := range required {
		if !strings.Contains(section, want) {
			t.Errorf("EmbeddedBeadsSection() missing %q", want)
		}
	}
}

func TestBeadsSectionDoesNotContainLanding(t *testing.T) {
	section := EmbeddedBeadsSection()
	if strings.Contains(section, "Landing the Plane") {
		t.Error("beads section should NOT contain landing-the-plane content")
	}
}

func TestDefaultContainsBothSections(t *testing.T) {
	content := EmbeddedDefault()

	beadsIdx := strings.Index(content, "BEGIN BEADS INTEGRATION")
	landingIdx := strings.Index(content, "Landing the Plane")

	if beadsIdx == -1 {
		t.Fatal("missing beads integration section")
	}
	if landingIdx == -1 {
		t.Fatal("missing landing the plane section")
	}
	if beadsIdx > landingIdx {
		t.Error("beads section should come before landing-the-plane section")
	}
}
