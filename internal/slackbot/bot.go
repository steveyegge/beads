// Package slackbot implements a Slack bot for beads decision management.
// It uses the slack-go/slack library with Socket Mode for WebSocket-based communication.
package slackbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// messageInfo tracks a posted Slack message for later updates/deletion.
type messageInfo struct {
	channelID string
	timestamp string
	agent     string // agent identity (for rig routing pending count tracking)
}

// Bot is a Slack bot for managing beads decisions.
type Bot struct {
	client     SlackAPI
	socketMode *socketmode.Client
	decisions  DecisionProvider
	channelID  string  // Default channel to post decision notifications
	router     *Router // Channel router for per-agent routing
	debug      bool
	beadsDir   string // .beads directory path for bd commands

	// Dynamic channel creation
	dynamicChannels bool              // Enable automatic channel creation
	channelPrefix   string            // Prefix for created channels (e.g., "bd-decisions")
	channelCache    map[string]string // Cache: channel name → channel ID
	channelCacheMu  sync.RWMutex
	autoInviteUsers []string // Users to auto-invite when routing to new channels

	// Decision message tracking for auto-dismiss
	decisionMessages   map[string]messageInfo // decision ID → message info
	decisionMessagesMu sync.RWMutex

	// Agent status cards: persistent top-level messages per agent (rig routing mode)
	agentStatusCards   map[string]messageInfo // agent identity → status card message
	agentStatusCardsMu sync.RWMutex
	agentPendingCount  map[string]int // agent identity → pending decision count
	agentPendingMu     sync.Mutex
	stateManager       *StateManager // Persists status card refs across restarts

	// Bot identity for filtering out own messages in thread replies
	botUserID string

	// User notification preferences
	preferenceManager *PreferenceManager
}

// BotConfig holds configuration for the Slack bot.
type BotConfig struct {
	BotToken         string   // xoxb-... Slack bot token
	AppToken         string   // xapp-... Slack app-level token (for Socket Mode)
	ChannelID        string   // Default channel for decision notifications
	RouterConfigPath string   // Optional path to slack.json for per-agent routing
	DynamicChannels  bool     // Enable automatic channel creation based on agent identity
	ChannelPrefix    string   // Prefix for dynamically created channels (default: "bd-decisions")
	BeadsDir         string   // .beads directory path (auto-discovered if empty)
	AutoInviteUsers  []string // Slack user IDs to auto-invite when routing to new channels
	Debug            bool
}

// NewBot creates a new Slack bot.
func NewBot(cfg BotConfig, decisions *DecisionClient) (*Bot, error) {
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("bot token is required")
	}
	if cfg.AppToken == "" {
		return nil, fmt.Errorf("app token is required for Socket Mode")
	}
	if !strings.HasPrefix(cfg.AppToken, "xapp-") {
		return nil, fmt.Errorf("app token must start with xapp-")
	}

	client := slack.New(
		cfg.BotToken,
		slack.OptionDebug(cfg.Debug),
		slack.OptionAppLevelToken(cfg.AppToken),
	)

	socketClient := socketmode.New(
		client,
		socketmode.OptionDebug(cfg.Debug),
	)

	// Discover beads directory
	beadsDir := cfg.BeadsDir
	if beadsDir == "" {
		beadsDir = os.Getenv("BEADS_DIR")
	}
	if beadsDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			beadsDir = findBeadsDir(cwd)
		}
	}
	if beadsDir != "" {
		log.Printf("slackbot: beads dir: %s", beadsDir)
		if err := os.Setenv("BEADS_DIR", beadsDir); err != nil {
			log.Printf("slackbot: warning: failed to set BEADS_DIR: %v", err)
		}
	}

	// Load channel router for per-agent routing
	var router *Router
	if cfg.RouterConfigPath != "" {
		var err error
		router, err = LoadRouterFromFile(cfg.RouterConfigPath)
		if err != nil {
			log.Printf("slackbot: warning: failed to load router from %s: %v", cfg.RouterConfigPath, err)
		} else if router.IsEnabled() {
			log.Printf("slackbot: channel router loaded from %s", cfg.RouterConfigPath)
		}
	} else {
		var err error
		router, err = LoadRouter()
		if err != nil {
			log.Printf("slackbot: router auto-discovery failed: %v", err)
		} else if router != nil && router.IsEnabled() {
			log.Printf("slackbot: channel router auto-loaded (enabled=%v)", router.IsEnabled())
		}
	}

	channelPrefix := cfg.ChannelPrefix
	if channelPrefix == "" {
		channelPrefix = "bd-decisions"
	}

	stateMgr := NewStateManager(beadsDir)

	// Hydrate in-memory status cards from persisted state
	agentCards := make(map[string]messageInfo)
	for agent, card := range stateMgr.AllAgentCards() {
		agentCards[agent] = messageInfo{channelID: card.ChannelID, timestamp: card.Timestamp}
	}
	if len(agentCards) > 0 {
		log.Printf("slackbot: restored %d agent status cards from state file", len(agentCards))
	}

	bot := &Bot{
		client:            client,
		socketMode:        socketClient,
		decisions:         decisions,
		channelID:         cfg.ChannelID,
		router:            router,
		debug:             cfg.Debug,
		beadsDir:          beadsDir,
		dynamicChannels:   cfg.DynamicChannels,
		channelPrefix:     channelPrefix,
		channelCache:      make(map[string]string),
		autoInviteUsers:   cfg.AutoInviteUsers,
		decisionMessages:  make(map[string]messageInfo),
		agentStatusCards:  agentCards,
		agentPendingCount: make(map[string]int),
		stateManager:      stateMgr,
		preferenceManager: NewPreferenceManager(beadsDir),
	}
	return bot, nil
}

// newBotForTest creates a Bot with injectable mock dependencies for testing.
// No Slack connection or token validation is performed.
func newBotForTest(slackAPI SlackAPI, decisions DecisionProvider, channelID string) *Bot {
	return &Bot{
		client:            slackAPI,
		decisions:         decisions,
		channelID:         channelID,
		channelCache:      make(map[string]string),
		decisionMessages:  make(map[string]messageInfo),
		agentStatusCards:  make(map[string]messageInfo),
		agentPendingCount: make(map[string]int),
		preferenceManager: NewPreferenceManager(""),
	}
}

// Run starts the bot event loop. Blocks until context is canceled.
func (b *Bot) Run(ctx context.Context) error {
	authResp, err := b.client.AuthTest()
	if err != nil {
		log.Printf("slackbot: warning: failed to get bot user ID: %v", err)
	} else {
		b.botUserID = authResp.UserID
		log.Printf("slackbot: bot user ID: %s", b.botUserID)
	}

	go func() {
		for evt := range b.socketMode.Events {
			b.handleEvent(evt)
		}
	}()

	// Auto-join all public channels on startup
	if err := b.JoinAllChannels(); err != nil {
		log.Printf("slackbot: warning: failed to auto-join channels: %v", err)
	}

	return b.socketMode.RunContext(ctx)
}

// findBeadsDir walks up from dir looking for a .beads directory.
func findBeadsDir(dir string) string {
	for d := dir; d != "/" && d != "."; d = filepath.Dir(d) {
		beadsDir := filepath.Join(d, ".beads")
		if info, err := os.Stat(beadsDir); err == nil && info.IsDir() {
			return beadsDir
		}
	}
	return ""
}

// ---------- Event dispatch ----------

func (b *Bot) handleEvent(evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		log.Println("slackbot: connecting to Socket Mode...")

	case socketmode.EventTypeConnected:
		log.Println("slackbot: connected to Socket Mode")
		SetConnected(true)

	case socketmode.EventTypeConnectionError:
		log.Printf("slackbot: connection error: %v", evt.Data)
		SetConnected(false)

	case socketmode.EventTypeEventsAPI:
		eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		b.socketMode.Ack(*evt.Request)
		b.handleEventsAPI(eventsAPIEvent)

	case socketmode.EventTypeSlashCommand:
		cmd, ok := evt.Data.(slack.SlashCommand)
		if !ok {
			return
		}
		b.socketMode.Ack(*evt.Request)
		b.handleSlashCommand(cmd)

	case socketmode.EventTypeInteractive:
		callback, ok := evt.Data.(slack.InteractionCallback)
		if !ok {
			return
		}
		b.socketMode.Ack(*evt.Request)
		b.handleInteraction(callback)
	}
}

// ---------- Slash commands ----------

func (b *Bot) handleSlashCommand(cmd slack.SlashCommand) {
	switch cmd.Command {
	case "/decisions", "/decide":
		b.handleDecisionsCommand(cmd)
	default:
		b.postEphemeral(cmd.ChannelID, cmd.UserID,
			fmt.Sprintf("Unknown command: %s", cmd.Command))
	}
}

