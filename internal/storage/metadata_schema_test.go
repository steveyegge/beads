package storage

import (
	"encoding/json"
	"testing"
)

func TestValidateMetadataSchema_NoFields(t *testing.T) {
	schema := MetadataSchemaConfig{Mode: "error", Fields: nil}
	errs := ValidateMetadataSchema(json.RawMessage(`{"anything":"goes"}`), schema)
	if len(errs) != 0 {
		t.Errorf("expected no errors for empty schema, got %v", errs)
	}
}

func TestValidateMetadataSchema_RequiredField(t *testing.T) {
	schema := MetadataSchemaConfig{
		Mode: "error",
		Fields: map[string]MetadataFieldSchema{
			"team": {Type: MetadataFieldEnum, Values: []string{"platform", "frontend"}, Required: true},
		},
	}

	// Missing required field
	errs := ValidateMetadataSchema(json.RawMessage(`{}`), schema)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "team" {
		t.Errorf("expected field 'team', got %q", errs[0].Field)
	}

	// Present and valid
	errs = ValidateMetadataSchema(json.RawMessage(`{"team":"platform"}`), schema)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidateMetadataSchema_EnumValidation(t *testing.T) {
	schema := MetadataSchemaConfig{
		Mode: "error",
		Fields: map[string]MetadataFieldSchema{
			"severity": {Type: MetadataFieldEnum, Values: []string{"low", "medium", "high", "critical"}},
		},
	}

	// Valid value
	errs := ValidateMetadataSchema(json.RawMessage(`{"severity":"high"}`), schema)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}

	// Invalid value
	errs = ValidateMetadataSchema(json.RawMessage(`{"severity":"extreme"}`), schema)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "severity" {
		t.Errorf("expected field 'severity', got %q", errs[0].Field)
	}

	// Wrong type
	errs = ValidateMetadataSchema(json.RawMessage(`{"severity":42}`), schema)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidateMetadataSchema_IntValidation(t *testing.T) {
	min := float64(0)
	max := float64(100)
	schema := MetadataSchemaConfig{
		Mode: "error",
		Fields: map[string]MetadataFieldSchema{
			"points": {Type: MetadataFieldInt, Min: &min, Max: &max},
		},
	}

	// Valid
	errs := ValidateMetadataSchema(json.RawMessage(`{"points":5}`), schema)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}

	// Below min
	errs = ValidateMetadataSchema(json.RawMessage(`{"points":-1}`), schema)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}

	// Above max
	errs = ValidateMetadataSchema(json.RawMessage(`{"points":101}`), schema)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}

	// Float when int expected
	errs = ValidateMetadataSchema(json.RawMessage(`{"points":3.5}`), schema)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}

	// Wrong type entirely
	errs = ValidateMetadataSchema(json.RawMessage(`{"points":"five"}`), schema)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidateMetadataSchema_StringValidation(t *testing.T) {
	schema := MetadataSchemaConfig{
		Mode: "error",
		Fields: map[string]MetadataFieldSchema{
			"tool": {Type: MetadataFieldString},
		},
	}

	// Valid
	errs := ValidateMetadataSchema(json.RawMessage(`{"tool":"golangci-lint"}`), schema)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}

	// Wrong type
	errs = ValidateMetadataSchema(json.RawMessage(`{"tool":42}`), schema)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidateMetadataSchema_BoolValidation(t *testing.T) {
	schema := MetadataSchemaConfig{
		Mode: "error",
		Fields: map[string]MetadataFieldSchema{
			"urgent": {Type: MetadataFieldBool},
		},
	}

	// Valid
	errs := ValidateMetadataSchema(json.RawMessage(`{"urgent":true}`), schema)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}

	// Wrong type
	errs = ValidateMetadataSchema(json.RawMessage(`{"urgent":"yes"}`), schema)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidateMetadataSchema_FloatValidation(t *testing.T) {
	min := float64(0.0)
	max := float64(1.0)
	schema := MetadataSchemaConfig{
		Mode: "error",
		Fields: map[string]MetadataFieldSchema{
			"confidence": {Type: MetadataFieldFloat, Min: &min, Max: &max},
		},
	}

	// Valid
	errs := ValidateMetadataSchema(json.RawMessage(`{"confidence":0.95}`), schema)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}

	// Above max
	errs = ValidateMetadataSchema(json.RawMessage(`{"confidence":1.5}`), schema)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidateMetadataSchema_UnknownKeysAllowed(t *testing.T) {
	schema := MetadataSchemaConfig{
		Mode: "error",
		Fields: map[string]MetadataFieldSchema{
			"team": {Type: MetadataFieldString, Required: true},
		},
	}

	// Has required field plus unknown keys — should pass
	errs := ValidateMetadataSchema(json.RawMessage(`{"team":"platform","foo":"bar","custom":123}`), schema)
	if len(errs) != 0 {
		t.Errorf("expected unknown keys to be allowed, got %v", errs)
	}
}

func TestValidateMetadataSchema_NilMetadata(t *testing.T) {
	schema := MetadataSchemaConfig{
		Mode: "error",
		Fields: map[string]MetadataFieldSchema{
			"optional_field": {Type: MetadataFieldString},
		},
	}

	// nil metadata with no required fields — should pass
	errs := ValidateMetadataSchema(nil, schema)
	if len(errs) != 0 {
		t.Errorf("expected no errors for nil metadata with no required fields, got %v", errs)
	}
}

func TestValidateMetadataSchema_NilMetadataWithRequired(t *testing.T) {
	schema := MetadataSchemaConfig{
		Mode: "error",
		Fields: map[string]MetadataFieldSchema{
			"team": {Type: MetadataFieldString, Required: true},
		},
	}

	// nil metadata with required field — should fail
	errs := ValidateMetadataSchema(nil, schema)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for nil metadata with required field, got %d: %v", len(errs), errs)
	}
}

func TestValidateMetadataSchema_NotAnObject(t *testing.T) {
	schema := MetadataSchemaConfig{
		Mode: "error",
		Fields: map[string]MetadataFieldSchema{
			"team": {Type: MetadataFieldString},
		},
	}

	// Array metadata — should error
	errs := ValidateMetadataSchema(json.RawMessage(`["a","b"]`), schema)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for non-object metadata, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "(root)" {
		t.Errorf("expected field '(root)', got %q", errs[0].Field)
	}
}

func TestValidateMetadataSchema_MultipleErrors(t *testing.T) {
	schema := MetadataSchemaConfig{
		Mode: "error",
		Fields: map[string]MetadataFieldSchema{
			"team":     {Type: MetadataFieldEnum, Values: []string{"a", "b"}, Required: true},
			"severity": {Type: MetadataFieldEnum, Values: []string{"low", "high"}, Required: true},
		},
	}

	// Both required fields missing
	errs := ValidateMetadataSchema(json.RawMessage(`{}`), schema)
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
}

func TestMetadataValidationError_Error(t *testing.T) {
	e := MetadataValidationError{Field: "team", Message: "required field is missing"}
	want := "metadata.team: required field is missing"
	if e.Error() != want {
		t.Errorf("got %q, want %q", e.Error(), want)
	}
}
