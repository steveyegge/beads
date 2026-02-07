// Package slackbot provides channel routing for per-agent Slack notifications.
package slackbot

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Config represents Slack integration configuration.
type Config struct {
	Type    string `json:"type"`    // "slack"
	Version int    `json:"version"` // schema version

	// Enabled controls whether Slack notifications are active.
	Enabled bool `json:"enabled"`

	// RoutingMode controls the primary routing strategy.
	// Values: "epic" (route by parent epic), "agent" (route by agent identity),
	// "general" (all to default channel). Default is "general".
	RoutingMode string `json:"routing_mode,omitempty"`

	// DynamicChannels enables automatic Slack channel creation when a
	// matching channel does not yet exist.
	DynamicChannels bool `json:"dynamic_channels,omitempty"`

	// ChannelPrefix is prepended to dynamically created channel names.
	// Default: "bd-decisions".
	ChannelPrefix string `json:"channel_prefix,omitempty"`

	// DefaultChannel is the fallback channel when no pattern matches.
	// Format: channel ID (e.g., "C0123456789") or name (e.g., "#decisions")
	DefaultChannel string `json:"default_channel"`

	// Channels maps agent patterns to Slack channel IDs.
	// Patterns support wildcards: "*" matches any single segment.
	// Examples:
	//   "gastown/polecats/*"  → all polecats in gastown
	//   "*/crew/*"            → all crew members across rigs
	//   "beads/*"             → all agents in beads rig
	Channels map[string]string `json:"channels"`

	// Overrides maps exact agent identities to dedicated channel IDs.
	// These take precedence over pattern matching. Created via "Break Out" button.
	// Example: "gastown/crew/slack_decisions" → "C0987654321"
	Overrides map[string]string `json:"overrides,omitempty"`

	// ChannelNames maps channel IDs to human-readable names for display.
	// Optional; used for logging and debugging.
	ChannelNames map[string]string `json:"channel_names,omitempty"`

	// WebhookURL is the default webhook for posting messages.
	// Individual channels may have their own webhooks in ChannelWebhooks.
	WebhookURL string `json:"webhook_url,omitempty"`

	// ChannelWebhooks maps channel IDs to their webhook URLs.
	// If a channel has no entry, uses WebhookURL.
	ChannelWebhooks map[string]string `json:"channel_webhooks,omitempty"`
}

// DefaultChannelPrefix is the default prefix for dynamically created channels.
const DefaultChannelPrefix = "bd-decisions"

// ChannelCreator is an interface for looking up and creating Slack channels.
// It is implemented by the Bot and can be stubbed for testing.
type ChannelCreator interface {
	// FindChannelByName looks up an existing Slack channel by name.
	// Returns the channel ID, or empty string if not found.
	FindChannelByName(name string) (string, error)
	// CreateChannel creates a new Slack channel with the given name.
	// Returns the channel ID.
	CreateChannel(name string) (string, error)
}

// Router resolves Slack channels for agent identities.
type Router struct {
	config      *Config
	configPath  string // Path to config file for saving overrides
	beadsBacked bool   // True if config loaded from beads (not file)
	patterns    []compiledPattern
	mu          sync.RWMutex

	// channelCreator is used for dynamic channel creation (set via SetChannelCreator).
	channelCreator ChannelCreator
	// channelCache maps derived channel names to Slack channel IDs.
	channelCache   map[string]string
	channelCacheMu sync.RWMutex
}

// compiledPattern is a pre-processed pattern for faster matching.
type compiledPattern struct {
	original string   // Original pattern string
	segments []string // Pattern split by "/"
	channel  string   // Target channel ID
}

// RouteResult contains the resolved channel information.
type RouteResult struct {
	ChannelID   string // Slack channel ID (e.g., "C0123456789")
	ChannelName string // Human-readable name if available
	WebhookURL  string // Webhook URL for this channel
	MatchedBy   string // Pattern that matched (for debugging)
	IsDefault   bool   // True if using default channel
}

// NewRouter creates a new channel router from config.
func NewRouter(cfg *Config) *Router {
	if cfg == nil {
		cfg = &Config{
			Type:    "slack",
			Version: 1,
		}
	}
	r := &Router{
		config:       cfg,
		channelCache: make(map[string]string),
	}
	r.compilePatterns()
	return r
}