func (b *Bot) handleDecisionsCommand(cmd slack.SlashCommand) {
	ctx := context.Background()
	pending, err := b.decisions.ListPending(ctx)
	if err != nil {
		b.postEphemeral(cmd.ChannelID, cmd.UserID,
			fmt.Sprintf("Error fetching decisions: %v", err))
		return
	}

	if len(pending) == 0 {
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn",
					"No pending decisions! All decisions have been resolved.",
					false, false),
				nil, nil),
		}
		_, _, _ = b.client.PostMessage(cmd.ChannelID,
			slack.MsgOptionBlocks(blocks...),
			slack.MsgOptionResponseURL(cmd.ResponseURL, slack.ResponseTypeEphemeral))
		return
	}

	highCount, medCount, lowCount := 0, 0, 0
	for _, d := range pending {
		switch d.Urgency {
		case "high":
			highCount++
		case "medium":
			medCount++
		default:
			lowCount++
		}
	}

	summaryText := fmt.Sprintf(":clipboard: *%d Pending Decision", len(pending))
	if len(pending) > 1 {
		summaryText += "s"
	}
	summaryText += "*"
	if highCount > 0 {
		summaryText += fmt.Sprintf(" (:red_circle: %d high", highCount)
		if medCount > 0 {
			summaryText += fmt.Sprintf(", :large_yellow_circle: %d med", medCount)
		}
		if lowCount > 0 {
			summaryText += fmt.Sprintf(", :large_green_circle: %d low", lowCount)
		}
		summaryText += ")"
	}

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", summaryText, false, false),
			nil, nil),
		slack.NewDividerBlock(),
	}

	for _, d := range pending {
		question := d.Question
		if len(question) > 100 {
			question = question[:97] + "..."
		}

		urgencyEmoji := urgencyToEmoji(d.Urgency)
		agentTag := ""
		if d.RequestedBy != "" {
			agentTag = fmt.Sprintf(" (%s)", d.RequestedBy)
		}

		displayID := d.SemanticSlug
		if displayID == "" {
			displayID = d.ID
		}

		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn",
					fmt.Sprintf("%s *%s*%s\n%s", urgencyEmoji, displayID, agentTag, question),
					false, false),
				nil,
				slack.NewAccessory(
					slack.NewButtonBlockElement("view_decision", d.ID,
						slack.NewTextBlockObject("plain_text", "View", false, false))),
			))
	}

	_, _, err = b.client.PostMessage(cmd.ChannelID,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionResponseURL(cmd.ResponseURL, slack.ResponseTypeEphemeral))
	if err != nil {
		log.Printf("slackbot: error posting decisions list: %v", err)
	}
}

// ---------- Interactive handlers ----------

func (b *Bot) handleInteraction(callback slack.InteractionCallback) {
	if callback.Type == slack.InteractionTypeViewSubmission {
		b.handleViewSubmission(callback)
		return
	}

	for _, action := range callback.ActionCallback.BlockActions {
		switch action.ActionID {
		case "view_decision":
			b.handleViewDecision(callback, action.Value)
		case "break_out":
			b.handleBreakOut(callback, action.Value)
		case "unbreak_out":
			b.handleUnbreakOut(callback, action.Value)
		case "dismiss_decision":
			b.handleDismissDecision(callback, action.Value)
		case "open_preferences":
			b.handleOpenPreferences(callback, action.Value)
		default:
			if strings.HasPrefix(action.ActionID, "peek_") {
				decisionID := strings.TrimPrefix(action.ActionID, "peek_")
				b.handlePeekButton(callback, decisionID)
			} else if strings.HasPrefix(action.ActionID, "resolve_other_") {
				decisionID := strings.TrimPrefix(action.ActionID, "resolve_other_")
				b.handleResolveOther(callback, decisionID)
			} else if strings.HasPrefix(action.ActionID, "resolve_") {
				b.handleResolveDecision(callback, action)
			} else if strings.HasPrefix(action.ActionID, "show_context_") {
				decisionID := strings.TrimPrefix(action.ActionID, "show_context_")
				b.handleShowContext(callback, decisionID)
			}
		}
	}
}

func (b *Bot) handleViewDecision(callback slack.InteractionCallback, decisionID string) {
	ctx := context.Background()
	decision, err := b.decisions.GetDecision(ctx, decisionID)
	if err != nil {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Error fetching decision: %v", err))
		return
	}

	if decision.Resolved {
		b.showResolvedDecisionView(callback, decision)
		return
	}

	displayID := decision.SemanticSlug
	if displayID == "" {
		displayID = decision.ID
	}

	blocks := []slack.Block{
		slack.NewHeaderBlock(
			slack.NewTextBlockObject("plain_text",
				fmt.Sprintf("Decision: %s", displayID), false, false)),
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("*From:* %s\n*Question:* %s", decision.RequestedBy, decision.Question),
				false, false),
			nil, nil),
	}

	if decision.Context != "" {
		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn",
					fmt.Sprintf("*Context:*\n%s", truncateForSlack(decision.Context, 2900)),
					false, false),
				nil, nil))
	}

	blocks = append(blocks, slack.NewDividerBlock())

	for i, opt := range decision.Options {
		label := opt.Label
		if opt.Recommended {
			label = "* " + label
		}
		buttonLabel := label
		if len(buttonLabel) > 75 {
			buttonLabel = buttonLabel[:72] + "..."
		}

		optText := fmt.Sprintf("*%d. %s*", i+1, label)
		if opt.Description != "" {
			optText += fmt.Sprintf("\n%s", opt.Description)
		}

		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", optText, false, false),
				nil,
				slack.NewAccessory(
					slack.NewButtonBlockElement(
						fmt.Sprintf("resolve_%s_%d", decision.ID, i+1),
						fmt.Sprintf("%s:%d", decision.ID, i+1),
						slack.NewTextBlockObject("plain_text", "Choose", false, false)))))
	}

	_, _, err = b.client.PostMessage(callback.Channel.ID,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionResponseURL(callback.ResponseURL, slack.ResponseTypeEphemeral))
	if err != nil {
		log.Printf("slackbot: error posting decision view: %v", err)
	}
}

func (b *Bot) showResolvedDecisionView(callback slack.InteractionCallback, decision *Decision) {
	displayID := decision.SemanticSlug
	if displayID == "" {
		displayID = decision.ID
	}

	resolverText := formatResolver(decision.ResolvedBy)

	blocks := []slack.Block{
		slack.NewHeaderBlock(
			slack.NewTextBlockObject("plain_text",
				fmt.Sprintf("Resolved: %s", displayID), false, false)),
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("*From:* %s\n*Question:* %s", decision.RequestedBy, decision.Question),
				false, false),
			nil, nil),
	}

	if decision.Context != "" {
		contextText := formatContextForSlack(decision.Context)
		if contextText != "" {
			blocks = append(blocks,
				slack.NewSectionBlock(
					slack.NewTextBlockObject("mrkdwn",
						fmt.Sprintf("*Context:*\n%s", contextText), false, false),
					nil, nil))
		}
	}

	blocks = append(blocks, slack.NewDividerBlock())

	for i, opt := range decision.Options {
		prefix := fmt.Sprintf("%d.", i+1)
		label := opt.Label
		if i+1 == decision.ChosenIndex {
			prefix = "chosen:"
			label = "*" + label + "* (chosen)"
		}
		if opt.Recommended {
			label = "* " + label
		}

		optText := fmt.Sprintf("%s %s", prefix, label)
		if opt.Description != "" {
			optText += fmt.Sprintf("\n_%s_", opt.Description)
		}

		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", optText, false, false),
				nil, nil))
	}

	blocks = append(blocks, slack.NewDividerBlock())

	resolutionText := fmt.Sprintf("*Resolved by:* %s", resolverText)
	if decision.Rationale != "" {
		rationale := decision.Rationale
		if len(rationale) > 300 {
			rationale = rationale[:297] + "..."
		}
		resolutionText += fmt.Sprintf("\n*Rationale:* %s", rationale)
	}

	blocks = append(blocks,
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", resolutionText, false, false),
			nil, nil),
		slack.NewContextBlock("",
			slack.NewTextBlockObject("mrkdwn",
				"_This decision has already been resolved._", false, false)))

	_, _, err := b.client.PostMessage(callback.Channel.ID,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionResponseURL(callback.ResponseURL, slack.ResponseTypeEphemeral))
	if err != nil {
		log.Printf("slackbot: error posting resolved view: %v", err)
	}
}

func (b *Bot) handleResolveDecision(callback slack.InteractionCallback, action *slack.BlockAction) {
	parts := strings.Split(action.Value, ":")
	if len(parts) != 2 {
		b.postEphemeral(callback.Channel.ID, callback.User.ID, "Invalid action value")
		return
	}

	decisionID := parts[0]
	var chosenIndex int
	_, _ = fmt.Sscanf(parts[1], "%d", &chosenIndex)

	ctx := context.Background()
	decision, err := b.decisions.GetDecision(ctx, decisionID)
	if err != nil {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Error fetching decision: %v", err))
		return
	}

	if decision.Resolved {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Decision %s has already been resolved.", decisionID))
		return
	}

	optionLabel := fmt.Sprintf("Option %d", chosenIndex)
	if chosenIndex > 0 && chosenIndex <= len(decision.Options) {
		optionLabel = decision.Options[chosenIndex-1].Label
	}

	messageTs := callback.Message.Timestamp
	modalRequest := b.buildResolveModal(decisionID, chosenIndex, decision.Question, optionLabel, callback.Channel.ID, messageTs)

	_, err = b.client.OpenView(callback.TriggerID, modalRequest)
	if err != nil {
		log.Printf("slackbot: error opening resolve modal: %v", err)
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Error opening dialog: %v", err))
	}
}

func (b *Bot) handleResolveOther(callback slack.InteractionCallback, decisionID string) {
	ctx := context.Background()
	decision, err := b.decisions.GetDecision(ctx, decisionID)
	if err != nil {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Error fetching decision: %v", err))
		return
	}

	if decision.Resolved {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Decision %s not found or already resolved.", decisionID))
		return
	}

	messageTs := callback.Message.Timestamp
	modalRequest := b.buildOtherModal(decisionID, decision.Question, callback.Channel.ID, messageTs)

	_, err = b.client.OpenView(callback.TriggerID, modalRequest)
	if err != nil {
		log.Printf("slackbot: error opening Other modal: %v", err)
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Error opening dialog: %v", err))
	}
}

