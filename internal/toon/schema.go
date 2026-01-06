package toon

import (
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

// IssueSchema defines the canonical TOON representation of issues.
// This schema guides serialization and validates all issue data.
// See docs/TOON_FORMAT.md for complete field documentation.

// TOONField describes a single field in the TOON schema
type TOONField struct {
	Name        string      // Field name in TOON output
	JSONName    string      // Corresponding JSON field name
	Type        FieldType   // Data type
	Required    bool        // Must always be present
	Omittable   bool        // Can be omitted if empty/null
	MaxLength   int         // Max string length (0 = unlimited)
	EnumValues  []string    // Valid enum values (nil = not enum)
	Description string      // Purpose of this field
}

// FieldType categorizes TOON field data types
type FieldType string

const (
	FieldTypeString    FieldType = "string"
	FieldTypeInt       FieldType = "int"
	FieldTypeBool      FieldType = "bool"
	FieldTypeTimestamp FieldType = "timestamp"
	FieldTypeArray     FieldType = "array"
	FieldTypeObject    FieldType = "object"
)

// Schema represents the complete TOON issue schema
type Schema struct {
	Fields       map[string]*TOONField // Field definitions by name
	FieldOrder   []string              // Canonical field order for serialization
	RequiredSet  map[string]bool       // Quick lookup for required fields
	EnumFields   map[string][]string   // Enum definitions by field
}

// NewSchema creates and returns the canonical TOON issue schema
func NewSchema() *Schema {
	s := &Schema{
		Fields:      make(map[string]*TOONField),
		RequiredSet: make(map[string]bool),
		EnumFields:  make(map[string][]string),
	}

	// Define all fields in canonical order
	// See docs/TOON_FORMAT.md for complete field documentation
	s.addField(&TOONField{
		Name:        "id",
		JSONName:    "id",
		Type:        FieldTypeString,
		Required:    true,
		Omittable:   false,
		MaxLength:   100,
		Description: "Unique issue identifier (alphanumeric, hyphens, underscores)",
	})

	s.addField(&TOONField{
		Name:        "title",
		JSONName:    "title",
		Type:        FieldTypeString,
		Required:    true,
		Omittable:   false,
		MaxLength:   500,
		Description: "Issue title/summary (1-500 characters)",
	})

	s.addField(&TOONField{
		Name:        "description",
		JSONName:    "description",
		Type:        FieldTypeString,
		Required:    false,
		Omittable:   true,
		MaxLength:   10000,
		Description: "Detailed problem statement or requirements",
	})

	s.addField(&TOONField{
		Name:        "design",
		JSONName:    "design",
		Type:        FieldTypeString,
		Required:    false,
		Omittable:   true,
		MaxLength:   10000,
		Description: "Design notes and approach documentation",
	})

	s.addField(&TOONField{
		Name:        "acceptance_criteria",
		JSONName:    "acceptance_criteria",
		Type:        FieldTypeString,
		Required:    false,
		Omittable:   true,
		MaxLength:   5000,
		Description: "Success criteria for completion",
	})

	s.addField(&TOONField{
		Name:        "notes",
		JSONName:    "notes",
		Type:        FieldTypeString,
		Required:    false,
		Omittable:   true,
		MaxLength:   10000,
		Description: "Implementation notes and progress tracking",
	})

	s.addField(&TOONField{
		Name:        "status",
		JSONName:    "status",
		Type:        FieldTypeString,
		Required:    true,
		Omittable:   false,
		EnumValues:  []string{"open", "in_progress", "blocked", "closed"},
		Description: "Current work status: open, in_progress, blocked, closed",
	})

	s.addField(&TOONField{
		Name:        "priority",
		JSONName:    "priority",
		Type:        FieldTypeInt,
		Required:    true,
		Omittable:   false,
		Description: "Priority level: 0=critical, 1=high, 2=medium, 3=low, 4=backlog",
	})

	s.addField(&TOONField{
		Name:        "issue_type",
		JSONName:    "issue_type",
		Type:        FieldTypeString,
		Required:    true,
		Omittable:   false,
		EnumValues:  []string{"bug", "feature", "task", "epic", "chore", "message", "merge-request"},
		Description: "Issue classification: bug, feature, task, epic, chore, message, merge-request",
	})

	s.addField(&TOONField{
		Name:        "assignee",
		JSONName:    "assignee",
		Type:        FieldTypeString,
		Required:    false,
		Omittable:   true,
		MaxLength:   100,
		Description: "Person responsible (email or name)",
	})

	s.addField(&TOONField{
		Name:        "estimated_minutes",
		JSONName:    "estimated_minutes",
		Type:        FieldTypeInt,
		Required:    false,
		Omittable:   true,
		Description: "Time estimate in minutes (must be positive if set)",
	})

	s.addField(&TOONField{
		Name:        "created_at",
		JSONName:    "created_at",
		Type:        FieldTypeTimestamp,
		Required:    true,
		Omittable:   false,
		Description: "Creation timestamp (ISO 8601 format YYYY-MM-DDTHH:MM:SSZ)",
	})

	s.addField(&TOONField{
		Name:        "updated_at",
		JSONName:    "updated_at",
		Type:        FieldTypeTimestamp,
		Required:    true,
		Omittable:   false,
		Description: "Last update timestamp (ISO 8601 format)",
	})

	s.addField(&TOONField{
		Name:        "closed_at",
		JSONName:    "closed_at",
		Type:        FieldTypeTimestamp,
		Required:    false,
		Omittable:   true,
		Description: "Closure timestamp (required if status=closed, null otherwise)",
	})

	s.addField(&TOONField{
		Name:        "close_reason",
		JSONName:    "close_reason",
		Type:        FieldTypeString,
		Required:    false,
		Omittable:   true,
		MaxLength:   500,
		Description: "Why issue was closed (required if status=closed, null otherwise)",
	})

	s.addField(&TOONField{
		Name:        "labels",
		JSONName:    "labels",
		Type:        FieldTypeArray,
		Required:    false,
		Omittable:   true,
		Description: "Array of string tags for categorization",
	})

	s.addField(&TOONField{
		Name:        "dependencies",
		JSONName:    "dependencies",
		Type:        FieldTypeArray,
		Required:    false,
		Omittable:   true,
		Description: "Array of dependency relationships (type + depends_on_id)",
	})

	s.addField(&TOONField{
		Name:        "comments",
		JSONName:    "comments",
		Type:        FieldTypeArray,
		Required:    false,
		Omittable:   true,
		Description: "Array of issue comments (id, author, text, created_at)",
	})

	return s
}

// addField adds a field definition to the schema
func (s *Schema) addField(field *TOONField) {
	s.Fields[field.Name] = field
	s.FieldOrder = append(s.FieldOrder, field.Name)

	if field.Required {
		s.RequiredSet[field.Name] = true
	}

	if len(field.EnumValues) > 0 {
		s.EnumFields[field.Name] = field.EnumValues
	}
}

// ValidateIssue checks if an issue conforms to the schema
func (s *Schema) ValidateIssue(issue *types.Issue) error {
	// Validate all required fields are present
	for fieldName := range s.RequiredSet {
		switch fieldName {
		case "id":
			if issue.ID == "" {
				return fmt.Errorf("required field missing: id")
			}
		case "title":
			if issue.Title == "" {
				return fmt.Errorf("required field missing: title")
			}
		case "status":
			if issue.Status == "" {
				return fmt.Errorf("required field missing: status")
			}
		case "priority":
			// priority is always set as int, validated elsewhere
		case "issue_type":
			if issue.IssueType == "" {
				return fmt.Errorf("required field missing: issue_type")
			}
		case "created_at":
			if issue.CreatedAt.IsZero() {
				return fmt.Errorf("required field missing: created_at")
			}
		case "updated_at":
			if issue.UpdatedAt.IsZero() {
				return fmt.Errorf("required field missing: updated_at")
			}
		}
	}

	// Validate enum fields
	if enums, ok := s.EnumFields["status"]; ok {
		valid := false
		for _, e := range enums {
			if string(issue.Status) == e {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid status: %s (must be one of: %v)", issue.Status, enums)
		}
	}

	if enums, ok := s.EnumFields["issue_type"]; ok {
		valid := false
		for _, e := range enums {
			if string(issue.IssueType) == e {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid issue_type: %s (must be one of: %v)", issue.IssueType, enums)
		}
	}

	// Validate closed issue invariants
	if issue.Status == types.StatusClosed {
		if issue.ClosedAt == nil {
			return fmt.Errorf("closed issues must have closed_at timestamp")
		}
		if issue.CloseReason == "" {
			return fmt.Errorf("closed issues must have close_reason")
		}
	} else {
		if issue.ClosedAt != nil {
			return fmt.Errorf("non-closed issues cannot have closed_at timestamp")
		}
	}

	// Validate timestamp ordering
	if issue.UpdatedAt.Before(issue.CreatedAt) {
		return fmt.Errorf("updated_at cannot be before created_at")
	}

	if issue.ClosedAt != nil && issue.ClosedAt.Before(issue.UpdatedAt) {
		return fmt.Errorf("closed_at cannot be before updated_at")
	}

	// Validate string lengths
	if len(issue.Title) > 500 {
		return fmt.Errorf("title exceeds max length (max 500, got %d)", len(issue.Title))
	}

	return nil
}

// GetFieldOrder returns the canonical field order for serialization
func (s *Schema) GetFieldOrder() []string {
	return s.FieldOrder
}

// GetField returns a field definition by name
func (s *Schema) GetField(name string) (*TOONField, bool) {
	f, ok := s.Fields[name]
	return f, ok
}
