package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joho/godotenv"
)

func TestResolveRemoteAddArgs_FullArgs(t *testing.T) {
	r, err := resolveRemoteAddArgs(
		[]string{"origin", "http://example.com:50051"},
		"alice", "secret", "",
		"", "", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Base URL gets directory name appended
	if !strings.HasPrefix(r.url, "http://example.com:50051/") {
		t.Errorf("url = %q, want base URL with db name appended", r.url)
	}
	if r.user != "alice" || r.password != "secret" {
		t.Errorf("creds = %q/%q, want alice/secret", r.user, r.password)
	}
	if r.needsPasswordPrompt {
		t.Error("should not need password prompt")
	}
}

func TestResolveRemoteAddArgs_DBNameFlag(t *testing.T) {
	r, err := resolveRemoteAddArgs(
		[]string{"origin", "http://example.com:50051"},
		"", "", "mydb",
		"", "", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.url != "http://example.com:50051/mydb" {
		t.Errorf("url = %q, want http://example.com:50051/mydb", r.url)
	}
}

func TestResolveRemoteAddArgs_DBNameDefault(t *testing.T) {
	// Without --db-name, derives from cwd
	r, err := resolveRemoteAddArgs(
		[]string{"origin", "http://example.com:50051"},
		"", "", "",
		"", "", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	derived := deriveRemoteDBName()
	if r.url != "http://example.com:50051/"+derived {
		t.Errorf("url = %q, want http://example.com:50051/%s", r.url, derived)
	}
}

func TestResolveRemoteAddArgs_TrailingSlash(t *testing.T) {
	r, err := resolveRemoteAddArgs(
		[]string{"origin", "http://example.com:50051/"},
		"", "", "mydb",
		"", "", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.url != "http://example.com:50051/mydb" {
		t.Errorf("url = %q, want no double slash", r.url)
	}
}

func TestResolveRemoteAddArgs_EnvVarsOnly(t *testing.T) {
	r, err := resolveRemoteAddArgs(
		[]string{"origin"},
		"", "", "mydb",
		"http://env.example.com:50051", "envuser", "envpass",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.url != "http://env.example.com:50051/mydb" {
		t.Errorf("url = %q, want env URL with db name", r.url)
	}
	if r.user != "envuser" || r.password != "envpass" {
		t.Errorf("creds = %q/%q, want envuser/envpass", r.user, r.password)
	}
}

func TestResolveRemoteAddArgs_FlagsOverrideEnv(t *testing.T) {
	r, err := resolveRemoteAddArgs(
		[]string{"origin", "http://flag-url.com"},
		"flaguser", "flagpass", "flagdb",
		"http://env-url.com", "envuser", "envpass",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.url != "http://flag-url.com/flagdb" {
		t.Errorf("url = %q, want positional URL with flag db name", r.url)
	}
	if r.user != "flaguser" {
		t.Errorf("user = %q, want flaguser", r.user)
	}
	if r.password != "flagpass" {
		t.Errorf("password = %q, want flagpass", r.password)
	}
}

func TestResolveRemoteAddArgs_EnvFillsGaps(t *testing.T) {
	r, err := resolveRemoteAddArgs(
		[]string{"origin", "http://example.com"},
		"flaguser", "", "mydb",
		"", "", "envpass",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.user != "flaguser" {
		t.Errorf("user = %q, want flaguser", r.user)
	}
	if r.password != "envpass" {
		t.Errorf("password = %q, want envpass", r.password)
	}
	if r.needsPasswordPrompt {
		t.Error("should not need prompt when env provides password")
	}
}

func TestResolveRemoteAddArgs_PromptNeeded(t *testing.T) {
	r, err := resolveRemoteAddArgs(
		[]string{"origin", "http://example.com"},
		"alice", "", "mydb",
		"", "", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.needsPasswordPrompt {
		t.Error("should need password prompt")
	}
}

func TestResolveRemoteAddArgs_NoURLError(t *testing.T) {
	_, err := resolveRemoteAddArgs(
		[]string{"origin"},
		"", "", "",
		"", "", "",
	)
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func TestResolveRemoteAddArgs_NoCredentials(t *testing.T) {
	r, err := resolveRemoteAddArgs(
		[]string{"origin", "http://example.com"},
		"", "", "mydb",
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
	r, err := resolveRemoteAddArgs(
		[]string{"origin"},
		"", "", "mydb",
		"http://env.example.com", "envuser", "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.needsPasswordPrompt {
		t.Error("should need password prompt when env has user but no password")
	}
}

func TestResolveRemoteAddArgs_DotEnvLoading(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envFile, []byte(
		"DOLT_REMOTE_ADDRESS=http://dotenv.example.com\n"+
			"DOLT_REMOTE_USERNAME=dotenvuser\n"+
			"DOLT_REMOTE_PASSWORD=dotenvpass\n",
	), 0600); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	os.Unsetenv("DOLT_REMOTE_ADDRESS")
	os.Unsetenv("DOLT_REMOTE_USERNAME")
	os.Unsetenv("DOLT_REMOTE_PASSWORD")

	oldCwd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()

	if err := godotenv.Load(); err != nil {
		t.Fatalf("godotenv.Load failed: %v", err)
	}

	r, err := resolveRemoteAddArgs(
		[]string{"origin"},
		"", "", "mydb",
		os.Getenv("DOLT_REMOTE_ADDRESS"),
		os.Getenv("DOLT_REMOTE_USERNAME"),
		os.Getenv("DOLT_REMOTE_PASSWORD"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.url != "http://dotenv.example.com/mydb" {
		t.Errorf("url = %q, want dotenv URL with db name", r.url)
	}
	if r.user != "dotenvuser" || r.password != "dotenvpass" {
		t.Errorf("creds = %q/%q, want dotenvuser/dotenvpass", r.user, r.password)
	}
}

func TestResolveRemoteAddArgs_DotEnvDoesNotOverrideExisting(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envFile, []byte(
		"DOLT_REMOTE_ADDRESS=http://dotenv.example.com\n"+
			"DOLT_REMOTE_USERNAME=dotenvuser\n"+
			"DOLT_REMOTE_PASSWORD=dotenvpass\n",
	), 0600); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	t.Setenv("DOLT_REMOTE_ADDRESS", "http://real.example.com")
	t.Setenv("DOLT_REMOTE_USERNAME", "realuser")
	t.Setenv("DOLT_REMOTE_PASSWORD", "realpass")

	oldCwd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldCwd) }()

	_ = godotenv.Load()

	r, err := resolveRemoteAddArgs(
		[]string{"origin"},
		"", "", "mydb",
		os.Getenv("DOLT_REMOTE_ADDRESS"),
		os.Getenv("DOLT_REMOTE_USERNAME"),
		os.Getenv("DOLT_REMOTE_PASSWORD"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.url != "http://real.example.com/mydb" {
		t.Errorf("url = %q, want existing env URL (not .env)", r.url)
	}
	if r.user != "realuser" || r.password != "realpass" {
		t.Errorf("creds = %q/%q, want realuser/realpass (not .env)", r.user, r.password)
	}
}

func TestDeriveRemoteDBName(t *testing.T) {
	name := deriveRemoteDBName()
	if name == "" {
		t.Error("derived name should not be empty")
	}
	// Should be the basename of the current directory
	cwd, _ := os.Getwd()
	expected := filepath.Base(cwd)
	if name != expected {
		t.Errorf("deriveRemoteDBName() = %q, want %q", name, expected)
	}
}
