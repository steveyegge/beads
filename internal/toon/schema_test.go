package toon

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TestNewSchemaCreation tests that schema is created correctly
func TestNewSchemaCreation(t *testing.T) {
	schema := NewSchema()

	if schema == nil {
		t.Fatal("NewSchema returned nil")
	}

	// Check that all expected fields are present
	expectedFields := []string{
		"id", "title", "description", "design", "acceptance_criteria", "notes",
		"status", "priority", "issue_type", "assignee", "estimated_minutes",
		"created_at", "updated_at", "closed_at", "close_reason",
		"labels", "dependencies", "comments",
	}

	for _, fieldName := range expectedFields {
		if _, ok := schema.GetField(fieldName); !ok {
			t.Errorf("expected field %q not found in schema", fieldName)
		}
	}
}

// TestSchemaFieldOrder tests that field order is canonical
func TestSchemaFieldOrder(t *testing.T) {
	schema := NewSchema()
	order := schema.GetFieldOrder()

	if len(order) == 0 {
		t.Fatal("field order is empty")
	}

	// Verify first few fields are as expected
	if order[0] != "id" {
		t.Errorf("expected first field to be 'id', got %q", order[0])
	}
	if order[1] != "title" {
		t.Errorf("expected second field to be 'title', got %q", order[1])
	}
}

// TestSchemaRequiredFields tests that required fields are marked
func TestSchemaRequiredFields(t *testing.T) {
	schema := NewSchema()

	requiredFields := []string{"id", "title", "status", "priority", "issue_type", "created_at", "updated_at"}

	for _, fieldName := range requiredFields {
		field, ok := schema.GetField(fieldName)
		if !ok {
			t.Errorf("required field %q not found", fieldName)
			continue
		}
		if !field.Required {
			t.Errorf("field %q should be required but is not", fieldName)
		}
	}
}

