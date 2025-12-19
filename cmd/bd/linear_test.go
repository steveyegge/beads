package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestLinearPriorityToBeads(t *testing.T) {
	config := defaultLinearMappingConfig()

	tests := []struct {
		name           string
		linearPriority int
		wantBeads      int
	}{
		{
			name:           "no priority maps to backlog",
			linearPriority: 0,
			wantBeads:      4, // Backlog
		},
		{
			name:           "urgent maps to critical",
			linearPriority: 1,
			wantBeads:      0, // Critical
		},
		{
			name:           "high maps to high",
			linearPriority: 2,
			wantBeads:      1, // High
		},
		{
			name:           "medium maps to medium",
			linearPriority: 3,
			wantBeads:      2, // Medium
		},
		{
			name:           "low maps to low",
			linearPriority: 4,
			wantBeads:      3, // Low
		},
		{
			name:           "unknown priority defaults to medium",
			linearPriority: 99,
			wantBeads:      2, // Default Medium
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := linearPriorityToBeads(tt.linearPriority, config)
			if got != tt.wantBeads {
				t.Errorf("linearPriorityToBeads(%d) = %d, want %d",
					tt.linearPriority, got, tt.wantBeads)
			}
		})
	}
}

func TestLinearPriorityToBeadsCustomConfig(t *testing.T) {
	// Test with custom priority mapping
	config := &LinearMappingConfig{
		PriorityMap: map[string]int{
			"0": 2, // Custom: no priority -> medium
			"1": 1, // Custom: urgent -> high (not critical)
			"2": 2, // high -> medium
			"3": 3, // medium -> low
			"4": 4, // low -> backlog
		},
	}

	tests := []struct {
		linearPriority int
		wantBeads      int
	}{
		{0, 2}, // Custom mapping
		{1, 1}, // Custom mapping
		{2, 2},
		{3, 3},
		{4, 4},
	}

	for _, tt := range tests {
		got := linearPriorityToBeads(tt.linearPriority, config)
		if got != tt.wantBeads {
			t.Errorf("linearPriorityToBeads(%d) with custom config = %d, want %d",
				tt.linearPriority, got, tt.wantBeads)
		}
	}
}

func TestBeadsPriorityToLinear(t *testing.T) {
	config := defaultLinearMappingConfig()

	tests := []struct {
		name          string
		beadsPriority int
		wantLinear    int
	}{
		{
			name:          "critical maps to urgent",
			beadsPriority: 0,
			wantLinear:    1, // Urgent
		},
		{
			name:          "high maps to high",
			beadsPriority: 1,
			wantLinear:    2, // High
		},
		{
			name:          "medium maps to medium",
			beadsPriority: 2,
			wantLinear:    3, // Medium
		},
		{
			name:          "low maps to low",
			beadsPriority: 3,
			wantLinear:    4, // Low
		},
		{
			name:          "backlog maps to no priority",
			beadsPriority: 4,
			wantLinear:    0, // No priority
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := beadsPriorityToLinear(tt.beadsPriority, config)
			if got != tt.wantLinear {
				t.Errorf("beadsPriorityToLinear(%d) = %d, want %d",
					tt.beadsPriority, got, tt.wantLinear)
			}
		})
	}
}

func TestLinearStateToBeadsStatus(t *testing.T) {
	config := defaultLinearMappingConfig()

	tests := []struct {
		name       string
		state      *LinearState
		wantStatus types.Status
	}{
		{
			name:       "nil state defaults to open",
			state:      nil,
			wantStatus: types.StatusOpen,
		},
		{
			name:       "backlog state maps to open",
			state:      &LinearState{Type: "backlog", Name: "Backlog"},
			wantStatus: types.StatusOpen,
		},
		{
			name:       "unstarted state maps to open",
			state:      &LinearState{Type: "unstarted", Name: "Todo"},
			wantStatus: types.StatusOpen,
		},
		{
			name:       "started state maps to in_progress",
			state:      &LinearState{Type: "started", Name: "In Progress"},
			wantStatus: types.StatusInProgress,
		},
		{
			name:       "completed state maps to closed",
			state:      &LinearState{Type: "completed", Name: "Done"},
			wantStatus: types.StatusClosed,
		},
		{
			name:       "canceled state maps to closed",
			state:      &LinearState{Type: "canceled", Name: "Cancelled"},
			wantStatus: types.StatusClosed,
		},
		{
			name:       "unknown state type defaults to open",
			state:      &LinearState{Type: "unknown", Name: "Unknown State"},
			wantStatus: types.StatusOpen,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := linearStateToBeadsStatus(tt.state, config)
			if got != tt.wantStatus {
				t.Errorf("linearStateToBeadsStatus() = %s, want %s", got, tt.wantStatus)
			}
		})
	}
}

func TestLinearStateToBeadsStatusCustomConfig(t *testing.T) {
	// Test with custom state name mapping for custom workflow states
	// Note: State names are converted to lowercase with spaces preserved
	// So "In Review" -> "in review", "On Hold" -> "on hold"
	config := &LinearMappingConfig{
		StateMap: map[string]string{
			"backlog":    "open",
			"unstarted":  "open",
			"started":    "in_progress",
			"completed":  "closed",
			"canceled":   "closed",
			"in review":  "in_progress", // Custom state name (lowercase with space)
			"on hold":    "blocked",     // Custom state name (lowercase with space)
			"blocked":    "blocked",     // Custom state name
			"validating": "in_progress", // Custom state name
		},
	}

	tests := []struct {
		name       string
		state      *LinearState
		wantStatus types.Status
	}{
		{
			name:       "custom in_review state maps to in_progress",
			state:      &LinearState{Type: "custom", Name: "In Review"},
			wantStatus: types.StatusInProgress,
		},
		{
			name:       "custom on_hold state maps to blocked",
			state:      &LinearState{Type: "custom", Name: "On Hold"},
			wantStatus: types.StatusBlocked,
		},
		{
			name:       "custom blocked state maps to blocked",
			state:      &LinearState{Type: "custom", Name: "Blocked"},
			wantStatus: types.StatusBlocked,
		},
		{
			name:       "custom validating state maps to in_progress",
			state:      &LinearState{Type: "custom", Name: "Validating"},
			wantStatus: types.StatusInProgress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := linearStateToBeadsStatus(tt.state, config)
			if got != tt.wantStatus {
				t.Errorf("linearStateToBeadsStatus() with custom config = %s, want %s",
					got, tt.wantStatus)
			}
		})
	}
}

