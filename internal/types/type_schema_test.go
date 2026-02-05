package types

import (
	"encoding/json"
	"testing"
)

func TestTypeSchemaValidation(t *testing.T) {
	tests := []struct {
		name    string
		issue   Issue
		schema  *TypeSchema
		labels  []string
		wantErr bool
		errMsg  string
	}{
		{
			name: "nil schema passes",
			issue: Issue{
				Title:     "Test",
				IssueType: TypeTask,
			},
			schema:  nil,
			labels:  nil,
			wantErr: false,
		},
		{
			name: "required field present passes",
			issue: Issue{
				Title:       "Test",
				IssueType:   TypeBug,
				Description: "This is a bug",
			},
			schema: &TypeSchema{
				RequiredFields: []string{"description"},
				Description:    "Bug reports must include a description",
			},
			labels:  nil,
			wantErr: false,
		},
		{
			name: "required field missing fails",
			issue: Issue{
				Title:     "Test",
				IssueType: TypeBug,
			},
			schema: &TypeSchema{
				RequiredFields: []string{"description"},
				Description:    "Bug reports must include a description",
			},
			labels:  nil,
			wantErr: true,
			errMsg:  `type "bug" requires field "description" to be non-empty (schema: Bug reports must include a description)`,
		},
		{
			name: "multiple required fields all present",
			issue: Issue{
				Title:     "Config bead",
				IssueType: IssueType("config"),
				Rig:       "gastown",
				Metadata:  json.RawMessage(`{"key":"val"}`),
			},
			schema: &TypeSchema{
				RequiredFields: []string{"rig", "metadata"},
				Description:    "Config beads require scope and payload",
			},
			labels:  nil,
			wantErr: false,
		},
		{
			name: "multiple required fields one missing",
			issue: Issue{
				Title:     "Config bead",
				IssueType: IssueType("config"),
				Rig:       "gastown",
			},
			schema: &TypeSchema{
				RequiredFields: []string{"rig", "metadata"},
				Description:    "Config beads require scope and payload",
			},
			labels:  nil,
			wantErr: true,
			errMsg:  `type "config" requires field "metadata" to be non-empty (schema: Config beads require scope and payload)`,
		},
		{
			name: "required label exact match passes",
			issue: Issue{
				Title:     "Test",
				IssueType: TypeTask,
			},
			schema: &TypeSchema{
				RequiredLabels: []string{"priority:high"},
			},
			labels:  []string{"priority:high", "team:backend"},
			wantErr: false,
		},
		{
			name: "required label exact match missing fails",
			issue: Issue{
				Title:     "Test",
				IssueType: TypeTask,
			},
			schema: &TypeSchema{
				RequiredLabels: []string{"priority:high"},
				Description:    "Must have priority",
			},
			labels:  []string{"team:backend"},
			wantErr: true,
			errMsg:  `type "task" requires a label matching "priority:high" (schema: Must have priority)`,
		},
		{
			name: "required label wildcard match passes",
			issue: Issue{
				Title:     "Config bead",
				IssueType: IssueType("config"),
			},
			schema: &TypeSchema{
				RequiredLabels: []string{"config:*"},
				Description:    "Config type needs a config category label",
			},
			labels:  []string{"config:identity", "scope:global"},
			wantErr: false,
		},
		{
			name: "required label wildcard no match fails",
			issue: Issue{
				Title:     "Config bead",
				IssueType: IssueType("config"),
			},
			schema: &TypeSchema{
				RequiredLabels: []string{"config:*"},
				Description:    "Config type needs a config category label",
			},
			labels:  []string{"scope:global"},
			wantErr: true,
			errMsg:  `type "config" requires a label matching "config:*" (schema: Config type needs a config category label)`,
		},
		{
			name: "combined fields and labels pass",
			issue: Issue{
				Title:     "Full config",
				IssueType: IssueType("config"),
				Rig:       "*",
				Metadata:  json.RawMessage(`{"key":"val"}`),
			},
			schema: &TypeSchema{
				RequiredFields: []string{"rig", "metadata"},
				RequiredLabels: []string{"config:*"},
				Description:    "Full config requirements",
			},
			labels:  []string{"config:identity", "scope:global"},
			wantErr: false,
		},
		{
			name: "combined fields pass but labels fail",
			issue: Issue{
				Title:     "Full config",
				IssueType: IssueType("config"),
				Rig:       "*",
				Metadata:  json.RawMessage(`{"key":"val"}`),
			},
			schema: &TypeSchema{
				RequiredFields: []string{"rig", "metadata"},
				RequiredLabels: []string{"config:*"},
				Description:    "Full config requirements",
			},
			labels:  []string{"scope:global"},
			wantErr: true,
			errMsg:  `type "config" requires a label matching "config:*" (schema: Full config requirements)`,
		},
		{
			name: "empty labels list with no required labels passes",
			issue: Issue{
				Title:       "Test",
				IssueType:   TypeTask,
				Description: "has desc",
			},
			schema: &TypeSchema{
				RequiredFields: []string{"description"},
			},
			labels:  nil,
			wantErr: false,
		},
		{
			name: "all supported fields validated",
			issue: Issue{
				Title:              "Test",
				IssueType:          TypeTask,
				Description:        "desc",
				Design:             "design doc",
				AcceptanceCriteria: "must work",
				Notes:              "some notes",
				Assignee:           "someone",
				Owner:              "owner@example.com",
			},
			schema: &TypeSchema{
				RequiredFields: []string{"description", "design", "acceptance_criteria", "notes", "assignee", "owner"},
			},
			labels:  nil,
			wantErr: false,
		},
		{
			name: "unknown field name treated as empty",
			issue: Issue{
				Title:     "Test",
				IssueType: TypeTask,
			},
			schema: &TypeSchema{
				RequiredFields: []string{"nonexistent_field"},
				Description:    "Unknown field",
			},
			labels:  nil,
			wantErr: true,
			errMsg:  `type "task" requires field "nonexistent_field" to be non-empty (schema: Unknown field)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.issue.ValidateAgainstSchema(tt.schema, tt.labels)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAgainstSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" {
				if err.Error() != tt.errMsg {
					t.Errorf("ValidateAgainstSchema() error = %q, want %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestMatchesLabelPattern(t *testing.T) {
	tests := []struct {
		pattern string
		labels  []string
		want    bool
	}{
		{"config:*", []string{"config:identity"}, true},
		{"config:*", []string{"config:"}, true},
		{"config:*", []string{"other:thing"}, false},
		{"config:*", nil, false},
		{"config:*", []string{}, false},
		{"exact-label", []string{"exact-label"}, true},
		{"exact-label", []string{"exact-label-plus"}, false},
		{"exact-label", []string{"other"}, false},
		{"scope:*", []string{"scope:global", "scope:local"}, true},
		{"*", []string{"anything"}, true},
		{"*", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+labelsStr(tt.labels), func(t *testing.T) {
			got := matchesLabelPattern(tt.pattern, tt.labels)
			if got != tt.want {
				t.Errorf("matchesLabelPattern(%q, %v) = %v, want %v", tt.pattern, tt.labels, got, tt.want)
			}
		})
	}
}

func labelsStr(labels []string) string {
	if len(labels) == 0 {
		return "empty"
	}
	result := ""
	for i, l := range labels {
		if i > 0 {
			result += ","
		}
		result += l
	}
	return result
}

func TestTypeSchemaJSON(t *testing.T) {
	schema := &TypeSchema{
		RequiredFields: []string{"rig", "metadata"},
		RequiredLabels: []string{"config:*"},
		Description:    "Configuration beads require scope and payload",
	}

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded TypeSchema
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if len(decoded.RequiredFields) != 2 {
		t.Errorf("Expected 2 required fields, got %d", len(decoded.RequiredFields))
	}
	if decoded.RequiredFields[0] != "rig" {
		t.Errorf("Expected first field 'rig', got %q", decoded.RequiredFields[0])
	}
	if len(decoded.RequiredLabels) != 1 {
		t.Errorf("Expected 1 required label, got %d", len(decoded.RequiredLabels))
	}
	if decoded.RequiredLabels[0] != "config:*" {
		t.Errorf("Expected label 'config:*', got %q", decoded.RequiredLabels[0])
	}
	if decoded.Description != schema.Description {
		t.Errorf("Description mismatch: got %q, want %q", decoded.Description, schema.Description)
	}
}

func TestGetFieldValue(t *testing.T) {
	extRef := "gh-123"
	issue := Issue{
		Title:              "Test",
		Description:        "desc",
		Design:             "design",
		AcceptanceCriteria: "ac",
		Notes:              "notes",
		Rig:                "gastown",
		Metadata:           json.RawMessage(`{"key":"val"}`),
		Assignee:           "worker",
		Owner:              "owner@test.com",
		ExternalRef:        &extRef,
	}

	tests := []struct {
		field string
		want  string
	}{
		{"title", "Test"},
		{"description", "desc"},
		{"design", "design"},
		{"acceptance_criteria", "ac"},
		{"notes", "notes"},
		{"rig", "gastown"},
		{"metadata", `{"key":"val"}`},
		{"assignee", "worker"},
		{"owner", "owner@test.com"},
		{"external_ref", "gh-123"},
		{"TITLE", "Test"},       // case insensitive
		{"Description", "desc"}, // case insensitive
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got := issue.getFieldValue(tt.field)
			if got != tt.want {
				t.Errorf("getFieldValue(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}
