package routing

import (
	"errors"
	"testing"
)

// TestHasUpstreamRemote tests the HasUpstreamRemote function with mocked git commands.
func TestHasUpstreamRemote(t *testing.T) {
	tests := map[string]struct {
		repoPath       string
		gitResponses   []gitResponse
		expectedResult bool
		description    string
	}{
		"upstream_exists": {
			repoPath: "/repo",
			gitResponses: []gitResponse{
				{
					expect: gitCall{"/repo", []string{"remote", "get-url", "upstream"}},
					output: "https://github.com/owner/repo.git\n",
					err:    nil,
				},
			},
			expectedResult: true,
			description:    "returns true when upstream remote exists",
		},
		"upstream_not_exists": {
			repoPath: "/repo",
			gitResponses: []gitResponse{
				{
					expect: gitCall{"/repo", []string{"remote", "get-url", "upstream"}},
					output: "",
					err:    errors.New("fatal: No such remote 'upstream'"),
				},
			},
			expectedResult: false,
			description:    "returns false when upstream remote does not exist",
		},
		"empty_repo_path": {
			repoPath: "",
			gitResponses: []gitResponse{
				{
					expect: gitCall{"", []string{"remote", "get-url", "upstream"}},
					output: "git@github.com:owner/repo.git\n",
					err:    nil,
				},
			},
			expectedResult: true,
			description:    "works with empty repo path (current directory)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			orig := gitCommandRunner
			stub := &gitStub{t: t, responses: tc.gitResponses}
			gitCommandRunner = stub.run
			t.Cleanup(func() {
				gitCommandRunner = orig
				stub.verify()
			})

			got := HasUpstreamRemote(tc.repoPath)
			if got != tc.expectedResult {
				t.Errorf("%s: HasUpstreamRemote() = %v, want %v", tc.description, got, tc.expectedResult)
			}
		})
	}
}

