package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockGitHubRepo represents a simplified GitHub API repository response
type mockGitHubRepo struct {
	Name        string          `json:"name"`
	FullName    string          `json:"full_name"`
	Fork        bool            `json:"fork"`
	Permissions map[string]bool `json:"permissions,omitempty"`
}

func TestGitHubChecker_ForkDetection(t *testing.T) {
	tests := map[string]struct {
		repo       mockGitHubRepo
		wantIsFork bool
		wantCanPush bool
	}{
		"fork with no push access (contributor)": {
			repo: mockGitHubRepo{
				Name:     "beads",
				FullName: "contributor/beads",
				Fork:     true,
				Permissions: map[string]bool{
					"admin": false,
					"push":  false,
					"pull":  true,
				},
			},
			wantIsFork:  true,
			wantCanPush: false,
		},
		"fork with push access (fork maintainer)": {
			repo: mockGitHubRepo{
				Name:     "beads",
				FullName: "contributor/beads",
				Fork:     true,
				Permissions: map[string]bool{
					"admin": false,
					"push":  true,
					"pull":  true,
				},
			},
			wantIsFork:  true,
			wantCanPush: true,
		},
		"original repo with push access (maintainer)": {
			repo: mockGitHubRepo{
				Name:     "beads",
				FullName: "steveyegge/beads",
				Fork:     false,
				Permissions: map[string]bool{
					"admin": true,
					"push":  true,
					"pull":  true,
				},
			},
			wantIsFork:  false,
			wantCanPush: true,
		},
		"original repo no push access (read-only)": {
			repo: mockGitHubRepo{
				Name:     "beads",
				FullName: "steveyegge/beads",
				Fork:     false,
				Permissions: map[string]bool{
					"admin": false,
					"push":  false,
					"pull":  true,
				},
			},
			wantIsFork:  false,
			wantCanPush: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Create a test server that returns the mock repo
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify the request path
				expectedPath := "/api/v3/repos/owner/repo"
				if r.URL.Path != expectedPath {
					t.Errorf("unexpected path: got %s, want %s", r.URL.Path, expectedPath)
				}

				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(tt.repo); err != nil {
					t.Fatalf("failed to encode response: %v", err)
				}
			}))
			defer server.Close()

			checker := NewGitHubCheckerWithHTTPClient(server.Client(), server.URL)
			result, err := checker.CheckRepo(context.Background(), "owner", "repo")

			if err != nil {
				t.Fatalf("CheckRepo() error = %v", err)
			}

			if result.IsFork != tt.wantIsFork {
				t.Errorf("IsFork = %v, want %v", result.IsFork, tt.wantIsFork)
			}

			if result.CanPush != tt.wantCanPush {
				t.Errorf("CanPush = %v, want %v", result.CanPush, tt.wantCanPush)
			}
		})
	}
}

func TestGitHubChecker_RateLimiting(t *testing.T) {
	tests := map[string]struct {
		statusCode int
		headers    map[string]string
		body       string
		wantError  bool
	}{
		"rate limited (403)": {
			statusCode: http.StatusForbidden,
			headers: map[string]string{
				"X-RateLimit-Remaining": "0",
				"X-RateLimit-Reset":     "1234567890",
			},
			body:      `{"message": "API rate limit exceeded", "documentation_url": "https://docs.github.com/rest/overview/resources-in-the-rest-api#rate-limiting"}`,
			wantError: true,
		},
		"not found (404)": {
			statusCode: http.StatusNotFound,
			body:       `{"message": "Not Found", "documentation_url": "https://docs.github.com/rest"}`,
			wantError:  true,
		},
		"server error (500)": {
			statusCode: http.StatusInternalServerError,
			body:       `{"message": "Internal Server Error"}`,
			wantError:  true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			checker := NewGitHubCheckerWithHTTPClient(server.Client(), server.URL)
			_, err := checker.CheckRepo(context.Background(), "owner", "repo")

			if (err != nil) != tt.wantError {
				t.Errorf("CheckRepo() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestGitHubChecker_NetworkError(t *testing.T) {
	// Create a server and immediately close it to simulate network error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	checker := NewGitHubCheckerWithHTTPClient(server.Client(), server.URL)
	_, err := checker.CheckRepo(context.Background(), "owner", "repo")

	if err == nil {
		t.Error("CheckRepo() expected error for closed server, got nil")
	}
}

func TestGitHubChecker_UnauthenticatedNoPermissions(t *testing.T) {
	// Unauthenticated requests don't include permissions field
	repo := mockGitHubRepo{
		Name:     "beads",
		FullName: "steveyegge/beads",
		Fork:     false,
		// No Permissions field for unauthenticated requests
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(repo); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	checker := NewGitHubCheckerWithHTTPClient(server.Client(), server.URL)
	result, err := checker.CheckRepo(context.Background(), "owner", "repo")

	if err != nil {
		t.Fatalf("CheckRepo() error = %v", err)
	}

	if result.IsFork {
		t.Error("IsFork = true, want false")
	}

	// Without auth, permissions should be false (safe default)
	if result.CanPush {
		t.Error("CanPush = true, want false for unauthenticated request")
	}
}

func TestParseGitHubRemote(t *testing.T) {
	tests := map[string]struct {
		remoteURL string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		"SSH format": {
			remoteURL: "git@github.com:steveyegge/beads.git",
			wantOwner: "steveyegge",
			wantRepo:  "beads",
		},
		"SSH format without .git": {
			remoteURL: "git@github.com:owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		"HTTPS format": {
			remoteURL: "https://github.com/steveyegge/beads.git",
			wantOwner: "steveyegge",
			wantRepo:  "beads",
		},
		"HTTPS format without .git": {
			remoteURL: "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		"HTTP format": {
			remoteURL: "http://github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		"HTTPS with trailing whitespace": {
			remoteURL: "https://github.com/owner/repo.git  \n",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		"not a GitHub URL (GitLab)": {
			remoteURL: "git@gitlab.com:owner/repo.git",
			wantErr:   true,
		},
		"not a GitHub URL (Bitbucket)": {
			remoteURL: "https://bitbucket.org/owner/repo.git",
			wantErr:   true,
		},
		"invalid SSH URL (no repo)": {
			remoteURL: "git@github.com:owner",
			wantErr:   true,
		},
		"invalid HTTPS URL (no repo)": {
			remoteURL: "https://github.com/owner",
			wantErr:   true,
		},
		"empty URL": {
			remoteURL: "",
			wantErr:   true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			owner, repo, err := ParseGitHubRemote(tt.remoteURL)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseGitHubRemote() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				if owner != tt.wantOwner {
					t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
				}
				if repo != tt.wantRepo {
					t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
				}
			}
		})
	}
}

func TestNewGitHubChecker(t *testing.T) {
	tests := map[string]struct {
		token string
	}{
		"with token": {
			token: "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		},
		"without token": {
			token: "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			checker := NewGitHubChecker(tt.token)
			if checker == nil {
				t.Fatal("NewGitHubChecker() returned nil")
			}
			if checker.client == nil {
				t.Error("NewGitHubChecker() has nil client")
			}
		})
	}
}

func TestGitHubChecker_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This should never be reached if context is cancelled
		t.Error("request reached server despite cancelled context")
	}))
	defer server.Close()

	checker := NewGitHubCheckerWithHTTPClient(server.Client(), server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := checker.CheckRepo(ctx, "owner", "repo")

	if err == nil {
		t.Error("CheckRepo() expected error for cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context canceled error, got: %v", err)
	}
}
