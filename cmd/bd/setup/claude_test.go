package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newClaudeTestEnv(t *testing.T) (claudeEnv, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	homeDir := filepath.Join(root, "home")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	env := claudeEnv{
		stdout:     stdout,
		stderr:     stderr,
		homeDir:    homeDir,
		projectDir: projectDir,
		ensureDir:  EnsureDir,
		readFile:   os.ReadFile,
		writeFile: func(path string, data []byte) error {
			return atomicWriteFile(path, data)
		},
	}
	return env, stdout, stderr
}

func stubClaudeEnvProvider(t *testing.T, env claudeEnv, err error) {
	t.Helper()
	orig := claudeEnvProvider
	claudeEnvProvider = func() (claudeEnv, error) {
		if err != nil {
			return claudeEnv{}, err
		}
		return env, nil
	}
	t.Cleanup(func() { claudeEnvProvider = orig })
}

func writeSettings(t *testing.T, path string, settings map[string]interface{}) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := atomicWriteFile(path, data); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

func TestAddHookCommand(t *testing.T) {
	tests := []struct {
		name          string
		existingHooks map[string]interface{}
		event         string
		command       string
		wantAdded     bool
	}{
		{
			name:          "add hook to empty hooks",
			existingHooks: make(map[string]interface{}),
			event:         "SessionStart",
			command:       "bd prime",
			wantAdded:     true,
		},
		{
			name:          "add stealth hook to empty hooks",
			existingHooks: make(map[string]interface{}),
			event:         "SessionStart",
			command:       "bd prime --stealth",
			wantAdded:     true,
		},
		{
			name: "hook already exists",
			existingHooks: map[string]interface{}{
				"SessionStart": []interface{}{
					map[string]interface{}{
						"matcher": "",
						"hooks": []interface{}{
							map[string]interface{}{
								"type":    "command",
								"command": "bd prime",
							},
						},
					},
				},
			},
			event:     "SessionStart",
			command:   "bd prime",
			wantAdded: false,
		},
		{
			name: "stealth hook already exists",
			existingHooks: map[string]interface{}{
				"SessionStart": []interface{}{
					map[string]interface{}{
						"matcher": "",
						"hooks": []interface{}{
							map[string]interface{}{
								"type":    "command",
								"command": "bd prime --stealth",
							},
						},
					},
				},
			},
			event:     "SessionStart",
			command:   "bd prime --stealth",
			wantAdded: false,
		},
		{
			name: "add second hook alongside existing",
			existingHooks: map[string]interface{}{
				"SessionStart": []interface{}{
					map[string]interface{}{
						"matcher": "",
						"hooks": []interface{}{
							map[string]interface{}{
								"type":    "command",
								"command": "other command",
							},
						},
					},
				},
			},
			event:     "SessionStart",
			command:   "bd prime",
			wantAdded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := addHookCommand(tt.existingHooks, tt.event, tt.command)
			if got != tt.wantAdded {
				t.Errorf("addHookCommand() = %v, want %v", got, tt.wantAdded)
			}

			// Verify hook exists in structure
			eventHooks, ok := tt.existingHooks[tt.event].([]interface{})
			if !ok {
				t.Fatal("Event hooks not found")
			}

			found := false
			for _, hook := range eventHooks {
				hookMap := hook.(map[string]interface{})
				commands := hookMap["hooks"].([]interface{})
				for _, cmd := range commands {
					cmdMap := cmd.(map[string]interface{})
					if cmdMap["command"] == tt.command {
						found = true
						break
					}
				}
			}

			if !found {
				t.Errorf("Hook command %q not found in event %q", tt.command, tt.event)
			}
		})
	}
}