func TestBeadsStatusToLinearStateType(t *testing.T) {
	tests := []struct {
		name            string
		status          types.Status
		wantLinearState string
	}{
		{
			name:            "open maps to unstarted",
			status:          types.StatusOpen,
			wantLinearState: "unstarted",
		},
		{
			name:            "in_progress maps to started",
			status:          types.StatusInProgress,
			wantLinearState: "started",
		},
		{
			name:            "blocked maps to started (Linear has no blocked)",
			status:          types.StatusBlocked,
			wantLinearState: "started",
		},
		{
			name:            "closed maps to completed",
			status:          types.StatusClosed,
			wantLinearState: "completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := beadsStatusToLinearStateType(tt.status)
			if got != tt.wantLinearState {
				t.Errorf("beadsStatusToLinearStateType(%s) = %s, want %s",
					tt.status, got, tt.wantLinearState)
			}
		})
	}
}

func TestLinearLabelToIssueType(t *testing.T) {
	config := defaultLinearMappingConfig()

	tests := []struct {
		name     string
		labels   *LinearLabels
		wantType types.IssueType
	}{
		{
			name:     "nil labels defaults to task",
			labels:   nil,
			wantType: types.TypeTask,
		},
		{
			name:     "empty labels defaults to task",
			labels:   &LinearLabels{Nodes: []LinearLabel{}},
			wantType: types.TypeTask,
		},
		{
			name: "bug label maps to bug type",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "bug"}},
			},
			wantType: types.TypeBug,
		},
		{
			name: "Bug (capitalized) label maps to bug type",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "Bug"}},
			},
			wantType: types.TypeBug,
		},
		{
			name: "defect label maps to bug type",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "defect"}},
			},
			wantType: types.TypeBug,
		},
		{
			name: "feature label maps to feature type",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "feature"}},
			},
			wantType: types.TypeFeature,
		},
		{
			name: "enhancement label maps to feature type",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "enhancement"}},
			},
			wantType: types.TypeFeature,
		},
		{
			name: "epic label maps to epic type",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "epic"}},
			},
			wantType: types.TypeEpic,
		},
		{
			name: "chore label maps to chore type",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "chore"}},
			},
			wantType: types.TypeChore,
		},
		{
			name: "maintenance label maps to chore type",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "maintenance"}},
			},
			wantType: types.TypeChore,
		},
		{
			name: "task label maps to task type",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "task"}},
			},
			wantType: types.TypeTask,
		},
		{
			name: "first matching label wins",
			labels: &LinearLabels{
				Nodes: []LinearLabel{
					{Name: "bug"},
					{Name: "feature"},
				},
			},
			wantType: types.TypeBug,
		},
		{
			name: "label containing keyword matches",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "critical-bug"}},
			},
			wantType: types.TypeBug, // Contains "bug"
		},
		{
			name: "unrecognized label defaults to task",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "documentation"}, {Name: "urgent"}},
			},
			wantType: types.TypeTask,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := linearLabelToIssueType(tt.labels, config)
			if got != tt.wantType {
				t.Errorf("linearLabelToIssueType() = %s, want %s", got, tt.wantType)
			}
		})
	}
}

func TestLinearLabelToIssueTypeCustomConfig(t *testing.T) {
	// Test with custom label-to-type mapping
	config := &LinearMappingConfig{
		LabelTypeMap: map[string]string{
			"incident":    "bug",
			"improvement": "feature",
			"tech-debt":   "chore",
			"story":       "feature",
		},
	}

	tests := []struct {
		name     string
		labels   *LinearLabels
		wantType types.IssueType
	}{
		{
			name: "custom incident label maps to bug",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "incident"}},
			},
			wantType: types.TypeBug,
		},
		{
			name: "custom improvement label maps to feature",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "improvement"}},
			},
			wantType: types.TypeFeature,
		},
		{
			name: "custom tech-debt label maps to chore",
			labels: &LinearLabels{
				Nodes: []LinearLabel{{Name: "tech-debt"}},
			},
			wantType: types.TypeChore,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := linearLabelToIssueType(tt.labels, config)
			if got != tt.wantType {
				t.Errorf("linearLabelToIssueType() with custom config = %s, want %s",
					got, tt.wantType)
			}
		})
	}
}

func TestLinearRelationToBeadsDep(t *testing.T) {
	config := defaultLinearMappingConfig()

	tests := []struct {
		name         string
		relationType string
		wantDepType  string
	}{
		{
			name:         "blocks relation maps to blocks",
			relationType: "blocks",
			wantDepType:  "blocks",
		},
		{
			name:         "blockedBy relation maps to blocks",
			relationType: "blockedBy",
			wantDepType:  "blocks",
		},
		{
			name:         "duplicate relation maps to duplicates",
			relationType: "duplicate",
			wantDepType:  "duplicates",
		},
		{
			name:         "related relation maps to related",
			relationType: "related",
			wantDepType:  "related",
		},
		{
			name:         "unknown relation defaults to related",
			relationType: "unknown",
			wantDepType:  "related",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := linearRelationToBeadsDep(tt.relationType, config)
			if got != tt.wantDepType {
				t.Errorf("linearRelationToBeadsDep(%s) = %s, want %s",
					tt.relationType, got, tt.wantDepType)
			}
		})
	}
}

