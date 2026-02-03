package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/steveyegge/beads/cmd/bd/doctor"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/syncbranch"
)

var configCmd = &cobra.Command{
	Use:     "config",
	GroupID: "setup",
	Short:   "Manage configuration settings",
	Long: `Manage configuration settings for external integrations and preferences.

Configuration is stored per-project in .beads/*.db and is version-control-friendly.

Common namespaces:
  - jira.*       Jira integration settings
  - linear.*     Linear integration settings
  - github.*     GitHub integration settings
  - custom.*     Custom integration settings
  - status.*     Issue status configuration

Custom Status States:
  You can define custom status states for multi-step pipelines using the
  status.custom config key. Statuses should be comma-separated.

  Example:
    bd config set status.custom "awaiting_review,awaiting_testing,awaiting_docs"

  This enables issues to use statuses like 'awaiting_review' in addition to
  the built-in statuses (open, in_progress, blocked, deferred, closed).

Examples:
  bd config set jira.url "https://company.atlassian.net"
  bd config set jira.project "PROJ"
  bd config set status.custom "awaiting_review,awaiting_testing"
  bd config get jira.url
  bd config list
  bd config unset jira.url`,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Args:  cobra.ExactArgs(2),
	Run: func(_ *cobra.Command, args []string) {
		key := args[0]
		value := args[1]

		// Check if this is a yaml-only key (startup settings like no-db, no-daemon, etc.)
		// These must be written to config.yaml, not SQLite, because they're read
		// before the database is opened. (GH#536)
		if config.IsYamlOnlyKey(key) {
			if err := config.SetYamlConfig(key, value); err != nil {
				fmt.Fprintf(os.Stderr, "Error setting config: %v\n", err)
				os.Exit(1)
			}

			if jsonOutput {
				outputJSON(map[string]interface{}{
					"key":      key,
					"value":    value,
					"location": "config.yaml",
				})
			} else {
				fmt.Printf("Set %s = %s (in config.yaml)\n", key, value)
			}
			return
		}

		// Use daemon RPC when available (bd-wmil)
		if daemonClient != nil {
			runConfigSetViaDaemon(key, value)
			return
		}

		// Fallback to direct store access
		if store == nil {
			fmt.Fprintf(os.Stderr, "Error: no database connection available\n")
			fmt.Fprintf(os.Stderr, "Hint: start the daemon with 'bd daemon start' or run in a beads workspace\n")
			os.Exit(1)
		}

		ctx := rootCtx

		// Special handling for sync.branch to apply validation
		if strings.TrimSpace(key) == syncbranch.ConfigKey {
			if err := syncbranch.Set(ctx, store, value); err != nil {
				fmt.Fprintf(os.Stderr, "Error setting config: %v\n", err)
				os.Exit(1)
			}
		} else {
			if err := store.SetConfig(ctx, key, value); err != nil {
				fmt.Fprintf(os.Stderr, "Error setting config: %v\n", err)
				os.Exit(1)
			}
		}

		if jsonOutput {
			outputJSON(map[string]string{
				"key":   key,
				"value": value,
			})
		} else {
			fmt.Printf("Set %s = %s\n", key, value)
		}
	},
}