func TestRemoveHookCommand(t *testing.T) {
	tests := []struct {
		name          string
		existingHooks map[string]interface{}
		event         string
		command       string
		wantRemaining int
	}{
		{
			name: "remove only hook",
			existingHooks: map[string]interface{}{
				"SessionStart": []interface{}{
					map[string]interface{}{
						"matcher": "",
						"hooks": []interface{}{
							map[string]interface{}{
								"type":    "command",
								"command": "bd prime",
							},
						},
					},
				},
			},
			event:         "SessionStart",
			command:       "bd prime",
			wantRemaining: 0,
		},
		{
			name: "remove stealth hook",
			existingHooks: map[string]interface{}{
				"SessionStart": []interface{}{
					map[string]interface{}{
						"matcher": "",
						"hooks": []interface{}{
							map[string]interface{}{
								"type":    "command",
								"command": "bd prime --stealth",
							},
						},
					},
				},
			},
			event:         "SessionStart",
			command:       "bd prime --stealth",
			wantRemaining: 0,
		},
		{
			name: "remove one of multiple hooks",
			existingHooks: map[string]interface{}{
				"SessionStart": []interface{}{
					map[string]interface{}{
						"matcher": "",
						"hooks": []interface{}{
							map[string]interface{}{
								"type":    "command",
								"command": "other command",
							},
						},
					},
					map[string]interface{}{
						"matcher": "",
						"hooks": []interface{}{
							map[string]interface{}{
								"type":    "command",
								"command": "bd prime",
							},
						},
					},
				},
			},
			event:         "SessionStart",
			command:       "bd prime",
			wantRemaining: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removeHookCommand(tt.existingHooks, tt.event, tt.command)

			eventHooks, ok := tt.existingHooks[tt.event].([]interface{})
			if !ok && tt.wantRemaining > 0 {
				t.Fatal("Event hooks not found")
			}

			if len(eventHooks) != tt.wantRemaining {
				t.Errorf("Expected %d remaining hooks, got %d", tt.wantRemaining, len(eventHooks))
			}

			// Verify target hook is actually gone
			for _, hook := range eventHooks {
				hookMap := hook.(map[string]interface{})
				commands := hookMap["hooks"].([]interface{})
				for _, cmd := range commands {
					cmdMap := cmd.(map[string]interface{})
					if cmdMap["command"] == tt.command {
						t.Errorf("Hook command %q still present after removal", tt.command)
					}
				}
			}
		})
	}
}

// TestRemoveHookCommandNoNull verifies that removing all hooks deletes the key
// instead of setting it to null. GH#955: null values in hooks cause Claude Code to fail.
func TestRemoveHookCommandNoNull(t *testing.T) {
	hooks := map[string]interface{}{
		"SessionStart": []interface{}{
			map[string]interface{}{
				"matcher": "",
				"hooks": []interface{}{
					map[string]interface{}{
						"type":    "command",
						"command": "bd prime",
					},
				},
			},
		},
	}

	removeHookCommand(hooks, "SessionStart", "bd prime")

	// Key should be deleted, not set to null or empty array
	if _, exists := hooks["SessionStart"]; exists {
		t.Error("Expected SessionStart key to be deleted after removing all hooks")
	}

	// Verify JSON serialization doesn't produce null
	data, err := json.Marshal(hooks)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "null") {
		t.Errorf("JSON contains null: %s", data)
	}
}

// TestInstallClaudeCleanupNullHooks verifies that install cleans up existing null values.
// GH#955: null values left by previous buggy removal cause Claude Code to fail.
func TestInstallClaudeCleanupNullHooks(t *testing.T) {
	env, stdout, _ := newClaudeTestEnv(t)

	// Create settings file with null hooks (simulating the bug)
	settingsPath := globalSettingsPath(env.homeDir)
	writeSettings(t, settingsPath, map[string]interface{}{
		"hooks": map[string]interface{}{
			"SessionStart": nil,
			"PreCompact":   nil,
		},
	})

	// Install should clean up null values and add proper hooks
	err := installClaude(env, false, false)
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Verify hooks were properly added
	if !strings.Contains(stdout.String(), "Registered SessionStart hook") {
		t.Error("Expected SessionStart hook to be registered")
	}

	// Read back the file and verify no null values
	data, err := env.readFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if strings.Contains(string(data), "null") {
		t.Errorf("Settings file still contains null: %s", data)
	}

	// Verify it parses as valid Claude settings
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("hooks section missing")
	}
	for _, event := range []string{"SessionStart", "PreCompact"} {
		eventHooks, ok := hooks[event].([]interface{})
		if !ok {
			t.Errorf("%s should be an array, not nil or missing", event)
		}
		if len(eventHooks) == 0 {
			t.Errorf("%s should have hooks", event)
		}
	}
}