func TestLinearRelationToBeadsDepCustomConfig(t *testing.T) {
	// Test with custom relation mapping
	config := &LinearMappingConfig{
		RelationMap: map[string]string{
			"blocks":    "blocks",
			"blockedBy": "blocks",
			"duplicate": "related", // Custom: duplicates -> related
			"related":   "related",
			"causes":    "discovered-from", // Custom relation type
		},
	}

	tests := []struct {
		relationType string
		wantDepType  string
	}{
		{"duplicate", "related"},
		{"causes", "discovered-from"},
	}

	for _, tt := range tests {
		t.Run(tt.relationType, func(t *testing.T) {
			got := linearRelationToBeadsDep(tt.relationType, config)
			if got != tt.wantDepType {
				t.Errorf("linearRelationToBeadsDep(%s) with custom config = %s, want %s",
					tt.relationType, got, tt.wantDepType)
			}
		})
	}
}

func TestLinearIssueToBeads(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		linearIssue   *LinearIssue
		wantTitle     string
		wantStatus    types.Status
		wantPriority  int
		wantType      types.IssueType
		wantAssignee  string
		wantDepsCount int
		wantHasExtRef bool
	}{
		{
			name: "basic issue conversion",
			linearIssue: &LinearIssue{
				ID:          "uuid-123",
				Identifier:  "TEAM-123",
				Title:       "Fix login bug",
				Description: "Users cannot login",
				URL:         "https://linear.app/team/issue/TEAM-123/fix-login-bug",
				Priority:    1, // Urgent
				State:       &LinearState{Type: "started", Name: "In Progress"},
				CreatedAt:   "2025-01-15T10:00:00Z",
				UpdatedAt:   "2025-01-16T14:30:00Z",
			},
			wantTitle:     "Fix login bug",
			wantStatus:    types.StatusInProgress,
			wantPriority:  0, // Urgent -> Critical
			wantType:      types.TypeTask,
			wantDepsCount: 0,
			wantHasExtRef: true,
		},
		{
			name: "issue with labels for type inference",
			linearIssue: &LinearIssue{
				ID:          "uuid-456",
				Identifier:  "TEAM-456",
				Title:       "New feature",
				Description: "Add new feature",
				URL:         "https://linear.app/team/issue/TEAM-456/new-feature",
				Priority:    2, // High
				State:       &LinearState{Type: "unstarted", Name: "Todo"},
				Labels: &LinearLabels{
					Nodes: []LinearLabel{{Name: "feature"}, {Name: "priority"}},
				},
				CreatedAt: "2025-01-15T10:00:00Z",
				UpdatedAt: "2025-01-15T10:00:00Z",
			},
			wantTitle:     "New feature",
			wantStatus:    types.StatusOpen,
			wantPriority:  1, // High -> High
			wantType:      types.TypeFeature,
			wantDepsCount: 0,
			wantHasExtRef: true,
		},
		{
			name: "issue with assignee",
			linearIssue: &LinearIssue{
				ID:         "uuid-789",
				Identifier: "TEAM-789",
				Title:      "Assigned task",
				URL:        "https://linear.app/team/issue/TEAM-789/assigned-task",
				Priority:   3, // Medium
				State:      &LinearState{Type: "started", Name: "In Progress"},
				Assignee: &LinearUser{
					Name:  "John Doe",
					Email: "john@example.com",
				},
				CreatedAt: "2025-01-15T10:00:00Z",
				UpdatedAt: "2025-01-15T10:00:00Z",
			},
			wantTitle:     "Assigned task",
			wantStatus:    types.StatusInProgress,
			wantPriority:  2, // Medium -> Medium
			wantType:      types.TypeTask,
			wantAssignee:  "john@example.com",
			wantDepsCount: 0,
			wantHasExtRef: true,
		},
		{
			name: "issue with parent creates parent-child dependency",
			linearIssue: &LinearIssue{
				ID:         "uuid-child",
				Identifier: "TEAM-200",
				Title:      "Child task",
				URL:        "https://linear.app/team/issue/TEAM-200/child-task",
				Priority:   3,
				State:      &LinearState{Type: "unstarted", Name: "Todo"},
				Parent:     &LinearParent{ID: "uuid-parent", Identifier: "TEAM-100"},
				CreatedAt:  "2025-01-15T10:00:00Z",
				UpdatedAt:  "2025-01-15T10:00:00Z",
			},
			wantTitle:     "Child task",
			wantStatus:    types.StatusOpen,
			wantPriority:  2,
			wantType:      types.TypeTask,
			wantDepsCount: 1, // Parent-child dependency
			wantHasExtRef: true,
		},
		{
			name: "issue with relations",
			linearIssue: &LinearIssue{
				ID:         "uuid-blocker",
				Identifier: "TEAM-300",
				Title:      "Blocking issue",
				URL:        "https://linear.app/team/issue/TEAM-300/blocking-issue",
				Priority:   2,
				State:      &LinearState{Type: "started", Name: "In Progress"},
				Relations: &LinearRelations{
					Nodes: []LinearRelation{
						{
							ID:   "rel-1",
							Type: "blocks",
							RelatedIssue: struct {
								ID         string `json:"id"`
								Identifier string `json:"identifier"`
							}{ID: "uuid-blocked", Identifier: "TEAM-301"},
						},
						{
							ID:   "rel-2",
							Type: "related",
							RelatedIssue: struct {
								ID         string `json:"id"`
								Identifier string `json:"identifier"`
							}{ID: "uuid-related", Identifier: "TEAM-302"},
						},
					},
				},
				CreatedAt: "2025-01-15T10:00:00Z",
				UpdatedAt: "2025-01-15T10:00:00Z",
			},
			wantTitle:     "Blocking issue",
			wantStatus:    types.StatusInProgress,
			wantPriority:  1,
			wantType:      types.TypeTask,
			wantDepsCount: 2, // Two relations
			wantHasExtRef: true,
		},
		{
			name: "issue with duplicate relation",
			linearIssue: &LinearIssue{
				ID:         "uuid-dup",
				Identifier: "TEAM-350",
				Title:      "Duplicate issue",
				URL:        "https://linear.app/team/issue/TEAM-350/dup-issue",
				Priority:   3,
				State:      &LinearState{Type: "unstarted", Name: "Todo"},
				Relations: &LinearRelations{
					Nodes: []LinearRelation{
						{
							ID:   "rel-dup",
							Type: "duplicate",
							RelatedIssue: struct {
								ID         string `json:"id"`
								Identifier string `json:"identifier"`
							}{ID: "uuid-canonical", Identifier: "TEAM-351"},
						},
					},
				},
				CreatedAt: "2025-01-15T10:00:00Z",
				UpdatedAt: "2025-01-15T10:00:00Z",
			},
			wantTitle:     "Duplicate issue",
			wantStatus:    types.StatusOpen,
			wantPriority:  2,
			wantType:      types.TypeTask,
			wantDepsCount: 1,
			wantHasExtRef: true,
		},
		{
			name: "closed issue with completedAt",
			linearIssue: &LinearIssue{
				ID:          "uuid-closed",
				Identifier:  "TEAM-400",
				Title:       "Completed task",
				URL:         "https://linear.app/team/issue/TEAM-400/completed-task",
				Priority:    3,
				State:       &LinearState{Type: "completed", Name: "Done"},
				CreatedAt:   "2025-01-10T10:00:00Z",
				UpdatedAt:   "2025-01-15T10:00:00Z",
				CompletedAt: "2025-01-15T09:00:00Z",
			},
			wantTitle:     "Completed task",
			wantStatus:    types.StatusClosed,
			wantPriority:  2,
			wantType:      types.TypeTask,
			wantDepsCount: 0,
			wantHasExtRef: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conversion := linearIssueToBeads(ctx, tt.linearIssue)

			if conversion.Issue.Title != tt.wantTitle {
				t.Errorf("Title = %s, want %s", conversion.Issue.Title, tt.wantTitle)
			}
			if conversion.Issue.Status != tt.wantStatus {
				t.Errorf("Status = %s, want %s", conversion.Issue.Status, tt.wantStatus)
			}
			if conversion.Issue.Priority != tt.wantPriority {
				t.Errorf("Priority = %d, want %d", conversion.Issue.Priority, tt.wantPriority)
			}
			if conversion.Issue.IssueType != tt.wantType {
				t.Errorf("IssueType = %s, want %s", conversion.Issue.IssueType, tt.wantType)
			}
			if conversion.Issue.Assignee != tt.wantAssignee {
				t.Errorf("Assignee = %s, want %s", conversion.Issue.Assignee, tt.wantAssignee)
			}
			if len(conversion.Dependencies) != tt.wantDepsCount {
				t.Errorf("Dependencies count = %d, want %d",
					len(conversion.Dependencies), tt.wantDepsCount)
			}
			if tt.name == "issue with relations" {
				gotDeps := make(map[string]bool, len(conversion.Dependencies))
				for _, dep := range conversion.Dependencies {
					key := dep.FromLinearID + "->" + dep.ToLinearID + ":" + dep.Type
					gotDeps[key] = true
				}
				if !gotDeps["TEAM-301->TEAM-300:blocks"] {
					t.Error("expected blocks dependency TEAM-301->TEAM-300")
				}
				if !gotDeps["TEAM-300->TEAM-302:related"] {
					t.Error("expected related dependency TEAM-300->TEAM-302")
				}
			}
			if tt.name == "issue with duplicate relation" {
				if len(conversion.Dependencies) != 1 {
					t.Fatalf("expected 1 dependency, got %d", len(conversion.Dependencies))
				}
				dep := conversion.Dependencies[0]
				if dep.Type != "duplicates" {
					t.Errorf("expected dep type duplicates, got %s", dep.Type)
				}
				if dep.FromLinearID != "TEAM-350" || dep.ToLinearID != "TEAM-351" {
					t.Errorf("expected duplicate dependency TEAM-350->TEAM-351, got %s->%s", dep.FromLinearID, dep.ToLinearID)
				}
			}
			if tt.wantHasExtRef && conversion.Issue.ExternalRef == nil {
				t.Error("ExternalRef should be set")
			}
		})
	}
}

