package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/slackbot"
)

var slackCmd = &cobra.Command{
	Use:   "slack",
	Short: "Manage the Slack decisions bot",
	Long: `The Slack bot posts decision point notifications to Slack channels and
allows humans to resolve them via interactive buttons and modals.

Commands:
  start   Launch the Slack bot (foreground)
  status  Show bot connection and routing status
  route   Show or set channel routing configuration
  test    Send a test decision notification`,
}

var slackStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Launch the Slack decisions bot",
	Long: `Starts the Slack bot in the foreground. The bot connects via Socket Mode
and listens for decision events via NATS JetStream.

Required environment variables:
  SLACK_BOT_TOKEN   Slack bot token (xoxb-...)
  SLACK_APP_TOKEN   Slack app-level token (xapp-...)

Optional:
  SLACK_CHANNEL     Default channel ID for decision notifications
  BD_DAEMON_HOST    Daemon address (default: localhost:9876)
  BD_NATS_URL       NATS server URL (default: nats://localhost:4222)
  HEALTH_PORT       Health check HTTP port (default: 8080)`,
	RunE: runSlackStart,
}

var (
	slackBotToken       string
	slackAppToken       string
	slackChannel        string
	slackNatsURL        string
	slackDaemonHost     string
	slackHealthPort     int
	slackDynamicChans   bool
	slackChanPrefix     string
	slackAutoInvite     string
	slackDebug          bool
	slackRouterConfig   string
)

var slackStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Slack bot status",
	RunE:  runSlackStatus,
}

var slackRouteCmd = &cobra.Command{
	Use:   "route",
	Short: "Show channel routing configuration",
	RunE:  runSlackRoute,
}

func init() {
	rootCmd.AddCommand(slackCmd)
	slackCmd.AddCommand(slackStartCmd)
	slackCmd.AddCommand(slackStatusCmd)
	slackCmd.AddCommand(slackRouteCmd)

	slackStartCmd.Flags().StringVar(&slackBotToken, "bot-token", "", "Slack bot token (or SLACK_BOT_TOKEN env)")
	slackStartCmd.Flags().StringVar(&slackAppToken, "app-token", "", "Slack app token (or SLACK_APP_TOKEN env)")
	slackStartCmd.Flags().StringVar(&slackChannel, "channel", "", "Default channel ID (or SLACK_CHANNEL env)")
	slackStartCmd.Flags().StringVar(&slackNatsURL, "nats-url", "", "NATS URL (or BD_NATS_URL env, default nats://localhost:4222)")
	slackStartCmd.Flags().StringVar(&slackDaemonHost, "daemon-host", "", "Daemon address (or BD_DAEMON_HOST env, default localhost:9876)")
	slackStartCmd.Flags().IntVar(&slackHealthPort, "health-port", 8080, "Health check HTTP port (or HEALTH_PORT env)")
	slackStartCmd.Flags().BoolVar(&slackDynamicChans, "dynamic-channels", false, "Enable automatic channel creation")
	slackStartCmd.Flags().StringVar(&slackChanPrefix, "channel-prefix", "bd-decisions", "Prefix for dynamically created channels")
	slackStartCmd.Flags().StringVar(&slackAutoInvite, "auto-invite", "", "Comma-separated user IDs to auto-invite to new channels")
	slackStartCmd.Flags().BoolVar(&slackDebug, "debug", false, "Enable debug logging")
	slackStartCmd.Flags().StringVar(&slackRouterConfig, "router-config", "", "Path to slack.json router config")
}