// SetChannelCreator sets the ChannelCreator used for dynamic channel operations.
// This is called by the Bot after initialisation.
func (r *Router) SetChannelCreator(cc ChannelCreator) {
	r.channelCreator = cc
}

// channelPrefix returns the configured channel prefix or the default.
func (r *Router) channelPrefix() string {
	if r.config.ChannelPrefix != "" {
		return r.config.ChannelPrefix
	}
	return DefaultChannelPrefix
}

// LoadRouter loads router configuration with the following priority:
//  1. Config bead (hq-cfg-slack-routing)
//  2. Beads config namespace (bd config slack.*)
//  3. File ($BEADS_DIR/settings/slack.json)
func LoadRouter() (*Router, error) {
	// Try config bead first (formal config beads system)
	if router, err := LoadRouterFromConfigBead(); err == nil {
		return router, nil
	}

	// Try legacy beads config namespace
	if router, err := LoadRouterFromBeads(); err == nil {
		return router, nil
	}

	// Fall back to file-based config
	configPath, err := findConfigPath()
	if err != nil {
		return nil, err
	}

	return LoadRouterFromFile(configPath)
}

// LoadRouterFromBeads loads router configuration from beads config namespace.
// Uses bd config get slack.* to retrieve configuration values.
func LoadRouterFromBeads() (*Router, error) {
	cfg := &Config{
		Type:         "slack",
		Version:      1,
		Channels:     make(map[string]string),
		Overrides:    make(map[string]string),
		ChannelNames: make(map[string]string),
	}

	// Check if slack config exists in beads
	enabled, err := bdConfigGet("slack.enabled")
	if err != nil || enabled == "" {
		return nil, fmt.Errorf("slack config not found in beads")
	}
	cfg.Enabled = enabled == "true"

	// Get default channel
	if defaultChannel, err := bdConfigGet("slack.default_channel"); err == nil && defaultChannel != "" {
		cfg.DefaultChannel = defaultChannel
	}

	// Get routing mode
	if routingMode, err := bdConfigGet("slack.routing_mode"); err == nil && routingMode != "" {
		cfg.RoutingMode = routingMode
	}

	// Get dynamic channels flag
	if dynCh, err := bdConfigGet("slack.dynamic_channels"); err == nil && dynCh == "true" {
		cfg.DynamicChannels = true
	}

	// Get channel prefix
	if prefix, err := bdConfigGet("slack.channel_prefix"); err == nil && prefix != "" {
		cfg.ChannelPrefix = prefix
	}

	// Get channel patterns (JSON map)
	if channelsJSON, err := bdConfigGet("slack.channels"); err == nil && channelsJSON != "" {
		if err := json.Unmarshal([]byte(channelsJSON), &cfg.Channels); err != nil {
			log.Printf("slack: failed to parse slack.channels: %v", err)
		}
	}

	// Get overrides (JSON map)
	if overridesJSON, err := bdConfigGet("slack.overrides"); err == nil && overridesJSON != "" {
		if err := json.Unmarshal([]byte(overridesJSON), &cfg.Overrides); err != nil {
			log.Printf("slack: failed to parse slack.overrides: %v", err)
		}
	}

	// Get channel names (JSON map)
	if namesJSON, err := bdConfigGet("slack.channel_names"); err == nil && namesJSON != "" {
		if err := json.Unmarshal([]byte(namesJSON), &cfg.ChannelNames); err != nil {
			log.Printf("slack: failed to parse slack.channel_names: %v", err)
		}
	}

	r := NewRouter(cfg)
	r.beadsBacked = true
	return r, nil
}

