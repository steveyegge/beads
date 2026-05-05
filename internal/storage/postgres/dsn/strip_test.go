package dsn

import (
	"strings"
	"testing"
)

func TestStrip(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantPass string // substring that must NOT appear in output
		wantFrag string // substring that MUST appear in output
		wantErr  bool
	}{
		{
			name:     "uri form with password",
			input:    "postgres://bd:s3cret@db.example.com:5432/beads?sslmode=require",
			wantPass: "s3cret",
			wantFrag: "bd@db.example.com:5432/beads",
		},
		{
			name:     "keyword form with password",
			input:    "host=db.example.com port=5432 user=bd password=s3cret dbname=beads sslmode=require",
			wantPass: "s3cret",
			wantFrag: "bd@db.example.com",
		},
		{
			name:     "uri form without password",
			input:    "postgres://bd@db.example.com:5432/beads",
			wantFrag: "bd@db.example.com:5432/beads",
		},
		{
			name:     "preserves sslmode runtime param",
			input:    "postgres://bd:pw@db.example.com/beads?sslmode=require",
			wantPass: "pw",
			wantFrag: "sslmode=require",
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "garbage input",
			input:   "this is not a dsn",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Strip(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Strip(%q) = %q, want error", tt.input, got)
				}
				// Skip the leak check for trivially-empty inputs — every error
				// message contains "" by construction.
				if tt.input != "" && strings.Contains(err.Error(), tt.input) {
					t.Errorf("error message echoes input %q: %v", tt.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Strip(%q) unexpected error: %v", tt.input, err)
			}
			if tt.wantPass != "" && strings.Contains(got, tt.wantPass) {
				t.Errorf("Strip(%q) leaked password substring %q in output: %s", tt.input, tt.wantPass, got)
			}
			if tt.wantFrag != "" && !strings.Contains(got, tt.wantFrag) {
				t.Errorf("Strip(%q) = %q, want substring %q", tt.input, got, tt.wantFrag)
			}
		})
	}
}

func TestComposeRoundTrip(t *testing.T) {
	original := "postgres://bd:s3cret@db.example.com:5432/beads?sslmode=require"
	stripped, err := Strip(original)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if strings.Contains(stripped, "s3cret") {
		t.Fatalf("stripped form leaked password: %s", stripped)
	}

	composed := Compose(stripped, "newpassword")
	if !strings.Contains(composed, "newpassword") {
		t.Errorf("Compose did not embed password; got: %s", composed)
	}
	if !strings.Contains(composed, "db.example.com:5432") {
		t.Errorf("Compose lost host/port; got: %s", composed)
	}
	if !strings.Contains(composed, "/beads") {
		t.Errorf("Compose lost database name; got: %s", composed)
	}
	if !strings.Contains(composed, "sslmode=require") {
		t.Errorf("Compose lost sslmode runtime param; got: %s", composed)
	}
}

func TestComposeWithEmptyPasswordIsIdentity(t *testing.T) {
	stripped := "postgres://bd@db.example.com:5432/beads?sslmode=require"
	composed := Compose(stripped, "")
	if composed != stripped {
		t.Errorf("Compose with empty password should return stripped form unchanged\n got: %s\nwant: %s", composed, stripped)
	}
}

func TestComposeWithMalformedStrippedDSNFallsThrough(t *testing.T) {
	// If the persisted DSN was already malformed (e.g. hand-edited), Compose
	// should pass it through so the postgres factory's own ParseConfig
	// surfaces a clean error with redaction — rather than swallowing the
	// failure here.
	bad := "not a dsn"
	got := Compose(bad, "anything")
	if got != bad {
		t.Errorf("Compose(malformed, _) = %q, want %q", got, bad)
	}
}

func TestStripIsIdempotent(t *testing.T) {
	// Stripping an already-stripped DSN should be a no-op (modulo
	// re-marshaling whitespace).
	original := "postgres://bd:s3cret@db.example.com:5432/beads"
	first, err := Strip(original)
	if err != nil {
		t.Fatalf("first Strip: %v", err)
	}
	second, err := Strip(first)
	if err != nil {
		t.Fatalf("second Strip: %v", err)
	}
	if first != second {
		t.Errorf("Strip not idempotent\n first:  %s\n second: %s", first, second)
	}
}