func (b *Bot) handleShowContext(callback slack.InteractionCallback, decisionID string) {
	b.decisionMessagesMu.RLock()
	msgInfo, ok := b.decisionMessages[decisionID]
	b.decisionMessagesMu.RUnlock()
	if !ok {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			"Could not find message info for this decision.")
		return
	}

	ctx := context.Background()
	decision, err := b.decisions.GetDecision(ctx, decisionID)
	if err != nil {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Error fetching decision: %v", err))
		return
	}

	var contextDisplay string
	if decision.Context == "" {
		contextDisplay = "(no context provided)"
	} else {
		contextDisplay = decision.Context
		var jsonObj interface{}
		if err := json.Unmarshal([]byte(decision.Context), &jsonObj); err == nil {
			if prettyJSON, err := json.MarshalIndent(jsonObj, "", "  "); err == nil {
				contextDisplay = string(prettyJSON)
			}
		}
	}

	contextDisplay = truncateForSlack(contextDisplay, 2900)

	blocks := []slack.Block{
		slack.NewHeaderBlock(
			slack.NewTextBlockObject("plain_text",
				fmt.Sprintf("Context: %s", decisionID), false, false)),
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("```%s```", contextDisplay), false, false),
			nil, nil),
	}

	_, _, err = b.client.PostMessage(msgInfo.channelID,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionTS(msgInfo.timestamp))
	if err != nil {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Error posting context: %v", err))
	}
}

// ---------- Break Out / Unbreak Out ----------

func (b *Bot) handleBreakOut(callback slack.InteractionCallback, agent string) {
	if b.router == nil {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			"Break Out is not available: channel router not configured")
		return
	}

	if b.router.HasOverride(agent) {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Agent %s already has a dedicated channel.", agent))
		return
	}

	channelName := b.agentToBreakOutChannelName(agent)
	channelID, err := b.ensureBreakOutChannelExists(agent, channelName)
	if err != nil {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Failed to create dedicated channel: %v", err))
		return
	}

	b.router.AddOverrideWithName(agent, channelID, channelName)
	if err := b.router.Save(); err != nil {
		log.Printf("slackbot: failed to save router config after Break Out: %v", err)
	}

	log.Printf("slackbot: Break Out: %s → #%s (%s)", agent, channelName, channelID)

	// Repost pending decisions for this agent to the new channel
	ctx := context.Background()
	pending, err := b.decisions.ListPending(ctx)
	if err == nil {
		for i := range pending {
			if pending[i].RequestedBy == agent {
				if err := b.notifyDecisionToChannel(&pending[i], channelID); err != nil {
					log.Printf("slackbot: break out: failed to repost %s: %v", pending[i].ID, err)
				}
			}
		}
	}

	b.postEphemeral(callback.Channel.ID, callback.User.ID,
		fmt.Sprintf("Broke out *%s* to dedicated channel <#%s>. Future decisions will go there.", agent, channelID))
}

func (b *Bot) handleUnbreakOut(callback slack.InteractionCallback, agent string) {
	if b.router == nil {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			"Unbreak Out is not available: channel router not configured")
		return
	}

	if !b.router.HasOverride(agent) {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Agent %s doesn't have a dedicated channel override.", agent))
		return
	}

	b.router.RemoveOverride(agent)
	if err := b.router.Save(); err != nil {
		log.Printf("slackbot: failed to save router config after Unbreak Out: %v", err)
	}

	result := b.router.Resolve(agent)
	b.postEphemeral(callback.Channel.ID, callback.User.ID,
		fmt.Sprintf("Unbroke out *%s*. Future decisions will go to <#%s>.", agent, result.ChannelID))
}

// ---------- Dismiss ----------

func (b *Bot) handleDismissDecision(callback slack.InteractionCallback, decisionID string) {
	messageTs := callback.Message.Timestamp
	if messageTs == "" {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			"Could not dismiss: message timestamp not found")
		return
	}

	_, _, err := b.client.DeleteMessage(callback.Channel.ID, messageTs)
	if err != nil {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Failed to dismiss: %v", err))
		return
	}

	log.Printf("slackbot: dismissed decision %s", decisionID)
}

// DismissDecisionByID deletes a decision's Slack notification message.
func (b *Bot) DismissDecisionByID(decisionID string) bool {
	b.decisionMessagesMu.RLock()
	msgInfo, found := b.decisionMessages[decisionID]
	b.decisionMessagesMu.RUnlock()
	if !found {
		return false
	}

	_, _, err := b.client.DeleteMessage(msgInfo.channelID, msgInfo.timestamp)
	if err != nil {
		log.Printf("slackbot: failed to auto-dismiss %s: %v", decisionID, err)
		return false
	}

	// Decrement agent pending count for rig routing mode
	if msgInfo.agent != "" {
		b.decrementAgentPending(msgInfo.agent)
		b.updateAgentStatusCard(msgInfo.agent)
	}

	b.decisionMessagesMu.Lock()
	delete(b.decisionMessages, decisionID)
	b.decisionMessagesMu.Unlock()
	return true
}

// ---------- Preferences ----------

func (b *Bot) handleOpenPreferences(callback slack.InteractionCallback, decisionID string) {
	userID := callback.User.ID

	if decisionID != "" {
		b.sendDecisionAsDM(decisionID, userID, callback.Channel.ID)
	}

	modalRequest := b.buildPreferencesModal(userID)
	_, err := b.client.OpenView(callback.TriggerID, modalRequest)
	if err != nil {
		log.Printf("slackbot: error opening preferences modal: %v", err)
		b.postEphemeral(callback.Channel.ID, userID,
			fmt.Sprintf("Error opening preferences: %v", err))
	}
}

func (b *Bot) sendDecisionAsDM(decisionID, userID, sourceChannelID string) {
	ctx := context.Background()
	decision, err := b.decisions.GetDecision(ctx, decisionID)
	if err != nil || decision == nil {
		b.postEphemeral(sourceChannelID, userID,
			fmt.Sprintf("Could not fetch decision: %v", err))
		return
	}

	channel, _, _, err := b.client.OpenConversation(&slack.OpenConversationParameters{
		Users: []string{userID},
	})
	if err != nil {
		b.postEphemeral(sourceChannelID, userID,
			fmt.Sprintf("Could not open DM: %v", err))
		return
	}

	urgencyEmoji := urgencyToEmoji(decision.Urgency)
	agentInfo := ""
	if decision.RequestedBy != "" {
		agentInfo = fmt.Sprintf(" from *%s*", decision.RequestedBy)
	}

	displayID := decision.SemanticSlug
	if displayID == "" {
		displayID = decision.ID
	}

	headerText := fmt.Sprintf("%s *%s*%s\n%s", urgencyEmoji, displayID, agentInfo, decision.Question)

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", headerText, false, false),
			nil, nil),
	}

	if len(decision.Options) > 0 {
		var optionButtons []slack.BlockElement
		for i, opt := range decision.Options {
			if i >= 5 {
				break
			}
			btn := slack.NewButtonBlockElement(
				fmt.Sprintf("resolve_%s_%d", decision.ID, i+1),
				fmt.Sprintf("%s:%d", decision.ID, i+1),
				slack.NewTextBlockObject("plain_text", opt.Label, false, false))
			optionButtons = append(optionButtons, btn)
		}
		if len(optionButtons) > 0 {
			blocks = append(blocks, slack.NewActionBlock("", optionButtons...))
		}
	}

	_, _, err = b.client.PostMessage(channel.ID,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(fmt.Sprintf("Decision: %s", decision.Question), false))
	if err != nil {
		log.Printf("slackbot: failed to DM decision %s to %s: %v", decisionID, userID, err)
	}
}

// ---------- Modals ----------

func (b *Bot) buildResolveModal(decisionID string, chosenIndex int, question, optionLabel, channelID, messageTs string) slack.ModalViewRequest {
	displayQuestion := question
	if len(displayQuestion) > 200 {
		displayQuestion = displayQuestion[:197] + "..."
	}

	metadataLabel := optionLabel
	if len(metadataLabel) > 100 {
		metadataLabel = metadataLabel[:97] + "..."
	}

	// Private metadata format: id:index:channel:messageTs|label
	metadata := fmt.Sprintf("%s:%d:%s:%s|%s", decisionID, chosenIndex, channelID, messageTs, metadataLabel)

	return slack.ModalViewRequest{
		Type:            slack.VTModal,
		CallbackID:      "resolve_decision_modal",
		Title:           slack.NewTextBlockObject("plain_text", "Resolve Decision", false, false),
		Submit:          slack.NewTextBlockObject("plain_text", "Resolve", false, false),
		Close:           slack.NewTextBlockObject("plain_text", "Cancel", false, false),
		PrivateMetadata: metadata,
		Blocks: slack.Blocks{
			BlockSet: []slack.Block{
				slack.NewSectionBlock(
					slack.NewTextBlockObject("mrkdwn",
						fmt.Sprintf("*Decision:* %s\n\n%s", decisionID, displayQuestion),
						false, false),
					nil, nil),
				slack.NewSectionBlock(
					slack.NewTextBlockObject("mrkdwn",
						fmt.Sprintf("*Selected Option:* %s", optionLabel),
						false, false),
					nil, nil),
				slack.NewDividerBlock(),
				func() *slack.InputBlock {
					ib := slack.NewInputBlock(
						"rationale_block",
						slack.NewTextBlockObject("plain_text", "Rationale", false, false),
						slack.NewTextBlockObject("plain_text", "Optionally explain your reasoning", false, false),
						slack.NewPlainTextInputBlockElement(
							slack.NewTextBlockObject("plain_text", "Enter your reasoning...", false, false),
							"rationale_input"))
					ib.Optional = true
					return ib
				}(),
			},
		},
	}
}