// runConfigSetViaDaemon executes config set via daemon RPC (bd-wmil)
func runConfigSetViaDaemon(key, value string) {
	args := &rpc.ConfigSetArgs{
		Key:   key,
		Value: value,
	}

	result, err := daemonClient.ConfigSet(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		outputJSON(map[string]string{
			"key":   result.Key,
			"value": result.Value,
		})
	} else {
		fmt.Printf("Set %s = %s\n", result.Key, result.Value)
	}
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a configuration value",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]

		// Check if this is a yaml-only key (startup settings)
		// These are read from config.yaml via viper, not SQLite. (GH#536)
		if config.IsYamlOnlyKey(key) {
			value := config.GetYamlConfig(key)

			if jsonOutput {
				outputJSON(map[string]interface{}{
					"key":      key,
					"value":    value,
					"location": "config.yaml",
				})
			} else {
				if value == "" {
					fmt.Printf("%s (not set in config.yaml)\n", key)
				} else {
					fmt.Printf("%s\n", value)
				}
			}
			return
		}

		// Use daemon RPC when available (bd-wmil)
		if daemonClient != nil {
			runConfigGetViaDaemon(key)
			return
		}

		// Fallback to direct store access
		if store == nil {
			fmt.Fprintf(os.Stderr, "Error: no database connection available\n")
			fmt.Fprintf(os.Stderr, "Hint: start the daemon with 'bd daemon start' or run in a beads workspace\n")
			os.Exit(1)
		}

		ctx := rootCtx
		var value string
		var err error

		// Special handling for sync.branch to support env var override
		if strings.TrimSpace(key) == syncbranch.ConfigKey {
			value, err = syncbranch.Get(ctx, store)
		} else {
			value, err = store.GetConfig(ctx, key)
		}

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting config: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(map[string]string{
				"key":   key,
				"value": value,
			})
		} else {
			if value == "" {
				fmt.Printf("%s (not set)\n", key)
			} else {
				fmt.Printf("%s\n", value)
			}
		}
	},
}

// runConfigGetViaDaemon executes config get via daemon RPC (bd-wmil)
func runConfigGetViaDaemon(key string) {
	args := &rpc.GetConfigArgs{
		Key: key,
	}

	result, err := daemonClient.GetConfig(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		outputJSON(map[string]string{
			"key":   result.Key,
			"value": result.Value,
		})
	} else {
		if result.Value == "" {
			fmt.Printf("%s (not set)\n", key)
		} else {
			fmt.Printf("%s\n", result.Value)
		}
	}
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configuration",
	Run: func(cmd *cobra.Command, args []string) {
		// Use daemon RPC when available (bd-wmil)
		if daemonClient != nil {
			runConfigListViaDaemon()
			return
		}

		// Fallback to direct store access
		if store == nil {
			fmt.Fprintf(os.Stderr, "Error: no database connection available\n")
			fmt.Fprintf(os.Stderr, "Hint: start the daemon with 'bd daemon start' or run in a beads workspace\n")
			os.Exit(1)
		}

		ctx := rootCtx
		cfg, err := store.GetAllConfig(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing config: %v\n", err)
			os.Exit(1)
		}

		printConfigList(cfg)
	},
}

// runConfigListViaDaemon executes config list via daemon RPC (bd-wmil)
func runConfigListViaDaemon() {
	result, err := daemonClient.ConfigList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	printConfigList(result.Config)
}

// printConfigList outputs the config map in the appropriate format
func printConfigList(cfg map[string]string) {
	if jsonOutput {
		outputJSON(cfg)
		return
	}

	if len(cfg) == 0 {
		fmt.Println("No configuration set")
		return
	}

	// Sort keys for consistent output
	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fmt.Println("\nConfiguration:")
	for _, k := range keys {
		fmt.Printf("  %s = %s\n", k, cfg[k])
	}

	// Check for config.yaml overrides that take precedence (bd-20j)
	// This helps diagnose when effective config differs from database config
	showConfigYAMLOverrides(cfg)
}

// showConfigYAMLOverrides warns when config.yaml or env vars override database settings.
// This addresses the confusion when `bd config list` shows one value but the effective
// value used by commands is different due to higher-priority config sources.
func showConfigYAMLOverrides(dbConfig map[string]string) {
	var overrides []string

	// Check sync.branch - can be overridden by BEADS_SYNC_BRANCH env var or config.yaml sync-branch
	if dbSyncBranch, ok := dbConfig[syncbranch.ConfigKey]; ok && dbSyncBranch != "" {
		effectiveBranch := syncbranch.GetFromYAML()
		if effectiveBranch != "" && effectiveBranch != dbSyncBranch {
			overrides = append(overrides, fmt.Sprintf("  sync.branch: database has '%s' but effective value is '%s' (from config.yaml or env)", dbSyncBranch, effectiveBranch))
		}
	}

	if len(overrides) > 0 {
		fmt.Println("\n⚠️  Config overrides (higher priority sources):")
		for _, o := range overrides {
			fmt.Println(o)
		}
		fmt.Println("\nNote: config.yaml and environment variables take precedence over database config.")
	}
}