// LoadRouterFromConfigBead loads router configuration from the hq-cfg-slack-routing
// config bead. This is the formal config beads approach (vs the legacy bd config get).
// Returns error if the config bead doesn't exist or can't be parsed.
func LoadRouterFromConfigBead() (*Router, error) {
	// Use bd show to retrieve the config bead
	cmd := exec.Command("bd", "show", "hq-cfg-slack-routing", "--json", "--quiet")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("config bead hq-cfg-slack-routing not found")
	}

	// Parse the bead JSON to extract the description
	var issue struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(output, &issue); err != nil {
		return nil, fmt.Errorf("parsing config bead JSON: %w", err)
	}

	// Extract metadata from the description (format: "metadata: {json}")
	metadata := extractMetadataFromDescription(issue.Description)
	if metadata == "" {
		return nil, fmt.Errorf("no metadata found in config bead description")
	}

	cfg := &Config{
		Channels:     make(map[string]string),
		Overrides:    make(map[string]string),
		ChannelNames: make(map[string]string),
	}
	if err := json.Unmarshal([]byte(metadata), cfg); err != nil {
		return nil, fmt.Errorf("parsing config bead metadata: %w", err)
	}

	r := NewRouter(cfg)
	r.beadsBacked = true
	return r, nil
}

// extractMetadataFromDescription extracts the metadata JSON from a config bead description.
// The description format has "metadata: {json}" as the last line.
func extractMetadataFromDescription(description string) string {
	for _, line := range strings.Split(description, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "metadata: ") {
			return strings.TrimPrefix(line, "metadata: ")
		}
	}
	return ""
}

// bdConfigGet retrieves a value from beads config using bd CLI.
// Returns empty string if key is not set.
func bdConfigGet(key string) (string, error) {
	cmd := exec.Command("bd", "config", "get", key, "--quiet")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(output))
	// bd config get returns "key (not set)" when key doesn't exist
	if strings.HasSuffix(value, "(not set)") {
		return "", nil
	}
	return value, nil
}

// bdConfigSet sets a value in beads config using bd CLI.
func bdConfigSet(key, value string) error {
	cmd := exec.Command("bd", "config", "set", key, value)
	return cmd.Run()
}

// LoadRouterFromFile loads router configuration from a specific file.
func LoadRouterFromFile(path string) (*Router, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	r := NewRouter(&cfg)
	r.configPath = path
	return r, nil
}

// findConfigPath locates the Slack config file.
func findConfigPath() (string, error) {
	// Check BEADS_DIR environment variable first
	if beadsDir := os.Getenv("BEADS_DIR"); beadsDir != "" {
		path := filepath.Join(beadsDir, "settings", "slack.json")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Check ~/beads/settings/slack.json
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	path := filepath.Join(home, "beads", "settings", "slack.json")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	// Check current town's settings (for multi-town setups)
	// Walk up from cwd looking for .beads directory indicating town root
	cwd, err := os.Getwd()
	if err == nil {
		for dir := cwd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
			beadsDir := filepath.Join(dir, ".beads")
			if _, err := os.Stat(beadsDir); err == nil {
				path := filepath.Join(dir, "settings", "slack.json")
				if _, err := os.Stat(path); err == nil {
					return path, nil
				}
				break
			}
		}
	}

	return "", fmt.Errorf("slack config not found (checked $BEADS_DIR/settings/slack.json, ~/beads/settings/slack.json)")
}

// compilePatterns pre-processes patterns for efficient matching.
func (r *Router) compilePatterns() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.patterns = make([]compiledPattern, 0, len(r.config.Channels))

	for pattern, channel := range r.config.Channels {
		r.patterns = append(r.patterns, compiledPattern{
			original: pattern,
			segments: strings.Split(pattern, "/"),
			channel:  channel,
		})
	}

	// Sort patterns by specificity (more segments = more specific = higher priority)
	// Also prioritize patterns without wildcards
	for i := 0; i < len(r.patterns); i++ {
		for j := i + 1; j < len(r.patterns); j++ {
			if patternLessThan(r.patterns[j], r.patterns[i]) {
				r.patterns[i], r.patterns[j] = r.patterns[j], r.patterns[i]
			}
		}
	}
}

// patternLessThan returns true if a should be matched before b (higher priority).
func patternLessThan(a, b compiledPattern) bool {
	// More segments = more specific = higher priority
	if len(a.segments) != len(b.segments) {
		return len(a.segments) > len(b.segments)
	}

	// Fewer wildcards = more specific = higher priority
	aWildcards := countWildcards(a.segments)
	bWildcards := countWildcards(b.segments)
	if aWildcards != bWildcards {
		return aWildcards < bWildcards
	}

	// Tie-breaker: alphabetical order for determinism
	return a.original < b.original
}

