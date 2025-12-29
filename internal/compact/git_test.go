package compact

import (
	"errors"
	"testing"
)

func TestGetCurrentCommitHashSuccess(t *testing.T) {
	orig := gitExec
	gitExec = func(string, ...string) ([]byte, error) {
		return []byte("abc123\n"), nil
	}
	t.Cleanup(func() { gitExec = orig })

	if got := GetCurrentCommitHash(); got != "abc123" {
		t.Fatalf("expected trimmed hash, got %q", got)
	}
}

func TestGetCurrentCommitHashError(t *testing.T) {
	orig := gitExec
	gitExec = func(string, ...string) ([]byte, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { gitExec = orig })

	if got := GetCurrentCommitHash(); got != "" {
		t.Fatalf("expected empty string on error, got %q", got)
	}
}