var configUnsetCmd = &cobra.Command{
	Use:   "unset <key>",
	Short: "Delete a configuration value",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]

		// Use daemon RPC when available (bd-wmil)
		if daemonClient != nil {
			runConfigUnsetViaDaemon(key)
			return
		}

		// Fallback to direct store access
		if store == nil {
			fmt.Fprintf(os.Stderr, "Error: no database connection available\n")
			fmt.Fprintf(os.Stderr, "Hint: start the daemon with 'bd daemon start' or run in a beads workspace\n")
			os.Exit(1)
		}

		ctx := rootCtx
		if err := store.DeleteConfig(ctx, key); err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting config: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(map[string]string{
				"key": key,
			})
		} else {
			fmt.Printf("Unset %s\n", key)
		}
	},
}

// runConfigUnsetViaDaemon executes config unset via daemon RPC (bd-wmil)
func runConfigUnsetViaDaemon(key string) {
	args := &rpc.ConfigUnsetArgs{
		Key: key,
	}

	result, err := daemonClient.ConfigUnset(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		outputJSON(map[string]string{
			"key": result.Key,
		})
	} else {
		fmt.Printf("Unset %s\n", result.Key)
	}
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate sync-related configuration",
	Long: `Validate sync-related configuration settings.

Checks:
  - sync.mode is a valid value (local, git-branch, external)
  - conflict.strategy is valid (lww, manual, ours, theirs)
  - federation.sovereignty is valid (if set)
  - federation.remote is set when sync.mode requires it
  - Remote URL format is valid (dolthub://, gs://, s3://, file://)
  - sync.branch is a valid git branch name
  - routing.mode is valid (auto, maintainer, contributor)

Examples:
  bd config validate
  bd config validate --json`,
	Run: func(cmd *cobra.Command, args []string) {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Find repo root by walking up to find .beads directory
		repoPath := findBeadsRepoRoot(cwd)
		if repoPath == "" {
			fmt.Fprintf(os.Stderr, "Error: not in a beads repository (no .beads directory found)\n")
			os.Exit(1)
		}

		// Run the existing doctor config values check
		doctorCheck := doctor.CheckConfigValues(repoPath)

		// Run additional sync-related validations
		syncIssues := validateSyncConfig(repoPath)

		// Combine results
		allIssues := []string{}
		if doctorCheck.Detail != "" {
			allIssues = append(allIssues, strings.Split(doctorCheck.Detail, "\n")...)
		}
		allIssues = append(allIssues, syncIssues...)

		// Output results
		if jsonOutput {
			result := map[string]interface{}{
				"valid":  len(allIssues) == 0,
				"issues": allIssues,
			}
			outputJSON(result)
			return
		}

		if len(allIssues) == 0 {
			fmt.Println("✓ All sync-related configuration is valid")
			return
		}

		fmt.Println("Configuration validation found issues:")
		for _, issue := range allIssues {
			if issue != "" {
				fmt.Printf("  • %s\n", issue)
			}
		}
		fmt.Println("\nRun 'bd config set <key> <value>' to fix configuration issues.")
		os.Exit(1)
	},
}