// countWildcards counts "*" segments in a pattern.
func countWildcards(segments []string) int {
	count := 0
	for _, s := range segments {
		if s == "*" {
			count++
		}
	}
	return count
}

// Resolve finds the appropriate Slack channel for an agent identity.
// Agent format: "rig/role/name" (e.g., "gastown/polecats/furiosa")
// Priority: 1. Exact override, 2. Pattern match, 3. Default channel
func (r *Router) Resolve(agent string) *RouteResult {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check overrides first (exact agent match, highest priority)
	if r.config.Overrides != nil {
		if channelID, ok := r.config.Overrides[agent]; ok {
			return &RouteResult{
				ChannelID:   channelID,
				ChannelName: r.config.ChannelNames[channelID],
				WebhookURL:  r.getWebhookForChannel(channelID),
				MatchedBy:   "(override)",
				IsDefault:   false,
			}
		}
	}

	agentSegments := strings.Split(agent, "/")

	// Try each pattern in priority order
	for _, p := range r.patterns {
		if matchPattern(p.segments, agentSegments) {
			return &RouteResult{
				ChannelID:   p.channel,
				ChannelName: r.config.ChannelNames[p.channel],
				WebhookURL:  r.getWebhookForChannel(p.channel),
				MatchedBy:   p.original,
				IsDefault:   false,
			}
		}
	}

	// Fall back to default channel
	return &RouteResult{
		ChannelID:   r.config.DefaultChannel,
		ChannelName: r.config.ChannelNames[r.config.DefaultChannel],
		WebhookURL:  r.getWebhookForChannel(r.config.DefaultChannel),
		MatchedBy:   "(default)",
		IsDefault:   true,
	}
}

// matchPattern checks if an agent matches a pattern.
// Pattern segments can be literal strings or "*" for wildcard.
func matchPattern(pattern, agent []string) bool {
	if len(pattern) != len(agent) {
		return false
	}

	for i, p := range pattern {
		if p != "*" && p != agent[i] {
			return false
		}
	}

	return true
}

// getWebhookForChannel returns the webhook URL for a channel.
func (r *Router) getWebhookForChannel(channelID string) string {
	if r.config.ChannelWebhooks != nil {
		if webhook, ok := r.config.ChannelWebhooks[channelID]; ok {
			return webhook
		}
	}
	return r.config.WebhookURL
}

// IsEnabled returns whether Slack notifications are enabled.
func (r *Router) IsEnabled() bool {
	return r.config.Enabled
}

// GetConfig returns the underlying configuration (for debugging).
func (r *Router) GetConfig() *Config {
	return r.config
}

// ResolveAll resolves channels for multiple agents, returning unique channels.
// Useful for notifications that should go to multiple channels.
func (r *Router) ResolveAll(agents []string) []*RouteResult {
	seen := make(map[string]bool)
	var results []*RouteResult

	for _, agent := range agents {
		result := r.Resolve(agent)
		if !seen[result.ChannelID] {
			seen[result.ChannelID] = true
			results = append(results, result)
		}
	}

	return results
}