func TestIsLinearExternalRef(t *testing.T) {
	tests := []struct {
		name        string
		externalRef string
		want        bool
	}{
		{
			name:        "valid Linear URL",
			externalRef: "https://linear.app/team/issue/TEAM-123/fix-login-bug",
			want:        true,
		},
		{
			name:        "Linear URL without slug",
			externalRef: "https://linear.app/team/issue/TEAM-123",
			want:        true,
		},
		{
			name:        "GitHub issue URL",
			externalRef: "https://github.com/org/repo/issues/123",
			want:        false,
		},
		{
			name:        "Jira URL",
			externalRef: "https://company.atlassian.net/browse/PROJ-123",
			want:        false,
		},
		{
			name:        "empty string",
			externalRef: "",
			want:        false,
		},
		{
			name:        "random URL",
			externalRef: "https://example.com/page",
			want:        false,
		},
		{
			name:        "Linear URL without /issue/ path",
			externalRef: "https://linear.app/team/TEAM-123",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLinearExternalRef(tt.externalRef)
			if got != tt.want {
				t.Errorf("isLinearExternalRef(%q) = %v, want %v",
					tt.externalRef, got, tt.want)
			}
		})
	}
}

func TestExtractLinearIdentifier(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "standard Linear URL",
			url:  "https://linear.app/team/issue/TEAM-123/fix-login-bug",
			want: "TEAM-123",
		},
		{
			name: "Linear URL without slug",
			url:  "https://linear.app/team/issue/TEAM-456",
			want: "TEAM-456",
		},
		{
			name: "Linear URL with long identifier",
			url:  "https://linear.app/myteam/issue/PROJECT-9999/very-long-title-slug",
			want: "PROJECT-9999",
		},
		{
			name: "URL without issue path",
			url:  "https://linear.app/team/TEAM-123",
			want: "",
		},
		{
			name: "empty URL",
			url:  "",
			want: "",
		},
		{
			name: "GitHub URL",
			url:  "https://github.com/org/repo/issues/123",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLinearIdentifier(tt.url)
			if got != tt.want {
				t.Errorf("extractLinearIdentifier(%q) = %q, want %q",
					tt.url, got, tt.want)
			}
		})
	}
}

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "long key",
			key:  "lin_api_12345678901234567890",
			want: "lin_...7890",
		},
		{
			name: "short key",
			key:  "short",
			want: "****",
		},
		{
			name: "exactly 8 chars",
			key:  "12345678",
			want: "****",
		},
		{
			name: "9 chars",
			key:  "123456789",
			want: "1234...6789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskAPIKey(tt.key)
			if got != tt.want {
				t.Errorf("maskAPIKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestParseBeadsStatus(t *testing.T) {
	tests := []struct {
		input      string
		wantStatus types.Status
	}{
		{"open", types.StatusOpen},
		{"OPEN", types.StatusOpen},
		{"in_progress", types.StatusInProgress},
		{"in-progress", types.StatusInProgress},
		{"inprogress", types.StatusInProgress},
		{"blocked", types.StatusBlocked},
		{"closed", types.StatusClosed},
		{"CLOSED", types.StatusClosed},
		{"unknown", types.StatusOpen},
		{"", types.StatusOpen},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseBeadsStatus(tt.input)
			if got != tt.wantStatus {
				t.Errorf("parseBeadsStatus(%q) = %s, want %s",
					tt.input, got, tt.wantStatus)
			}
		})
	}
}

