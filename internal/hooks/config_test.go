package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadEventHooks_NoConfig(t *testing.T) {
	v := viper.New()
	hooks, err := LoadEventHooks(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hooks != nil {
		t.Errorf("expected nil hooks, got %v", hooks)
	}
}

func TestLoadEventHooks_NilViper(t *testing.T) {
	hooks, err := LoadEventHooks(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hooks != nil {
		t.Errorf("expected nil hooks, got %v", hooks)
	}
}

func TestLoadEventHooks_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `event-hooks:
  - event: post-create
    command: "echo created ${BEAD_ID}"
    async: true
    filter: "priority:P0,P1"
  - event: post-write
    command: "bobbin index-bead --id ${BEAD_ID}"
    async: true
`
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read config: %v", err)
	}

	hooks, err := LoadEventHooks(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(hooks))
	}

	h0 := hooks[0]
	if h0.Event != "post-create" {
		t.Errorf("hook[0].Event = %q, want %q", h0.Event, "post-create")
	}
	if h0.Command != "echo created ${BEAD_ID}" {
		t.Errorf("hook[0].Command = %q", h0.Command)
	}
	if !h0.Async {
		t.Error("hook[0].Async should be true")
	}
	if h0.Filter != "priority:P0,P1" {
		t.Errorf("hook[0].Filter = %q", h0.Filter)
	}

	h1 := hooks[1]
	if h1.Event != "post-write" {
		t.Errorf("hook[1].Event = %q, want %q", h1.Event, "post-write")
	}
	if h1.Async != true {
		t.Error("hook[1].Async should be true")
	}
}

func TestLoadEventHooks_InvalidEvent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `event-hooks:
  - event: pre-create
    command: "echo nope"
`
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read config: %v", err)
	}

	_, err := LoadEventHooks(v)
	if err == nil {
		t.Fatal("expected error for invalid event")
	}
}

func TestLoadEventHooks_MissingCommand(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `event-hooks:
  - event: post-create
`
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read config: %v", err)
	}

	_, err := LoadEventHooks(v)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestIsValidEvent(t *testing.T) {
	valid := []string{"post-create", "post-update", "post-close", "post-comment", "post-write"}
	for _, e := range valid {
		if !isValidEvent(e) {
			t.Errorf("isValidEvent(%q) = false, want true", e)
		}
	}

	invalid := []string{"pre-create", "create", "post-delete", ""}
	for _, e := range invalid {
		if isValidEvent(e) {
			t.Errorf("isValidEvent(%q) = true, want false", e)
		}
	}
}
