package routing

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetermineTargetRepo(t *testing.T) {
	tests := []struct {
		name     string
		config   *RoutingConfig
		userRole UserRole
		repoPath string
		want     string
	}{
		{
			name: "explicit override takes precedence",
			config: &RoutingConfig{
				Mode:             "auto",
				DefaultRepo:      "~/planning",
				MaintainerRepo:   ".",
				ContributorRepo:  "~/contributor-planning",
				ExplicitOverride: "/tmp/custom",
			},
			userRole: Maintainer,
			repoPath: ".",
			want:     "/tmp/custom",
		},
		{
			name: "auto mode - maintainer uses maintainer repo",
			config: &RoutingConfig{
				Mode:            "auto",
				MaintainerRepo:  ".",
				ContributorRepo: "~/contributor-planning",
			},
			userRole: Maintainer,
			repoPath: ".",
			want:     ".",
		},
		{
			name: "auto mode - contributor uses contributor repo",
			config: &RoutingConfig{
				Mode:            "auto",
				MaintainerRepo:  ".",
				ContributorRepo: "~/contributor-planning",
			},
			userRole: Contributor,
			repoPath: ".",
			want:     "~/contributor-planning",
		},
		{
			name: "explicit mode uses default",
			config: &RoutingConfig{
				Mode:        "explicit",
				DefaultRepo: "~/planning",
			},
			userRole: Maintainer,
			repoPath: ".",
			want:     "~/planning",
		},
		{
			name: "no config defaults to current directory",
			config: &RoutingConfig{
				Mode: "auto",
			},
			userRole: Maintainer,
			repoPath: ".",
			want:     ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineTargetRepo(tt.config, tt.userRole, tt.repoPath)
			if got != tt.want {
				t.Errorf("DetermineTargetRepo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectUserRole_Fallback(t *testing.T) {
	// Test fallback behavior when git is not available - local projects default to maintainer
	role, err := DetectUserRole("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("DetectUserRole() error = %v, want nil", err)
	}
	if role != Maintainer {
		t.Errorf("DetectUserRole() = %v, want %v (local project fallback)", role, Maintainer)
	}
}

func TestExtractPrefix(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"gt-abc123", "gt-"},
		{"bd-xyz", "bd-"},
		{"hq-1234", "hq-"},
		{"abc123", ""}, // No hyphen
		{"", ""},       // Empty string
		{"-abc", "-"},  // Starts with hyphen
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := ExtractPrefix(tt.id)
			if got != tt.want {
				t.Errorf("ExtractPrefix(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestExtractProjectFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"beads/mayor/rig", "beads"},
		{"gastown/crew/max", "gastown"},
		{"simple", "simple"},
		{"", ""},
		{"/absolute/path", ""}, // Starts with /, first component is empty
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := ExtractProjectFromPath(tt.path)
			if got != tt.want {
				t.Errorf("ExtractProjectFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestResolveToExternalRef(t *testing.T) {
	// This test is limited since it requires a routes.jsonl file
	// Just test that it returns empty string for nonexistent directory
	got := ResolveToExternalRef("bd-abc", "/nonexistent/path")
	if got != "" {
		t.Errorf("ResolveToExternalRef() = %q, want empty string for nonexistent path", got)
	}
}

type gitCall struct {
	repo string
	args []string
}

type gitResponse struct {
	expect gitCall
	output string
	err    error
}

type gitStub struct {
	t         *testing.T
	responses []gitResponse
	idx       int
}

func (s *gitStub) run(repo string, args ...string) ([]byte, error) {
	if s.idx >= len(s.responses) {
		s.t.Fatalf("unexpected git call %v in repo %s", args, repo)
	}
	resp := s.responses[s.idx]
	s.idx++
	if resp.expect.repo != repo {
		s.t.Fatalf("repo mismatch: got %q want %q", repo, resp.expect.repo)
	}
	if !reflect.DeepEqual(resp.expect.args, args) {
		s.t.Fatalf("args mismatch: got %v want %v", args, resp.expect.args)
	}
	return []byte(resp.output), resp.err
}

func (s *gitStub) verify() {
	if s.idx != len(s.responses) {
		s.t.Fatalf("expected %d git calls, got %d", len(s.responses), s.idx)
	}
}

func TestDetectUserRole_ConfigOverrideMaintainer(t *testing.T) {
	orig := gitCommandRunner
	stub := &gitStub{t: t, responses: []gitResponse{
		{expect: gitCall{"", []string{"config", "--get", "beads.role"}}, output: "maintainer\n"},
	}}
	gitCommandRunner = stub.run
	t.Cleanup(func() {
		gitCommandRunner = orig
		stub.verify()
	})

	role, err := DetectUserRole("")
	if err != nil {
		t.Fatalf("DetectUserRole error = %v", err)
	}
	if role != Maintainer {
		t.Fatalf("expected %s, got %s", Maintainer, role)
	}
}

func TestDetectUserRole_ConfigOverrideContributor(t *testing.T) {
	orig := gitCommandRunner
	stub := &gitStub{t: t, responses: []gitResponse{
		{expect: gitCall{"/repo", []string{"config", "--get", "beads.role"}}, output: "contributor\n"},
	}}
	gitCommandRunner = stub.run
	t.Cleanup(func() {
		gitCommandRunner = orig
		stub.verify()
	})

	role, err := DetectUserRole("/repo")
	if err != nil {
		t.Fatalf("DetectUserRole error = %v", err)
	}
	if role != Contributor {
		t.Fatalf("expected %s, got %s", Contributor, role)
	}
}

func TestDetectUserRole_PushURLMaintainer(t *testing.T) {
	orig := gitCommandRunner
	stub := &gitStub{t: t, responses: []gitResponse{
		{expect: gitCall{"/repo", []string{"config", "--get", "beads.role"}}, output: "unknown"},
		{expect: gitCall{"/repo", []string{"remote", "get-url", "--push", "origin"}}, output: "git@github.com:owner/repo.git"},
	}}
	gitCommandRunner = stub.run
	t.Cleanup(func() {
		gitCommandRunner = orig
		stub.verify()
	})

	role, err := DetectUserRole("/repo")
	if err != nil {
		t.Fatalf("DetectUserRole error = %v", err)
	}
	if role != Maintainer {
		t.Fatalf("expected %s, got %s", Maintainer, role)
	}
}

func TestDetectUserRole_HTTPSCredentialsMaintainer(t *testing.T) {
	orig := gitCommandRunner
	stub := &gitStub{t: t, responses: []gitResponse{
		{expect: gitCall{"/repo", []string{"config", "--get", "beads.role"}}, output: ""},
		{expect: gitCall{"/repo", []string{"remote", "get-url", "--push", "origin"}}, output: "https://token@github.com/owner/repo.git"},
	}}
	gitCommandRunner = stub.run
	t.Cleanup(func() {
		gitCommandRunner = orig
		stub.verify()
	})

	role, err := DetectUserRole("/repo")
	if err != nil {
		t.Fatalf("DetectUserRole error = %v", err)
	}
	if role != Maintainer {
		t.Fatalf("expected %s, got %s", Maintainer, role)
	}
}

func TestDetectUserRole_HTTPSNoCredentialsContributor(t *testing.T) {
	orig := gitCommandRunner
	stub := &gitStub{t: t, responses: []gitResponse{
		{expect: gitCall{"", []string{"config", "--get", "beads.role"}}, err: errors.New("missing")},
		{expect: gitCall{"", []string{"remote", "get-url", "--push", "origin"}}, err: errors.New("no push")},
		{expect: gitCall{"", []string{"remote", "get-url", "origin"}}, output: "https://github.com/owner/repo.git"},
	}}
	gitCommandRunner = stub.run
	t.Cleanup(func() {
		gitCommandRunner = orig
		stub.verify()
	})

	role, err := DetectUserRole("")
	if err != nil {
		t.Fatalf("DetectUserRole error = %v", err)
	}
	if role != Contributor {
		t.Fatalf("expected %s, got %s", Contributor, role)
	}
}

func TestDetectUserRole_NoRemoteMaintainer(t *testing.T) {
	// When no git remote is configured, default to maintainer (local project)
	orig := gitCommandRunner
	stub := &gitStub{t: t, responses: []gitResponse{
		{expect: gitCall{"/local", []string{"config", "--get", "beads.role"}}, err: errors.New("missing")},
		{expect: gitCall{"/local", []string{"remote", "get-url", "--push", "origin"}}, err: errors.New("no remote")},
		{expect: gitCall{"/local", []string{"remote", "get-url", "origin"}}, err: errors.New("no remote")},
	}}
	gitCommandRunner = stub.run
	t.Cleanup(func() {
		gitCommandRunner = orig
		stub.verify()
	})

	role, err := DetectUserRole("/local")
	if err != nil {
		t.Fatalf("DetectUserRole error = %v", err)
	}
	if role != Maintainer {
		t.Fatalf("expected %s for local project with no remote, got %s", Maintainer, role)
	}
}

// TestFindTownRoutes_SymlinkedBeadsDir verifies that findTownRoutes correctly
// handles symlinked .beads directories by using findTownRootFromCWD() instead of
// walking up from the beadsDir path.
//
// Scenario: ~/gt/.beads is a symlink to ~/gt/olympus/.beads
// Before fix: walking up from ~/gt/olympus/.beads finds ~/gt/olympus (WRONG)
// After fix: findTownRootFromCWD() walks up from CWD to find mayor/town.json at ~/gt
func TestFindTownRoutes_SymlinkedBeadsDir(t *testing.T) {
	// Create temporary directory structure simulating Gas Town:
	// tmpDir/
	//   mayor/
	//     town.json    <- town root marker
	//   olympus/       <- actual beads storage
	//     .beads/
	//       routes.jsonl
	//   .beads -> olympus/.beads  <- symlink
	//   daedalus/
	//     mayor/
	//       rig/
	//         .beads/  <- target rig
	tmpDir, err := os.MkdirTemp("", "routing-symlink-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Resolve symlinks in tmpDir (macOS /var -> /private/var)
	tmpDir, err = filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create mayor/town.json to mark town root
	mayorDir := filepath.Join(tmpDir, "mayor")
	if err := os.MkdirAll(mayorDir, 0750); err != nil {
		t.Fatal(err)
	}
	townJSON := filepath.Join(mayorDir, "town.json")
	if err := os.WriteFile(townJSON, []byte(`{"name": "test-town"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Create olympus/.beads with routes.jsonl
	olympusBeadsDir := filepath.Join(tmpDir, "olympus", ".beads")
	if err := os.MkdirAll(olympusBeadsDir, 0750); err != nil {
		t.Fatal(err)
	}
	routesContent := `{"prefix": "gt-", "path": "daedalus/mayor/rig"}
`
	routesPath := filepath.Join(olympusBeadsDir, "routes.jsonl")
	if err := os.WriteFile(routesPath, []byte(routesContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Create daedalus/mayor/rig/.beads as target rig
	daedalusBeadsDir := filepath.Join(tmpDir, "daedalus", "mayor", "rig", ".beads")
	if err := os.MkdirAll(daedalusBeadsDir, 0750); err != nil {
		t.Fatal(err)
	}
	// Create metadata.json so the rig is recognized as valid
	if err := os.WriteFile(filepath.Join(daedalusBeadsDir, "metadata.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Create symlink: tmpDir/.beads -> olympus/.beads
	symlinkPath := filepath.Join(tmpDir, ".beads")
	if err := os.Symlink(olympusBeadsDir, symlinkPath); err != nil {
		t.Skip("Cannot create symlinks on this system (may require admin on Windows)")
	}

	// Change to the town root directory - this simulates the user running bd from ~/gt
	// The fix uses findTownRootFromCWD() which needs CWD to be inside the town
	t.Chdir(tmpDir)

	// Simulate what happens when FindBeadsDir() returns the resolved symlink path
	// (this is what CanonicalizePath does)
	resolvedBeadsDir := olympusBeadsDir // This is what would be passed to findTownRoutes

	// Call findTownRoutes with the resolved symlink path
	routes, townRoot := findTownRoutes(resolvedBeadsDir)

	// Verify we got the routes
	if len(routes) == 0 {
		t.Fatal("findTownRoutes returned no routes")
	}

	// Verify the town root is correct (should be tmpDir, NOT tmpDir/olympus)
	if townRoot != tmpDir {
		t.Errorf("findTownRoutes returned wrong townRoot:\n  got:  %s\n  want: %s", townRoot, tmpDir)
	}

	// Verify route resolution works - the route should resolve to the correct path
	expectedRigPath := filepath.Join(tmpDir, "daedalus", "mayor", "rig", ".beads")
	for _, route := range routes {
		if route.Prefix == "gt-" {
			actualPath := filepath.Join(townRoot, route.Path, ".beads")
			if actualPath != expectedRigPath {
				t.Errorf("Route resolution failed:\n  got:  %s\n  want: %s", actualPath, expectedRigPath)
			}
		}
	}
}

// TestLoadRoutes_MalformedJSON verifies that malformed JSON lines are skipped
// and valid routes are still loaded.
func TestLoadRoutes_MalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	routesPath := filepath.Join(beadsDir, "routes.jsonl")
	routes := `{"prefix":"gt-","path":"gastown"}
{invalid json here}
{"prefix":"bd-","path":"beads"}
`
	if err := os.WriteFile(routesPath, []byte(routes), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadRoutes(beadsDir)
	if err != nil {
		t.Fatalf("LoadRoutes should not error on malformed lines: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("Expected 2 valid routes, got %d", len(loaded))
	}
	// Verify the correct routes were loaded
	if loaded[0].Prefix != "gt-" || loaded[0].Path != "gastown" {
		t.Errorf("First route incorrect: got %+v", loaded[0])
	}
	if loaded[1].Prefix != "bd-" || loaded[1].Path != "beads" {
		t.Errorf("Second route incorrect: got %+v", loaded[1])
	}
}

// TestLoadRoutes_EmptyPrefix verifies that routes with empty prefix are skipped.
func TestLoadRoutes_EmptyPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	routesPath := filepath.Join(beadsDir, "routes.jsonl")
	routes := `{"prefix":"gt-","path":"gastown"}
{"prefix":"","path":"empty-prefix"}
{"prefix":"bd-","path":"beads"}
`
	if err := os.WriteFile(routesPath, []byte(routes), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadRoutes(beadsDir)
	if err != nil {
		t.Fatalf("LoadRoutes should not error on empty prefix: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("Expected 2 valid routes (empty prefix skipped), got %d", len(loaded))
	}
}

// TestLoadRoutes_EmptyPath verifies that routes with empty path are skipped.
func TestLoadRoutes_EmptyPath(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	routesPath := filepath.Join(beadsDir, "routes.jsonl")
	routes := `{"prefix":"gt-","path":"gastown"}
{"prefix":"empty-","path":""}
{"prefix":"bd-","path":"beads"}
`
	if err := os.WriteFile(routesPath, []byte(routes), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadRoutes(beadsDir)
	if err != nil {
		t.Fatalf("LoadRoutes should not error on empty path: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("Expected 2 valid routes (empty path skipped), got %d", len(loaded))
	}
}

// TestLoadRoutes_EmptyFile verifies that an empty file returns nil slice, not error.
func TestLoadRoutes_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	routesPath := filepath.Join(beadsDir, "routes.jsonl")
	if err := os.WriteFile(routesPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadRoutes(beadsDir)
	if err != nil {
		t.Fatalf("LoadRoutes should not error on empty file: %v", err)
	}
	if loaded != nil && len(loaded) != 0 {
		t.Errorf("Expected nil or empty slice for empty file, got %d routes", len(loaded))
	}
}

// TestLoadRoutes_CommentsOnly verifies that a file with only comments and blank lines
// returns nil slice, not error.
func TestLoadRoutes_CommentsOnly(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	routesPath := filepath.Join(beadsDir, "routes.jsonl")
	routes := `# This is a comment
# Another comment

   # Comment with leading whitespace

`
	if err := os.WriteFile(routesPath, []byte(routes), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadRoutes(beadsDir)
	if err != nil {
		t.Fatalf("LoadRoutes should not error on comments-only file: %v", err)
	}
	if loaded != nil && len(loaded) != 0 {
		t.Errorf("Expected nil or empty slice for comments-only file, got %d routes", len(loaded))
	}
}

// TestLoadRoutes_FileNotExist verifies that a non-existent file returns nil, nil (not an error).
func TestLoadRoutes_FileNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Don't create routes.jsonl

	loaded, err := LoadRoutes(beadsDir)
	if err != nil {
		t.Fatalf("LoadRoutes should not error on non-existent file: %v", err)
	}
	if loaded != nil {
		t.Errorf("Expected nil slice for non-existent file, got %d routes", len(loaded))
	}
}

// TestLoadRoutes_MixedContent verifies that LoadRoutes correctly handles a file with
// valid routes, malformed JSON, empty fields, comments, and blank lines mixed together.
func TestLoadRoutes_MixedContent(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	routesPath := filepath.Join(beadsDir, "routes.jsonl")
	routes := `# Routes configuration
{"prefix":"gt-","path":"gastown"}

{broken json
{"prefix":"","path":"no-prefix"}
{"prefix":"bd-","path":"beads"}
  # inline comment with whitespace
{"prefix":"empty-","path":""}
{"prefix":"hq-","path":"headquarters"}
`
	if err := os.WriteFile(routesPath, []byte(routes), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadRoutes(beadsDir)
	if err != nil {
		t.Fatalf("LoadRoutes should not error on mixed content: %v", err)
	}
	// Should have: gt-, bd-, hq- (3 valid routes)
	// Skipped: comment, blank, broken json, empty prefix, empty path, another comment
	if len(loaded) != 3 {
		t.Errorf("Expected 3 valid routes, got %d", len(loaded))
	}
	expectedPrefixes := []string{"gt-", "bd-", "hq-"}
	for i, expected := range expectedPrefixes {
		if i >= len(loaded) {
			break
		}
		if loaded[i].Prefix != expected {
			t.Errorf("Route %d: expected prefix %q, got %q", i, expected, loaded[i].Prefix)
		}
	}
}

func TestAutoDetectTargetRig(t *testing.T) {
	tests := []struct {
		name             string
		setupFunc        func(t *testing.T) (beadsDir string, cleanup func())
		configuredPrefix string
		wantRig          string
		wantShouldRoute  bool
		wantErr          bool
	}{
		{
			name: "no prefix configured",
			setupFunc: func(t *testing.T) (string, func()) {
				tmpDir := t.TempDir()
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.MkdirAll(beadsDir, 0755); err != nil {
					t.Fatal(err)
				}
				return beadsDir, func() {}
			},
			configuredPrefix: "",
			wantRig:          "",
			wantShouldRoute:  false,
			wantErr:          false,
		},
		{
			name: "no routes file",
			setupFunc: func(t *testing.T) (string, func()) {
				tmpDir := t.TempDir()
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.MkdirAll(beadsDir, 0755); err != nil {
					t.Fatal(err)
				}
				return beadsDir, func() {}
			},
			configuredPrefix: "gt",
			wantRig:          "",
			wantShouldRoute:  false,
			wantErr:          false,
		},
		{
			name: "prefix not in routes",
			setupFunc: func(t *testing.T) (string, func()) {
				tmpDir := t.TempDir()
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.MkdirAll(beadsDir, 0755); err != nil {
					t.Fatal(err)
				}
				// Create routes.jsonl with different prefix
				routesPath := filepath.Join(beadsDir, "routes.jsonl")
				routes := `{"prefix":"bd-","path":"beads/mayor/rig"}` + "\n"
				if err := os.WriteFile(routesPath, []byte(routes), 0644); err != nil {
					t.Fatal(err)
				}
				return beadsDir, func() {}
			},
			configuredPrefix: "gt",
			wantRig:          "",
			wantShouldRoute:  false,
			wantErr:          false,
		},
		{
			name: "already in correct location",
			setupFunc: func(t *testing.T) (string, func()) {
				tmpDir := t.TempDir()
				// Create town structure
				townBeadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.MkdirAll(townBeadsDir, 0755); err != nil {
					t.Fatal(err)
				}
				// Create mayor/town.json marker
				mayorDir := filepath.Join(tmpDir, "mayor")
				if err := os.MkdirAll(mayorDir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(mayorDir, "town.json"), []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
				// Create routes.jsonl pointing hq- to current location
				routesPath := filepath.Join(townBeadsDir, "routes.jsonl")
				routes := `{"prefix":"hq-","path":"."}` + "\n"
				if err := os.WriteFile(routesPath, []byte(routes), 0644); err != nil {
					t.Fatal(err)
				}
				return townBeadsDir, func() {}
			},
			configuredPrefix: "hq",
			wantRig:          "",
			wantShouldRoute:  false,
			wantErr:          false,
		},
		{
			name: "should route to different rig",
			setupFunc: func(t *testing.T) (string, func()) {
				tmpDir := t.TempDir()
				// Create town structure
				townBeadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.MkdirAll(townBeadsDir, 0755); err != nil {
					t.Fatal(err)
				}
				// Create mayor/town.json marker
				mayorDir := filepath.Join(tmpDir, "mayor")
				if err := os.MkdirAll(mayorDir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(mayorDir, "town.json"), []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
				// Create gastown rig
				gastownBeadsDir := filepath.Join(tmpDir, "gastown", "mayor", "rig", ".beads")
				if err := os.MkdirAll(gastownBeadsDir, 0755); err != nil {
					t.Fatal(err)
				}
				// Create routes.jsonl with gt- pointing to gastown
				routesPath := filepath.Join(townBeadsDir, "routes.jsonl")
				routes := `{"prefix":"hq-","path":"."}` + "\n" +
					`{"prefix":"gt-","path":"gastown/mayor/rig"}` + "\n"
				if err := os.WriteFile(routesPath, []byte(routes), 0644); err != nil {
					t.Fatal(err)
				}
				// Return town beads dir (simulating running from town root with gt- prefix config)
				return townBeadsDir, func() {}
			},
			configuredPrefix: "gt",
			wantRig:          "gt",
			wantShouldRoute:  true,
			wantErr:          false,
		},
		{
			name: "should route with prefix hyphen already included",
			setupFunc: func(t *testing.T) (string, func()) {
				tmpDir := t.TempDir()
				townBeadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.MkdirAll(townBeadsDir, 0755); err != nil {
					t.Fatal(err)
				}
				mayorDir := filepath.Join(tmpDir, "mayor")
				if err := os.MkdirAll(mayorDir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(mayorDir, "town.json"), []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
				gastownBeadsDir := filepath.Join(tmpDir, "gastown", "mayor", "rig", ".beads")
				if err := os.MkdirAll(gastownBeadsDir, 0755); err != nil {
					t.Fatal(err)
				}
				routesPath := filepath.Join(townBeadsDir, "routes.jsonl")
				routes := `{"prefix":"hq-","path":"."}` + "\n" +
					`{"prefix":"gt-","path":"gastown/mayor/rig"}` + "\n"
				if err := os.WriteFile(routesPath, []byte(routes), 0644); err != nil {
					t.Fatal(err)
				}
				return townBeadsDir, func() {}
			},
			configuredPrefix: "gt-", // Already has hyphen
			wantRig:          "gt",
			wantShouldRoute:  true,
			wantErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beadsDir, cleanup := tt.setupFunc(t)
			defer cleanup()

			gotRig, gotShouldRoute, err := AutoDetectTargetRig(beadsDir, tt.configuredPrefix)

			if (err != nil) != tt.wantErr {
				t.Errorf("AutoDetectTargetRig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotRig != tt.wantRig {
				t.Errorf("AutoDetectTargetRig() gotRig = %v, want %v", gotRig, tt.wantRig)
			}
			if gotShouldRoute != tt.wantShouldRoute {
				t.Errorf("AutoDetectTargetRig() gotShouldRoute = %v, want %v", gotShouldRoute, tt.wantShouldRoute)
			}
		})
	}
}