// ResolveForDecision determines the appropriate channel for a decision.
// Resolution order:
//  1. Decision-level override (explicit channel hint in channelHint)
//  2. Exact agent override (break-out channel)
//  3. Pattern match (wildcard patterns sorted by specificity)
//  4. Epic routing (if decision has a ParentBeadTitle and routing_mode is "epic" or unset)
//  5. Default channel
//
// For steps 4, dynamic channel creation is attempted when DynamicChannels is
// enabled and a ChannelCreator has been set.
func (r *Router) ResolveForDecision(agent string, decision *Decision, channelHint string) *RouteResult {
	// Priority 1: Explicit channel hint (decision-level override)
	if channelHint != "" {
		return &RouteResult{
			ChannelID:   channelHint,
			ChannelName: r.channelName(channelHint),
			WebhookURL:  r.getWebhookForChannel(channelHint),
			MatchedBy:   "(decision-hint)",
		}
	}

	// Priority 2 & 3: Exact override then pattern match
	r.mu.RLock()
	if agent != "" && r.config.Overrides != nil {
		if channelID, ok := r.config.Overrides[agent]; ok {
			r.mu.RUnlock()
			return &RouteResult{
				ChannelID:   channelID,
				ChannelName: r.channelName(channelID),
				WebhookURL:  r.getWebhookForChannel(channelID),
				MatchedBy:   "(override)",
			}
		}
	}

	if agent != "" {
		agentSegments := strings.Split(agent, "/")
		for _, p := range r.patterns {
			if matchPattern(p.segments, agentSegments) {
				r.mu.RUnlock()
				return &RouteResult{
					ChannelID:   p.channel,
					ChannelName: r.channelName(p.channel),
					WebhookURL:  r.getWebhookForChannel(p.channel),
					MatchedBy:   p.original,
				}
			}
		}
	}
	r.mu.RUnlock()

	// Priority 4: Epic-based routing
	if decision != nil && decision.ParentBeadTitle != "" {
		mode := r.config.RoutingMode
		if mode == "" || mode == "epic" {
			channelID := r.ensureEpicChannel(decision.ParentBeadTitle)
			if channelID != "" {
				return &RouteResult{
					ChannelID:   channelID,
					ChannelName: r.channelName(channelID),
					WebhookURL:  r.getWebhookForChannel(channelID),
					MatchedBy:   "(epic:" + decision.ParentBeadTitle + ")",
				}
			}
		}
	}

	// Priority 5: Default channel
	return &RouteResult{
		ChannelID:   r.config.DefaultChannel,
		ChannelName: r.channelName(r.config.DefaultChannel),
		WebhookURL:  r.getWebhookForChannel(r.config.DefaultChannel),
		MatchedBy:   "(default)",
		IsDefault:   true,
	}
}

// channelName returns the human-readable name for a channel ID, or the ID itself.
func (r *Router) channelName(channelID string) string {
	if r.config.ChannelNames != nil {
		if name, ok := r.config.ChannelNames[channelID]; ok {
			return name
		}
	}
	return channelID
}

// ensureEpicChannel looks up or creates a channel for the given epic title.
// Returns the channel ID, or empty string if the channel cannot be resolved.
func (r *Router) ensureEpicChannel(epicTitle string) string {
	slug := DeriveChannelSlug(epicTitle)
	if slug == "" {
		return ""
	}

	channelName := sanitizeChannelName(r.channelPrefix() + "-" + slug)

	// Check cache
	r.channelCacheMu.RLock()
	if id, ok := r.channelCache[channelName]; ok {
		r.channelCacheMu.RUnlock()
		return id
	}
	r.channelCacheMu.RUnlock()

	if r.channelCreator == nil {
		return ""
	}

	// Try to find existing channel
	channelID, err := r.channelCreator.FindChannelByName(channelName)
	if err == nil && channelID != "" {
		r.cacheChannel(channelName, channelID)
		return channelID
	}

	// Create if dynamic channels enabled
	if !r.config.DynamicChannels {
		return ""
	}

	channelID, err = r.channelCreator.CreateChannel(channelName)
	if err != nil {
		log.Printf("slack: failed to create epic channel %s: %v", channelName, err)
		// If name_taken, try finding again
		if strings.Contains(err.Error(), "name_taken") {
			channelID, err = r.channelCreator.FindChannelByName(channelName)
			if err == nil && channelID != "" {
				r.cacheChannel(channelName, channelID)
				return channelID
			}
		}
		return ""
	}

	r.cacheChannel(channelName, channelID)
	log.Printf("slack: created epic channel #%s (%s) for %q", channelName, channelID, epicTitle)
	return channelID
}

// EnsureAgentChannel looks up or creates a channel for the given agent identity.
// Returns the channel ID, or empty string if the channel cannot be resolved.
func (r *Router) EnsureAgentChannel(agent string) string {
	channelName := AgentToChannelName(agent, r.channelPrefix())

	// Check cache
	r.channelCacheMu.RLock()
	if id, ok := r.channelCache[channelName]; ok {
		r.channelCacheMu.RUnlock()
		return id
	}
	r.channelCacheMu.RUnlock()

	if r.channelCreator == nil {
		return ""
	}

	// Try to find existing channel
	channelID, err := r.channelCreator.FindChannelByName(channelName)
	if err == nil && channelID != "" {
		r.cacheChannel(channelName, channelID)
		return channelID
	}

	// Create if dynamic channels enabled
	if !r.config.DynamicChannels {
		return ""
	}

	channelID, err = r.channelCreator.CreateChannel(channelName)
	if err != nil {
		log.Printf("slack: failed to create agent channel %s: %v", channelName, err)
		if strings.Contains(err.Error(), "name_taken") {
			channelID, err = r.channelCreator.FindChannelByName(channelName)
			if err == nil && channelID != "" {
				r.cacheChannel(channelName, channelID)
				return channelID
			}
		}
		return ""
	}

	r.cacheChannel(channelName, channelID)
	log.Printf("slack: created agent channel #%s (%s) for %s", channelName, channelID, agent)
	return channelID
}

