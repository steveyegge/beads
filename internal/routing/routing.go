package routing

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var gitCommandRunner = func(repoPath string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	return cmd.Output()
}

// HasUpstreamRemote checks if the repository has an "upstream" remote configured.
// The presence of an upstream remote is a strong signal that this is a fork,
// indicating the user is likely a contributor rather than a maintainer.
func HasUpstreamRemote(repoPath string) bool {
	_, err := gitCommandRunner(repoPath, "remote", "get-url", "upstream")
	return err == nil
}

// UserRole represents whether the user is a maintainer or contributor
type UserRole string

const (
	Maintainer  UserRole = "maintainer"
	Contributor UserRole = "contributor"
)

// RoleSource indicates how the user role was determined.
type RoleSource string

const (
	// RoleSourceConfig indicates role was set via git config beads.role.
	RoleSourceConfig RoleSource = "config"
	// RoleSourceCache indicates role was read from git config beads.role.cache.
	RoleSourceCache RoleSource = "cache"
	// RoleSourceUpstream indicates role was detected via upstream remote.
	RoleSourceUpstream RoleSource = "upstream"
	// RoleSourceAPI indicates role was determined via GitHub API.
	RoleSourceAPI RoleSource = "api"
	// RoleSourceHeuristic indicates role was inferred from URL patterns.
	RoleSourceHeuristic RoleSource = "heuristic"
)

// DetectUserRole determines if the user is a maintainer or contributor
// based on git configuration and repository permissions.
//
// Detection strategy (in priority order):
// 1. Check git config for beads.role setting (explicit override)
// 2. Check for upstream remote (fork signal - implies contributor)
// 3. Check push URL pattern (SSH/HTTPS with credentials → maintainer)
// 4. Fall back to contributor if uncertain
func DetectUserRole(repoPath string) (UserRole, error) {
	// First check for explicit role in git config
	output, err := gitCommandRunner(repoPath, "config", "--get", "beads.role")
	if err == nil {
		role := strings.TrimSpace(string(output))
		if role == string(Maintainer) {
			return Maintainer, nil
		}
		if role == string(Contributor) {
			return Contributor, nil
		}
	}

	// Check for upstream remote - presence indicates a fork (contributor)
	if HasUpstreamRemote(repoPath) {
		return Contributor, nil
	}

	// Check push access by examining remote URL
	output, err = gitCommandRunner(repoPath, "remote", "get-url", "--push", "origin")
	if err != nil {
		// Fallback to standard fetch URL if push URL fails (some git versions/configs)
		output, err = gitCommandRunner(repoPath, "remote", "get-url", "origin")
		if err != nil {
			// No remote or error - default to contributor
			return Contributor, nil
		}
	}

	pushURL := strings.TrimSpace(string(output))

	// Check if URL indicates write access
	// SSH URLs (git@github.com:user/repo.git) typically indicate write access
	// HTTPS with token/password also indicates write access
	if strings.HasPrefix(pushURL, "git@") ||
		strings.HasPrefix(pushURL, "ssh://") ||
		strings.Contains(pushURL, "@") {
		return Maintainer, nil
	}

	// HTTPS without credentials likely means read-only contributor
	return Contributor, nil
}

// RoleDetectionResult contains the detected role and metadata about how it was determined.
type RoleDetectionResult struct {
	Role       UserRole
	Source     RoleSource
	IsFork     bool   // True if GitHub API confirmed this is a fork
	OriginURL  string // The origin remote URL
	UpstreamURL string // The upstream remote URL (empty if none)
	HasUpstream bool   // True if an upstream remote exists
}

