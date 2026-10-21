package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/fizzy"
	"github.com/steveyegge/beads/internal/metrics"
	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// FizzyConfig holds the Fizzy connection configuration.
type FizzyConfig struct {
	Token   string // Personal access token
	URL     string // Base URL of the Fizzy instance
	Account string // Account slug
	Board   string // Board ID
}

var fizzyCmd = &cobra.Command{
	Use:   "fizzy",
	Short: "Fizzy integration commands",
	Long: `Commands for syncing issues between beads and a Fizzy board.

Configuration can be set via 'bd config' or environment variables:
  fizzy.token / FIZZY_TOKEN     - Personal access token ("Read + Write" for push)
  fizzy.url / FIZZY_URL         - URL of the Fizzy instance (default: https://app.fizzy.do)
  fizzy.account / FIZZY_ACCOUNT - Account slug (the number in the Fizzy URLs)
  fizzy.board / FIZZY_BOARD     - ID of the board to sync

Optional status map, from a beads status to a lane of the board:
  fizzy.status_map.<status>     - A workflow column, "Maybe?", "Not Now" or "Done"

Without a status map, open goes to "Maybe?", in_progress and blocked go to
the first workflow column, deferred goes to "Not Now" and closed goes to "Done".`,
}

var fizzySyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync issues with Fizzy",
	Long: `Synchronize issues between beads and a Fizzy board.

By default, performs bidirectional sync:
- Pulls new/updated cards from Fizzy to beads
- Pushes local beads issues to Fizzy

Use --pull-only or --push-only to limit direction.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runFizzySync,
}

var fizzyStatusCmd = &cobra.Command{
	Use:           "status",
	Short:         "Show Fizzy sync status",
	Long:          `Display current Fizzy configuration and sync status.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runFizzyStatus,
}

var (
	fizzySyncDryRun     bool
	fizzySyncPullOnly   bool
	fizzySyncPushOnly   bool
	fizzyPreferLocal    bool
	fizzyPreferFizzy    bool
	fizzyPreferNewer    bool
	fizzyTypeFilter     string
	fizzyExcludeTypes   string
	fizzyNoEphemeral    bool
	fizzyConfigEnvNames = map[string]string{
		"fizzy.token":   "FIZZY_TOKEN",
		"fizzy.url":     "FIZZY_URL",
		"fizzy.account": "FIZZY_ACCOUNT",
		"fizzy.board":   "FIZZY_BOARD",
	}
)

func init() {
	fizzyCmd.AddCommand(fizzySyncCmd)
	fizzyCmd.AddCommand(fizzyStatusCmd)

	fizzySyncCmd.Flags().BoolVar(&fizzySyncDryRun, "dry-run", false, "Show what would be synced without making changes")
	fizzySyncCmd.Flags().BoolVar(&fizzySyncPullOnly, "pull-only", false, "Only pull cards from Fizzy")
	fizzySyncCmd.Flags().BoolVar(&fizzySyncPushOnly, "push-only", false, "Only push issues to Fizzy")

	// Conflict resolution flags (mutually exclusive)
	fizzySyncCmd.Flags().BoolVar(&fizzyPreferLocal, "prefer-local", false, "On conflict, keep local beads version")
	fizzySyncCmd.Flags().BoolVar(&fizzyPreferFizzy, "prefer-fizzy", false, "On conflict, use Fizzy version")
	fizzySyncCmd.Flags().BoolVar(&fizzyPreferNewer, "prefer-newer", false, "On conflict, use most recent version (default)")

	fizzySyncCmd.Flags().StringVar(&fizzyTypeFilter, "type", "", "Only sync these issue types (comma-separated)")
	fizzySyncCmd.Flags().StringVar(&fizzyExcludeTypes, "exclude-type", "", "Exclude these issue types from sync (comma-separated)")
	fizzySyncCmd.Flags().BoolVar(&fizzyNoEphemeral, "no-ephemeral", true, "Exclude ephemeral/wisp issues from push (default: true)")
	registerSelectiveSyncFlags(fizzySyncCmd)

	rootCmd.AddCommand(fizzyCmd)
}

// getFizzyConfig returns the Fizzy configuration from bd config or the environment.
func getFizzyConfig() FizzyConfig {
	ctx := context.Background()
	return FizzyConfig{
		Token:   getFizzyConfigValue(ctx, "fizzy.token"),
		URL:     getFizzyConfigValue(ctx, "fizzy.url"),
		Account: getFizzyConfigValue(ctx, "fizzy.account"),
		Board:   getFizzyConfigValue(ctx, "fizzy.board"),
	}
}