// cacheChannel stores a channel name to ID mapping in the cache.
func (r *Router) cacheChannel(name, id string) {
	r.channelCacheMu.Lock()
	r.channelCache[name] = id
	r.channelCacheMu.Unlock()
}

// DeriveChannelSlug derives a Slack-safe slug from a title.
// Lowercases, replaces non-alphanumeric with hyphens, collapses runs,
// trims, and truncates to 30 characters at a word boundary.
func DeriveChannelSlug(title string) string {
	return DeriveChannelSlugWithMaxLen(title, 30)
}

// DeriveChannelSlugWithMaxLen derives a channel slug with a custom max length.
func DeriveChannelSlugWithMaxLen(title string, maxLen int) string {
	if title == "" {
		return ""
	}

	slug := strings.ToLower(title)

	// Replace non-alphanumeric characters with hyphens
	var result []rune
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			result = append(result, r)
		} else {
			result = append(result, '-')
		}
	}
	slug = string(result)

	// Collapse consecutive hyphens
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}

	// Strip leading/trailing hyphens
	slug = strings.Trim(slug, "-")

	// Truncate at word boundary
	if len(slug) > maxLen {
		truncated := slug[:maxLen]
		if idx := strings.LastIndex(truncated, "-"); idx > maxLen/2 {
			truncated = truncated[:idx]
		}
		slug = strings.TrimRight(truncated, "-")
	}

	return slug
}

// AgentToChannelName converts an agent identity to a Slack channel name.
// Uses rig and role segments (drops the agent name) for grouping.
// Examples:
//
//	"gastown/polecats/furiosa" + "bd-decisions" → "bd-decisions-gastown-polecats"
//	"mayor" + "bd-decisions" → "bd-decisions-mayor"
func AgentToChannelName(agent, prefix string) string {
	parts := strings.Split(agent, "/")

	// Use rig and role, drop the agent name for grouping
	var nameParts []string
	if len(parts) >= 2 {
		nameParts = parts[:2]
	} else {
		nameParts = parts
	}

	name := prefix + "-" + strings.Join(nameParts, "-")
	return sanitizeChannelName(name)
}

// sanitizeChannelName ensures a channel name is valid for Slack.
// Lowercase, only a-z 0-9 hyphens, max 80 chars.
func sanitizeChannelName(name string) string {
	name = strings.ToLower(name)

	var result []rune
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result = append(result, r)
		} else {
			result = append(result, '-')
		}
	}
	name = string(result)

	// Collapse consecutive hyphens
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")

	if len(name) > 80 {
		name = name[:80]
		name = strings.TrimRight(name, "-")
	}

	return name
}

// HasOverride returns true if the agent has a dedicated channel override.
func (r *Router) HasOverride(agent string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.config.Overrides == nil {
		return false
	}
	_, ok := r.config.Overrides[agent]
	return ok
}

// GetOverride returns the override channel ID for an agent, or empty string if none.
func (r *Router) GetOverride(agent string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.config.Overrides == nil {
		return ""
	}
	return r.config.Overrides[agent]
}

// GetAgentByChannel returns the agent address for a channel ID (reverse lookup).
// Returns empty string if no agent is mapped to this channel.
// This is used for forwarding messages from agent-specific channels to the agent.
func (r *Router) GetAgentByChannel(channelID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.config.Overrides == nil {
		return ""
	}
	for agent, ch := range r.config.Overrides {
		if ch == channelID {
			return agent
		}
	}
	return ""
}