func (b *Bot) buildOtherModal(decisionID, question, channelID, messageTs string) slack.ModalViewRequest {
	displayQuestion := question
	if len(displayQuestion) > 200 {
		displayQuestion = displayQuestion[:197] + "..."
	}

	metadata := fmt.Sprintf("other:%s:%s:%s", decisionID, channelID, messageTs)

	return slack.ModalViewRequest{
		Type:            slack.VTModal,
		CallbackID:      "resolve_other_modal",
		Title:           slack.NewTextBlockObject("plain_text", "Custom Response", false, false),
		Submit:          slack.NewTextBlockObject("plain_text", "Submit", false, false),
		Close:           slack.NewTextBlockObject("plain_text", "Cancel", false, false),
		PrivateMetadata: metadata,
		Blocks: slack.Blocks{
			BlockSet: []slack.Block{
				slack.NewSectionBlock(
					slack.NewTextBlockObject("mrkdwn",
						fmt.Sprintf("*Decision:* %s\n\n%s", decisionID, displayQuestion),
						false, false),
					nil, nil),
				slack.NewSectionBlock(
					slack.NewTextBlockObject("mrkdwn",
						"*None of the predefined options fit?*\nProvide your own guidance below.",
						false, false),
					nil, nil),
				slack.NewDividerBlock(),
				func() *slack.InputBlock {
					textInput := slack.NewPlainTextInputBlockElement(
						slack.NewTextBlockObject("plain_text", "Enter your response...", false, false),
						"custom_text_input")
					textInput.Multiline = true
					return slack.NewInputBlock(
						"custom_text_block",
						slack.NewTextBlockObject("plain_text", "Your Response", false, false),
						slack.NewTextBlockObject("plain_text", "Describe what you want the agent to do", false, false),
						textInput)
				}(),
			},
		},
	}
}

func (b *Bot) buildPreferencesModal(userID string) slack.ModalViewRequest {
	prefs := b.preferenceManager.GetUserPreferences(userID)

	levelOptions := []*slack.OptionBlockObject{
		slack.NewOptionBlockObject("all", slack.NewTextBlockObject("plain_text", "All decisions", false, false), nil),
		slack.NewOptionBlockObject("high", slack.NewTextBlockObject("plain_text", "High urgency only", false, false), nil),
		slack.NewOptionBlockObject("muted", slack.NewTextBlockObject("plain_text", "Muted", false, false), nil),
	}

	var currentLevel *slack.OptionBlockObject
	for _, opt := range levelOptions {
		if opt.Value == prefs.NotificationLevel {
			currentLevel = opt
			break
		}
	}
	if currentLevel == nil {
		currentLevel = levelOptions[1]
	}

	dmOptInOption := slack.NewOptionBlockObject(
		"dm_opt_in",
		slack.NewTextBlockObject("mrkdwn", "*Receive decisions as DMs*\nGet decision notifications as direct messages", false, false),
		nil)
	dmCheckbox := slack.NewCheckboxGroupsBlockElement("dm_opt_in", dmOptInOption)
	if prefs.DMOptIn {
		dmCheckbox.InitialOptions = []*slack.OptionBlockObject{dmOptInOption}
	}

	threadOption := slack.NewOptionBlockObject(
		"thread_notify",
		slack.NewTextBlockObject("mrkdwn", "*Thread reply notifications*\nGet notified when decisions are resolved", false, false),
		nil)
	threadCheckbox := slack.NewCheckboxGroupsBlockElement("thread_notify", threadOption)
	if prefs.ThreadNotifications {
		threadCheckbox.InitialOptions = []*slack.OptionBlockObject{threadOption}
	}

	return slack.ModalViewRequest{
		Type:            slack.VTModal,
		CallbackID:      "preferences_modal",
		Title:           slack.NewTextBlockObject("plain_text", "Notification Preferences", false, false),
		Submit:          slack.NewTextBlockObject("plain_text", "Save", false, false),
		Close:           slack.NewTextBlockObject("plain_text", "Cancel", false, false),
		PrivateMetadata: userID,
		Blocks: slack.Blocks{
			BlockSet: []slack.Block{
				slack.NewSectionBlock(
					slack.NewTextBlockObject("mrkdwn",
						"Configure how you receive decision notifications.",
						false, false),
					nil, nil),
				slack.NewDividerBlock(),
				slack.NewInputBlock("dm_opt_in_block",
					slack.NewTextBlockObject("plain_text", "Direct Messages", false, false),
					nil, dmCheckbox).WithOptional(true),
				slack.NewInputBlock("notification_level_block",
					slack.NewTextBlockObject("plain_text", "Notification Level", false, false),
					nil,
					slack.NewOptionsSelectBlockElement(
						slack.OptTypeStatic,
						slack.NewTextBlockObject("plain_text", "Select level", false, false),
						"notification_level",
						levelOptions...).WithInitialOption(currentLevel)),
				slack.NewInputBlock("thread_notify_block",
					slack.NewTextBlockObject("plain_text", "Thread Notifications", false, false),
					nil, threadCheckbox).WithOptional(true),
			},
		},
	}
}

// ---------- View submissions ----------

func (b *Bot) handleViewSubmission(callback slack.InteractionCallback) {
	switch callback.View.CallbackID {
	case "resolve_decision_modal":
		b.handleResolveModalSubmission(callback)
	case "resolve_other_modal":
		b.handleOtherModalSubmission(callback)
	case "preferences_modal":
		b.handlePreferencesModalSubmission(callback)
	}
}

func (b *Bot) handleResolveModalSubmission(callback slack.InteractionCallback) {
	metadata := callback.View.PrivateMetadata
	labelSep := strings.LastIndex(metadata, "|")
	optionLabel := ""
	if labelSep > 0 {
		optionLabel = metadata[labelSep+1:]
		metadata = metadata[:labelSep]
	}

	parts := strings.Split(metadata, ":")
	if len(parts) < 4 {
		log.Printf("slackbot: invalid resolve modal metadata: %s", callback.View.PrivateMetadata)
		return
	}

	decisionID := parts[0]
	var chosenIndex int
	_, _ = fmt.Sscanf(parts[1], "%d", &chosenIndex)
	channelID := parts[2]
	messageTs := parts[3]

	rationale := ""
	if rationaleBlock, ok := callback.View.State.Values["rationale_block"]; ok {
		if rationaleInput, ok := rationaleBlock["rationale_input"]; ok {
			rationale = rationaleInput.Value
		}
	}

	userAttribution := fmt.Sprintf("Resolved via Slack by <@%s>", callback.User.ID)
	if rationale == "" {
		rationale = userAttribution
	} else {
		rationale = rationale + "\n\n— " + userAttribution
	}

	ctx := context.Background()
	resolvedBy := fmt.Sprintf("slack:%s", callback.User.ID)
	resolved, err := b.decisions.Resolve(ctx, decisionID, chosenIndex, rationale, resolvedBy)
	if err != nil {
		b.postErrorMessage(channelID, callback.User.ID, decisionID, err)
		return
	}

	if messageTs != "" {
		b.updateMessageAsResolved(channelID, messageTs, resolved, callback.User.ID)
	} else {
		b.postResolutionConfirmation(channelID, callback.User.ID, optionLabel, rationale)
	}

	if b.channelID != "" && b.channelID != channelID {
		b.postResolutionNotification(optionLabel, callback.User.ID)
	}
}

func (b *Bot) handleOtherModalSubmission(callback slack.InteractionCallback) {
	metadata := callback.View.PrivateMetadata
	parts := strings.Split(metadata, ":")
	if len(parts) < 4 || parts[0] != "other" {
		log.Printf("slackbot: invalid Other modal metadata: %s", metadata)
		return
	}

	decisionID := parts[1]
	channelID := parts[2]
	messageTs := parts[3]

	customText := ""
	if customBlock, ok := callback.View.State.Values["custom_text_block"]; ok {
		if customInput, ok := customBlock["custom_text_input"]; ok {
			customText = customInput.Value
		}
	}

	if customText == "" {
		b.postEphemeral(channelID, callback.User.ID, "Custom response text is required.")
		return
	}

	userAttribution := fmt.Sprintf("Resolved via Slack (Other) by <@%s>", callback.User.ID)
	fullText := customText + "\n\n— " + userAttribution

	ctx := context.Background()
	resolvedBy := fmt.Sprintf("slack:%s", callback.User.ID)
	resolved, err := b.decisions.ResolveWithText(ctx, decisionID, fullText, resolvedBy)
	if err != nil {
		b.postErrorMessage(channelID, callback.User.ID, decisionID, err)
		return
	}

	if messageTs != "" {
		b.updateMessageAsResolved(channelID, messageTs, resolved, callback.User.ID)
	} else {
		b.postEphemeral(channelID, callback.User.ID,
			fmt.Sprintf("Decision resolved with custom response: %s", customText))
	}
}

func (b *Bot) handlePreferencesModalSubmission(callback slack.InteractionCallback) {
	userID := callback.View.PrivateMetadata
	if userID == "" {
		userID = callback.User.ID
	}

	values := callback.View.State.Values

	dmOptIn := false
	if dmBlock, ok := values["dm_opt_in_block"]; ok {
		if dmInput, ok := dmBlock["dm_opt_in"]; ok {
			dmOptIn = len(dmInput.SelectedOptions) > 0
		}
	}

	notificationLevel := "high"
	if levelBlock, ok := values["notification_level_block"]; ok {
		if levelInput, ok := levelBlock["notification_level"]; ok {
			if levelInput.SelectedOption.Value != "" {
				notificationLevel = levelInput.SelectedOption.Value
			}
		}
	}

	threadNotify := false
	if threadBlock, ok := values["thread_notify_block"]; ok {
		if threadInput, ok := threadBlock["thread_notify"]; ok {
			threadNotify = len(threadInput.SelectedOptions) > 0
		}
	}

	_ = b.preferenceManager.SetDMOptIn(userID, dmOptIn)
	_ = b.preferenceManager.SetNotificationLevel(userID, notificationLevel)
	_ = b.preferenceManager.SetThreadNotifications(userID, threadNotify)

	if err := b.preferenceManager.Save(); err != nil {
		log.Printf("slackbot: warning: failed to save preferences: %v", err)
	}

	log.Printf("slackbot: preferences saved for %s: dm=%v level=%s thread=%v",
		userID, dmOptIn, notificationLevel, threadNotify)
}