// getFizzyConfigValue reads a Fizzy configuration value from the store or the environment.
func getFizzyConfigValue(ctx context.Context, key string) string {
	envKey := fizzyConfigEnvNames[key]
	// Secret keys (fizzy.token) are stored in config.yaml, not the Dolt
	// database, to avoid leaking secrets when pushing to remotes.
	if config.IsYamlOnlyKey(key) {
		if value := config.GetString(key); value != "" {
			return value
		}
		return os.Getenv(envKey)
	}

	if store != nil {
		if value, _ := store.GetConfig(ctx, key); value != "" {
			return value
		}
	} else if dbPath != "" {
		if tempStore, err := openReadOnlyStoreForDBPath(ctx, dbPath); err == nil {
			defer func() { _ = tempStore.Close() }()
			if value, _ := tempStore.GetConfig(ctx, key); value != "" {
				return value
			}
		}
	}
	return os.Getenv(envKey)
}

// validateFizzyConfig checks that the required configuration is present.
func validateFizzyConfig(config FizzyConfig) error {
	if config.Token == "" {
		return fmt.Errorf("fizzy.token is not configured. Set via 'bd config set fizzy.token <token>' or FIZZY_TOKEN environment variable")
	}
	if config.Account == "" {
		return fmt.Errorf("fizzy.account is not configured. Set via 'bd config set fizzy.account <slug>' or FIZZY_ACCOUNT environment variable")
	}
	if config.Board == "" {
		return fmt.Errorf("fizzy.board is not configured. Set via 'bd config set fizzy.board <board-id>' or FIZZY_BOARD environment variable")
	}
	return fizzy.ValidateBaseURL(config.URL)
}

// maskFizzyToken masks a token for safe display.
func maskFizzyToken(token string) string {
	if token == "" {
		return "(not set)"
	}
	if len(token) <= 4 {
		return "****"
	}
	return token[:4] + "****"
}

// fizzyStatusMap returns the fizzy.status_map.* entries, sorted by status.
func fizzyStatusMap(ctx context.Context) []string {
	if store == nil {
		return nil
	}
	allConfig, err := store.GetAllConfig(ctx)
	if err != nil {
		return nil
	}
	var entries []string
	for key, value := range allConfig {
		if status, ok := strings.CutPrefix(key, "fizzy.status_map."); ok && value != "" {
			entries = append(entries, fmt.Sprintf("%s -> %s", status, value))
		}
	}
	sort.Strings(entries)
	return entries
}

func runFizzyStatus(cmd *cobra.Command, args []string) error {
	if usesProxiedServer() {
		return HandleErrorRespectJSON("fizzy status is not supported in proxied-server mode")
	}
	evt := metrics.NewCommandEvent("fizzy-status")
	defer func() {
		if c := metrics.Global(); c != nil {
			c.CloseEventAndAdd(evt)
		}
	}()

	config := getFizzyConfig()
	ctx := context.Background()
	url := config.URL
	if url == "" {
		url = fizzy.DefaultBaseURL + " (default)"
	}

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintln(out, "Fizzy Configuration")
	_, _ = fmt.Fprintln(out, "===================")
	_, _ = fmt.Fprintf(out, "Token:   %s\n", maskFizzyToken(config.Token))
	_, _ = fmt.Fprintf(out, "URL:     %s\n", url)
	_, _ = fmt.Fprintf(out, "Account: %s\n", config.Account)
	_, _ = fmt.Fprintf(out, "Board:   %s\n", config.Board)
	if entries := fizzyStatusMap(ctx); len(entries) > 0 {
		_, _ = fmt.Fprintln(out, "Status map:")
		for _, entry := range entries {
			_, _ = fmt.Fprintf(out, "  %s\n", entry)
		}
	}
	if store != nil {
		if lastSync, err := store.GetLocalMetadata(ctx, "fizzy.last_sync"); err == nil && lastSync != "" {
			_, _ = fmt.Fprintf(out, "Last sync: %s\n", lastSync)
		}
	}

	if err := validateFizzyConfig(config); err != nil {
		_, _ = fmt.Fprintf(out, "\nStatus: ❌ Not configured\n")
		_, _ = fmt.Fprintf(out, "Error: %v\n", err)
		return nil
	}
	_, _ = fmt.Fprintf(out, "\nStatus: ✓ Configured\n")
	return nil
}