// AddOverride sets a dedicated channel override for an agent.
// The override takes precedence over pattern matching.
func (r *Router) AddOverride(agent, channelID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.config.Overrides == nil {
		r.config.Overrides = make(map[string]string)
	}
	r.config.Overrides[agent] = channelID

	// Also add to channel names if we know the name
	if r.config.ChannelNames == nil {
		r.config.ChannelNames = make(map[string]string)
	}
}

// AddOverrideWithName sets a dedicated channel override and records the channel name.
func (r *Router) AddOverrideWithName(agent, channelID, channelName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.config.Overrides == nil {
		r.config.Overrides = make(map[string]string)
	}
	r.config.Overrides[agent] = channelID

	if r.config.ChannelNames == nil {
		r.config.ChannelNames = make(map[string]string)
	}
	r.config.ChannelNames[channelID] = channelName
}

// RemoveOverride removes a dedicated channel override for an agent.
// Returns the previous channel ID if one existed, empty string otherwise.
func (r *Router) RemoveOverride(agent string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.config.Overrides == nil {
		return ""
	}
	prev := r.config.Overrides[agent]
	delete(r.config.Overrides, agent)
	return prev
}

// Save persists the current configuration to beads or file.
// If loaded from beads, saves to beads. Otherwise saves to config file.
// Uses atomic write for file-based storage to prevent data corruption.
func (r *Router) Save() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.beadsBacked {
		return r.saveToBeadsLocked()
	}

	return r.saveToFileLocked()
}

// saveToBeadsLocked saves config to beads (must hold lock).
func (r *Router) saveToBeadsLocked() error {
	// Save enabled flag
	enabledStr := "false"
	if r.config.Enabled {
		enabledStr = "true"
	}
	if err := bdConfigSet("slack.enabled", enabledStr); err != nil {
		return fmt.Errorf("save slack.enabled: %w", err)
	}

	// Save default channel
	if err := bdConfigSet("slack.default_channel", r.config.DefaultChannel); err != nil {
		return fmt.Errorf("save slack.default_channel: %w", err)
	}

	// Save routing mode
	if r.config.RoutingMode != "" {
		if err := bdConfigSet("slack.routing_mode", r.config.RoutingMode); err != nil {
			return fmt.Errorf("save slack.routing_mode: %w", err)
		}
	}

	// Save dynamic channels flag
	dynStr := "false"
	if r.config.DynamicChannels {
		dynStr = "true"
	}
	if err := bdConfigSet("slack.dynamic_channels", dynStr); err != nil {
		return fmt.Errorf("save slack.dynamic_channels: %w", err)
	}

	// Save channel prefix
	if r.config.ChannelPrefix != "" {
		if err := bdConfigSet("slack.channel_prefix", r.config.ChannelPrefix); err != nil {
			return fmt.Errorf("save slack.channel_prefix: %w", err)
		}
	}

	// Save channels (JSON)
	if len(r.config.Channels) > 0 {
		channelsJSON, err := json.Marshal(r.config.Channels)
		if err != nil {
			return fmt.Errorf("marshal channels: %w", err)
		}
		if err := bdConfigSet("slack.channels", string(channelsJSON)); err != nil {
			return fmt.Errorf("save slack.channels: %w", err)
		}
	}

	// Save overrides (JSON)
	if len(r.config.Overrides) > 0 {
		overridesJSON, err := json.Marshal(r.config.Overrides)
		if err != nil {
			return fmt.Errorf("marshal overrides: %w", err)
		}
		if err := bdConfigSet("slack.overrides", string(overridesJSON)); err != nil {
			return fmt.Errorf("save slack.overrides: %w", err)
		}
	}

	// Save channel names (JSON)
	if len(r.config.ChannelNames) > 0 {
		namesJSON, err := json.Marshal(r.config.ChannelNames)
		if err != nil {
			return fmt.Errorf("marshal channel_names: %w", err)
		}
		if err := bdConfigSet("slack.channel_names", string(namesJSON)); err != nil {
			return fmt.Errorf("save slack.channel_names: %w", err)
		}
	}

	return nil
}

// saveToFileLocked saves config to file (must hold lock).
func (r *Router) saveToFileLocked() error {
	if r.configPath == "" {
		return fmt.Errorf("no config path: router was not loaded from file")
	}

	data, err := json.MarshalIndent(r.config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Write to temp file first, then rename for atomicity
	tmpPath := r.configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}

	if err := os.Rename(tmpPath, r.configPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename config: %w", err)
	}

	return nil
}