// ---------- Notifications ----------

// NotifyNewDecision posts a new decision notification to the appropriate channel.
func (b *Bot) NotifyNewDecision(decision *Decision) error {
	targetChannel := b.resolveChannelForDecision(decision)
	if targetChannel == "" {
		return fmt.Errorf("no target channel for decision %s", decision.ID)
	}

	urgencyEmoji := urgencyToEmoji(decision.Urgency)
	agentInfo := ""
	if decision.RequestedBy != "" {
		agentInfo = fmt.Sprintf(" from *%s*", decision.RequestedBy)
	}

	displayID := decision.SemanticSlug
	if displayID == "" {
		displayID = decision.ID
	}

	typeEmoji, typeLabel := buildTypeHeader(decision.Context)
	var headerText string
	if typeLabel != "" {
		headerText = fmt.Sprintf("%s %s *%s*: %s%s\n%s",
			urgencyEmoji, typeEmoji, typeLabel, displayID, agentInfo, decision.Question)
	} else {
		headerText = fmt.Sprintf("%s *%s*%s\n%s",
			urgencyEmoji, displayID, agentInfo, decision.Question)
	}

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", headerText, false, false),
			nil, nil),
	}

	if decision.PredecessorID != "" {
		blocks = append(blocks,
			slack.NewContextBlock("",
				slack.NewTextBlockObject("mrkdwn",
					fmt.Sprintf(":link: _Chained from: %s_", decision.PredecessorID),
					false, false)))
	}

	if decision.Context != "" {
		contextText := formatContextForSlack(decision.Context)
		if contextText != "" {
			blocks = append(blocks,
				slack.NewSectionBlock(
					slack.NewTextBlockObject("mrkdwn", contextText, false, false),
					nil, nil))
		}
	}

	if len(decision.Options) > 0 {
		blocks = append(blocks, slack.NewDividerBlock())

		for i, opt := range decision.Options {
			label := opt.Label
			if opt.Recommended {
				label = "* " + label
			}

			optText := fmt.Sprintf("*%d. %s*", i+1, label)
			if opt.Description != "" {
				desc := opt.Description
				if len(desc) > 150 {
					desc = desc[:147] + "..."
				}
				optText += fmt.Sprintf("\n%s", desc)
			}

			buttonLabel := "Choose"
			if len(decision.Options) <= 4 {
				buttonLabel = fmt.Sprintf("Choose %d", i+1)
			}

			blocks = append(blocks,
				slack.NewSectionBlock(
					slack.NewTextBlockObject("mrkdwn", optText, false, false),
					nil,
					slack.NewAccessory(
						slack.NewButtonBlockElement(
							fmt.Sprintf("resolve_%s_%d", decision.ID, i+1),
							fmt.Sprintf("%s:%d", decision.ID, i+1),
							slack.NewTextBlockObject("plain_text", buttonLabel, false, false)))))
		}

		// "Other" option for custom text response
		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn",
					"*Other*\n_None of the above? Provide your own response._",
					false, false),
				nil,
				slack.NewAccessory(
					slack.NewButtonBlockElement(
						fmt.Sprintf("resolve_other_%s", decision.ID),
						decision.ID,
						slack.NewTextBlockObject("plain_text", "Other...", false, false)))))
	}

	// Action buttons: Dismiss, Peek, DM Me, Break Out/Unbreak Out
	dismissButton := slack.NewButtonBlockElement("dismiss_decision", decision.ID,
		slack.NewTextBlockObject("plain_text", "Dismiss", false, false))
	peekButton := slack.NewButtonBlockElement("peek_"+decision.ID, decision.ID,
		slack.NewTextBlockObject("plain_text", "Peek", false, false))
	dmButton := slack.NewButtonBlockElement("open_preferences", decision.ID,
		slack.NewTextBlockObject("plain_text", "DM Me", false, false))

	if decision.RequestedBy != "" {
		var breakOutButton *slack.ButtonBlockElement
		if b.router != nil && b.router.HasOverride(decision.RequestedBy) {
			breakOutButton = slack.NewButtonBlockElement("unbreak_out", decision.RequestedBy,
				slack.NewTextBlockObject("plain_text", "Unbreak Out", false, false))
		} else {
			breakOutButton = slack.NewButtonBlockElement("break_out", decision.RequestedBy,
				slack.NewTextBlockObject("plain_text", "Break Out", false, false))
		}
		blocks = append(blocks,
			slack.NewActionBlock("", dismissButton, peekButton, dmButton, breakOutButton))
	} else {
		blocks = append(blocks,
			slack.NewActionBlock("", dismissButton, peekButton, dmButton))
	}

	// Threading: in rig mode, thread under agent's status card.
	// Otherwise, thread under predecessor decision if present.
	var msgOpts []slack.MsgOption
	msgOpts = append(msgOpts, slack.MsgOptionBlocks(blocks...))

	var statusCardTS string
	var predecessorThreadTS string
	agent := decision.RequestedBy

	if b.isRigRoutingMode() && agent != "" {
		// Rig routing: thread decisions under the agent's status card
		statusCardTS = b.ensureAgentStatusCard(agent, targetChannel)
		if statusCardTS != "" {
			msgOpts = append(msgOpts, slack.MsgOptionTS(statusCardTS))
		}
	} else if decision.PredecessorID != "" {
		// Non-rig: thread under predecessor decision
		b.decisionMessagesMu.RLock()
		predMsgInfo, hasPredecessor := b.decisionMessages[decision.PredecessorID]
		b.decisionMessagesMu.RUnlock()
		if hasPredecessor && predMsgInfo.channelID == targetChannel {
			predecessorThreadTS = predMsgInfo.timestamp
			msgOpts = append(msgOpts, slack.MsgOptionTS(predecessorThreadTS))
		}
	}

	_, ts, err := b.client.PostMessage(targetChannel, msgOpts...)
	if err != nil {
		log.Printf("slackbot: error posting decision %s to %s: %v", decision.ID, targetChannel, err)
		return err
	}

	if ts != "" {
		b.decisionMessagesMu.Lock()
		b.decisionMessages[decision.ID] = messageInfo{
			channelID: targetChannel,
			timestamp: ts,
			agent:     agent,
		}
		b.decisionMessagesMu.Unlock()

		if statusCardTS != "" {
			// Rig mode: update agent status card with pending count
			b.incrementAgentPending(agent)
			b.updateAgentStatusCard(agent)
		} else if predecessorThreadTS != "" {
			b.markDecisionSuperseded(decision.PredecessorID, decision.ID, targetChannel, predecessorThreadTS)
		}
	}

	// Send DMs to opted-in users
	b.sendDMsToOptedInUsers(decision, blocks)

	return nil
}

// NotifyResolution updates or posts a resolution notification.
func (b *Bot) NotifyResolution(decision *Decision) error {
	if strings.HasPrefix(decision.ResolvedBy, "slack:") {
		return nil
	}

	optionLabel := "Unknown"
	if decision.ChosenIndex > 0 && decision.ChosenIndex <= len(decision.Options) {
		optionLabel = decision.Options[decision.ChosenIndex-1].Label
	}

	resolvedBy := decision.ResolvedBy
	if resolvedBy == "" {
		resolvedBy = "unknown"
	}

	b.decisionMessagesMu.RLock()
	msgInfo, hasTracked := b.decisionMessages[decision.ID]
	b.decisionMessagesMu.RUnlock()

	if hasTracked {
		b.updateMessageAsResolved(msgInfo.channelID, msgInfo.timestamp, decision, resolvedBy)

		// Decrement agent pending count for rig routing mode
		if msgInfo.agent != "" {
			b.decrementAgentPending(msgInfo.agent)
			b.updateAgentStatusCard(msgInfo.agent)
		}

		b.decisionMessagesMu.Lock()
		delete(b.decisionMessages, decision.ID)
		b.decisionMessagesMu.Unlock()
		return nil
	}

	// Post new resolution message
	targetChannel := b.resolveChannelForDecision(decision)
	if targetChannel == "" {
		return nil
	}

	displayID := decision.SemanticSlug
	if displayID == "" {
		displayID = decision.ID
	}

	resolverText := formatResolver(resolvedBy)

	var blocks []slack.Block
	blocks = append(blocks,
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf(":clipboard: *Decision Resolved: %s*", displayID), false, false),
			nil, nil),
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("*Question:* %s", decision.Question), false, false),
			nil, nil))

	if decision.RequestedBy != "" {
		blocks = append(blocks,
			slack.NewContextBlock("",
				slack.NewTextBlockObject("mrkdwn",
					fmt.Sprintf("*Requested by:* %s", decision.RequestedBy), false, false)))
	}

	if len(decision.Options) > 0 {
		blocks = append(blocks, slack.NewDividerBlock())
		optionsText := "*Options:*\n"
		for i, opt := range decision.Options {
			prefix := "○"
			if i+1 == decision.ChosenIndex {
				prefix = "chosen:"
			}
			optionsText += fmt.Sprintf("%s %d. *%s*", prefix, i+1, opt.Label)
			if opt.Description != "" {
				desc := opt.Description
				if len(desc) > 100 {
					desc = desc[:97] + "..."
				}
				optionsText += fmt.Sprintf(" — _%s_", desc)
			}
			optionsText += "\n"
		}
		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", optionsText, false, false),
				nil, nil))
	}

	blocks = append(blocks, slack.NewDividerBlock())
	resolutionText := fmt.Sprintf("*Choice:* %s\n*Resolved by:* %s", optionLabel, resolverText)
	if decision.Rationale != "" {
		displayRationale := decision.Rationale
		if len(displayRationale) > 400 {
			displayRationale = displayRationale[:397] + "..."
		}
		resolutionText = fmt.Sprintf("*Choice:* %s\n*Rationale:* %s\n*Resolved by:* %s",
			optionLabel, displayRationale, resolverText)
	}
	blocks = append(blocks,
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", resolutionText, false, false),
			nil, nil))

	_, _, err := b.client.PostMessage(targetChannel, slack.MsgOptionBlocks(blocks...))
	return err
}