// TestSchemaEnumFields tests enum value definitions
func TestSchemaEnumFields(t *testing.T) {
	schema := NewSchema()

	// Check status enum
	statusField, ok := schema.GetField("status")
	if !ok {
		t.Fatal("status field not found")
	}
	if len(statusField.EnumValues) == 0 {
		t.Error("status field should have enum values")
	}

	expectedStatuses := []string{"open", "in_progress", "blocked", "closed"}
	for _, status := range expectedStatuses {
		found := false
		for _, e := range statusField.EnumValues {
			if e == status {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected status %q not in enum", status)
		}
	}

	// Check issue_type enum
	typeField, ok := schema.GetField("issue_type")
	if !ok {
		t.Fatal("issue_type field not found")
	}
	if len(typeField.EnumValues) == 0 {
		t.Error("issue_type field should have enum values")
	}
}

// TestValidateIssueSuccess tests validating a valid issue
func TestValidateIssueSuccess(t *testing.T) {
	schema := NewSchema()

	now := time.Now()
	issue := &types.Issue{
		ID:        "1",
		Title:     "Valid issue",
		Description: "A description",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeBug,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := schema.ValidateIssue(issue); err != nil {
		t.Errorf("expected valid issue to pass validation, got error: %v", err)
	}
}

// TestValidateIssueMissingRequired tests validation with missing required field
func TestValidateIssueMissingRequired(t *testing.T) {
	schema := NewSchema()

	now := time.Now()
	issue := &types.Issue{
		// ID is missing
		Title:     "No ID",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeBug,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := schema.ValidateIssue(issue); err == nil {
		t.Error("expected validation to fail for missing ID")
	}
}

// TestValidateIssueInvalidStatus tests validation with invalid status
func TestValidateIssueInvalidStatus(t *testing.T) {
	schema := NewSchema()

	now := time.Now()
	issue := &types.Issue{
		ID:        "1",
		Title:     "Invalid status",
		Status:    types.Status("invalid"),
		Priority:  1,
		IssueType: types.TypeBug,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := schema.ValidateIssue(issue); err == nil {
		t.Error("expected validation to fail for invalid status")
	}
}

// TestValidateIssueClosed tests validation of closed issues
func TestValidateIssueClosed(t *testing.T) {
	schema := NewSchema()

	now := time.Now()
	later := now.Add(1 * time.Hour)

	tests := []struct {
		name    string
		issue   *types.Issue
		wantErr bool
	}{
		{
			name: "valid closed issue",
			issue: &types.Issue{
				ID:         "1",
				Title:      "Closed issue",
				Status:     types.StatusClosed,
				Priority:   1,
				IssueType:  types.TypeBug,
				CreatedAt:  now,
				UpdatedAt:  now,
				ClosedAt:   &later,
				CloseReason: "Done",
			},
			wantErr: false,
		},
		{
			name: "closed but missing closed_at",
			issue: &types.Issue{
				ID:          "1",
				Title:       "No closed_at",
				Status:      types.StatusClosed,
				Priority:    1,
				IssueType:   types.TypeBug,
				CreatedAt:   now,
				UpdatedAt:   now,
				CloseReason: "Done",
				// ClosedAt missing
			},
			wantErr: true,
		},
		{
			name: "closed but missing close_reason",
			issue: &types.Issue{
				ID:        "1",
				Title:     "No close_reason",
				Status:    types.StatusClosed,
				Priority:  1,
				IssueType: types.TypeBug,
				CreatedAt: now,
				UpdatedAt: now,
				ClosedAt:  &later,
				// CloseReason missing
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schema.ValidateIssue(tt.issue)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIssue() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateIssueTimestampOrdering tests timestamp validation
func TestValidateIssueTimestampOrdering(t *testing.T) {
	schema := NewSchema()

	now := time.Now()
	past := now.Add(-1 * time.Hour)

	tests := []struct {
		name    string
		issue   *types.Issue
		wantErr bool
	}{
		{
			name: "valid timestamp order",
			issue: &types.Issue{
				ID:        "1",
				Title:     "Valid",
				Status:    types.StatusOpen,
				Priority:  1,
				IssueType: types.TypeBug,
				CreatedAt: past,
				UpdatedAt: now,
			},
			wantErr: false,
		},
		{
			name: "updated before created",
			issue: &types.Issue{
				ID:        "1",
				Title:     "Invalid",
				Status:    types.StatusOpen,
				Priority:  1,
				IssueType: types.TypeBug,
				CreatedAt: now,
				UpdatedAt: past,
			},
			wantErr: true,
		},
		{
			name: "closed before updated",
			issue: &types.Issue{
				ID:         "1",
				Title:      "Invalid closed",
				Status:     types.StatusClosed,
				Priority:   1,
				IssueType:  types.TypeBug,
				CreatedAt:  past,
				UpdatedAt:  now,
				ClosedAt:   &past, // Before updated_at
				CloseReason: "Done",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := schema.ValidateIssue(tt.issue)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIssue() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateIssueTitleLength tests title length validation
func TestValidateIssueTitleLength(t *testing.T) {
	schema := NewSchema()

	now := time.Now()

	// Create a title that's too long (> 500 chars)
	longTitle := ""
	for i := 0; i < 501; i++ {
		longTitle += "x"
	}

	issue := &types.Issue{
		ID:        "1",
		Title:     longTitle,
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeBug,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := schema.ValidateIssue(issue); err == nil {
		t.Error("expected validation to fail for title exceeding 500 characters")
	}
}

// TestSchemaFieldDefinitions tests that field definitions are complete
func TestSchemaFieldDefinitions(t *testing.T) {
	schema := NewSchema()

	// Check a few important fields have correct properties
	idField, ok := schema.GetField("id")
	if !ok {
		t.Fatal("id field not found")
	}
	if idField.MaxLength != 100 {
		t.Errorf("id field max length should be 100, got %d", idField.MaxLength)
	}
	if !idField.Required {
		t.Error("id field should be required")
	}

	titleField, ok := schema.GetField("title")
	if !ok {
		t.Fatal("title field not found")
	}
	if titleField.MaxLength != 500 {
		t.Errorf("title field max length should be 500, got %d", titleField.MaxLength)
	}

	descriptionField, ok := schema.GetField("description")
	if !ok {
		t.Fatal("description field not found")
	}
	if descriptionField.Required {
		t.Error("description field should not be required")
	}
	if !descriptionField.Omittable {
		t.Error("description field should be omittable")
	}
}