// DetectUserRoleWithSource determines user role using a 4-tier detection strategy.
// Returns both the role and metadata about how it was detected.
//
// Detection strategy (in priority order):
//  1. Explicit git config (beads.role) - user override
//  2. Cached role (beads.role.cache) - from previous detection
//  3. Upstream remote detection - presence indicates fork/contributor
//  4. GitHub API (if token available) - authoritative fork detection
//  5. SSH/HTTPS URL heuristic - fallback
//
// After successful API or heuristic detection, the role is cached to git config
// to avoid repeated API calls.
func DetectUserRoleWithSource(repoPath string) (*RoleDetectionResult, error) {
	result := &RoleDetectionResult{}

	// Get origin URL for result metadata
	originOutput, err := gitCommandRunner(repoPath, "remote", "get-url", "origin")
	if err == nil {
		result.OriginURL = strings.TrimSpace(string(originOutput))
	}

	// Check for upstream remote
	upstreamOutput, err := gitCommandRunner(repoPath, "remote", "get-url", "upstream")
	if err == nil {
		result.UpstreamURL = strings.TrimSpace(string(upstreamOutput))
		result.HasUpstream = true
	}

	// 1. Check for explicit role in git config (highest priority)
	output, err := gitCommandRunner(repoPath, "config", "--get", "beads.role")
	if err == nil {
		role := strings.TrimSpace(string(output))
		if role == string(Maintainer) || role == string(Contributor) {
			result.Role = UserRole(role)
			result.Source = RoleSourceConfig
			return result, nil
		}
	}

	// 2. Check for cached role
	output, err = gitCommandRunner(repoPath, "config", "--get", "beads.role.cache")
	if err == nil {
		role := strings.TrimSpace(string(output))
		if role == string(Maintainer) || role == string(Contributor) {
			result.Role = UserRole(role)
			result.Source = RoleSourceCache
			return result, nil
		}
	}

	// 3. Check for upstream remote - presence indicates contributor
	if result.HasUpstream {
		result.Role = Contributor
		result.Source = RoleSourceUpstream
		// Cache the detected role
		cacheRole(repoPath, Contributor)
		return result, nil
	}

	// 4. Try GitHub API if we have a token and a GitHub URL
	if result.OriginURL != "" {
		owner, repo, parseErr := ParseGitHubRemote(result.OriginURL)
		if parseErr == nil {
			// Try to get a token
			tokenDiscoverer := NewTokenDiscoverer()
			token := tokenDiscoverer.DiscoverToken()
			if token != "" {
				checker := NewGitHubChecker(token)
				checkResult, apiErr := checker.CheckRepo(contextBackground(), owner, repo)
				if apiErr == nil {
					result.IsFork = checkResult.IsFork
					if checkResult.IsFork {
						result.Role = Contributor
						result.Source = RoleSourceAPI
						cacheRole(repoPath, Contributor)
						return result, nil
					}
					if checkResult.CanPush {
						result.Role = Maintainer
						result.Source = RoleSourceAPI
						cacheRole(repoPath, Maintainer)
						return result, nil
					}
					// API returned but no push access and not a fork - contributor
					result.Role = Contributor
					result.Source = RoleSourceAPI
					cacheRole(repoPath, Contributor)
					return result, nil
				}
				// API failed, fall through to heuristic
			}
		}
	}

	// 5. Fall back to URL heuristic
	role := detectRoleFromURL(result.OriginURL, repoPath)
	result.Role = role
	result.Source = RoleSourceHeuristic
	cacheRole(repoPath, role)
	return result, nil
}

// contextBackground returns a background context for API calls.
// This is a simple wrapper to avoid importing context in package-level var.
func contextBackground() context.Context {
	return context.Background()
}

// detectRoleFromURL determines role based on URL patterns.
func detectRoleFromURL(pushURL string, repoPath string) UserRole {
	if pushURL == "" {
		// Try to get push URL
		output, err := gitCommandRunner(repoPath, "remote", "get-url", "--push", "origin")
		if err != nil {
			output, err = gitCommandRunner(repoPath, "remote", "get-url", "origin")
			if err != nil {
				return Contributor
			}
		}
		pushURL = strings.TrimSpace(string(output))
	}

	// SSH URLs indicate write access
	if strings.HasPrefix(pushURL, "git@") ||
		strings.HasPrefix(pushURL, "ssh://") ||
		strings.Contains(pushURL, "@") {
		return Maintainer
	}

	// HTTPS without credentials = contributor
	return Contributor
}

// cacheRole writes the detected role to git config for future lookups.
func cacheRole(repoPath string, role UserRole) {
	// Best-effort caching - ignore errors
	args := []string{"config", "beads.role.cache", string(role)}
	_, _ = gitCommandRunner(repoPath, args...)
}

// RoutingConfig defines routing rules for issues
type RoutingConfig struct {
	Mode             string // "auto" or "explicit"
	DefaultRepo      string // Default repo for new issues
	MaintainerRepo   string // Repo for maintainers (in auto mode)
	ContributorRepo  string // Repo for contributors (in auto mode)
	ExplicitOverride string // Explicit --repo flag override
}

// DetermineTargetRepo determines which repo should receive a new issue
// based on routing configuration and user role
func DetermineTargetRepo(config *RoutingConfig, userRole UserRole, repoPath string) string {
	// Explicit override takes precedence
	if config.ExplicitOverride != "" {
		return config.ExplicitOverride
	}

	// Auto mode: route based on user role
	if config.Mode == "auto" {
		if userRole == Maintainer && config.MaintainerRepo != "" {
			return config.MaintainerRepo
		}
		if userRole == Contributor && config.ContributorRepo != "" {
			return config.ContributorRepo
		}
	}

	// Fall back to default repo
	if config.DefaultRepo != "" {
		return config.DefaultRepo
	}

	// No routing configured - use current repo
	return "."
}

// ExpandPath expands ~ to home directory and resolves relative paths to absolute.
// Returns the original path if expansion fails.
func ExpandPath(path string) string {
	if path == "" || path == "." {
		return path
	}

	// Expand ~ to home directory
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}

	// Convert relative paths to absolute
	if !filepath.IsAbs(path) {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
	}

	return path
}