// validateSyncConfig performs additional sync-related config validation
// beyond what doctor.CheckConfigValues covers.
func validateSyncConfig(repoPath string) []string {
	var issues []string

	// Load config.yaml directly from the repo path
	configPath := repoPath + "/.beads/config.yaml"
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetConfigFile(configPath)

	// Try to read config, but don't error if it doesn't exist
	if err := v.ReadInConfig(); err != nil {
		// Config file doesn't exist or is unreadable - nothing to validate
		return issues
	}

	// Get config from yaml
	syncMode := v.GetString("sync.mode")
	conflictStrategy := v.GetString("conflict.strategy")
	federationSov := v.GetString("federation.sovereignty")
	federationRemote := v.GetString("federation.remote")

	// Validate sync.mode
	validSyncModes := map[string]bool{
		"":           true, // not set is valid (uses default)
		"local":      true,
		"git-branch": true,
		"external":   true,
	}
	if syncMode != "" && !validSyncModes[syncMode] {
		issues = append(issues, fmt.Sprintf("sync.mode: %q is invalid (valid values: local, git-branch, external)", syncMode))
	}

	// Validate conflict.strategy
	validConflictStrategies := map[string]bool{
		"":       true, // not set is valid (uses default lww)
		"lww":    true, // last-write-wins (default)
		"manual": true, // require manual resolution
		"ours":   true, // prefer local changes
		"theirs": true, // prefer remote changes
	}
	if conflictStrategy != "" && !validConflictStrategies[conflictStrategy] {
		issues = append(issues, fmt.Sprintf("conflict.strategy: %q is invalid (valid values: lww, manual, ours, theirs)", conflictStrategy))
	}

	// Validate federation.sovereignty
	validSovereignties := map[string]bool{
		"":          true, // not set is valid
		"none":      true, // no sovereignty restrictions
		"isolated":  true, // fully isolated, no federation
		"federated": true, // participates in federation
	}
	if federationSov != "" && !validSovereignties[federationSov] {
		issues = append(issues, fmt.Sprintf("federation.sovereignty: %q is invalid (valid values: none, isolated, federated)", federationSov))
	}

	// Validate federation.remote when required
	if syncMode == "external" && federationRemote == "" {
		issues = append(issues, "federation.remote: required when sync.mode is 'external'")
	}

	// Validate remote URL format
	if federationRemote != "" {
		if !isValidRemoteURL(federationRemote) {
			issues = append(issues, fmt.Sprintf("federation.remote: %q is not a valid remote URL (expected dolthub://, gs://, s3://, file://, or standard git URL)", federationRemote))
		}
	}

	return issues
}

// isValidRemoteURL validates remote URL formats for sync configuration
func isValidRemoteURL(url string) bool {
	// Valid URL schemes for beads remotes
	validSchemes := []string{
		"dolthub://",
		"gs://",
		"s3://",
		"file://",
		"https://",
		"http://",
		"ssh://",
	}

	for _, scheme := range validSchemes {
		if strings.HasPrefix(url, scheme) {
			return true
		}
	}

	// Also allow standard git remote patterns (user@host:path)
	// The host must have at least one character before the colon
	// Pattern: username@hostname:path where hostname has at least 2 chars
	gitSSHPattern := regexp.MustCompile(`^[a-zA-Z0-9._-]+@[a-zA-Z0-9][a-zA-Z0-9._-]*:.+$`)
	return gitSSHPattern.MatchString(url)
}

// findBeadsRepoRoot walks up from the given path to find the repo root (containing .beads).
// Stops at the system temp directory root to avoid finding stray .beads directories in /tmp.
func findBeadsRepoRoot(startPath string) string {
	path := startPath
	tempDir := filepath.Clean(os.TempDir())

	for {
		// Don't traverse into the temp directory root - it's a system directory
		cleanPath := filepath.Clean(path)
		if cleanPath == tempDir {
			return ""
		}

		beadsDir := filepath.Join(path, ".beads")
		if info, err := os.Stat(beadsDir); err == nil && info.IsDir() {
			return path
		}

		parent := filepath.Dir(path)
		if parent == path {
			// Reached filesystem root
			return ""
		}
		path = parent
	}
}

func init() {
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configUnsetCmd)
	configCmd.AddCommand(configValidateCmd)
	rootCmd.AddCommand(configCmd)
}
