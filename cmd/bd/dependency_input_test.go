package main

import (
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestParseDependencyTypeStrict_AcceptsAllKnownTypes(t *testing.T) {
	for _, depType := range knownDependencyTypes {
		if !depType.IsWellKnown() {
			t.Fatalf("test setup bug: %q is not a well-known dependency type", depType)
		}

		got, err := parseDependencyTypeStrict(string(depType))
		if err != nil {
			t.Fatalf("parseDependencyTypeStrict(%q) returned error: %v", depType, err)
		}
		if got != depType {
			t.Fatalf("parseDependencyTypeStrict(%q) = %q, want %q", depType, got, depType)
		}
	}
}

// TestKnownDependencyTypes_BidirectionalSync ensures knownDependencyTypes and
// IsWellKnown() stay in sync in both directions: every well-known constant
// defined in types.go must appear in knownDependencyTypes, and vice versa.
func TestKnownDependencyTypes_BidirectionalSync(t *testing.T) {
	// All well-known dependency type constants defined in internal/types.
	// Keep this list in sync with the const block in types.go.
	allWellKnownConstants := []types.DependencyType{
		types.DepBlocks,
		types.DepParentChild,
		types.DepConditionalBlocks,
		types.DepWaitsFor,
		types.DepRelated,
		types.DepDiscoveredFrom,
		types.DepRepliesTo,
		types.DepRelatesTo,
		types.DepDuplicates,
		types.DepSupersedes,
		types.DepAuthoredBy,
		types.DepAssignedTo,
		types.DepApprovedBy,
		types.DepAttests,
		types.DepTracks,
		types.DepUntil,
		types.DepCausedBy,
		types.DepValidates,
		types.DepDelegatedFrom,
	}

	knownSet := make(map[types.DependencyType]bool, len(knownDependencyTypes))
	for _, dt := range knownDependencyTypes {
		knownSet[dt] = true
	}

	// Forward: every constant in knownDependencyTypes must be well-known.
	for _, dt := range knownDependencyTypes {
		if !dt.IsWellKnown() {
			t.Errorf("knownDependencyTypes contains %q which is not well-known", dt)
		}
	}

	// Reverse: every well-known constant must appear in knownDependencyTypes.
	for _, dt := range allWellKnownConstants {
		if !dt.IsWellKnown() {
			t.Errorf("test setup bug: %q listed in allWellKnownConstants but IsWellKnown() returns false", dt)
			continue
		}
		if !knownSet[dt] {
			t.Errorf("well-known type %q is accepted by IsWellKnown() but missing from knownDependencyTypes", dt)
		}
	}
}

func TestParseDependencyTypeStrict_RejectsUnknownTypes(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantContain string
	}{
		{
			name:        "empty",
			raw:         "",
			wantContain: "invalid dependency type",
		},
		{
			name:        "custom",
			raw:         "custom-type",
			wantContain: "custom types are non-blocking",
		},
		{
			name:        "needs alias",
			raw:         "needs",
			wantContain: `use "blocks"`,
		},
		{
			name:        "depends-on alias",
			raw:         "depends-on",
			wantContain: `use "blocks"`,
		},
		{
			name:        "blocked_by alias",
			raw:         "blocked_by",
			wantContain: `use "blocks"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDependencyTypeStrict(tt.raw)
			if err == nil {
				t.Fatalf("parseDependencyTypeStrict(%q) should fail", tt.raw)
			}
			if !strings.Contains(err.Error(), tt.wantContain) {
				t.Fatalf("parseDependencyTypeStrict(%q) error = %q, want substring %q", tt.raw, err, tt.wantContain)
			}
		})
	}
}

func TestParseDependencySpec(t *testing.T) {
	tests := []struct {
		name          string
		spec          string
		wantType      types.DependencyType
		wantDependsOn string
		wantErr       string
	}{
		{
			name:          "bare issue id defaults to blocks",
			spec:          "bd-123",
			wantType:      types.DepBlocks,
			wantDependsOn: "bd-123",
		},
		{
			name:          "typed dependency",
			spec:          "discovered-from:bd-123",
			wantType:      types.DepDiscoveredFrom,
			wantDependsOn: "bd-123",
		},
		{
			name:          "typed dependency normalizes case",
			spec:          "ReLaTeD:bd-123",
			wantType:      types.DepRelated,
			wantDependsOn: "bd-123",
		},
		{
			name:          "bare external dependency defaults to blocks",
			spec:          "external:other-rig:capability-x",
			wantType:      types.DepBlocks,
			wantDependsOn: "external:other-rig:capability-x",
		},
		{
			name:          "typed external dependency target",
			spec:          "blocks:external:other-rig:capability-x",
			wantType:      types.DepBlocks,
			wantDependsOn: "external:other-rig:capability-x",
		},
		{
			name:    "missing target id",
			spec:    "blocks:",
			wantErr: "missing target ID",
		},
		{
			name:    "unknown type alias",
			spec:    "needs:bd-123",
			wantErr: `use "blocks"`,
		},
		{
			name:    "empty dependency",
			spec:    "  ",
			wantErr: "cannot be empty",
		},
		// Finding #1: malformed external refs must be rejected.
		{
			name:    "bare external missing capability",
			spec:    "external:proj",
			wantErr: "invalid external",
		},
		{
			name:    "bare external empty project",
			spec:    "external::cap",
			wantErr: "missing project",
		},
		{
			name:    "bare external empty ref",
			spec:    "external:",
			wantErr: "invalid external",
		},
		{
			name:    "typed external missing capability",
			spec:    "related:external:proj",
			wantErr: "invalid external",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDependencySpec(tt.spec)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parseDependencySpec(%q) should fail", tt.spec)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseDependencySpec(%q) error = %q, want substring %q", tt.spec, err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseDependencySpec(%q) error: %v", tt.spec, err)
			}
			if got.Type != tt.wantType {
				t.Fatalf("parseDependencySpec(%q) type = %q, want %q", tt.spec, got.Type, tt.wantType)
			}
			if got.DependsOnID != tt.wantDependsOn {
				t.Fatalf("parseDependencySpec(%q) dependsOn = %q, want %q", tt.spec, got.DependsOnID, tt.wantDependsOn)
			}
		})
	}
}

func TestParseDependencySpecs(t *testing.T) {
	deps, err := parseDependencySpecs([]string{
		"  ",
		"bd-1",
		"related:bd-2",
		"",
	})
	if err != nil {
		t.Fatalf("parseDependencySpecs() error: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("parseDependencySpecs() returned %d entries, want 2", len(deps))
	}
	if deps[0].Type != types.DepBlocks || deps[0].DependsOnID != "bd-1" {
		t.Fatalf("deps[0] = (%q, %q), want (%q, %q)", deps[0].Type, deps[0].DependsOnID, types.DepBlocks, "bd-1")
	}
	if deps[1].Type != types.DepRelated || deps[1].DependsOnID != "bd-2" {
		t.Fatalf("deps[1] = (%q, %q), want (%q, %q)", deps[1].Type, deps[1].DependsOnID, types.DepRelated, "bd-2")
	}
}

// TestParseDependencySpecs_FailFast verifies that the first invalid spec
// causes an immediate error (no partial results).
func TestParseDependencySpecs_FailFast(t *testing.T) {
	_, err := parseDependencySpecs([]string{
		"blocks:bd-1",
		"bad-type:bd-2",
		"related:bd-3",
	})
	if err == nil {
		t.Fatal("expected error for invalid dependency type in batch, got nil")
	}
	if !strings.Contains(err.Error(), "bad-type") {
		t.Fatalf("error should mention the bad type, got: %v", err)
	}
}