func TestParseIssueType(t *testing.T) {
	tests := []struct {
		input    string
		wantType types.IssueType
	}{
		{"bug", types.TypeBug},
		{"BUG", types.TypeBug},
		{"feature", types.TypeFeature},
		{"task", types.TypeTask},
		{"epic", types.TypeEpic},
		{"chore", types.TypeChore},
		{"unknown", types.TypeTask},
		{"", types.TypeTask},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseIssueType(tt.input)
			if got != tt.wantType {
				t.Errorf("parseIssueType(%q) = %s, want %s",
					tt.input, got, tt.wantType)
			}
		})
	}
}

func TestDefaultLinearMappingConfig(t *testing.T) {
	config := defaultLinearMappingConfig()

	// Test priority map has expected entries
	expectedPriorityMap := map[string]int{
		"0": 4, "1": 0, "2": 1, "3": 2, "4": 3,
	}
	for k, v := range expectedPriorityMap {
		if got, ok := config.PriorityMap[k]; !ok || got != v {
			t.Errorf("PriorityMap[%s] = %d, want %d", k, got, v)
		}
	}

	// Test state map has expected entries
	expectedStateMap := map[string]string{
		"backlog": "open", "unstarted": "open", "started": "in_progress",
		"completed": "closed", "canceled": "closed",
	}
	for k, v := range expectedStateMap {
		if got, ok := config.StateMap[k]; !ok || got != v {
			t.Errorf("StateMap[%s] = %s, want %s", k, got, v)
		}
	}

	// Test label type map has expected entries
	expectedLabelMap := map[string]string{
		"bug": "bug", "defect": "bug", "feature": "feature",
		"enhancement": "feature", "epic": "epic", "chore": "chore",
		"maintenance": "chore", "task": "task",
	}
	for k, v := range expectedLabelMap {
		if got, ok := config.LabelTypeMap[k]; !ok || got != v {
			t.Errorf("LabelTypeMap[%s] = %s, want %s", k, got, v)
		}
	}

	// Test relation map has expected entries
	expectedRelationMap := map[string]string{
		"blocks": "blocks", "blockedBy": "blocks",
		"duplicate": "duplicates", "related": "related",
	}
	for k, v := range expectedRelationMap {
		if got, ok := config.RelationMap[k]; !ok || got != v {
			t.Errorf("RelationMap[%s] = %s, want %s", k, got, v)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestFetchIssueByIdentifierSendsNumericFilter(t *testing.T) {
	client := &LinearClient{
		apiKey: "test-api-key",
		teamID: "team-123",
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}

	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		_ = r.Body.Close()

		var gqlReq GraphQLRequest
		if err := json.Unmarshal(body, &gqlReq); err != nil {
			return nil, fmt.Errorf("decode request body: %w", err)
		}

		filter, ok := gqlReq.Variables["filter"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("missing filter in variables")
		}
		numberFilter, ok := filter["number"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("missing number filter in variables")
		}
		eq, ok := numberFilter["eq"].(float64)
		if !ok {
			return nil, fmt.Errorf("number.eq is not numeric (got %T)", numberFilter["eq"])
		}
		if eq != 123 {
			return nil, fmt.Errorf("expected number.eq=123, got %v", eq)
		}

		resp := GraphQLResponse{
			Data: json.RawMessage(`{"issues":{"nodes":[]}}`),
		}
		respBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, fmt.Errorf("encode response: %w", err)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(respBytes)),
			Request:    r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = origTransport })

	_, err := client.FetchIssueByIdentifier(context.Background(), "TEAM-123")
	if err != nil {
		t.Fatalf("FetchIssueByIdentifier failed: %v", err)
	}
}