// TestDetectionPriority verifies the detection order: config > upstream > heuristic.
func TestDetectionPriority(t *testing.T) {
	tests := map[string]struct {
		repoPath     string
		gitResponses []gitResponse
		expectedRole UserRole
		description  string
	}{
		"config_maintainer_overrides_upstream": {
			repoPath: "/repo",
			gitResponses: []gitResponse{
				// Config check returns maintainer
				{
					expect: gitCall{"/repo", []string{"config", "--get", "beads.role"}},
					output: "maintainer\n",
					err:    nil,
				},
				// Should NOT reach upstream check (config takes precedence)
			},
			expectedRole: Maintainer,
			description:  "explicit config=maintainer should override upstream detection",
		},
		"config_contributor_overrides_ssh": {
			repoPath: "/repo",
			gitResponses: []gitResponse{
				// Config check returns contributor
				{
					expect: gitCall{"/repo", []string{"config", "--get", "beads.role"}},
					output: "contributor\n",
					err:    nil,
				},
				// Should NOT check SSH URL (config takes precedence)
			},
			expectedRole: Contributor,
			description:  "explicit config=contributor should override SSH URL detection",
		},
		"upstream_overrides_ssh": {
			repoPath: "/repo",
			gitResponses: []gitResponse{
				// No config set
				{
					expect: gitCall{"/repo", []string{"config", "--get", "beads.role"}},
					output: "",
					err:    errors.New("config not found"),
				},
				// Upstream exists
				{
					expect: gitCall{"/repo", []string{"remote", "get-url", "upstream"}},
					output: "https://github.com/owner/repo.git\n",
					err:    nil,
				},
				// Should NOT reach SSH URL check (upstream takes precedence)
			},
			expectedRole: Contributor,
			description:  "upstream remote should make user contributor even with SSH origin",
		},
		"ssh_fallback_when_no_upstream": {
			repoPath: "/repo",
			gitResponses: []gitResponse{
				// No config set
				{
					expect: gitCall{"/repo", []string{"config", "--get", "beads.role"}},
					output: "",
					err:    errors.New("config not found"),
				},
				// No upstream
				{
					expect: gitCall{"/repo", []string{"remote", "get-url", "upstream"}},
					output: "",
					err:    errors.New("fatal: No such remote 'upstream'"),
				},
				// SSH URL detected - maintainer
				{
					expect: gitCall{"/repo", []string{"remote", "get-url", "--push", "origin"}},
					output: "git@github.com:owner/repo.git\n",
					err:    nil,
				},
			},
			expectedRole: Maintainer,
			description:  "SSH origin without upstream should return maintainer",
		},
		"https_fallback_when_no_upstream": {
			repoPath: "/repo",
			gitResponses: []gitResponse{
				// No config set
				{
					expect: gitCall{"/repo", []string{"config", "--get", "beads.role"}},
					output: "",
					err:    errors.New("config not found"),
				},
				// No upstream
				{
					expect: gitCall{"/repo", []string{"remote", "get-url", "upstream"}},
					output: "",
					err:    errors.New("fatal: No such remote 'upstream'"),
				},
				// HTTPS URL without credentials - contributor
				{
					expect: gitCall{"/repo", []string{"remote", "get-url", "--push", "origin"}},
					output: "https://github.com/owner/repo.git\n",
					err:    nil,
				},
			},
			expectedRole: Contributor,
			description:  "HTTPS origin without upstream should return contributor",
		},
		"fork_with_ssh_origin": {
			repoPath: "/repo",
			gitResponses: []gitResponse{
				// No config set
				{
					expect: gitCall{"/repo", []string{"config", "--get", "beads.role"}},
					output: "",
					err:    errors.New("config not found"),
				},
				// Upstream exists (fork pattern)
				{
					expect: gitCall{"/repo", []string{"remote", "get-url", "upstream"}},
					output: "https://github.com/owner/repo.git\n",
					err:    nil,
				},
				// Note: SSH origin would normally indicate maintainer, but upstream takes precedence
			},
			expectedRole: Contributor,
			description:  "fork with SSH origin should be detected as contributor via upstream",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			orig := gitCommandRunner
			stub := &gitStub{t: t, responses: tc.gitResponses}
			gitCommandRunner = stub.run
			t.Cleanup(func() {
				gitCommandRunner = orig
				stub.verify()
			})

			role, err := DetectUserRole(tc.repoPath)
			if err != nil {
				t.Fatalf("%s: DetectUserRole() error = %v", tc.description, err)
			}
			if role != tc.expectedRole {
				t.Errorf("%s: DetectUserRole() = %v, want %v", tc.description, role, tc.expectedRole)
			}
		})
	}
}

// TestUpstreamRemoteIntegration tests the upstream remote detection in a realistic scenario.
// This test verifies the fix for GH#1174: Fork contributors using SSH are incorrectly
// detected as maintainers.
func TestUpstreamRemoteIntegration(t *testing.T) {
	t.Run("ssh_fork_with_upstream_is_contributor", func(t *testing.T) {
		// Simulates the problematic scenario from GH#1174:
		// - User has forked a repo
		// - User uses SSH for their fork (origin = git@github.com:user/fork.git)
		// - User has added upstream (upstream = https://github.com/owner/repo.git)
		// Expected: User should be detected as CONTRIBUTOR, not maintainer

		orig := gitCommandRunner
		stub := &gitStub{t: t, responses: []gitResponse{
			// No explicit config
			{
				expect: gitCall{"/my/fork", []string{"config", "--get", "beads.role"}},
				output: "",
				err:    errors.New("not set"),
			},
			// Upstream exists (the key signal that this is a fork)
			{
				expect: gitCall{"/my/fork", []string{"remote", "get-url", "upstream"}},
				output: "https://github.com/owner/original.git\n",
				err:    nil,
			},
		}}
		gitCommandRunner = stub.run
		t.Cleanup(func() {
			gitCommandRunner = orig
			stub.verify()
		})

		role, err := DetectUserRole("/my/fork")
		if err != nil {
			t.Fatalf("DetectUserRole() error = %v", err)
		}

		// The critical assertion: despite SSH URL, upstream remote means contributor
		if role != Contributor {
			t.Errorf("Fork with upstream should be Contributor, got %s", role)
		}
	})
}
