package routing

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/config"
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
	// Test fallback behavior when git is not available
	// When git remote fails (nonexistent path), defaults to Maintainer (local project assumption)
	role, err := DetectUserRole("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("DetectUserRole() error = %v, want nil", err)
	}
	if role != Maintainer {
		t.Errorf("DetectUserRole() = %v, want %v (fallback for local project)", role, Maintainer)
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

func TestParseRouteFromTitle(t *testing.T) {
	tests := []struct {
		title      string
		wantPrefix string
		wantPath   string
	}{
		{"gt- → gastown", "gt-", "gastown"},
		{"bd- → beads/mayor/rig", "bd-", "beads/mayor/rig"},
		{"hq- → town root", "hq-", "."},
		{"hq- → .", "hq-", "."},
		{"gt -> gastown", "gt-", "gastown"}, // ASCII arrow
		{"gt- -> gastown", "gt-", "gastown"},
		{"prefix without arrow", "", ""},      // No arrow
		{"→ path only", "", ""},               // Missing prefix
		{"prefix →", "", ""},                  // Missing path (won't split correctly)
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			got := ParseRouteFromTitle(tt.title)
			if got.Prefix != tt.wantPrefix {
				t.Errorf("ParseRouteFromTitle(%q).Prefix = %q, want %q", tt.title, got.Prefix, tt.wantPrefix)
			}
			if got.Path != tt.wantPath {
				t.Errorf("ParseRouteFromTitle(%q).Path = %q, want %q", tt.title, got.Path, tt.wantPath)
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

func TestDetectUserRole_DefaultContributor(t *testing.T) {
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

// TestFindTownRoutes_SymlinkedBeadsDir verifies that findTownRoutes correctly
// handles symlinked .beads directories by using findTownRootFromCWD() instead of
// walking up from the beadsDir path.
//
// Scenario: ~/gt/.beads is a symlink to ~/gt/olympus/.beads
// Before fix: walking up from ~/gt/olympus/.beads finds ~/gt/olympus (WRONG)
// After fix: findTownRootFromCWD() walks up from CWD to find mayor/town.json at ~/gt
func TestFindTownRoutes_SymlinkedBeadsDir(t *testing.T) {
	// Ensure BD_DAEMON_HOST is not set for this test (we want file-based routing)
	originalHost := os.Getenv("BD_DAEMON_HOST")
	os.Unsetenv("BD_DAEMON_HOST")
	defer func() {
		if originalHost != "" {
			os.Setenv("BD_DAEMON_HOST", originalHost)
		}
	}()

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

// TestLookupRigForgiving_DirectoryBased verifies that LookupRigForgiving falls back
// to directory-based resolution when a rig exists but isn't registered in routes.jsonl.
//
// Scenario: --rig gastown is used, but gastown has no entry in routes.jsonl
// Before fix: LookupRigForgiving returns false (not found)
// After fix: LookupRigForgiving checks if gastown/.beads exists and returns a synthetic route
func TestLookupRigForgiving_DirectoryBased(t *testing.T) {
	// Ensure BD_DAEMON_HOST is not set for this test (we want file-based routing)
	originalHost := os.Getenv("BD_DAEMON_HOST")
	os.Unsetenv("BD_DAEMON_HOST")
	defer func() {
		if originalHost != "" {
			os.Setenv("BD_DAEMON_HOST", originalHost)
		}
	}()

	// Create temporary directory structure:
	// tmpDir/
	//   mayor/
	//     town.json    <- town root marker
	//   .beads/
	//     routes.jsonl <- with ONLY hq- prefix, NOT gastown
	//   gastown/       <- rig directory with no route entry
	//     .beads/
	//       metadata.json  <- with prefix "gs-"
	tmpDir, err := os.MkdirTemp("", "routing-directory-test")
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

	// Create .beads/routes.jsonl with only hq- prefix (NOT gastown)
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}
	routesContent := `{"prefix": "hq-", "path": "."}
`
	if err := os.WriteFile(filepath.Join(beadsDir, "routes.jsonl"), []byte(routesContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Create gastown/.beads with metadata.json containing prefix
	gastownBeadsDir := filepath.Join(tmpDir, "gastown", ".beads")
	if err := os.MkdirAll(gastownBeadsDir, 0750); err != nil {
		t.Fatal(err)
	}
	metadataContent := `{"prefix": "gs-", "backend": "dolt"}`
	if err := os.WriteFile(filepath.Join(gastownBeadsDir, "metadata.json"), []byte(metadataContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Change to the town root directory
	t.Chdir(tmpDir)

	// Test 1: hq- should be found via routes.jsonl
	route, found := LookupRigForgiving("hq-", beadsDir)
	if !found {
		t.Fatal("LookupRigForgiving should find hq- in routes.jsonl")
	}
	if route.Prefix != "hq-" {
		t.Errorf("Expected prefix hq-, got %s", route.Prefix)
	}

	// Test 2: gastown should be found via directory-based resolution
	route, found = LookupRigForgiving("gastown", beadsDir)
	if !found {
		t.Fatal("LookupRigForgiving should find gastown via directory-based resolution")
	}
	if route.Prefix != "gs-" {
		t.Errorf("Expected prefix gs- (from metadata.json), got %s", route.Prefix)
	}
	if route.Path != "gastown" {
		t.Errorf("Expected path gastown, got %s", route.Path)
	}

	// Test 3: nonexistent rig should not be found
	_, found = LookupRigForgiving("nonexistent", beadsDir)
	if found {
		t.Error("LookupRigForgiving should not find nonexistent rig")
	}
}

// TestReadPrefixFromBeadsDir tests reading prefix from metadata.json and config.yaml
func TestReadPrefixFromBeadsDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "read-prefix-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Test 1: Read prefix from metadata.json
	t.Run("metadata.json", func(t *testing.T) {
		beadsDir := filepath.Join(tmpDir, "test1")
		if err := os.MkdirAll(beadsDir, 0750); err != nil {
			t.Fatal(err)
		}
		metadataContent := `{"prefix": "gs-", "backend": "dolt"}`
		if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(metadataContent), 0600); err != nil {
			t.Fatal(err)
		}

		got := readPrefixFromBeadsDir(beadsDir)
		if got != "gs-" {
			t.Errorf("Expected gs-, got %s", got)
		}
	})

	// Test 2: Read prefix from config.yaml
	t.Run("config.yaml", func(t *testing.T) {
		beadsDir := filepath.Join(tmpDir, "test2")
		if err := os.MkdirAll(beadsDir, 0750); err != nil {
			t.Fatal(err)
		}
		configContent := `# Beads config
issue-prefix: "gt"
storage-backend: sqlite`
		if err := os.WriteFile(filepath.Join(beadsDir, "config.yaml"), []byte(configContent), 0600); err != nil {
			t.Fatal(err)
		}

		got := readPrefixFromBeadsDir(beadsDir)
		if got != "gt" {
			t.Errorf("Expected gt, got %s", got)
		}
	})

	// Test 3: No prefix found
	t.Run("no_prefix", func(t *testing.T) {
		beadsDir := filepath.Join(tmpDir, "test3")
		if err := os.MkdirAll(beadsDir, 0750); err != nil {
			t.Fatal(err)
		}

		got := readPrefixFromBeadsDir(beadsDir)
		if got != "" {
			t.Errorf("Expected empty string, got %s", got)
		}
	})
}

// TestLoadRoutes_RemoteDaemonNoFallback verifies that when BD_DAEMON_HOST is set,
// LoadRoutes does not fall back to routes.jsonl when the daemon query fails.
func TestLoadRoutes_RemoteDaemonNoFallback(t *testing.T) {
	// Create a temp directory with routes.jsonl
	tmpDir, err := os.MkdirTemp("", "routing-remote-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create routes.jsonl with a route
	routesContent := `{"prefix": "gt-", "path": "gastown"}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "routes.jsonl"), []byte(routesContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Test 1: Without BD_DAEMON_HOST, LoadRoutes should fall back to file
	t.Run("without_remote_daemon", func(t *testing.T) {
		// Ensure BD_DAEMON_HOST is not set
		originalHost := os.Getenv("BD_DAEMON_HOST")
		os.Unsetenv("BD_DAEMON_HOST")
		defer func() {
			if originalHost != "" {
				os.Setenv("BD_DAEMON_HOST", originalHost)
			}
		}()

		// Clear the route querier to simulate no daemon connection
		originalQuerier := routeQuerier
		SetRouteQuerier(nil)
		defer SetRouteQuerier(originalQuerier)

		routes, err := LoadRoutes(tmpDir)
		if err != nil {
			t.Fatalf("LoadRoutes should not error without BD_DAEMON_HOST: %v", err)
		}
		if len(routes) == 0 {
			t.Fatal("LoadRoutes should fall back to routes.jsonl without BD_DAEMON_HOST")
		}
		if routes[0].Prefix != "gt-" {
			t.Errorf("Expected prefix gt-, got %s", routes[0].Prefix)
		}
	})

	// Test 2: With BD_DAEMON_HOST but no querier, should error
	t.Run("with_remote_daemon_no_querier", func(t *testing.T) {
		originalHost := os.Getenv("BD_DAEMON_HOST")
		os.Setenv("BD_DAEMON_HOST", "192.168.1.100:9876")
		defer func() {
			if originalHost != "" {
				os.Setenv("BD_DAEMON_HOST", originalHost)
			} else {
				os.Unsetenv("BD_DAEMON_HOST")
			}
		}()

		// Clear the route querier
		originalQuerier := routeQuerier
		SetRouteQuerier(nil)
		defer SetRouteQuerier(originalQuerier)

		routes, err := LoadRoutes(tmpDir)
		if err == nil {
			t.Fatal("LoadRoutes should error with BD_DAEMON_HOST but no querier")
		}
		if routes != nil {
			t.Errorf("Expected nil routes, got %v", routes)
		}
		expectedMsg := "route querier not initialized"
		if !strings.Contains(err.Error(), expectedMsg) {
			t.Errorf("Expected error containing %q, got: %v", expectedMsg, err)
		}
	})

	// Test 6: With config.yaml daemon-host (no env var) and no querier, should error
	t.Run("with_config_daemon_host_no_querier", func(t *testing.T) {
		// Ensure BD_DAEMON_HOST env var is NOT set
		originalHost := os.Getenv("BD_DAEMON_HOST")
		os.Unsetenv("BD_DAEMON_HOST")
		defer func() {
			if originalHost != "" {
				os.Setenv("BD_DAEMON_HOST", originalHost)
			}
		}()

		// Initialize config so viper is available for Set/GetString
		if err := config.Initialize(); err != nil {
			t.Fatalf("config.Initialize() error: %v", err)
		}

		// Set daemon-host via config (simulates config.yaml)
		config.Set("daemon-host", "http://remote:9080")
		defer config.Set("daemon-host", "")

		// Clear the route querier
		originalQuerier := routeQuerier
		SetRouteQuerier(nil)
		defer SetRouteQuerier(originalQuerier)

		routes, err := LoadRoutes(tmpDir)
		if err == nil {
			t.Fatal("LoadRoutes should error with config.yaml daemon-host but no querier")
		}
		if routes != nil {
			t.Errorf("Expected nil routes, got %v", routes)
		}
		expectedMsg := "route querier not initialized"
		if !strings.Contains(err.Error(), expectedMsg) {
			t.Errorf("Expected error containing %q, got: %v", expectedMsg, err)
		}
	})

	// Test 7: With config.yaml daemon-host and failing querier, should error (not fall back)
	t.Run("with_config_daemon_host_failing_querier", func(t *testing.T) {
		// Ensure BD_DAEMON_HOST env var is NOT set
		originalHost := os.Getenv("BD_DAEMON_HOST")
		os.Unsetenv("BD_DAEMON_HOST")
		defer func() {
			if originalHost != "" {
				os.Setenv("BD_DAEMON_HOST", originalHost)
			}
		}()

		// Initialize config so viper is available for Set/GetString
		if err := config.Initialize(); err != nil {
			t.Fatalf("config.Initialize() error: %v", err)
		}

		// Set daemon-host via config
		config.Set("daemon-host", "http://remote:9080")
		defer config.Set("daemon-host", "")

		// Set a failing route querier
		originalQuerier := routeQuerier
		SetRouteQuerier(func(_ string) ([]Route, error) {
			return nil, errors.New("daemon connection refused")
		})
		defer SetRouteQuerier(originalQuerier)

		routes, err := LoadRoutes(tmpDir)
		if err == nil {
			t.Fatal("LoadRoutes should error when config.yaml remote daemon fails")
		}
		if routes != nil {
			t.Errorf("Expected nil routes, got %v", routes)
		}
		if !strings.Contains(err.Error(), "daemon connection refused") {
			t.Errorf("Expected error containing daemon error, got: %v", err)
		}
	})

	// Test 3: With BD_DAEMON_HOST and failing querier, should error (not fall back)
	t.Run("with_remote_daemon_failing_querier", func(t *testing.T) {
		originalHost := os.Getenv("BD_DAEMON_HOST")
		os.Setenv("BD_DAEMON_HOST", "192.168.1.100:9876")
		defer func() {
			if originalHost != "" {
				os.Setenv("BD_DAEMON_HOST", originalHost)
			} else {
				os.Unsetenv("BD_DAEMON_HOST")
			}
		}()

		// Set a failing route querier
		originalQuerier := routeQuerier
		SetRouteQuerier(func(_ string) ([]Route, error) {
			return nil, errors.New("daemon connection refused")
		})
		defer SetRouteQuerier(originalQuerier)

		routes, err := LoadRoutes(tmpDir)
		if err == nil {
			t.Fatal("LoadRoutes should error when remote daemon fails")
		}
		if routes != nil {
			t.Errorf("Expected nil routes, got %v", routes)
		}
		if !strings.Contains(err.Error(), "daemon connection refused") {
			t.Errorf("Expected error containing daemon error, got: %v", err)
		}
	})

	// Test 4: With BD_DAEMON_HOST and successful querier, should return routes
	t.Run("with_remote_daemon_successful_querier", func(t *testing.T) {
		originalHost := os.Getenv("BD_DAEMON_HOST")
		os.Setenv("BD_DAEMON_HOST", "192.168.1.100:9876")
		defer func() {
			if originalHost != "" {
				os.Setenv("BD_DAEMON_HOST", originalHost)
			} else {
				os.Unsetenv("BD_DAEMON_HOST")
			}
		}()

		// Set a successful route querier returning different routes than the file
		originalQuerier := routeQuerier
		SetRouteQuerier(func(_ string) ([]Route, error) {
			return []Route{{Prefix: "bd-", Path: "beads"}}, nil
		})
		defer SetRouteQuerier(originalQuerier)

		routes, err := LoadRoutes(tmpDir)
		if err != nil {
			t.Fatalf("LoadRoutes should not error with successful querier: %v", err)
		}
		if len(routes) != 1 {
			t.Fatalf("Expected 1 route, got %d", len(routes))
		}
		// Should return daemon routes, not file routes
		if routes[0].Prefix != "bd-" {
			t.Errorf("Expected prefix bd- from daemon, got %s", routes[0].Prefix)
		}
	})

	// Test 5: With BD_DAEMON_HOST and querier returning empty (no error), should return empty
	t.Run("with_remote_daemon_empty_querier", func(t *testing.T) {
		originalHost := os.Getenv("BD_DAEMON_HOST")
		os.Setenv("BD_DAEMON_HOST", "192.168.1.100:9876")
		defer func() {
			if originalHost != "" {
				os.Setenv("BD_DAEMON_HOST", originalHost)
			} else {
				os.Unsetenv("BD_DAEMON_HOST")
			}
		}()

		// Set a querier that returns empty routes (daemon reachable but no routes)
		originalQuerier := routeQuerier
		SetRouteQuerier(func(_ string) ([]Route, error) {
			return nil, nil
		})
		defer SetRouteQuerier(originalQuerier)

		routes, err := LoadRoutes(tmpDir)
		if err != nil {
			t.Fatalf("LoadRoutes should not error when daemon returns empty: %v", err)
		}
		// Should NOT fall back to file routes
		if len(routes) != 0 {
			t.Errorf("Expected 0 routes (not file fallback), got %d", len(routes))
		}
	})
}