func runSlackStart(cmd *cobra.Command, args []string) error {
	// Daemon host must come from flag/env (chicken-and-egg: can't read from DB without connection)
	daemonHost := firstNonEmpty(slackDaemonHost, os.Getenv("BD_DAEMON_HOST"), "localhost:9876")

	// Connect to daemon RPC via TCP
	rpcClient, err := rpc.TryConnectTCP(daemonHost, rpc.GetDaemonToken())
	if err != nil {
		return fmt.Errorf("connect to daemon at %s: %w", daemonHost, err)
	}
	defer rpcClient.Close()

	// Hydrate deploy.* config from daemon DB into env vars.
	// This allows the slack bot to pick up SLACK_CHANNEL, BD_NATS_URL, etc.
	// from the database without requiring explicit Helm env vars.
	hydrateSlackConfigFromDaemon(rpcClient)

	// Resolve config: flags > env vars (now hydrated from DB) > defaults
	botToken := firstNonEmpty(slackBotToken, os.Getenv("SLACK_BOT_TOKEN"))
	appToken := firstNonEmpty(slackAppToken, os.Getenv("SLACK_APP_TOKEN"))
	channelID := firstNonEmpty(slackChannel, os.Getenv("SLACK_CHANNEL"))
	natsURL := firstNonEmpty(slackNatsURL, os.Getenv("BD_NATS_URL"), "nats://localhost:4222")

	if botToken == "" {
		return fmt.Errorf("Slack bot token required (--bot-token or SLACK_BOT_TOKEN env)")
	}
	if appToken == "" {
		return fmt.Errorf("Slack app token required (--app-token or SLACK_APP_TOKEN env)")
	}
	if channelID == "" {
		return fmt.Errorf("default channel required (--channel or SLACK_CHANNEL env)")
	}

	// Health port from env
	if hp := os.Getenv("HEALTH_PORT"); hp != "" && slackHealthPort == 8080 {
		fmt.Sscanf(hp, "%d", &slackHealthPort)
	}

	decisions := slackbot.NewDecisionClient(rpcClient)

	// Parse auto-invite users
	var autoInviteUsers []string
	inviteStr := firstNonEmpty(slackAutoInvite, os.Getenv("SLACK_AUTO_INVITE"))
	if inviteStr != "" {
		for _, u := range strings.Split(inviteStr, ",") {
			u = strings.TrimSpace(u)
			if u != "" {
				autoInviteUsers = append(autoInviteUsers, u)
			}
		}
	}

	cfg := slackbot.BotConfig{
		BotToken:         botToken,
		AppToken:         appToken,
		ChannelID:        channelID,
		RouterConfigPath: slackRouterConfig,
		DynamicChannels:  slackDynamicChans,
		ChannelPrefix:    slackChanPrefix,
		AutoInviteUsers:  autoInviteUsers,
		Debug:            slackDebug,
	}

	bot, err := slackbot.NewBot(cfg, decisions)
	if err != nil {
		return fmt.Errorf("create Slack bot: %w", err)
	}

	// Set up context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start health server
	health := slackbot.NewHealthServer(bot, slackHealthPort)
	go func() {
		if err := health.Start(ctx); err != nil {
			log.Printf("slackbot: health server error: %v", err)
		}
	}()

	// Start NATS watcher (pass daemon token for NATS auth)
	natsToken := rpc.GetDaemonToken()
	watcher := slackbot.NewNATSWatcher(natsURL, natsToken, bot, decisions)
	go func() {
		if err := watcher.Run(ctx); err != nil {
			log.Printf("slackbot: NATS watcher error: %v", err)
		}
	}()

	// Run bot (blocks until context canceled)
	log.Printf("slackbot: starting (channel=%s, nats=%s, daemon=%s)", channelID, natsURL, daemonHost)
	return bot.Run(ctx)
}

func runSlackStatus(cmd *cobra.Command, args []string) error {
	router, err := slackbot.LoadRouter()
	if err != nil {
		fmt.Println("Slack routing: not configured")
		return nil
	}

	cfg := router.GetConfig()
	fmt.Printf("Slack routing:\n")
	fmt.Printf("  Enabled: %v\n", cfg.Enabled)
	fmt.Printf("  Default channel: %s\n", cfg.DefaultChannel)
	fmt.Printf("  Patterns: %d\n", len(cfg.Channels))
	fmt.Printf("  Overrides: %d\n", len(cfg.Overrides))
	fmt.Printf("  Beads-backed: %v\n", router.IsBeadsBacked())
	return nil
}

func runSlackRoute(cmd *cobra.Command, args []string) error {
	router, err := slackbot.LoadRouter()
	if err != nil {
		return fmt.Errorf("load router: %w", err)
	}

	cfg := router.GetConfig()
	fmt.Printf("Channel Routing Configuration\n")
	fmt.Printf("============================\n\n")
	fmt.Printf("Enabled: %v\n", cfg.Enabled)
	fmt.Printf("Default Channel: %s\n\n", cfg.DefaultChannel)

	if len(cfg.Channels) > 0 {
		fmt.Printf("Patterns:\n")
		for pattern, channel := range cfg.Channels {
			name := cfg.ChannelNames[channel]
			if name != "" {
				fmt.Printf("  %-30s → %s (%s)\n", pattern, channel, name)
			} else {
				fmt.Printf("  %-30s → %s\n", pattern, channel)
			}
		}
		fmt.Println()
	}

	if len(cfg.Overrides) > 0 {
		fmt.Printf("Overrides (Break Out):\n")
		for agent, channel := range cfg.Overrides {
			name := cfg.ChannelNames[channel]
			if name != "" {
				fmt.Printf("  %-30s → %s (%s)\n", agent, channel, name)
			} else {
				fmt.Printf("  %-30s → %s\n", agent, channel)
			}
		}
		fmt.Println()
	}

	return nil
}

// firstNonEmpty returns the first non-empty string from the arguments.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// hydrateSlackConfigFromDaemon reads deploy.* config from the daemon's database
// via RPC and sets corresponding environment variables that aren't already set.
// This allows the slack bot sidecar to pick up SLACK_CHANNEL, BD_NATS_URL, etc.
// from the database without requiring explicit Helm env vars for each setting.
func hydrateSlackConfigFromDaemon(client *rpc.Client) {
	result, err := client.ConfigList()
	if err != nil {
		log.Printf("slackbot: could not read deploy config from daemon: %v", err)
		return
	}

	envMap := config.DeployKeyEnvMap()
	hydrated := 0

	for key, value := range result.Config {
		if !config.IsDeployKey(key) {
			continue
		}
		envVar, ok := envMap[key]
		if !ok || envVar == "" {
			continue
		}
		if existing := os.Getenv(envVar); existing != "" {
			continue
		}
		if err := os.Setenv(envVar, value); err != nil {
			log.Printf("slackbot: failed to set %s from %s: %v", envVar, key, err)
			continue
		}
		hydrated++
		log.Printf("slackbot: hydrated %s=%s (from %s)", envVar, value, key)
	}

	if hydrated > 0 {
		log.Printf("slackbot: hydrated %d deploy config values from daemon", hydrated)
	}
}
