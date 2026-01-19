package routing

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"
)

// GitHubCheckResult contains the results of a GitHub API check for a repository.
type GitHubCheckResult struct {
	IsFork  bool // True if the repository is a fork
	CanPush bool // True if the user has push access
}

// GitHubChecker provides methods to check repository status via GitHub API.
// Implementations should handle rate limiting gracefully and return errors
// that allow callers to fall back to heuristic detection.
type GitHubChecker interface {
	// CheckRepo queries GitHub API to determine if the repo is a fork and
	// whether the authenticated user has push access.
	// Returns (result, nil) on success, or (GitHubCheckResult{}, error) on failure.
	// Rate limiting errors should be logged as warnings, not returned as errors,
	// allowing the caller to fall back to heuristic detection.
	CheckRepo(ctx context.Context, owner, repo string) (GitHubCheckResult, error)
}

// RealGitHubChecker implements GitHubChecker using the go-github library.
type RealGitHubChecker struct {
	client *github.Client
}

// NewGitHubChecker creates a new GitHubChecker with the provided OAuth token.
// If token is empty, creates an unauthenticated client (limited to 60 req/hour).
func NewGitHubChecker(token string) *RealGitHubChecker {
	var client *github.Client
	if token != "" {
		client = github.NewClient(nil).WithAuthToken(token)
	} else {
		client = github.NewClient(nil)
	}
	return &RealGitHubChecker{client: client}
}

// NewGitHubCheckerWithHTTPClient creates a GitHubChecker with a custom HTTP client.
// This is primarily used for testing with httptest servers.
func NewGitHubCheckerWithHTTPClient(httpClient *http.Client, baseURL string) *RealGitHubChecker {
	client := github.NewClient(httpClient)
	if baseURL != "" {
		// For testing, we need to set the base URL
		client, _ = client.WithEnterpriseURLs(baseURL, baseURL)
	}
	return &RealGitHubChecker{client: client}
}

// CheckRepo queries GitHub API to determine fork status and push permissions.
func (c *RealGitHubChecker) CheckRepo(ctx context.Context, owner, repo string) (GitHubCheckResult, error) {
	// Set a reasonable timeout for API calls
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Get repository information
	repoInfo, resp, err := c.client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		// Check for rate limiting
		if resp != nil && resp.StatusCode == http.StatusForbidden {
			if _, ok := err.(*github.RateLimitError); ok {
				log.Printf("WARNING: GitHub API rate limited, falling back to heuristic detection")
				return GitHubCheckResult{}, fmt.Errorf("rate limited: %w", err)
			}
			// Check for secondary rate limit (abuse detection)
			if _, ok := err.(*github.AbuseRateLimitError); ok {
				log.Printf("WARNING: GitHub API abuse rate limited, falling back to heuristic detection")
				return GitHubCheckResult{}, fmt.Errorf("abuse rate limited: %w", err)
			}
		}
		return GitHubCheckResult{}, fmt.Errorf("failed to get repository info: %w", err)
	}

	result := GitHubCheckResult{
		IsFork: repoInfo.GetFork(),
	}

	// Check permissions - this tells us if we can push
	// The Permissions field is only populated for authenticated requests
	if repoInfo.Permissions != nil {
		result.CanPush = repoInfo.Permissions["push"]
	}

	return result, nil
}

// ParseGitHubRemote extracts owner and repo from a GitHub remote URL.
// Supports both SSH and HTTPS formats:
//   - git@github.com:owner/repo.git
//   - https://github.com/owner/repo.git
//   - https://github.com/owner/repo
//
// Returns ("", "", error) if the URL is not a valid GitHub URL.
func ParseGitHubRemote(remoteURL string) (owner, repo string, err error) {
	remoteURL = strings.TrimSpace(remoteURL)

	// Handle SSH format: git@github.com:owner/repo.git
	if strings.HasPrefix(remoteURL, "git@github.com:") {
		path := strings.TrimPrefix(remoteURL, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("invalid GitHub SSH URL: %s", remoteURL)
		}
		return parts[0], parts[1], nil
	}

	// Handle HTTPS format: https://github.com/owner/repo.git
	if strings.HasPrefix(remoteURL, "https://github.com/") {
		path := strings.TrimPrefix(remoteURL, "https://github.com/")
		path = strings.TrimSuffix(path, ".git")
		// Remove any username/password from URL
		if atIdx := strings.Index(path, "@"); atIdx != -1 {
			path = path[atIdx+1:]
		}
		parts := strings.SplitN(path, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("invalid GitHub HTTPS URL: %s", remoteURL)
		}
		return parts[0], parts[1], nil
	}

	// Handle HTTP format (rare but possible)
	if strings.HasPrefix(remoteURL, "http://github.com/") {
		path := strings.TrimPrefix(remoteURL, "http://github.com/")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("invalid GitHub HTTP URL: %s", remoteURL)
		}
		return parts[0], parts[1], nil
	}

	return "", "", fmt.Errorf("not a GitHub URL: %s", remoteURL)
}