// NotifyEscalation posts a highlighted notification for an escalated decision.
func (b *Bot) NotifyEscalation(decision *Decision) error {
	targetChannel := b.resolveChannelForDecision(decision)
	if targetChannel == "" {
		return nil
	}

	displayID := decision.SemanticSlug
	if displayID == "" {
		displayID = decision.ID
	}

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf(":rotating_light: *ESCALATED: %s*\n%s",
					displayID, decision.Question),
				false, false),
			nil, nil),
	}

	if decision.RequestedBy != "" {
		blocks = append(blocks,
			slack.NewContextBlock("",
				slack.NewTextBlockObject("mrkdwn",
					fmt.Sprintf("*Requested by:* %s | *Urgency:* %s",
						decision.RequestedBy, decision.Urgency),
					false, false)))
	}

	_, _, err := b.client.PostMessage(targetChannel, slack.MsgOptionBlocks(blocks...))
	return err
}

// ---------- Message updates ----------

func (b *Bot) updateMessageAsResolved(channelID, messageTs string, decision *Decision, resolverID string) {
	displayID := decision.SemanticSlug
	if displayID == "" {
		displayID = decision.ID
	}

	resolverText := formatResolver(resolverID)

	var blocks []slack.Block
	blocks = append(blocks,
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("*%s* — Resolved", displayID), false, false),
			nil, nil),
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("*Question:* %s", decision.Question), false, false),
			nil, nil))

	if decision.RequestedBy != "" {
		blocks = append(blocks,
			slack.NewContextBlock("",
				slack.NewTextBlockObject("mrkdwn",
					fmt.Sprintf("*Requested by:* %s", decision.RequestedBy), false, false)))
	}

	blocks = append(blocks, slack.NewDividerBlock())

	if len(decision.Options) > 0 {
		optionsText := "*Options:*\n"
		for i, opt := range decision.Options {
			prefix := "○"
			if i+1 == decision.ChosenIndex {
				prefix = "chosen:"
			}
			label := opt.Label
			if opt.Recommended {
				label += " *"
			}
			optionsText += fmt.Sprintf("%s %d. *%s*", prefix, i+1, label)
			if opt.Description != "" {
				desc := opt.Description
				if len(desc) > 100 {
					desc = desc[:97] + "..."
				}
				optionsText += fmt.Sprintf(" — _%s_", desc)
			}
			optionsText += "\n"
		}
		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", optionsText, false, false),
				nil, nil))
	}

	blocks = append(blocks, slack.NewDividerBlock())

	chosenLabel := "Unknown"
	if decision.ChosenIndex > 0 && decision.ChosenIndex <= len(decision.Options) {
		chosenLabel = decision.Options[decision.ChosenIndex-1].Label
	}
	resolutionText := fmt.Sprintf("*Choice:* %s\n", chosenLabel)
	if decision.Rationale != "" {
		displayRationale := decision.Rationale
		if len(displayRationale) > 400 {
			displayRationale = displayRationale[:397] + "..."
		}
		resolutionText += fmt.Sprintf("*Rationale:* %s\n", displayRationale)
	}
	resolutionText += fmt.Sprintf("*Resolved by:* %s", resolverText)

	blocks = append(blocks,
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", resolutionText, false, false),
			nil, nil))

	_, _, _, err := b.client.UpdateMessage(channelID, messageTs,
		slack.MsgOptionBlocks(blocks...))
	if err != nil {
		log.Printf("slackbot: failed to update message as resolved: %v", err)
	}
}

func (b *Bot) markDecisionSuperseded(predecessorID, newDecisionID, channelID, messageTs string) {
	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("*Superseded*\n\nA follow-up decision (`%s`) has been posted in this thread.\n_Please refer to the latest decision in the thread below._", newDecisionID),
				false, false),
			nil, nil),
		slack.NewContextBlock("",
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("Original decision: `%s`", predecessorID), false, false)),
	}

	_, _, _, err := b.client.UpdateMessage(channelID, messageTs,
		slack.MsgOptionBlocks(blocks...))
	if err != nil {
		log.Printf("slackbot: failed to mark %s as superseded: %v", predecessorID, err)
	}
}

// ---------- Channel routing ----------

func (b *Bot) resolveChannel(agent string) string {
	if b.router != nil && b.router.IsEnabled() && agent != "" {
		result := b.router.Resolve(agent)
		if result != nil && result.ChannelID != "" && !result.IsDefault {
			return result.ChannelID
		}
	}

	if b.dynamicChannels && agent != "" {
		channelID, err := b.ensureChannelExists(agent)
		if err != nil {
			log.Printf("slackbot: failed to ensure channel for %s: %v", agent, err)
		} else if channelID != "" {
			return channelID
		}
	}

	if b.router != nil && b.router.IsEnabled() {
		cfg := b.router.GetConfig()
		if cfg != nil && cfg.DefaultChannel != "" {
			return cfg.DefaultChannel
		}
	}

	return b.channelID
}

func (b *Bot) resolveChannelForDecision(decision *Decision) string {
	// Use router if available and enabled
	if b.router != nil && b.router.IsEnabled() {
		result := b.router.ResolveForDecision(decision.RequestedBy, decision, "")
		if result != nil && result.ChannelID != "" {
			return result.ChannelID
		}
	}
	return b.channelID
}

// isRigRoutingMode returns true if the bot is configured for rig-based routing.
func (b *Bot) isRigRoutingMode() bool {
	if b.router == nil || !b.router.IsEnabled() {
		return false
	}
	return b.router.GetConfig().RoutingMode == "rig"
}

// ensureAgentStatusCard ensures a persistent top-level status card message
// exists for the given agent in the specified channel. Returns the status
// card's message timestamp for threading.
func (b *Bot) ensureAgentStatusCard(agent, channelID string) string {
	b.agentStatusCardsMu.RLock()
	card, ok := b.agentStatusCards[agent]
	b.agentStatusCardsMu.RUnlock()
	if ok && card.channelID == channelID {
		return card.timestamp
	}

	// Create a new status card
	blocks := b.buildAgentStatusCardBlocks(agent, 0)
	_, ts, err := b.client.PostMessage(channelID, slack.MsgOptionBlocks(blocks...))
	if err != nil {
		log.Printf("slackbot: error creating status card for %s in %s: %v", agent, channelID, err)
		return ""
	}

	b.agentStatusCardsMu.Lock()
	b.agentStatusCards[agent] = messageInfo{channelID: channelID, timestamp: ts}
	b.agentStatusCardsMu.Unlock()

	// Persist so status card survives bot restarts
	if b.stateManager != nil {
		if err := b.stateManager.SetAgentCard(agent, channelID, ts); err != nil {
			log.Printf("slackbot: warning: failed to persist status card for %s: %v", agent, err)
		}
	}

	log.Printf("slackbot: created status card for %s in %s (ts=%s)", agent, channelID, ts)
	return ts
}

// updateAgentStatusCard updates the status card message with current pending count.
func (b *Bot) updateAgentStatusCard(agent string) {
	b.agentStatusCardsMu.RLock()
	card, ok := b.agentStatusCards[agent]
	b.agentStatusCardsMu.RUnlock()
	if !ok {
		return
	}

	b.agentPendingMu.Lock()
	count := b.agentPendingCount[agent]
	b.agentPendingMu.Unlock()

	blocks := b.buildAgentStatusCardBlocks(agent, count)
	_, _, _, err := b.client.UpdateMessage(card.channelID, card.timestamp, slack.MsgOptionBlocks(blocks...))
	if err != nil {
		log.Printf("slackbot: error updating status card for %s: %v", agent, err)
	}
}

// buildAgentStatusCardBlocks builds the Block Kit blocks for an agent status card.
func (b *Bot) buildAgentStatusCardBlocks(agent string, pendingCount int) []slack.Block {
	parts := strings.Split(agent, "/")
	rig := parts[0]
	agentName := agent
	if len(parts) >= 3 {
		agentName = parts[2]
	} else if len(parts) >= 2 {
		agentName = parts[1]
	}

	statusEmoji := ":white_circle:"
	statusText := "idle"
	if pendingCount > 0 {
		statusEmoji = ":large_yellow_circle:"
		statusText = fmt.Sprintf("%d pending", pendingCount)
		if pendingCount == 1 {
			statusText = "1 pending"
		}
	}

	headerText := fmt.Sprintf("%s *%s* · _%s_ · %s", statusEmoji, agentName, rig, statusText)

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", headerText, false, false),
			nil, nil),
		slack.NewContextBlock("",
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf(":busts_in_silhouette: `%s` · Decisions appear as thread replies below", agent),
				false, false)),
	}

	return blocks
}

// incrementAgentPending increments the pending decision count for an agent.
func (b *Bot) incrementAgentPending(agent string) {
	b.agentPendingMu.Lock()
	b.agentPendingCount[agent]++
	b.agentPendingMu.Unlock()
}

// decrementAgentPending decrements the pending decision count for an agent.
func (b *Bot) decrementAgentPending(agent string) {
	b.agentPendingMu.Lock()
	if b.agentPendingCount[agent] > 0 {
		b.agentPendingCount[agent]--
	}
	b.agentPendingMu.Unlock()
}