func TestHasBeadsHooks(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name         string
		settingsData map[string]interface{}
		want         bool
	}{
		{
			name: "has bd prime hook",
			settingsData: map[string]interface{}{
				"hooks": map[string]interface{}{
					"SessionStart": []interface{}{
						map[string]interface{}{
							"matcher": "",
							"hooks": []interface{}{
								map[string]interface{}{
									"type":    "command",
									"command": "bd prime",
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "has bd prime --stealth hook",
			settingsData: map[string]interface{}{
				"hooks": map[string]interface{}{
					"SessionStart": []interface{}{
						map[string]interface{}{
							"matcher": "",
							"hooks": []interface{}{
								map[string]interface{}{
									"type":    "command",
									"command": "bd prime --stealth",
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "has bd prime in PreCompact",
			settingsData: map[string]interface{}{
				"hooks": map[string]interface{}{
					"PreCompact": []interface{}{
						map[string]interface{}{
							"matcher": "",
							"hooks": []interface{}{
								map[string]interface{}{
									"type":    "command",
									"command": "bd prime",
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "has bd prime --stealth in PreCompact",
			settingsData: map[string]interface{}{
				"hooks": map[string]interface{}{
					"PreCompact": []interface{}{
						map[string]interface{}{
							"matcher": "",
							"hooks": []interface{}{
								map[string]interface{}{
									"type":    "command",
									"command": "bd prime --stealth",
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name:         "no hooks",
			settingsData: map[string]interface{}{},
			want:         false,
		},
		{
			name: "has other hooks but not bd prime",
			settingsData: map[string]interface{}{
				"hooks": map[string]interface{}{
					"SessionStart": []interface{}{
						map[string]interface{}{
							"matcher": "",
							"hooks": []interface{}{
								map[string]interface{}{
									"type":    "command",
									"command": "other command",
								},
							},
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settingsPath := filepath.Join(tmpDir, "settings.json")

			data, err := json.Marshal(tt.settingsData)
			if err != nil {
				t.Fatalf("Failed to marshal test data: %v", err)
			}

			if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			got := hasBeadsHooks(settingsPath)
			if got != tt.want {
				t.Errorf("hasBeadsHooks() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdempotency(t *testing.T) {
	// Test that running addHookCommand twice doesn't duplicate hooks
	hooks := make(map[string]interface{})

	// First add
	added1 := addHookCommand(hooks, "SessionStart", "bd prime")
	if !added1 {
		t.Error("First call should have added the hook")
	}

	// Second add (should detect existing)
	added2 := addHookCommand(hooks, "SessionStart", "bd prime")
	if added2 {
		t.Error("Second call should have detected existing hook")
	}

	// Verify only one hook exists
	eventHooks := hooks["SessionStart"].([]interface{})
	if len(eventHooks) != 1 {
		t.Errorf("Expected 1 hook, got %d", len(eventHooks))
	}
}

// Test that running addHookCommand twice with stealth doesn't duplicate hooks
func TestIdempotencyWithStealth(t *testing.T) {
	hooks := make(map[string]any)

	if !addHookCommand(hooks, "SessionStart", "bd prime --stealth") {
		t.Error("First call should have added the stealth hook")
	}

	// Second add (should detect existing)
	if addHookCommand(hooks, "SessionStart", "bd prime --stealth") {
		t.Error("Second call should have detected existing stealth hook")
	}

	// Verify only one hook exists
	eventHooks := hooks["SessionStart"].([]any)
	if len(eventHooks) != 1 {
		t.Errorf("Expected 1 hook, got %d", len(eventHooks))
	}

	// and that it's the correct one
	hookMap := eventHooks[0].(map[string]any)
	commands := hookMap["hooks"].([]any)
	cmdMap := commands[0].(map[string]any)
	if cmdMap["command"] != "bd prime --stealth" {
		t.Errorf("Expected 'bd prime --stealth', got %v", cmdMap["command"])
	}
}

func TestInstallClaudeProject(t *testing.T) {
	env, stdout, stderr := newClaudeTestEnv(t)
	if err := installClaude(env, true, false); err != nil {
		t.Fatalf("installClaude: %v", err)
	}
	data, err := os.ReadFile(projectSettingsPath(env.projectDir))
	if err != nil {
		t.Fatalf("read project settings: %v", err)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if !hasBeadsHooks(projectSettingsPath(env.projectDir)) {
		t.Fatal("project hooks not detected")
	}
	if !strings.Contains(stdout.String(), "project") {
		t.Error("expected project installation message")
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr output: %s", stderr.String())
	}
}

func TestInstallClaudeGlobalStealth(t *testing.T) {
	env, stdout, _ := newClaudeTestEnv(t)
	if err := installClaude(env, false, true); err != nil {
		t.Fatalf("installClaude: %v", err)
	}
	data, err := os.ReadFile(globalSettingsPath(env.homeDir))
	if err != nil {
		t.Fatalf("read global settings: %v", err)
	}
	// With event bus rollout, stealth mode is handled by handler configuration,
	// not the hook command. Both stealth and non-stealth use bus emit.
	if !strings.Contains(string(data), "bd bus emit --hook=SessionStart") {
		t.Error("expected bus emit SessionStart command in settings")
	}
	if !strings.Contains(stdout.String(), "globally") {
		t.Error("expected global installation message")
	}
}

func TestInstallClaudeErrors(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		env, _, stderr := newClaudeTestEnv(t)
		path := projectSettingsPath(env.projectDir)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if err := installClaude(env, true, false); err == nil {
			t.Fatal("expected parse error")
		}
		if !strings.Contains(stderr.String(), "failed to parse") {
			t.Error("expected parse error output")
		}
	})

	t.Run("ensure dir error", func(t *testing.T) {
		env, _, _ := newClaudeTestEnv(t)
		env.ensureDir = func(string, os.FileMode) error { return errors.New("boom") }
		if err := installClaude(env, true, false); err == nil {
			t.Fatal("expected ensureDir error")
		}
	})
}

func TestCheckClaudeScenarios(t *testing.T) {
	t.Run("global hooks", func(t *testing.T) {
		env, stdout, _ := newClaudeTestEnv(t)
		writeSettings(t, globalSettingsPath(env.homeDir), map[string]interface{}{
			"hooks": map[string]interface{}{
				"SessionStart": []interface{}{
					map[string]interface{}{
						"matcher": "",
						"hooks": []interface{}{
							map[string]interface{}{"type": "command", "command": "bd prime"},
						},
					},
				},
			},
		})
		if err := checkClaude(env); err != nil {
			t.Fatalf("checkClaude: %v", err)
		}
		if !strings.Contains(stdout.String(), "Global hooks installed") {
			t.Error("expected global hooks message")
		}
	})

	t.Run("project hooks", func(t *testing.T) {
		env, stdout, _ := newClaudeTestEnv(t)
		writeSettings(t, projectSettingsPath(env.projectDir), map[string]interface{}{
			"hooks": map[string]interface{}{
				"PreCompact": []interface{}{
					map[string]interface{}{
						"matcher": "",
						"hooks": []interface{}{
							map[string]interface{}{"type": "command", "command": "bd prime"},
						},
					},
				},
			},
		})
		if err := checkClaude(env); err != nil {
			t.Fatalf("checkClaude: %v", err)
		}
		if !strings.Contains(stdout.String(), "Project hooks installed") {
			t.Error("expected project hooks message")
		}
	})

	t.Run("missing hooks", func(t *testing.T) {
		env, stdout, _ := newClaudeTestEnv(t)
		if err := checkClaude(env); !errors.Is(err, errClaudeHooksMissing) {
			t.Fatalf("expected errClaudeHooksMissing, got %v", err)
		}
		if !strings.Contains(stdout.String(), "Run: bd setup claude") {
			t.Error("expected guidance message")
		}
	})
}

func TestRemoveClaudeScenarios(t *testing.T) {
	t.Run("remove global hooks", func(t *testing.T) {
		env, stdout, _ := newClaudeTestEnv(t)
		path := globalSettingsPath(env.homeDir)
		writeSettings(t, path, map[string]interface{}{
			"hooks": map[string]interface{}{
				"SessionStart": []interface{}{
					map[string]interface{}{
						"matcher": "",
						"hooks": []interface{}{
							map[string]interface{}{"type": "command", "command": "bd prime"},
							map[string]interface{}{"type": "command", "command": "other"},
						},
					},
				},
			},
		})
		if err := removeClaude(env, false); err != nil {
			t.Fatalf("removeClaude: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if strings.Contains(string(data), "bd prime") {
			t.Error("expected bd prime hooks removed")
		}
		if !strings.Contains(stdout.String(), "hooks removed") {
			t.Error("expected success message")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		env, stdout, _ := newClaudeTestEnv(t)
		if err := removeClaude(env, true); err != nil {
			t.Fatalf("removeClaude: %v", err)
		}
		if !strings.Contains(stdout.String(), "No settings file found") {
			t.Error("expected missing file message")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		env, _, stderr := newClaudeTestEnv(t)
		path := projectSettingsPath(env.projectDir)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if err := removeClaude(env, true); err == nil {
			t.Fatal("expected parse error")
		}
		if !strings.Contains(stderr.String(), "failed to parse") {
			t.Error("expected parse error output")
		}
	})
}

func TestClaudeWrappersExit(t *testing.T) {
	t.Run("install provider error", func(t *testing.T) {
		cap := stubSetupExit(t)
		stubClaudeEnvProvider(t, claudeEnv{}, errors.New("boom"))
		InstallClaude(false, false)
		if !cap.called || cap.code != 1 {
			t.Fatal("InstallClaude should exit on provider error")
		}
	})

	t.Run("install internal error", func(t *testing.T) {
		cap := stubSetupExit(t)
		env, _, _ := newClaudeTestEnv(t)
		env.ensureDir = func(string, os.FileMode) error { return errors.New("boom") }
		stubClaudeEnvProvider(t, env, nil)
		InstallClaude(true, false)
		if !cap.called || cap.code != 1 {
			t.Fatal("InstallClaude should exit when installClaude fails")
		}
	})

	t.Run("check missing hooks", func(t *testing.T) {
		cap := stubSetupExit(t)
		env, _, _ := newClaudeTestEnv(t)
		stubClaudeEnvProvider(t, env, nil)
		CheckClaude()
		if !cap.called || cap.code != 1 {
			t.Fatal("CheckClaude should exit when hooks missing")
		}
	})

	t.Run("remove parse error", func(t *testing.T) {
		cap := stubSetupExit(t)
		env, _, _ := newClaudeTestEnv(t)
		path := globalSettingsPath(env.homeDir)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("oops"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		stubClaudeEnvProvider(t, env, nil)
		RemoveClaude(false)
		if !cap.called || cap.code != 1 {
			t.Fatal("RemoveClaude should exit on parse error")
		}
	})
}

// ---------------------------------------------------------------------------
// Legacy hook migration to bus emit tests
// ---------------------------------------------------------------------------

// makeHookEntry builds the Claude Code hook JSON structure for a single command
// on a given event.
func makeHookEntry(command string) map[string]interface{} {
	return map[string]interface{}{
		"matcher": "",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": command,
			},
		},
	}
}

// settingsWithHooks is a small helper that returns a settings map
// with the given hooks section.
func settingsWithHooks(hooks map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"hooks": hooks,
	}
}

// readSettingsFile reads and parses a settings JSON file.
func readSettingsFile(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings file: %v", err)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	return settings
}

// assertNoLegacyHooks verifies that no legacy hook commands exist in the
// settings file at the given path.
func assertNoLegacyHooks(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	raw := string(data)
	for _, lh := range legacyHookCommands() {
		if strings.Contains(raw, lh.command) {
			t.Errorf("legacy hook command still present in settings: %q (event %s)", lh.command, lh.event)
		}
	}
}

// assertBusEmitHooksPresent verifies that all bus emit hook commands exist
// in the hooks map.
func assertBusEmitHooksPresent(t *testing.T, hooks map[string]interface{}) {
	t.Helper()
	for _, bh := range busEmitHookCommands() {
		eventHooks, ok := hooks[bh.event].([]interface{})
		if !ok {
			t.Errorf("bus emit hook event %q missing from hooks", bh.event)
			continue
		}
		found := false
		for _, hook := range eventHooks {
			hookMap, ok := hook.(map[string]interface{})
			if !ok {
				continue
			}
			commands, ok := hookMap["hooks"].([]interface{})
			if !ok {
				continue
			}
			for _, cmd := range commands {
				cmdMap, ok := cmd.(map[string]interface{})
				if !ok {
					continue
				}
				if cmdMap["command"] == bh.command {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("bus emit hook %q not found in event %q", bh.command, bh.event)
		}
	}
}

// TestInstallClaudeMigratesLegacyHooks verifies that installClaude removes all
// legacy hook variants and replaces them with bus emit hooks.
func TestInstallClaudeMigratesLegacyHooks(t *testing.T) {
	env, stdout, _ := newClaudeTestEnv(t)
	settingsPath := globalSettingsPath(env.homeDir)

	// Build hooks map with ALL legacy hook variants.
	hooks := make(map[string]interface{})
	for _, lh := range legacyHookCommands() {
		eventHooks, _ := hooks[lh.event].([]interface{})
		eventHooks = append(eventHooks, makeHookEntry(lh.command))
		hooks[lh.event] = eventHooks
	}

	writeSettings(t, settingsPath, settingsWithHooks(hooks))

	// Run install - should migrate.
	if err := installClaude(env, false, false); err != nil {
		t.Fatalf("installClaude: %v", err)
	}

	// 1. All legacy hooks must be removed from the file.
	assertNoLegacyHooks(t, settingsPath)

	// 2. Bus emit hooks must be present.
	settings := readSettingsFile(t, settingsPath)
	hooksResult, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("hooks section missing after install")
	}
	assertBusEmitHooksPresent(t, hooksResult)

	// 3. Output mentions "Registered" for each bus emit hook.
	out := stdout.String()
	for _, bh := range busEmitHookCommands() {
		expected := fmt.Sprintf("Registered %s hook", bh.event)
		if !strings.Contains(out, expected) {
			t.Errorf("expected output to contain %q, got:\n%s", expected, out)
		}
	}

	// 4. Double-check: raw file has no legacy commands at all.
	data, _ := os.ReadFile(settingsPath)
	for _, lh := range legacyHookCommands() {
		if strings.Contains(string(data), lh.command) {
			t.Errorf("raw settings still contains legacy command %q", lh.command)
		}
	}
}

// TestInstallClaudeMigratesPartialLegacy verifies that a partial set of legacy
// hooks is correctly migrated (some events have legacy hooks, others do not).
func TestInstallClaudeMigratesPartialLegacy(t *testing.T) {
	env, stdout, _ := newClaudeTestEnv(t)
	settingsPath := globalSettingsPath(env.homeDir)

	// Only install a subset of legacy hooks: SessionStart and Stop.
	hooks := map[string]interface{}{
		"SessionStart": []interface{}{
			makeHookEntry("bd prime"),
		},
		"Stop": []interface{}{
			makeHookEntry("bd gate session-check --hook Stop --json"),
		},
	}

	writeSettings(t, settingsPath, settingsWithHooks(hooks))

	if err := installClaude(env, false, false); err != nil {
		t.Fatalf("installClaude: %v", err)
	}

	// Legacy hooks should be removed.
	assertNoLegacyHooks(t, settingsPath)

	// All four bus emit hooks should be present.
	settings := readSettingsFile(t, settingsPath)
	hooksResult, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("hooks section missing")
	}
	assertBusEmitHooksPresent(t, hooksResult)

	// Output should mention "Registered" for all four events since none existed before.
	out := stdout.String()
	for _, bh := range busEmitHookCommands() {
		expected := fmt.Sprintf("Registered %s hook", bh.event)
		if !strings.Contains(out, expected) {
			t.Errorf("expected output to contain %q, got:\n%s", expected, out)
		}
	}
}

// TestHasBeadsHooksDetectsBusEmit verifies that hasBeadsHooks detects bus emit
// hooks (not only legacy ones). It only checks SessionStart and PreCompact.
func TestHasBeadsHooksDetectsBusEmit(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		settings map[string]interface{}
		want     bool
	}{
		{
			name: "bus emit SessionStart",
			settings: settingsWithHooks(map[string]interface{}{
				"SessionStart": []interface{}{
					makeHookEntry("bd bus emit --hook=SessionStart"),
				},
			}),
			want: true,
		},
		{
			name: "bus emit PreCompact",
			settings: settingsWithHooks(map[string]interface{}{
				"PreCompact": []interface{}{
					makeHookEntry("bd bus emit --hook=PreCompact"),
				},
			}),
			want: true,
		},
		{
			name: "bus emit Stop and PreToolUse only - not checked events",
			settings: settingsWithHooks(map[string]interface{}{
				"Stop": []interface{}{
					makeHookEntry("bd bus emit --hook=Stop"),
				},
				"PreToolUse": []interface{}{
					makeHookEntry("bd bus emit --hook=PreToolUse"),
				},
			}),
			want: false,
		},
		{
			name:     "empty hooks",
			settings: settingsWithHooks(map[string]interface{}{}),
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, tt.name+".json")
			data, err := json.Marshal(tt.settings)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			got := hasBeadsHooks(path)
			if got != tt.want {
				t.Errorf("hasBeadsHooks() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRemoveClaudeRemovesBusEmitHooks installs bus emit hooks, then verifies
// that removeClaude removes them all.
func TestRemoveClaudeRemovesBusEmitHooks(t *testing.T) {
	env, _, _ := newClaudeTestEnv(t)
	settingsPath := globalSettingsPath(env.homeDir)

	// Install hooks first.
	if err := installClaude(env, false, false); err != nil {
		t.Fatalf("installClaude: %v", err)
	}

	// Verify bus emit hooks are present before removal.
	settings := readSettingsFile(t, settingsPath)
	hooksBeforeRemove, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("hooks missing after install")
	}
	assertBusEmitHooksPresent(t, hooksBeforeRemove)

	// Reset stdout/stderr buffers for removeClaude.
	env2, stdout2, _ := newClaudeTestEnv(t)
	env2.homeDir = env.homeDir
	env2.projectDir = env.projectDir

	// Remove hooks.
	if err := removeClaude(env2, false); err != nil {
		t.Fatalf("removeClaude: %v", err)
	}

	// Verify all bus emit hooks are gone.
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	raw := string(data)
	for _, bh := range busEmitHookCommands() {
		if strings.Contains(raw, bh.command) {
			t.Errorf("bus emit hook %q still present after removeClaude", bh.command)
		}
	}

	// Output should mention removal.
	if !strings.Contains(stdout2.String(), "hooks removed") {
		t.Error("expected 'hooks removed' in output")
	}
}

// TestInstallClaudeIdempotentBusEmit verifies that running installClaude twice
// does not duplicate bus emit hooks.
func TestInstallClaudeIdempotentBusEmit(t *testing.T) {
	env, _, _ := newClaudeTestEnv(t)
	settingsPath := globalSettingsPath(env.homeDir)

	// First install.
	if err := installClaude(env, false, false); err != nil {
		t.Fatalf("first installClaude: %v", err)
	}

	// Second install (use fresh buffers to avoid mixing output).
	env2, stdout2, _ := newClaudeTestEnv(t)
	env2.homeDir = env.homeDir
	env2.projectDir = env.projectDir

	if err := installClaude(env2, false, false); err != nil {
		t.Fatalf("second installClaude: %v", err)
	}

	// The second call should NOT print "Registered" since hooks already exist.
	out := stdout2.String()
	for _, bh := range busEmitHookCommands() {
		expected := fmt.Sprintf("Registered %s hook", bh.event)
		if strings.Contains(out, expected) {
			t.Errorf("second install should not re-register %s hook", bh.event)
		}
	}

	// Verify no duplicate hooks in the settings file.
	settings := readSettingsFile(t, settingsPath)
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("hooks section missing")
	}

	for _, bh := range busEmitHookCommands() {
		eventHooks, ok := hooks[bh.event].([]interface{})
		if !ok {
			t.Errorf("event %q missing", bh.event)
			continue
		}
		count := 0
		for _, hook := range eventHooks {
			hookMap, ok := hook.(map[string]interface{})
			if !ok {
				continue
			}
			commands, ok := hookMap["hooks"].([]interface{})
			if !ok {
				continue
			}
			for _, cmd := range commands {
				cmdMap, ok := cmd.(map[string]interface{})
				if !ok {
					continue
				}
				if cmdMap["command"] == bh.command {
					count++
				}
			}
		}
		if count != 1 {
			t.Errorf("expected exactly 1 instance of %q in event %q, got %d", bh.command, bh.event, count)
		}
	}
}

// TestInstallClaudePreservesThirdPartyHooks verifies that third-party hooks
// are preserved through install and remove cycles.
func TestInstallClaudePreservesThirdPartyHooks(t *testing.T) {
	env, _, _ := newClaudeTestEnv(t)
	settingsPath := globalSettingsPath(env.homeDir)

	// Pre-populate with third-party hooks on events that beads also uses.
	thirdPartyCmd := "custom-linter --check"
	hooks := map[string]interface{}{
		"SessionStart": []interface{}{
			makeHookEntry(thirdPartyCmd),
		},
		"PreToolUse": []interface{}{
			makeHookEntry("security-scanner run"),
		},
	}
	writeSettings(t, settingsPath, settingsWithHooks(hooks))

	// Install beads hooks.
	if err := installClaude(env, false, false); err != nil {
		t.Fatalf("installClaude: %v", err)
	}

	// Third-party hooks should still be present.
	settings := readSettingsFile(t, settingsPath)
	hooksResult := settings["hooks"].(map[string]interface{})

	assertCommandPresent := func(event, command string) {
		t.Helper()
		eventHooks, ok := hooksResult[event].([]interface{})
		if !ok {
			t.Fatalf("event %q missing", event)
		}
		for _, hook := range eventHooks {
			hookMap, ok := hook.(map[string]interface{})
			if !ok {
				continue
			}
			commands, ok := hookMap["hooks"].([]interface{})
			if !ok {
				continue
			}
			for _, cmd := range commands {
				cmdMap, ok := cmd.(map[string]interface{})
				if !ok {
					continue
				}
				if cmdMap["command"] == command {
					return
				}
			}
		}
		t.Errorf("third-party command %q not found in event %q after install", command, event)
	}

	assertCommandPresent("SessionStart", thirdPartyCmd)
	assertCommandPresent("PreToolUse", "security-scanner run")

	// Bus emit hooks should also be present.
	assertBusEmitHooksPresent(t, hooksResult)

	// Now remove beads hooks.
	env2, _, _ := newClaudeTestEnv(t)
	env2.homeDir = env.homeDir
	env2.projectDir = env.projectDir

	if err := removeClaude(env2, false); err != nil {
		t.Fatalf("removeClaude: %v", err)
	}

	// Third-party hooks should survive removal.
	settings2 := readSettingsFile(t, settingsPath)
	hooksAfterRemove := settings2["hooks"].(map[string]interface{})

	// Check third-party hooks are still there.
	for _, tc := range []struct {
		event, command string
	}{
		{"SessionStart", thirdPartyCmd},
		{"PreToolUse", "security-scanner run"},
	} {
		eventHooks, ok := hooksAfterRemove[tc.event].([]interface{})
		if !ok {
			t.Fatalf("event %q missing after remove", tc.event)
		}
		found := false
		for _, hook := range eventHooks {
			hookMap, ok := hook.(map[string]interface{})
			if !ok {
				continue
			}
			commands, ok := hookMap["hooks"].([]interface{})
			if !ok {
				continue
			}
			for _, cmd := range commands {
				cmdMap, ok := cmd.(map[string]interface{})
				if !ok {
					continue
				}
				if cmdMap["command"] == tc.command {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("third-party command %q not found in event %q after remove", tc.command, tc.event)
		}
	}

	// Bus emit hooks should be gone.
	data, _ := os.ReadFile(settingsPath)
	raw := string(data)
	for _, bh := range busEmitHookCommands() {
		if strings.Contains(raw, bh.command) {
			t.Errorf("bus emit hook %q still present after remove", bh.command)
		}
	}
}

// TestBusEmitHookCommandsComplete verifies that busEmitHookCommands returns
// entries for all 4 expected events.
func TestBusEmitHookCommandsComplete(t *testing.T) {
	cmds := busEmitHookCommands()

	expectedEvents := map[string]string{
		"SessionStart": "bd bus emit --hook=SessionStart",
		"PreCompact":   "bd bus emit --hook=PreCompact",
		"Stop":         "bd bus emit --hook=Stop",
		"PreToolUse":   "bd bus emit --hook=PreToolUse",
	}

	if len(cmds) != len(expectedEvents) {
		t.Fatalf("busEmitHookCommands() returned %d entries, want %d", len(cmds), len(expectedEvents))
	}

	found := make(map[string]bool)
	for _, cmd := range cmds {
		wantCmd, ok := expectedEvents[cmd.event]
		if !ok {
			t.Errorf("unexpected event %q in busEmitHookCommands()", cmd.event)
			continue
		}
		if cmd.command != wantCmd {
			t.Errorf("event %q: command = %q, want %q", cmd.event, cmd.command, wantCmd)
		}
		if found[cmd.event] {
			t.Errorf("duplicate event %q in busEmitHookCommands()", cmd.event)
		}
		found[cmd.event] = true
	}

	for event := range expectedEvents {
		if !found[event] {
			t.Errorf("missing event %q in busEmitHookCommands()", event)
		}
	}
}
