package main

// Category represents a script category
type Category string

const (
	CategoryAgents     Category = "agents"
	CategoryHooks      Category = "hooks"
	CategoryCompaction Category = "compaction"
)

// DryRunMode represents how a script handles dry-run
type DryRunMode string

const (
	DryRunSafe      DryRunMode = "safe"      // Script is read-only, no wrapping needed
	DryRunIntercept DryRunMode = "intercept" // Wrap bd commands to prevent state changes
	DryRunNative    DryRunMode = "native"    // Script has --dry-run flag
	DryRunBlock     DryRunMode = "block"     // Block execution without --execute flag
)

// Script represents metadata about an example script
type Script struct {
	Path          string     // Relative path from examples/ directory
	Category      Category   // Category grouping
	Description   string     // Short description
	Prerequisites []string   // Required commands/env vars: "bd", "jq", "ANTHROPIC_API_KEY"
	DryRunMode    DryRunMode // How to handle dry-run
	Interactive   bool       // Requires interactive input
	DefaultArgs   []string   // Default arguments to pass
}

// Registry contains all known example scripts
var Registry = []Script{
	// bash-agent
	{
		Path:          "bash-agent/agent.sh",
		Category:      CategoryAgents,
		Description:   "Autonomous task executor that loops through ready work",
		Prerequisites: []string{"bd", "jq", ".beads"},
		DryRunMode:    DryRunIntercept,
		Interactive:   false,
	},

	// startup-hooks
	{
		Path:          "startup-hooks/bd-version-check.sh",
		Category:      CategoryHooks,
		Description:   "Detect bd upgrades between sessions and show changelog",
		Prerequisites: []string{"bd"},
		DryRunMode:    DryRunSafe,
		Interactive:   false,
	},
	{
		Path:          "startup-hooks/test-version-check.sh",
		Category:      CategoryHooks,
		Description:   "Test suite for the version check script",
		Prerequisites: []string{"bd", "jq", ".beads"},
		DryRunMode:    DryRunIntercept, // Modifies .beads/metadata.json
		Interactive:   false,
	},

	// compaction
	{
		Path:          "compaction/auto-compact.sh",
		Category:      CategoryCompaction,
		Description:   "Smart threshold-based compaction for CI/CD",
		Prerequisites: []string{"bd", "jq", "ANTHROPIC_API_KEY"},
		DryRunMode:    DryRunNative,
		Interactive:   false,
		DefaultArgs:   []string{"--dry-run"},
	},
	{
		Path:          "compaction/workflow.sh",
		Category:      CategoryCompaction,
		Description:   "Interactive manual compaction with user prompts",
		Prerequisites: []string{"bd", "ANTHROPIC_API_KEY"},
		DryRunMode:    DryRunBlock,
		Interactive:   true,
	},
	{
		Path:          "compaction/cron-compact.sh",
		Category:      CategoryCompaction,
		Description:   "Fully automated compaction with git operations",
		Prerequisites: []string{"bd", "jq", "ANTHROPIC_API_KEY", "git"},
		DryRunMode:    DryRunBlock, // Does git push - too dangerous
		Interactive:   false,
	},

	// git-hooks
	{
		Path:          "git-hooks/pre-commit",
		Category:      CategoryHooks,
		Description:   "Flush bd changes before commit",
		Prerequisites: []string{"bd"},
		DryRunMode:    DryRunSafe,
		Interactive:   false,
	},
	{
		Path:          "git-hooks/pre-push",
		Category:      CategoryHooks,
		Description:   "Block stale JSONL from being pushed",
		Prerequisites: []string{"bd"},
		DryRunMode:    DryRunSafe,
		Interactive:   true, // Can prompt for confirmation
	},
	{
		Path:          "git-hooks/post-merge",
		Category:      CategoryHooks,
		Description:   "Sync bd database after merge/pull",
		Prerequisites: []string{"bd"},
		DryRunMode:    DryRunSafe,
		Interactive:   false,
	},
	{
		Path:          "git-hooks/post-checkout",
		Category:      CategoryHooks,
		Description:   "Sync bd database after branch checkout",
		Prerequisites: []string{"bd"},
		DryRunMode:    DryRunSafe,
		Interactive:   false,
	},
}

// GetScriptsByCategory returns scripts filtered by category
func GetScriptsByCategory(cat Category) []Script {
	if cat == "" {
		return Registry
	}
	var result []Script
	for _, s := range Registry {
		if s.Category == cat {
			result = append(result, s)
		}
	}
	return result
}

// GetScript returns a script by path
func GetScript(path string) *Script {
	for i := range Registry {
		if Registry[i].Path == path {
			return &Registry[i]
		}
	}
	return nil
}

// GetScriptsByFolder returns scripts in a given folder
func GetScriptsByFolder(folder string) []Script {
	var result []Script
	for _, s := range Registry {
		// Check if path starts with folder
		if len(s.Path) > len(folder) && s.Path[:len(folder)] == folder {
			result = append(result, s)
		}
	}
	return result
}

// CategoryDescription returns a human-readable description of a category
func CategoryDescription(cat Category) string {
	switch cat {
	case CategoryAgents:
		return "Agent integration examples"
	case CategoryHooks:
		return "Git hooks and startup hooks"
	case CategoryCompaction:
		return "Compaction workflow scripts"
	default:
		return string(cat)
	}
}
