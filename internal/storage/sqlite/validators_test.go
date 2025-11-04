package sqlite

import (
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestValidatePriority(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		wantErr bool
	}{
		{"valid priority 0", 0, false},
		{"valid priority 1", 1, false},
		{"valid priority 2", 2, false},
		{"valid priority 3", 3, false},
		{"valid priority 4", 4, false},
		{"invalid negative", -1, true},
		{"invalid too high", 5, true},
		{"non-int ignored", "not an int", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePriority(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePriority() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateStatus(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		wantErr bool
	}{
		{"valid open", string(types.StatusOpen), false},
		{"valid in_progress", string(types.StatusInProgress), false},
		{"valid blocked", string(types.StatusBlocked), false},
		{"valid closed", string(types.StatusClosed), false},
		{"invalid status", "invalid", true},
		{"non-string ignored", 123, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStatus(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateIssueType(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		wantErr bool
	}{
		{"valid bug", string(types.TypeBug), false},
		{"valid feature", string(types.TypeFeature), false},
		{"valid task", string(types.TypeTask), false},
		{"valid epic", string(types.TypeEpic), false},
		{"valid chore", string(types.TypeChore), false},
		{"invalid type", "invalid", true},
		{"non-string ignored", 123, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIssueType(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateIssueType() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateTitle(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		wantErr bool
	}{
		{"valid title", "Valid Title", false},
		{"empty title", "", true},
		{"max length title", string(make([]byte, 500)), false},
		{"too long title", string(make([]byte, 501)), true},
		{"non-string ignored", 123, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTitle(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTitle() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateEstimatedMinutes(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		wantErr bool
	}{
		{"valid zero", 0, false},
		{"valid positive", 60, false},
		{"invalid negative", -1, true},
		{"non-int ignored", "not an int", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEstimatedMinutes(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEstimatedMinutes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateFieldUpdate(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   interface{}
		wantErr bool
	}{
		{"valid priority", "priority", 1, false},
		{"invalid priority", "priority", 5, true},
		{"valid status", "status", string(types.StatusOpen), false},
		{"invalid status", "status", "invalid", true},
		{"unknown field", "unknown_field", "any value", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFieldUpdate(tt.key, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateFieldUpdate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