func (b *Bot) agentToChannelName(agent string) string {
	parts := strings.Split(agent, "/")
	var nameParts []string
	if len(parts) >= 2 {
		nameParts = parts[:2]
	} else {
		nameParts = parts
	}

	name := b.channelPrefix + "-" + strings.Join(nameParts, "-")
	name = sanitizeChannelName(name)
	return name
}

func (b *Bot) agentToBreakOutChannelName(agent string) string {
	parts := strings.Split(agent, "/")
	name := b.channelPrefix + "-" + strings.Join(parts, "-")
	name = sanitizeChannelName(name)
	return name
}

// sanitizeChannelName is defined in router.go

func (b *Bot) ensureChannelExists(agent string) (string, error) {
	channelName := b.agentToChannelName(agent)
	return b.ensureChannelByName(channelName)
}

func (b *Bot) ensureBreakOutChannelExists(agent, channelName string) (string, error) {
	return b.ensureChannelByName(channelName)
}

func (b *Bot) ensureChannelByName(channelName string) (string, error) {
	b.channelCacheMu.RLock()
	if cachedID, ok := b.channelCache[channelName]; ok {
		b.channelCacheMu.RUnlock()
		return cachedID, nil
	}
	b.channelCacheMu.RUnlock()

	channelID, err := b.findChannelByName(channelName)
	if err == nil && channelID != "" {
		b.cacheChannel(channelName, channelID)
		return channelID, nil
	}

	channel, err := b.client.CreateConversation(slack.CreateConversationParams{
		ChannelName: channelName,
		IsPrivate:   false,
	})
	if err != nil {
		if strings.Contains(err.Error(), "name_taken") {
			channelID, findErr := b.findChannelByName(channelName)
			if findErr == nil && channelID != "" {
				b.cacheChannel(channelName, channelID)
				return channelID, nil
			}
		}
		return "", fmt.Errorf("create channel %s: %w", channelName, err)
	}

	b.cacheChannel(channelName, channel.ID)
	_ = b.autoInviteToChannel(channel.ID)
	return channel.ID, nil
}

func (b *Bot) findChannelByName(name string) (string, error) {
	var cursor string
	for {
		params := &slack.GetConversationsParameters{
			Types:           []string{"public_channel"},
			Limit:           200,
			Cursor:          cursor,
			ExcludeArchived: true,
		}

		channels, nextCursor, err := b.client.GetConversations(params)
		if err != nil {
			return "", err
		}

		for _, ch := range channels {
			if ch.Name == name {
				return ch.ID, nil
			}
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return "", nil
}

func (b *Bot) cacheChannel(name, id string) {
	b.channelCacheMu.Lock()
	b.channelCache[name] = id
	b.channelCacheMu.Unlock()
}

func (b *Bot) autoInviteToChannel(channelID string) error {
	if len(b.autoInviteUsers) == 0 {
		return nil
	}

	_, err := b.client.InviteUsersToConversation(channelID, b.autoInviteUsers...)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "already_in_channel") || strings.Contains(errStr, "cant_invite_self") {
			return nil
		}
		return err
	}
	return nil
}

// notifyDecisionToChannel posts a decision notification to a specific channel.
func (b *Bot) notifyDecisionToChannel(decision *Decision, channelID string) error {
	urgencyEmoji := urgencyToEmoji(decision.Urgency)
	agentInfo := ""
	if decision.RequestedBy != "" {
		agentInfo = fmt.Sprintf(" from *%s*", decision.RequestedBy)
	}

	displayID := decision.SemanticSlug
	if displayID == "" {
		displayID = decision.ID
	}

	typeEmoji, typeLabel := buildTypeHeader(decision.Context)
	var headerText string
	if typeLabel != "" {
		headerText = fmt.Sprintf("%s %s *%s*: %s%s\n%s",
			urgencyEmoji, typeEmoji, typeLabel, displayID, agentInfo, decision.Question)
	} else {
		headerText = fmt.Sprintf("%s *%s*%s\n%s",
			urgencyEmoji, displayID, agentInfo, decision.Question)
	}

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", headerText, false, false),
			nil, nil),
	}

	if len(decision.Options) > 0 {
		blocks = append(blocks, slack.NewDividerBlock())
		for i, opt := range decision.Options {
			label := opt.Label
			if opt.Recommended {
				label = "* " + label
			}
			optText := fmt.Sprintf("*%d. %s*", i+1, label)
			if opt.Description != "" {
				desc := opt.Description
				if len(desc) > 150 {
					desc = desc[:147] + "..."
				}
				optText += fmt.Sprintf("\n%s", desc)
			}

			buttonLabel := "Choose"
			if len(decision.Options) <= 4 {
				buttonLabel = fmt.Sprintf("Choose %d", i+1)
			}

			blocks = append(blocks,
				slack.NewSectionBlock(
					slack.NewTextBlockObject("mrkdwn", optText, false, false),
					nil,
					slack.NewAccessory(
						slack.NewButtonBlockElement(
							fmt.Sprintf("resolve_%s_%d", decision.ID, i+1),
							fmt.Sprintf("%s:%d", decision.ID, i+1),
							slack.NewTextBlockObject("plain_text", buttonLabel, false, false)))))
		}

		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn",
					"*Other*\n_None of the above? Provide your own response._",
					false, false),
				nil,
				slack.NewAccessory(
					slack.NewButtonBlockElement(
						fmt.Sprintf("resolve_other_%s", decision.ID),
						decision.ID,
						slack.NewTextBlockObject("plain_text", "Other...", false, false)))))
	}

	dismissButton := slack.NewButtonBlockElement("dismiss_decision", decision.ID,
		slack.NewTextBlockObject("plain_text", "Dismiss", false, false))
	peekButton := slack.NewButtonBlockElement("peek_"+decision.ID, decision.ID,
		slack.NewTextBlockObject("plain_text", "Peek", false, false))
	dmButton := slack.NewButtonBlockElement("open_preferences", decision.ID,
		slack.NewTextBlockObject("plain_text", "DM Me", false, false))

	if decision.RequestedBy != "" {
		blocks = append(blocks,
			slack.NewActionBlock("", dismissButton, peekButton, dmButton,
				slack.NewButtonBlockElement("unbreak_out", decision.RequestedBy,
					slack.NewTextBlockObject("plain_text", "Unbreak Out", false, false))))
	} else {
		blocks = append(blocks,
			slack.NewActionBlock("", dismissButton, peekButton, dmButton))
	}

	_, ts, err := b.client.PostMessage(channelID, slack.MsgOptionBlocks(blocks...))
	if err == nil && ts != "" {
		b.decisionMessagesMu.Lock()
		b.decisionMessages[decision.ID] = messageInfo{
			channelID: channelID,
			timestamp: ts,
		}
		b.decisionMessagesMu.Unlock()
	}
	return err
}

// ---------- DM ----------

func (b *Bot) sendDMsToOptedInUsers(decision *Decision, blocks []slack.Block) {
	eligibleUsers := b.preferenceManager.ListUsers()
	for _, userID := range eligibleUsers {
		if !b.preferenceManager.IsEligibleForDM(userID) {
			continue
		}

		prefs := b.preferenceManager.GetUserPreferences(userID)
		if prefs.NotificationLevel == "muted" {
			continue
		}
		if prefs.NotificationLevel == "high" && decision.Urgency != "high" {
			continue
		}

		channel, _, _, err := b.client.OpenConversation(&slack.OpenConversationParameters{
			Users: []string{userID},
		})
		if err != nil {
			log.Printf("slackbot: failed to open DM with %s: %v", userID, err)
			continue
		}

		_, _, err = b.client.PostMessage(channel.ID,
			slack.MsgOptionBlocks(blocks...),
			slack.MsgOptionText(fmt.Sprintf("Decision notification: %s", decision.Question), false))
		if err != nil {
			log.Printf("slackbot: failed to DM %s: %v", userID, err)
		}
	}
}

// ---------- Events API ----------

func (b *Bot) handleEventsAPI(event slackevents.EventsAPIEvent) {
	if event.Type != slackevents.CallbackEvent {
		return
	}

	switch ev := event.InnerEvent.Data.(type) {
	case *slackevents.ChannelCreatedEvent:
		b.handleChannelCreated(ev)
	case *slackevents.MessageEvent:
		if ev.SubType != "" {
			return
		}
		if ev.ThreadTimeStamp != "" {
			b.handleThreadReply(ev)
			return
		}
		b.handleAgentChannelMessage(ev)
	}
}

func (b *Bot) handleChannelCreated(event *slackevents.ChannelCreatedEvent) {
	_, _, _, err := b.client.JoinConversation(event.Channel.ID)
	if err != nil {
		log.Printf("slackbot: failed to join new channel %s: %v", event.Channel.Name, err)
	}
}

func (b *Bot) handleThreadReply(ev *slackevents.MessageEvent) {
	if ev.User == b.botUserID || ev.BotID != "" {
		return
	}

	decisionID := b.getDecisionByThread(ev.Channel, ev.ThreadTimeStamp)
	if decisionID == "" {
		return
	}

	ctx := context.Background()
	decision, err := b.decisions.GetDecision(ctx, decisionID)
	if err != nil || decision.RequestedBy == "" || decision.RequestedBy == "unknown" {
		return
	}

	userName := ev.User
	if userInfo, err := b.client.GetUserInfo(ev.User); err == nil {
		if userInfo.RealName != "" {
			userName = userInfo.RealName
		} else if userInfo.Name != "" {
			userName = userInfo.Name
		}
	}

	log.Printf("slackbot: thread reply from %s on decision %s (agent: %s)",
		userName, decisionID, decision.RequestedBy)
}

