package routing

import (
	"errors"
	"testing"
)

func TestDiscoverToken_PriorityOrder(t *testing.T) {
	tests := map[string]struct {
		envVars     map[string]string
		ghCLIOutput string
		ghCLIError  error
		wantToken   string
	}{
		"GITHUB_TOKEN takes priority": {
			envVars: map[string]string{
				"GITHUB_TOKEN": "token-from-github-token",
				"GH_TOKEN":     "token-from-gh-token",
			},
			ghCLIOutput: "token-from-gh-cli",
			wantToken:   "token-from-github-token",
		},
		"GH_TOKEN used when no GITHUB_TOKEN": {
			envVars: map[string]string{
				"GITHUB_TOKEN": "",
				"GH_TOKEN":     "token-from-gh-token",
			},
			ghCLIOutput: "token-from-gh-cli",
			wantToken:   "token-from-gh-token",
		},
		"gh CLI used when no env vars": {
			envVars: map[string]string{
				"GITHUB_TOKEN": "",
				"GH_TOKEN":     "",
			},
			ghCLIOutput: "token-from-gh-cli\n",
			wantToken:   "token-from-gh-cli",
		},
		"graceful degradation when nothing available": {
			envVars: map[string]string{
				"GITHUB_TOKEN": "",
				"GH_TOKEN":     "",
			},
			ghCLIOutput: "",
			ghCLIError:  errors.New("gh not installed"),
			wantToken:   "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockEnv := func(key string) string {
				return tt.envVars[key]
			}

			mockCommand := func(name string, args ...string) ([]byte, error) {
				if name == "gh" && len(args) >= 2 && args[0] == "auth" && args[1] == "token" {
					if tt.ghCLIError != nil {
						return nil, tt.ghCLIError
					}
					return []byte(tt.ghCLIOutput), nil
				}
				// git credential fill is not expected in priority tests
				return nil, errors.New("unexpected command")
			}

			d := NewTokenDiscovererWithMocks(mockEnv, mockCommand)
			got := d.DiscoverToken()

			if got != tt.wantToken {
				t.Errorf("DiscoverToken() = %q, want %q", got, tt.wantToken)
			}
		})
	}
}

func TestDiscoverToken_GHCLISource(t *testing.T) {
	tests := map[string]struct {
		ghOutput  string
		ghError   error
		wantToken string
	}{
		"successful gh auth token": {
			ghOutput:  "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n",
			wantToken: "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		},
		"gh output with trailing whitespace": {
			ghOutput:  "  ghp_token123  \n",
			wantToken: "ghp_token123",
		},
		"gh not installed": {
			ghError:   errors.New("executable file not found"),
			wantToken: "",
		},
		"gh auth failed": {
			ghError:   errors.New("not logged in"),
			wantToken: "",
		},
		"empty gh output": {
			ghOutput:  "",
			wantToken: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockEnv := func(key string) string {
				return "" // No env vars set
			}

			commandCalled := false
			mockCommand := func(name string, args ...string) ([]byte, error) {
				if name == "gh" && len(args) >= 2 && args[0] == "auth" && args[1] == "token" {
					commandCalled = true
					if tt.ghError != nil {
						return nil, tt.ghError
					}
					return []byte(tt.ghOutput), nil
				}
				return nil, errors.New("unexpected command")
			}

			d := NewTokenDiscovererWithMocks(mockEnv, mockCommand)
			got := d.DiscoverToken()

			if !commandCalled && tt.ghError == nil && tt.ghOutput != "" {
				t.Error("expected gh command to be called")
			}

			if got != tt.wantToken {
				t.Errorf("DiscoverToken() = %q, want %q", got, tt.wantToken)
			}
		})
	}
}

func TestDiscoverToken_EnvVarSources(t *testing.T) {
	tests := map[string]struct {
		envKey    string
		envValue  string
		wantToken string
	}{
		"GITHUB_TOKEN set": {
			envKey:    "GITHUB_TOKEN",
			envValue:  "ghp_from_github_token",
			wantToken: "ghp_from_github_token",
		},
		"GH_TOKEN set": {
			envKey:    "GH_TOKEN",
			envValue:  "ghp_from_gh_token",
			wantToken: "ghp_from_gh_token",
		},
		"GITHUB_TOKEN empty string": {
			envKey:    "GITHUB_TOKEN",
			envValue:  "",
			wantToken: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mockEnv := func(key string) string {
				if key == tt.envKey {
					return tt.envValue
				}
				return ""
			}

			mockCommand := func(name string, args ...string) ([]byte, error) {
				return nil, errors.New("should not be called when env var is set")
			}

			d := NewTokenDiscovererWithMocks(mockEnv, mockCommand)
			got := d.DiscoverToken()

			if got != tt.wantToken {
				t.Errorf("DiscoverToken() = %q, want %q", got, tt.wantToken)
			}
		})
	}
}

func TestDiscoverToken_GracefulDegradation(t *testing.T) {
	// Test that no token found returns empty string (not error)
	mockEnv := func(key string) string {
		return ""
	}

	mockCommand := func(name string, args ...string) ([]byte, error) {
		if name == "gh" {
			return nil, errors.New("gh: command not found")
		}
		if name == "git" {
			return nil, errors.New("credential helper failed")
		}
		return nil, errors.New("unknown command")
	}

	d := NewTokenDiscovererWithMocks(mockEnv, mockCommand)
	got := d.DiscoverToken()

	if got != "" {
		t.Errorf("DiscoverToken() = %q, want empty string for graceful degradation", got)
	}
}

func TestNewTokenDiscoverer(t *testing.T) {
	// Test that NewTokenDiscoverer returns a valid instance
	d := NewTokenDiscoverer()
	if d == nil {
		t.Fatal("NewTokenDiscoverer() returned nil")
	}
	if d.getEnv == nil {
		t.Error("NewTokenDiscoverer() has nil getEnv")
	}
	if d.runCommand == nil {
		t.Error("NewTokenDiscoverer() has nil runCommand")
	}
}
