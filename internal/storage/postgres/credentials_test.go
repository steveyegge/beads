package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
)

// TestRedactDSN exercises the regex-based scrubber in redact.go.
func TestRedactDSN(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string // substrings that MUST appear in output
		bad  []string // substrings that MUST NOT appear in output
	}{
		{
			name: "url form with password",
			in:   "postgres://alice:hunter2@db.example.com:5432/bd?sslmode=disable",
			want: []string{"[REDACTED_DSN]"},
			bad:  []string{"hunter2", "alice"},
		},
		{
			name: "libpq form with password",
			in:   "host=db user=alice password=hunter2 dbname=bd",
			want: []string{"password=[REDACTED]"},
			bad:  []string{"hunter2"},
		},
		{
			name: "wrapped error with embedded url",
			in:   "failed to connect: postgres://bob:s3cret@host/db: timeout",
			want: []string{"[REDACTED_DSN]"},
			bad:  []string{"s3cret"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactDSN(tc.in)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("expected %q in %q", w, got)
				}
			}
			for _, b := range tc.bad {
				if strings.Contains(got, b) {
					t.Errorf("did NOT expect %q in %q", b, got)
				}
			}
		})
	}
}

// TestErrUnknownDSNParam verifies the typed error from the allowlist check.
func TestErrUnknownDSNParam(t *testing.T) {
	err := validateDSNParams("postgres://h/db?bogus_param=1")
	if err == nil {
		t.Fatal("expected error for unknown param")
	}
	var typed ErrUnknownDSNParam
	if !errors.As(err, &typed) {
		t.Fatalf("expected ErrUnknownDSNParam, got %T", err)
	}
	if typed.Name != "bogus_param" {
		t.Errorf("expected param name bogus_param, got %q", typed.Name)
	}
}

// TestSentinelPasswordNeverLeaks builds a deliberately-bad DSN with a sentinel
// password, calls openStore against an unreachable host, and asserts the
// sentinel never appears in the returned error.
func TestSentinelPasswordNeverLeaks(t *testing.T) {
	const sentinel = "RIDICULOUSLY_SECRET_SENTINEL_PASSWORD_PLEASE_DONT_LOG_ME"
	dsn := "postgres://alice:" + sentinel + "@127.0.0.1:1/bd?connect_timeout=1"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := openStore(ctx, storage.ConnectionConfig{DSN: dsn})
	if err == nil {
		t.Skip("connect unexpectedly succeeded — sentinel test is meaningful only on failed connect")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Errorf("sentinel password leaked into error: %q", err.Error())
	}
}
