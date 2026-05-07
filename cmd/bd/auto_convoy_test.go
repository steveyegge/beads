//go:build cgo

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestIsCrewActor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		actor string
		want  bool
	}{
		{"whatsapp_automation/crew/digo", true},
		{"lexbh/crew/mila", true},
		{"property_scrapers/crew/thies", true},

		// Non-crew actors must NOT trigger auto-convoy.
		{"mayor", false},
		{"daemon", false},
		{"whatsapp_automation/polecats/furiosa", false},
		{"whatsapp_automation/dogs/deacon", false},
		{"whatsapp_automation/digo", false},  // missing /crew/ segment
		{"crew/digo", false},                 // missing rig segment
		{"whatsapp_automation/crew/", false}, // empty name
		{"/crew/digo", false},                // empty rig
		{"", false},
		{"whatsapp_automation/crew/digo/extra", false}, // too many segments
	}
	for _, c := range cases {
		if got := isCrewActor(c.actor); got != c.want {
			t.Errorf("isCrewActor(%q) = %v, want %v", c.actor, got, c.want)
		}
	}
}

func TestGenerateConvoyShortID(t *testing.T) {
	t.Parallel()

	// Length and charset are part of the contract: gastown's hq-cv-<5lower>
	// convention is what tooling parses.
	for i := 0; i < 50; i++ {
		id := generateConvoyShortID()
		if len(id) != shortIDLen {
			t.Fatalf("expected %d chars, got %d (%q)", shortIDLen, len(id), id)
		}
		for _, r := range id {
			if !((r >= 'a' && r <= 'z') || (r >= '2' && r <= '7')) {
				t.Fatalf("non-base32-lower char %q in id %q", r, id)
			}
		}
	}
}

func TestAutoConvoyDescriptionMatchesGastownPattern(t *testing.T) {
	t.Parallel()

	// gastown's findConvoyByDescription matches "tracking <beadID>" — keeping
	// this stable is what makes cross-rig discovery work when the tracks dep
	// can't be persisted.
	desc := autoConvoyDescription("wa-abc123")
	if !strings.Contains(desc, "tracking wa-abc123") {
		t.Fatalf("description %q must contain `tracking wa-abc123` for gastown discovery", desc)
	}
}

// TestCreateAutoConvoyBeadWritesToHQ verifies that the storage-API path
// produces an open convoy bead with the canonical title prefix and the
// gastown-compatible description, so ConvoyManager + findConvoyByDescription
// can both pick it up.
func TestCreateAutoConvoyBeadWritesToHQ(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	hqBeadsDir := filepath.Join(tmpDir, ".beads")
	hqDB := filepath.Join(hqBeadsDir, "beads.db")

	hqStore := newTestStore(t, hqDB)
	// Configure HQ with custom types so "convoy" is accepted (matches real HQ).
	if err := hqStore.SetConfig(context.Background(), "types.custom",
		"agent,role,rig,convoy,slot,queue,event,message,molecule,gate,merge-request"); err != nil {
		t.Fatalf("seed types.custom: %v", err)
	}
	_ = hqStore.Close()

	convoyID := "hq-cv-" + generateConvoyShortID()
	trackedID := "wa-test001"
	if err := createAutoConvoyBead(context.Background(), hqBeadsDir, convoyID,
		"Work (crew): example task", trackedID, "test/crew/example"); err != nil {
		t.Fatalf("createAutoConvoyBead: %v", err)
	}

	verifyStore := newTestStore(t, hqDB)
	defer func() { _ = verifyStore.Close() }()

	got, err := verifyStore.GetIssue(context.Background(), convoyID)
	if err != nil {
		t.Fatalf("GetIssue(%s): %v", convoyID, err)
	}
	if got.IssueType != types.IssueType("convoy") {
		t.Errorf("issue type = %q, want convoy", got.IssueType)
	}
	if got.Status != types.StatusOpen {
		t.Errorf("status = %q, want open", got.Status)
	}
	if !strings.HasPrefix(got.Title, "Work (crew):") {
		t.Errorf("title = %q, want prefix `Work (crew):`", got.Title)
	}
	if !strings.Contains(got.Description, "tracking "+trackedID) {
		t.Errorf("description = %q, must contain `tracking %s`", got.Description, trackedID)
	}
}
