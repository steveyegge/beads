package main

import (
	"testing"
)

func TestResolveRemoteAddArgs_FullArgs(t *testing.T) {
	// URL from positional arg, credentials from flags
	r, err := resolveRemoteAddArgs(
		[]string{"origin", "http://example.com:50051/org/db"},
		"alice", "secret",
		"", "", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.url != "http://example.com:50051/org/db" {
		t.Errorf("url = %q, want positional URL", r.url)
	}
	if r.user != "alice" || r.password != "secret" {
		t.Errorf("creds = %q/%q, want alice/secret", r.user, r.password)
	}
	if r.needsPasswordPrompt {
		t.Error("should not need password prompt")
	}
}

func TestResolveRemoteAddArgs_EnvVarsOnly(t *testing.T) {
	// Name-only arg, everything from env
	r, err := resolveRemoteAddArgs(
		[]string{"origin"},
		"", "",
		"http://env.example.com:50051/org/db", "envuser", "envpass",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.url != "http://env.example.com:50051/org/db" {
		t.Errorf("url = %q, want env URL", r.url)
	}
	if r.user != "envuser" || r.password != "envpass" {
		t.Errorf("creds = %q/%q, want envuser/envpass", r.user, r.password)
	}
	if r.needsPasswordPrompt {
		t.Error("should not need password prompt")
	}
}

func TestResolveRemoteAddArgs_FlagsOverrideEnv(t *testing.T) {
	// Flags take precedence over env vars
	r, err := resolveRemoteAddArgs(
		[]string{"origin", "http://flag-url.com/org/db"},
		"flaguser", "flagpass",
		"http://env-url.com/org/db", "envuser", "envpass",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.url != "http://flag-url.com/org/db" {
		t.Errorf("url = %q, want positional URL over env", r.url)
	}
	if r.user != "flaguser" {
		t.Errorf("user = %q, want flaguser (flag overrides env)", r.user)
	}
	if r.password != "flagpass" {
		t.Errorf("password = %q, want flagpass (flag overrides env)", r.password)
	}
}

func TestResolveRemoteAddArgs_EnvFillsGaps(t *testing.T) {
	// Flag provides user, env provides password
	r, err := resolveRemoteAddArgs(
		[]string{"origin", "http://example.com/org/db"},
		"flaguser", "",
		"", "", "envpass",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.user != "flaguser" {
		t.Errorf("user = %q, want flaguser", r.user)
	}
	if r.password != "envpass" {
		t.Errorf("password = %q, want envpass (env fills gap)", r.password)
	}
	if r.needsPasswordPrompt {
		t.Error("should not need prompt when env provides password")
	}
}

func TestResolveRemoteAddArgs_PromptNeeded(t *testing.T) {
	// User provided but no password anywhere → needs prompt
	r, err := resolveRemoteAddArgs(
		[]string{"origin", "http://example.com/org/db"},
		"alice", "",
		"", "", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.user != "alice" {
		t.Errorf("user = %q, want alice", r.user)
	}
	if !r.needsPasswordPrompt {
		t.Error("should need password prompt")
	}
}

func TestResolveRemoteAddArgs_NoURLError(t *testing.T) {
	// Name only, no env → error
	_, err := resolveRemoteAddArgs(
		[]string{"origin"},
		"", "",
		"", "", "",
	)
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func TestResolveRemoteAddArgs_NoCredentials(t *testing.T) {
	// URL provided, no credentials at all → works, no auth
	r, err := resolveRemoteAddArgs(
		[]string{"origin", "http://example.com/org/db"},
		"", "",
		"", "", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.user != "" || r.password != "" {
		t.Errorf("expected empty creds, got %q/%q", r.user, r.password)
	}
	if r.needsPasswordPrompt {
		t.Error("should not need prompt with no user")
	}
}

func TestResolveRemoteAddArgs_EnvUserOnly(t *testing.T) {
	// Env provides user but no password → needs prompt
	r, err := resolveRemoteAddArgs(
		[]string{"origin"},
		"", "",
		"http://env.example.com/org/db", "envuser", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.user != "envuser" {
		t.Errorf("user = %q, want envuser", r.user)
	}
	if !r.needsPasswordPrompt {
		t.Error("should need password prompt when env has user but no password")
	}
}