// IsBeadsBacked returns true if this router is backed by beads config.
func (r *Router) IsBeadsBacked() bool {
	return r.beadsBacked
}

// MigrateToBeads migrates the current file-based config to beads.
// After migration, the router switches to beads-backed mode.
func (r *Router) MigrateToBeads() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.beadsBacked {
		return fmt.Errorf("already backed by beads")
	}

	// Save to beads
	if err := r.saveToBeadsLocked(); err != nil {
		return fmt.Errorf("migrate to beads: %w", err)
	}

	// Switch to beads-backed mode
	r.beadsBacked = true
	r.configPath = ""

	return nil
}

// ChannelMode represents an agent's preferred channel routing mode.
type ChannelMode string

const (
	// ChannelModeGeneral routes to the default/general channel.
	ChannelModeGeneral ChannelMode = "general"
	// ChannelModeAgent routes to a dedicated per-agent channel.
	ChannelModeAgent ChannelMode = "agent"
	// ChannelModeEpic routes to a channel based on the work's parent epic.
	ChannelModeEpic ChannelMode = "epic"
	// ChannelModeDM routes to a direct message with the overseer.
	ChannelModeDM ChannelMode = "dm"
)

// ValidChannelModes is the list of valid channel mode values.
var ValidChannelModes = []ChannelMode{
	ChannelModeGeneral,
	ChannelModeAgent,
	ChannelModeEpic,
	ChannelModeDM,
}

// IsValidChannelMode checks if a string is a valid channel mode.
func IsValidChannelMode(mode string) bool {
	for _, m := range ValidChannelModes {
		if string(m) == mode {
			return true
		}
	}
	return false
}

// normalizeAgent normalizes agent names for consistent config key lookup.
// Trims trailing slashes to ensure "mayor/" and "mayor" map to the same key.
func normalizeAgent(agent string) string {
	return strings.TrimRight(agent, "/")
}

// GetAgentChannelMode retrieves the channel mode preference for an agent.
// Returns empty string if no preference is set.
// Agent format: "rig/role/name" (e.g., "gastown/polecats/furiosa")
func GetAgentChannelMode(agent string) (ChannelMode, error) {
	// Normalize agent name for config key (trim trailing slashes, replace / with .)
	normalized := normalizeAgent(agent)
	key := "slack.channel_mode." + strings.ReplaceAll(normalized, "/", ".")
	value, err := bdConfigGet(key)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", nil
	}
	return ChannelMode(value), nil
}

// SetAgentChannelMode sets the channel mode preference for an agent.
// Agent format: "rig/role/name" (e.g., "gastown/polecats/furiosa")
func SetAgentChannelMode(agent string, mode ChannelMode) error {
	if !IsValidChannelMode(string(mode)) {
		return fmt.Errorf("invalid channel mode %q: must be one of %v", mode, ValidChannelModes)
	}
	normalized := normalizeAgent(agent)
	key := "slack.channel_mode." + strings.ReplaceAll(normalized, "/", ".")
	return bdConfigSet(key, string(mode))
}

// ClearAgentChannelMode removes the channel mode preference for an agent.
func ClearAgentChannelMode(agent string) error {
	normalized := normalizeAgent(agent)
	key := "slack.channel_mode." + strings.ReplaceAll(normalized, "/", ".")
	// bd config set with empty value effectively unsets
	return bdConfigSet(key, "")
}

// GetDefaultChannelMode returns the default channel mode for all agents.
// Returns empty string if no default is set.
func GetDefaultChannelMode() (ChannelMode, error) {
	value, err := bdConfigGet("slack.default_channel_mode")
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", nil
	}
	return ChannelMode(value), nil
}

// SetDefaultChannelMode sets the default channel mode for all agents.
func SetDefaultChannelMode(mode ChannelMode) error {
	if !IsValidChannelMode(string(mode)) {
		return fmt.Errorf("invalid channel mode %q: must be one of %v", mode, ValidChannelModes)
	}
	return bdConfigSet("slack.default_channel_mode", string(mode))
}
