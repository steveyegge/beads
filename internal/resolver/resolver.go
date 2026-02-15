package resolver

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// Requirement describes what is needed from a resource
type Requirement struct {
	Type    string
	Tags    []string
	Profile string // "cheap", "performance", "balanced"
}

// Resolver selects the best resource for a given requirement
type Resolver interface {
	ResolveBest(resources []*types.Resource, req Requirement) *types.Resource
	ResolveAll(resources []*types.Resource, req Requirement) []*types.Resource
}

// StandardResolver implements the default matching logic
type StandardResolver struct{}

// NewStandardResolver creates a new StandardResolver
func NewStandardResolver() *StandardResolver {
	return &StandardResolver{}
}

// ResolveBest selects the single best resource
func (r *StandardResolver) ResolveBest(resources []*types.Resource, req Requirement) *types.Resource {
	matches := r.ResolveAll(resources, req)
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

// ResolveAll ranks all matching resources
func (r *StandardResolver) ResolveAll(resources []*types.Resource, req Requirement) []*types.Resource {
	type scoredResource struct {
		resource *types.Resource
		score    int
	}

	var candidates []scoredResource

	for _, res := range resources {
		// 1. Basic filtering by Type
		if req.Type != "" && res.Type != req.Type {
			continue
		}

		score := 0

		// 2. Tag Matching
		// +10 for each matching tag
		for _, reqTag := range req.Tags {
			for _, resTag := range res.Tags {
				if strings.EqualFold(reqTag, resTag) {
					score += 10
				}
			}
		}

		// 3. Profile Matching (Budget/Performance)
		// We need to look at the config to determine cost/capability
		// For now, use simple heuristics on the resource name/tags
		profileScore := r.scoreProfile(res, req.Profile)
		score += profileScore

		// Only include if it has at least some relevance (or if no specific tags requested)
		if len(req.Tags) > 0 && score == 0 && profileScore == 0 {
			// If tags were requested but none matched, and no profile match, maybe skip?
			// For now, let's include everything that matches Type, but sort by score.
		}

		candidates = append(candidates, scoredResource{resource: res, score: score})
	}

	// Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	result := make([]*types.Resource, len(candidates))
	for i, c := range candidates {
		result[i] = c.resource
	}

	return result
}

func (r *StandardResolver) scoreProfile(res *types.Resource, profile string) int {
	if profile == "" {
		return 0
	}

	// Parse config to find cost/performance attributes if available
	// This is a placeholder for real cost analysis
	var config map[string]interface{}
	if res.Config != "" {
		_ = json.Unmarshal([]byte(res.Config), &config)
	}

	// Heuristics based on name/tags
	isCheap := contains(res.Tags, "cheap") || strings.Contains(strings.ToLower(res.Name), "gpt-3.5") || strings.Contains(strings.ToLower(res.Name), "haiku")
	isPerformance := contains(res.Tags, "smart") || contains(res.Tags, "complex") || strings.Contains(strings.ToLower(res.Name), "gpt-4") || strings.Contains(strings.ToLower(res.Name), "opus")

	switch profile {
	case "cheap":
		if isCheap {
			return 20
		}
		if isPerformance {
			return -10 // Penalize expensive models if asking for cheap
		}
	case "performance":
		if isPerformance {
			return 20
		}
		if isCheap {
			return -5 // Slightly penalize weak models
		}
	}
	return 0
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, item) {
			return true
		}
	}
	return false
}