func runFizzySync(cmd *cobra.Command, args []string) error {
	if usesProxiedServer() {
		return HandleErrorRespectJSON("fizzy sync is not supported in proxied-server mode")
	}
	evt := metrics.NewCommandEvent("fizzy-sync")
	defer func() {
		if c := metrics.Global(); c != nil {
			c.CloseEventAndAdd(evt)
		}
	}()

	if err := validateFizzyConfig(getFizzyConfig()); err != nil {
		return HandleError("%v", err)
	}
	if !fizzySyncDryRun {
		CheckReadonly("fizzy sync")
	}
	if fizzySyncPullOnly && fizzySyncPushOnly {
		return HandleError("cannot use both --pull-only and --push-only")
	}
	conflictResolution, err := getFizzyConflictResolution(fizzyPreferLocal, fizzyPreferFizzy, fizzyPreferNewer)
	if err != nil {
		return HandleError("%v (--prefer-local, --prefer-fizzy, --prefer-newer)", err)
	}
	if err := ensureStoreActive(); err != nil {
		return HandleError("database not available: %v", err)
	}

	out := cmd.OutOrStdout()
	ctx := context.Background()

	ft := &fizzy.Tracker{}
	if err := ft.Init(ctx, store); err != nil {
		return HandleError("initializing Fizzy tracker: %v", err)
	}

	engine := tracker.NewEngine(ft, store, currentActor())
	engine.OnMessage = func(msg string) { _, _ = fmt.Fprintln(out, "  "+msg) }
	engine.OnWarning = func(msg string) { _, _ = fmt.Fprintf(os.Stderr, "Warning: %s\n", msg) }
	engine.PullHooks = ft.PullHooks(fizzyGenerateID(ctx))
	engine.PushHooks = ft.PushHooks(ctx)

	push := !fizzySyncPullOnly
	excludeTypes := parseTypeList(fizzyExcludeTypes)
	// Default: exclude internal coordination types from push unless
	// the user provided an explicit --type whitelist.
	if fizzyTypeFilter == "" && fizzyExcludeTypes == "" {
		excludeTypes = []types.IssueType{types.TypeMolecule, types.TypeMessage, types.TypeEvent}
	}
	opts := tracker.SyncOptions{
		Pull:               !fizzySyncPushOnly,
		Push:               push,
		DryRun:             fizzySyncDryRun,
		ExcludeEphemeral:   fizzyNoEphemeral,
		TypeFilter:         parseTypeList(fizzyTypeFilter),
		ExcludeTypes:       excludeTypes,
		ConflictResolution: conflictResolution,
	}
	if err := applySelectiveSyncFlags(cmd, &opts, push); err != nil {
		return HandleError("%v", err)
	}

	if fizzySyncDryRun {
		_, _ = fmt.Fprintln(out, "Dry run mode - no changes will be made")
		_, _ = fmt.Fprintln(out)
	}

	result, err := engine.Sync(ctx, opts)
	if err != nil {
		return HandleError("%v", err)
	}

	if !fizzySyncDryRun {
		if result.Stats.Pulled > 0 {
			_, _ = fmt.Fprintf(out, "✓ Pulled %d issues (%d created, %d updated)\n",
				result.Stats.Pulled, result.Stats.Created, result.Stats.Updated)
		}
		if result.Stats.Pushed > 0 {
			_, _ = fmt.Fprintf(out, "✓ Pushed %d issues\n", result.Stats.Pushed)
		}
		if result.Stats.Conflicts > 0 {
			_, _ = fmt.Fprintf(out, "→ Resolved %d conflicts\n", result.Stats.Conflicts)
		}
		commandDidWrite.Store(true)
	} else {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "Run without --dry-run to apply changes")
	}
	return nil
}

// getFizzyConflictResolution maps the conflict flags to an engine strategy.
func getFizzyConflictResolution(preferLocal, preferFizzy, preferNewer bool) (tracker.ConflictResolution, error) {
	set := 0
	for _, flag := range []bool{preferLocal, preferFizzy, preferNewer} {
		if flag {
			set++
		}
	}
	switch {
	case set > 1:
		return "", fmt.Errorf("cannot use multiple conflict resolution flags")
	case preferLocal:
		return tracker.ConflictLocal, nil
	case preferFizzy:
		return tracker.ConflictExternal, nil
	}
	return tracker.ConflictTimestamp, nil
}

// fizzyGenerateID gives an ID with the issue prefix to an issue that a pull creates.
func fizzyGenerateID(ctx context.Context) func(context.Context, *types.Issue) error {
	prefix := "bd"
	// YAML config takes precedence — in shared-server mode the DB
	// may belong to a different project (GH#2469).
	if p := config.GetString("issue-prefix"); p != "" {
		prefix = p
	} else if store != nil {
		if p, err := store.GetConfig(ctx, "issue_prefix"); err == nil && p != "" {
			prefix = p
		}
	}
	return func(_ context.Context, issue *types.Issue) error {
		if issue.ID == "" {
			issue.ID = generateIssueID(prefix)
		}
		return nil
	}
}