func (b *Bot) getDecisionByThread(channelID, threadTS string) string {
	b.decisionMessagesMu.RLock()
	defer b.decisionMessagesMu.RUnlock()

	for decisionID, msgInfo := range b.decisionMessages {
		if msgInfo.channelID == channelID && msgInfo.timestamp == threadTS {
			return decisionID
		}
	}
	return ""
}

func (b *Bot) handleAgentChannelMessage(ev *slackevents.MessageEvent) {
	if ev.User == b.botUserID || ev.BotID != "" {
		return
	}

	if b.router == nil {
		return
	}
	agentAddress := b.router.GetAgentByChannel(ev.Channel)
	if agentAddress == "" {
		return
	}

	userName := ev.User
	if userInfo, err := b.client.GetUserInfo(ev.User); err == nil {
		if userInfo.RealName != "" {
			userName = userInfo.RealName
		} else if userInfo.Name != "" {
			userName = userInfo.Name
		}
	}

	log.Printf("slackbot: channel message from %s in agent channel for %s", userName, agentAddress)
}

// ---------- Peek (agent terminal output) ----------

func (b *Bot) handlePeekButton(callback slack.InteractionCallback, decisionID string) {
	b.decisionMessagesMu.RLock()
	msgInfo, ok := b.decisionMessages[decisionID]
	b.decisionMessagesMu.RUnlock()
	if !ok {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			"Could not find message info for this decision.")
		return
	}

	ctx := context.Background()
	decision, err := b.decisions.GetDecision(ctx, decisionID)
	if err != nil || decision == nil {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Could not find decision: %v", err))
		return
	}

	if decision.RequestedBy == "" {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			"No agent associated with this decision.")
		return
	}

	// Show decision details instead of terminal peek (bd doesn't have gt peek)
	blocks := []slack.Block{
		slack.NewHeaderBlock(
			slack.NewTextBlockObject("plain_text",
				fmt.Sprintf("Decision Details: %s", decisionID), false, false)),
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("*Agent:* %s\n*Urgency:* %s\n*Question:* %s",
					decision.RequestedBy, decision.Urgency, decision.Question),
				false, false),
			nil, nil),
	}

	if decision.Context != "" {
		contextDisplay := decision.Context
		var jsonObj interface{}
		if err := json.Unmarshal([]byte(decision.Context), &jsonObj); err == nil {
			if prettyJSON, err := json.MarshalIndent(jsonObj, "", "  "); err == nil {
				contextDisplay = string(prettyJSON)
			}
		}
		contextDisplay = truncateForSlack(contextDisplay, 2900)
		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn",
					fmt.Sprintf("*Context:*\n```%s```", contextDisplay), false, false),
				nil, nil))
	}

	_, _, err = b.client.PostMessage(msgInfo.channelID,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionTS(msgInfo.timestamp))
	if err != nil {
		b.postEphemeral(callback.Channel.ID, callback.User.ID,
			fmt.Sprintf("Error posting peek: %v", err))
	}
}

// ---------- JoinAllChannels ----------

// JoinAllChannels joins all public channels the bot isn't already in.
func (b *Bot) JoinAllChannels() error {
	log.Println("slackbot: auto-joining all public channels...")
	var cursor string
	joinedCount := 0

	for {
		params := &slack.GetConversationsParameters{
			Types:           []string{"public_channel"},
			Limit:           200,
			Cursor:          cursor,
			ExcludeArchived: true,
		}

		channels, nextCursor, err := b.client.GetConversations(params)
		if err != nil {
			return fmt.Errorf("list channels: %w", err)
		}

		for _, ch := range channels {
			if ch.IsMember {
				continue
			}
			_, _, _, err := b.client.JoinConversation(ch.ID)
			if err != nil {
				log.Printf("slackbot: could not join #%s: %v", ch.Name, err)
				continue
			}
			joinedCount++
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	log.Printf("slackbot: auto-join complete: joined %d channels", joinedCount)
	return nil
}

// ---------- Utility functions ----------

func (b *Bot) postEphemeral(channelID, userID, text string) {
	_, err := b.client.PostEphemeral(channelID, userID,
		slack.MsgOptionText(text, false))
	if err != nil {
		log.Printf("slackbot: error posting ephemeral: %v", err)
	}
}

func (b *Bot) postErrorMessage(channelID, userID, decisionID string, err error) {
	errMsg := err.Error()
	hint := ""
	if strings.Contains(errMsg, "not found") {
		hint = "\n\nThis decision may have already been resolved by someone else."
	} else if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "connection") {
		hint = "\n\nThe server may be temporarily unavailable. Please try again."
	}

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("*Failed to resolve decision*\n\n*Decision ID:* `%s`\n*Error:* %s%s",
					decisionID, errMsg, hint),
				false, false),
			nil, nil),
	}

	_, _ = b.client.PostEphemeral(channelID, userID,
		slack.MsgOptionBlocks(blocks...))
}

func (b *Bot) postResolutionConfirmation(channelID, userID, optionLabel, rationale string) {
	displayRationale := rationale
	if len(displayRationale) > 200 {
		displayRationale = displayRationale[:197] + "..."
	}

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("*Decision Resolved*\n\n*Choice:* %s\n*Rationale:* %s",
					optionLabel, displayRationale),
				false, false),
			nil, nil),
	}

	_, _ = b.client.PostEphemeral(channelID, userID,
		slack.MsgOptionBlocks(blocks...))
}

func (b *Bot) postResolutionNotification(optionLabel, userID string) {
	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf(":clipboard: *Decision Resolved*\n\n*Choice:* %s\n*Resolved by:* <@%s>",
					optionLabel, userID),
				false, false),
			nil, nil),
	}

	_, _, _ = b.client.PostMessage(b.channelID, slack.MsgOptionBlocks(blocks...))
}

// ---------- Context formatting ----------

// decisionTypeEmoji maps decision types to display emojis.
var decisionTypeEmoji = map[string]string{
	"tradeoff":       "⚖️",
	"confirmation":   "✅",
	"checkpoint":     "🚧",
	"assessment":     "📊",
	"decomposition":  "🧩",
	"root-cause":     "🔍",
	"scope":          "📐",
	"custom":         "🔧",
	"ambiguity":      "❓",
	"exception":      "⚠️",
	"prioritization": "📋",
	"quality":        "✨",
	"stuck":          "🚨",
}

var decisionTypeLabel = map[string]string{
	"tradeoff":       "Tradeoff Decision",
	"confirmation":   "Confirmation",
	"checkpoint":     "Checkpoint",
	"assessment":     "Assessment",
	"decomposition":  "Decomposition",
	"root-cause":     "Root Cause Analysis",
	"scope":          "Scope Decision",
	"custom":         "Custom Decision",
	"ambiguity":      "Ambiguity Clarification",
	"exception":      "Exception Handling",
	"prioritization": "Prioritization",
	"quality":        "Quality Assessment",
	"stuck":          "Stuck - Need Help",
}

func extractTypeFromContext(ctx string) string {
	if ctx == "" {
		return ""
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(ctx), &obj); err != nil {
		return ""
	}
	if typeVal, ok := obj["_type"].(string); ok {
		return typeVal
	}
	return ""
}

func buildTypeHeader(ctx string) (emoji string, label string) {
	decisionType := extractTypeFromContext(ctx)
	if decisionType == "" {
		return "", ""
	}

	emoji = decisionTypeEmoji[decisionType]
	if emoji == "" {
		emoji = "📋"
	}

	label = decisionTypeLabel[decisionType]
	if label == "" {
		label = strings.ToTitle(decisionType[:1]) + decisionType[1:] + " Decision"
	}

	return emoji, label
}

func formatContextForSlack(ctx string) string {
	if ctx == "" {
		return ""
	}

	const slackBlockLimit = 2900

	var parsed interface{}
	if err := json.Unmarshal([]byte(ctx), &parsed); err != nil {
		return truncateForSlack(ctx, slackBlockLimit)
	}

	if obj, ok := parsed.(map[string]interface{}); ok {
		if valueField, hasValue := obj["_value"]; hasValue {
			if strVal, isStr := valueField.(string); isStr {
				return truncateForSlack(strVal, slackBlockLimit)
			}
			parsed = valueField
		} else {
			delete(obj, "_type")
			delete(obj, "_session_id")
			delete(obj, "session_id")
			delete(obj, "referenced_beads")
			delete(obj, "successor_schemas")

			if len(obj) == 0 {
				return ""
			}
			parsed = obj
		}
	}

	prettyJSON, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return truncateForSlack(ctx, slackBlockLimit)
	}

	formatted := "```\n" + string(prettyJSON) + "\n```"
	if len(formatted) > slackBlockLimit {
		maxContent := slackBlockLimit - 10
		truncated := string(prettyJSON)
		if len(truncated) > maxContent {
			truncated = truncated[:maxContent-3] + "..."
		}
		formatted = "```\n" + truncated + "\n```"
	}

	return formatted
}

// ---------- Helpers ----------

func urgencyToEmoji(urgency string) string {
	switch urgency {
	case "high":
		return ":red_circle:"
	case "medium":
		return ":large_yellow_circle:"
	case "low":
		return ":large_green_circle:"
	default:
		return ":white_circle:"
	}
}

func formatResolver(resolverID string) string {
	if resolverID == "" {
		return "unknown"
	}
	if strings.HasPrefix(resolverID, "slack:") {
		return fmt.Sprintf("<@%s>", strings.TrimPrefix(resolverID, "slack:"))
	}
	// If it looks like a raw Slack user ID
	if !strings.Contains(resolverID, ":") && len(resolverID) > 3 && strings.HasPrefix(resolverID, "U") {
		return fmt.Sprintf("<@%s>", resolverID)
	}
	return resolverID
}

func truncateForSlack(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

// extractAgentShortName extracts the short name from an agent path.
func extractAgentShortName(agent string) string {
	parts := strings.Split(agent, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return agent
}