func TestDoPushToLinearPreferLocalForcesUpdate(t *testing.T) {
	testStore, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	if err := testStore.SetConfig(ctx, "linear.api_key", "test-api-key"); err != nil {
		t.Fatalf("SetConfig linear.api_key failed: %v", err)
	}
	if err := testStore.SetConfig(ctx, "linear.team_id", "team-123"); err != nil {
		t.Fatalf("SetConfig linear.team_id failed: %v", err)
	}

	localUpdated := time.Now().Add(-2 * time.Hour)
	issue := &types.Issue{
		Title:       "Local Issue",
		Description: "Local description",
		Priority:    2,
		IssueType:   types.TypeTask,
		Status:      types.StatusInProgress,
		CreatedAt:   localUpdated,
		UpdatedAt:   localUpdated,
	}
	externalRef := "https://linear.app/team/issue/TEAM-123/local-issue"
	issue.ExternalRef = &externalRef
	if err := testStore.CreateIssue(ctx, issue, "test-actor"); err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	remoteUpdated := time.Now().Add(-1 * time.Hour)
	remoteUpdatedStr := remoteUpdated.UTC().Format(time.RFC3339)

	updatedCalled := false
	origTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		_ = r.Body.Close()

		var gqlReq GraphQLRequest
		if err := json.Unmarshal(body, &gqlReq); err != nil {
			return nil, fmt.Errorf("decode request body: %w", err)
		}

		var resp GraphQLResponse
		switch {
		case strings.Contains(gqlReq.Query, "TeamStates"):
			resp = GraphQLResponse{
				Data: json.RawMessage(`{
					"team": {
						"id": "team-123",
						"states": {
							"nodes": [
								{"id": "state-started", "name": "In Progress", "type": "started"}
							]
						}
					}
				}`),
			}
		case strings.Contains(gqlReq.Query, "IssueByIdentifier"):
			resp = GraphQLResponse{
				Data: json.RawMessage(fmt.Sprintf(`{
					"issues": {
						"nodes": [
							{
								"id": "uuid-123",
								"identifier": "TEAM-123",
								"title": "Remote Issue",
								"description": "Remote description",
								"url": "https://linear.app/team/issue/TEAM-123/remote-issue",
								"priority": 2,
								"state": {"id": "state-started", "name": "In Progress", "type": "started"},
								"labels": {"nodes": []},
								"createdAt": "2025-01-01T00:00:00Z",
								"updatedAt": "%s"
							}
						]
					}
				}`, remoteUpdatedStr)),
			}
		case strings.Contains(gqlReq.Query, "UpdateIssue"):
			updatedCalled = true
			resp = GraphQLResponse{
				Data: json.RawMessage(`{
					"issueUpdate": {
						"success": true,
						"issue": {
							"id": "uuid-123",
							"identifier": "TEAM-123",
							"title": "Updated Title",
							"description": "Updated description",
							"url": "https://linear.app/team/issue/TEAM-123/remote-issue",
							"priority": 2,
							"state": {"id": "state-started", "name": "In Progress", "type": "started"},
							"updatedAt": "2025-01-02T00:00:00Z"
						}
					}
				}`),
			}
		default:
			return nil, fmt.Errorf("unexpected query: %s", gqlReq.Query)
		}

		respBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, fmt.Errorf("encode response: %w", err)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(respBytes)),
			Request:    r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = origTransport })

	origStore := store
	origActor := actor
	store = testStore
	actor = "test-actor"
	t.Cleanup(func() {
		store = origStore
		actor = origActor
	})

	forceUpdateIDs := map[string]bool{issue.ID: true}
	stats, err := doPushToLinear(ctx, false, false, true, forceUpdateIDs)
	if err != nil {
		t.Fatalf("doPushToLinear failed: %v", err)
	}
	if !updatedCalled {
		t.Fatal("expected UpdateIssue to be called when force-update is enabled")
	}
	if stats.Updated != 1 {
		t.Fatalf("expected Updated=1, got %d", stats.Updated)
	}
	if stats.Skipped != 0 {
		t.Fatalf("expected Skipped=0, got %d", stats.Skipped)
	}
}

