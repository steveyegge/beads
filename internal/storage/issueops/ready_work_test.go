package issueops

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestBuildSQLInClause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		ids              []string
		wantPlaceholders string
		wantArgs         []interface{}
	}{
		{
			name:             "single ID",
			ids:              []string{"42"},
			wantPlaceholders: "?",
			wantArgs:         []interface{}{"42"},
		},
		{
			name:             "multiple IDs",
			ids:              []string{"1", "2", "3"},
			wantPlaceholders: "?,?,?",
			wantArgs:         []interface{}{"1", "2", "3"},
		},
		{
			name:             "empty slice",
			ids:              []string{},
			wantPlaceholders: "",
			wantArgs:         []interface{}{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotPlaceholders, gotArgs := buildSQLInClause(tt.ids)

			if gotPlaceholders != tt.wantPlaceholders {
				t.Errorf("placeholders = %q, want %q", gotPlaceholders, tt.wantPlaceholders)
			}

			if len(gotArgs) != len(tt.wantArgs) {
				t.Fatalf("args length = %d, want %d", len(gotArgs), len(tt.wantArgs))
			}

			for i := range gotArgs {
				if gotArgs[i] != tt.wantArgs[i] {
					t.Errorf("args[%d] = %v, want %v", i, gotArgs[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestGetReadyWorkInTx_UnboundedPropagatesBlockedComputationError(t *testing.T) {
	t.Parallel()

	blockedErr := errors.New("blocked graph unavailable")
	_, err := GetReadyWorkInTx(
		context.Background(),
		nil,
		types.WorkFilter{IncludeDeferred: true},
		func(context.Context, *sql.Tx, bool) ([]string, error) {
			return nil, blockedErr
		},
	)
	if err == nil {
		t.Fatal("expected blocked computation error")
	}
	if !errors.Is(err, blockedErr) {
		t.Fatalf("expected wrapped blocked computation error, got %v", err)
	}
	if !strings.Contains(err.Error(), "compute blocked IDs") {
		t.Fatalf("expected compute blocked IDs context, got %v", err)
	}
}