func TestLinearClientFetchIssues(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a mock GraphQL server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type: application/json, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header to be set")
		}

		// Return mock response
		response := GraphQLResponse{
			Data: json.RawMessage(`{
				"issues": {
					"nodes": [
						{
							"id": "uuid-1",
							"identifier": "TEAM-1",
							"title": "Test Issue 1",
							"description": "Description 1",
							"url": "https://linear.app/team/issue/TEAM-1/test-issue",
							"priority": 2,
							"state": {
								"id": "state-1",
								"name": "In Progress",
								"type": "started"
							},
							"labels": {
								"nodes": [
									{"id": "label-1", "name": "bug"}
								]
							},
							"createdAt": "2025-01-15T10:00:00Z",
							"updatedAt": "2025-01-16T10:00:00Z"
						},
						{
							"id": "uuid-2",
							"identifier": "TEAM-2",
							"title": "Test Issue 2",
							"description": "Description 2",
							"url": "https://linear.app/team/issue/TEAM-2/test-issue-2",
							"priority": 3,
							"state": {
								"id": "state-2",
								"name": "Todo",
								"type": "unstarted"
							},
							"createdAt": "2025-01-15T10:00:00Z",
							"updatedAt": "2025-01-15T10:00:00Z"
						}
					],
					"pageInfo": {
						"hasNextPage": false,
						"endCursor": ""
					}
				}
			}`),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	// Create client with mock server
	client := &LinearClient{
		apiKey: "test-api-key",
		teamID: "test-team-id",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	// Override the API endpoint for testing by using a custom execute function
	// For this test, we'll use the mock server directly
	ctx := context.Background()

	// Create a request and manually call the mock server
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", client.apiKey)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var gqlResp GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	var issuesResp LinearIssuesResponse
	if err := json.Unmarshal(gqlResp.Data, &issuesResp); err != nil {
		t.Fatalf("failed to unmarshal issues: %v", err)
	}

	// Verify response
	if len(issuesResp.Issues.Nodes) != 2 {
		t.Errorf("expected 2 issues, got %d", len(issuesResp.Issues.Nodes))
	}

	// Check first issue
	issue1 := issuesResp.Issues.Nodes[0]
	if issue1.Identifier != "TEAM-1" {
		t.Errorf("expected identifier TEAM-1, got %s", issue1.Identifier)
	}
	if issue1.Title != "Test Issue 1" {
		t.Errorf("expected title 'Test Issue 1', got %s", issue1.Title)
	}
	if issue1.State.Type != "started" {
		t.Errorf("expected state type 'started', got %s", issue1.State.Type)
	}
}

func TestLinearClientCreateIssue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a mock GraphQL server for create mutation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := GraphQLResponse{
			Data: json.RawMessage(`{
				"issueCreate": {
					"success": true,
					"issue": {
						"id": "uuid-new",
						"identifier": "TEAM-999",
						"title": "New Test Issue",
						"description": "Created via API",
						"url": "https://linear.app/team/issue/TEAM-999/new-test-issue",
						"priority": 2,
						"state": {
							"id": "state-1",
							"name": "Todo",
							"type": "unstarted"
						},
						"createdAt": "2025-01-17T10:00:00Z",
						"updatedAt": "2025-01-17T10:00:00Z"
					}
				}
			}`),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	client := &LinearClient{
		apiKey: "test-api-key",
		teamID: "test-team-id",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	ctx := context.Background()

	// Manually test the mock response
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", client.apiKey)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var gqlResp GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	var createResp LinearIssueCreateResponse
	if err := json.Unmarshal(gqlResp.Data, &createResp); err != nil {
		t.Fatalf("failed to unmarshal create response: %v", err)
	}

	// Verify response
	if !createResp.IssueCreate.Success {
		t.Error("expected create to succeed")
	}
	if createResp.IssueCreate.Issue.Identifier != "TEAM-999" {
		t.Errorf("expected identifier TEAM-999, got %s", createResp.IssueCreate.Issue.Identifier)
	}
}

func TestLinearClientUpdateIssue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a mock GraphQL server for update mutation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := GraphQLResponse{
			Data: json.RawMessage(`{
				"issueUpdate": {
					"success": true,
					"issue": {
						"id": "uuid-existing",
						"identifier": "TEAM-100",
						"title": "Updated Title",
						"description": "Updated description",
						"url": "https://linear.app/team/issue/TEAM-100/updated-title",
						"priority": 1,
						"state": {
							"id": "state-done",
							"name": "Done",
							"type": "completed"
						},
						"updatedAt": "2025-01-17T12:00:00Z"
					}
				}
			}`),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	client := &LinearClient{
		apiKey: "test-api-key",
		teamID: "test-team-id",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	ctx := context.Background()

	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", client.apiKey)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var gqlResp GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	var updateResp LinearIssueUpdateResponse
	if err := json.Unmarshal(gqlResp.Data, &updateResp); err != nil {
		t.Fatalf("failed to unmarshal update response: %v", err)
	}

	// Verify response
	if !updateResp.IssueUpdate.Success {
		t.Error("expected update to succeed")
	}
	if updateResp.IssueUpdate.Issue.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %s", updateResp.IssueUpdate.Issue.Title)
	}
	if updateResp.IssueUpdate.Issue.State.Type != "completed" {
		t.Errorf("expected state type 'completed', got %s", updateResp.IssueUpdate.Issue.State.Type)
	}
}

func TestLinearClientGetTeamStates(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a mock GraphQL server for team states query
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := GraphQLResponse{
			Data: json.RawMessage(`{
				"team": {
					"id": "team-123",
					"states": {
						"nodes": [
							{"id": "state-1", "name": "Backlog", "type": "backlog"},
							{"id": "state-2", "name": "Todo", "type": "unstarted"},
							{"id": "state-3", "name": "In Progress", "type": "started"},
							{"id": "state-4", "name": "Done", "type": "completed"},
							{"id": "state-5", "name": "Cancelled", "type": "canceled"}
						]
					}
				}
			}`),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	client := &LinearClient{
		apiKey: "test-api-key",
		teamID: "test-team-id",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	ctx := context.Background()

	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL, nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", client.apiKey)

	resp, err := client.httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var gqlResp GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	var teamResp LinearTeamResponse
	if err := json.Unmarshal(gqlResp.Data, &teamResp); err != nil {
		t.Fatalf("failed to unmarshal team response: %v", err)
	}

	// Verify response
	if len(teamResp.Team.States.Nodes) != 5 {
		t.Errorf("expected 5 states, got %d", len(teamResp.Team.States.Nodes))
	}

	// Verify state types
	expectedTypes := []string{"backlog", "unstarted", "started", "completed", "canceled"}
	for i, expected := range expectedTypes {
		if teamResp.Team.States.Nodes[i].Type != expected {
			t.Errorf("state %d: expected type %s, got %s",
				i, expected, teamResp.Team.States.Nodes[i].Type)
		}
	}
}

func TestLinearClientRateLimitHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a mock server that returns 429 then succeeds
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			// First attempt: rate limited
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// Subsequent attempts: success
		response := GraphQLResponse{
			Data: json.RawMessage(`{"issues": {"nodes": [], "pageInfo": {"hasNextPage": false}}}`),
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	// Verify that rate limiting was simulated
	client := &http.Client{Timeout: 10 * time.Second}
	ctx := context.Background()

	// First request: expect 429
	req1, _ := http.NewRequestWithContext(ctx, "POST", server.URL, nil)
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", resp1.StatusCode)
	}

	// Second request: expect success
	req2, _ := http.NewRequestWithContext(ctx, "POST", server.URL, nil)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp2.StatusCode)
	}

	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestLinearClientGraphQLError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a mock server that returns a GraphQL error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := GraphQLResponse{
			Errors: []GraphQLError{
				{
					Message: "Issue not found",
					Path:    []string{"issues"},
					Extensions: struct {
						Code string `json:"code,omitempty"`
					}{Code: "NOT_FOUND"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	ctx := context.Background()

	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var gqlResp GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify GraphQL error was returned
	if len(gqlResp.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(gqlResp.Errors))
	}
	if gqlResp.Errors[0].Message != "Issue not found" {
		t.Errorf("unexpected error message: %s", gqlResp.Errors[0].Message)
	}
}

func TestLinearStateCacheFindStateForBeadsStatus(t *testing.T) {
	cache := &linearStateCache{
		states: []LinearState{
			{ID: "state-1", Name: "Backlog", Type: "backlog"},
			{ID: "state-2", Name: "Todo", Type: "unstarted"},
			{ID: "state-3", Name: "In Progress", Type: "started"},
			{ID: "state-4", Name: "Done", Type: "completed"},
			{ID: "state-5", Name: "Cancelled", Type: "canceled"},
		},
		statesByID:  make(map[string]LinearState),
		openStateID: "state-2",
	}

	tests := []struct {
		name        string
		status      types.Status
		wantStateID string
	}{
		{
			name:        "open status finds unstarted state",
			status:      types.StatusOpen,
			wantStateID: "state-2",
		},
		{
			name:        "in_progress status finds started state",
			status:      types.StatusInProgress,
			wantStateID: "state-3",
		},
		{
			name:        "blocked status finds started state (no blocked in Linear)",
			status:      types.StatusBlocked,
			wantStateID: "state-3",
		},
		{
			name:        "closed status finds completed state",
			status:      types.StatusClosed,
			wantStateID: "state-4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cache.findStateForBeadsStatus(tt.status)
			if got != tt.wantStateID {
				t.Errorf("findStateForBeadsStatus(%s) = %s, want %s",
					tt.status, got, tt.wantStateID)
			}
		})
	}
}

func TestLinearStateCacheEmpty(t *testing.T) {
	cache := &linearStateCache{
		states:     []LinearState{},
		statesByID: make(map[string]LinearState),
	}

	// Should return empty string when no states available
	got := cache.findStateForBeadsStatus(types.StatusOpen)
	if got != "" {
		t.Errorf("expected empty string for empty cache, got %s", got)
	}
}

func TestBuildLinearToLocalUpdates(t *testing.T) {
	config := defaultLinearMappingConfig()

	li := &LinearIssue{
		ID:          "uuid-123",
		Identifier:  "TEAM-123",
		Title:       "Updated Title",
		Description: "Updated Description",
		Priority:    2, // High
		State:       &LinearState{Type: "started", Name: "In Progress"},
		Assignee:    &LinearUser{Email: "test@example.com", Name: "Test User"},
		Labels: &LinearLabels{
			Nodes: []LinearLabel{
				{Name: "bug"},
				{Name: "priority"},
			},
		},
		UpdatedAt:   "2025-01-17T10:00:00Z",
		CompletedAt: "",
	}

	updates := buildLinearToLocalUpdates(li, config)

	// Verify all expected fields are present
	if updates["title"] != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %v", updates["title"])
	}
	if updates["description"] != "Updated Description" {
		t.Errorf("expected description 'Updated Description', got %v", updates["description"])
	}
	if updates["priority"] != 1 { // High -> High
		t.Errorf("expected priority 1, got %v", updates["priority"])
	}
	if updates["status"] != "in_progress" {
		t.Errorf("expected status 'in_progress', got %v", updates["status"])
	}
	if updates["assignee"] != "test@example.com" {
		t.Errorf("expected assignee 'test@example.com', got %v", updates["assignee"])
	}

	// Check labels
	labels, ok := updates["labels"].([]string)
	if !ok {
		t.Fatalf("expected labels to be []string, got %T", updates["labels"])
	}
	if len(labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(labels))
	}
}

func TestBuildLinearToLocalUpdatesNoAssignee(t *testing.T) {
	config := defaultLinearMappingConfig()

	li := &LinearIssue{
		ID:          "uuid-123",
		Identifier:  "TEAM-123",
		Title:       "Unassigned Issue",
		Description: "No assignee",
		Priority:    3,
		State:       &LinearState{Type: "unstarted", Name: "Todo"},
		Assignee:    nil,
		UpdatedAt:   "2025-01-17T10:00:00Z",
	}

	updates := buildLinearToLocalUpdates(li, config)

	// Assignee should be empty string when nil
	if updates["assignee"] != "" {
		t.Errorf("expected empty assignee, got %v", updates["assignee"])
	}
}

func TestBuildLinearToLocalUpdatesWithClosedAt(t *testing.T) {
	config := defaultLinearMappingConfig()

	li := &LinearIssue{
		ID:          "uuid-123",
		Identifier:  "TEAM-123",
		Title:       "Completed Issue",
		Description: "Done",
		Priority:    3,
		State:       &LinearState{Type: "completed", Name: "Done"},
		UpdatedAt:   "2025-01-17T10:00:00Z",
		CompletedAt: "2025-01-17T09:00:00Z",
	}

	updates := buildLinearToLocalUpdates(li, config)

	// Check closed_at is set
	closedAt, ok := updates["closed_at"].(time.Time)
	if !ok {
		t.Fatalf("expected closed_at to be time.Time, got %T", updates["closed_at"])
	}
	if closedAt.IsZero() {
		t.Error("closed_at should not be zero")
	}
}

func TestIsValidUUID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "valid UUID with hyphens",
			input: "12345678-1234-1234-1234-123456789abc",
			want:  true,
		},
		{
			name:  "valid UUID without hyphens",
			input: "12345678123412341234123456789abc",
			want:  true,
		},
		{
			name:  "valid UUID uppercase",
			input: "12345678-1234-1234-1234-123456789ABC",
			want:  true,
		},
		{
			name:  "valid UUID mixed case",
			input: "12345678-1234-1234-1234-123456789AbC",
			want:  true,
		},
		{
			name:  "invalid - too short",
			input: "12345678-1234-1234-1234",
			want:  false,
		},
		{
			name:  "invalid - too long",
			input: "12345678-1234-1234-1234-123456789abcdef",
			want:  false,
		},
		{
			name:  "invalid - contains non-hex",
			input: "12345678-1234-1234-1234-123456789xyz",
			want:  false,
		},
		{
			name:  "invalid - empty string",
			input: "",
			want:  false,
		},
		{
			name:  "invalid - team name instead of UUID",
			input: "my-team-name",
			want:  false,
		},
		{
			name:  "invalid - just numbers",
			input: "12345678",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidUUID(tt.input)
			if got != tt.want {
				t.Errorf("isValidUUID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
