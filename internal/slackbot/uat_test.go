package slackbot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slack-go/slack"
)

// ---------- Mock Slack API ----------

// postedMessage captures a PostMessage call for assertion.
type postedMessage struct {
	ChannelID string
	Options   []slack.MsgOption
	Blocks    []slack.Block // populated by applying options to a dummy msg
}

// postedEphemeral captures a PostEphemeral call.
type postedEphemeral struct {
	ChannelID string
	UserID    string
	Options   []slack.MsgOption
}

// updatedMessage captures an UpdateMessage call.
type updatedMessage struct {
	ChannelID string
	Timestamp string
	Options   []slack.MsgOption
}

// deletedMessage captures a DeleteMessage call.
type deletedMessage struct {
	ChannelID string
	Timestamp string
}

// openedView captures an OpenView call.
type openedView struct {
	TriggerID string
	View      slack.ModalViewRequest
}

type mockSlackAPI struct {
	mu sync.Mutex

	// Captured calls
	PostedMessages  []postedMessage
	Ephemerals      []postedEphemeral
	UpdatedMessages []updatedMessage
	DeletedMessages []deletedMessage
	OpenedViews     []openedView

	// Auto-increment message timestamps
	nextTS int

	// Configurable errors
	postMessageErr error
	openViewErr    error
	deleteErr      error
	updateErr      error

	// Conversation management
	createdChannels []slack.CreateConversationParams
	channelsByName  map[string]*slack.Channel
}

func newMockSlackAPI() *mockSlackAPI {
	return &mockSlackAPI{
		channelsByName: make(map[string]*slack.Channel),
	}
}

func (m *mockSlackAPI) AuthTest() (*slack.AuthTestResponse, error) {
	return &slack.AuthTestResponse{UserID: "UBOTTEST"}, nil
}

func (m *mockSlackAPI) PostMessage(channelID string, options ...slack.MsgOption) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.postMessageErr != nil {
		return "", "", m.postMessageErr
	}
	m.nextTS++
	ts := fmt.Sprintf("1234567890.%06d", m.nextTS)
	m.PostedMessages = append(m.PostedMessages, postedMessage{
		ChannelID: channelID,
		Options:   options,
	})
	return channelID, ts, nil
}

func (m *mockSlackAPI) PostEphemeral(channelID, userID string, options ...slack.MsgOption) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Ephemerals = append(m.Ephemerals, postedEphemeral{
		ChannelID: channelID,
		UserID:    userID,
		Options:   options,
	})
	return "1234567890.000001", nil
}

func (m *mockSlackAPI) UpdateMessage(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return "", "", "", m.updateErr
	}
	m.UpdatedMessages = append(m.UpdatedMessages, updatedMessage{
		ChannelID: channelID,
		Timestamp: timestamp,
		Options:   options,
	})
	return channelID, timestamp, "", nil
}

func (m *mockSlackAPI) DeleteMessage(channelID, timestamp string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteErr != nil {
		return "", "", m.deleteErr
	}
	m.DeletedMessages = append(m.DeletedMessages, deletedMessage{
		ChannelID: channelID,
		Timestamp: timestamp,
	})
	return channelID, timestamp, nil
}

func (m *mockSlackAPI) OpenView(triggerID string, view slack.ModalViewRequest) (*slack.ViewResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.openViewErr != nil {
		return nil, m.openViewErr
	}
	m.OpenedViews = append(m.OpenedViews, openedView{
		TriggerID: triggerID,
		View:      view,
	})
	return &slack.ViewResponse{}, nil
}

func (m *mockSlackAPI) CreateConversation(params slack.CreateConversationParams) (*slack.Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createdChannels = append(m.createdChannels, params)
	ch := &slack.Channel{}
	ch.ID = "C" + strings.ToUpper(params.ChannelName)[:8]
	ch.Name = params.ChannelName
	m.channelsByName[params.ChannelName] = ch
	return ch, nil
}

func (m *mockSlackAPI) GetConversations(params *slack.GetConversationsParameters) ([]slack.Channel, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var channels []slack.Channel
	for _, ch := range m.channelsByName {
		channels = append(channels, *ch)
	}
	return channels, "", nil
}

func (m *mockSlackAPI) InviteUsersToConversation(channelID string, users ...string) (*slack.Channel, error) {
	return &slack.Channel{}, nil
}

func (m *mockSlackAPI) JoinConversation(channelID string) (*slack.Channel, string, []string, error) {
	return &slack.Channel{}, "", nil, nil
}

func (m *mockSlackAPI) OpenConversation(params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
	ch := &slack.Channel{}
	ch.ID = "D" + params.Users[0]
	return ch, false, false, nil
}

func (m *mockSlackAPI) GetUserInfo(userID string) (*slack.User, error) {
	return &slack.User{
		ID:       userID,
		RealName: "Test User " + userID,
	}, nil
}

// ---------- Mock Decision Provider ----------

type mockDecisionProvider struct {
	mu        sync.Mutex
	decisions map[string]*Decision
	resolved  map[string]resolveCall
}

type resolveCall struct {
	ChosenIndex int
	Rationale   string
	ResolvedBy  string
	Text        string // for ResolveWithText
}

func newMockDecisionProvider() *mockDecisionProvider {
	return &mockDecisionProvider{
		decisions: make(map[string]*Decision),
		resolved:  make(map[string]resolveCall),
	}
}

func (m *mockDecisionProvider) AddDecision(d Decision) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decisions[d.ID] = &d
}

func (m *mockDecisionProvider) ListPending(_ context.Context) ([]Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Decision
	for _, d := range m.decisions {
		if !d.Resolved {
			out = append(out, *d)
		}
	}
	return out, nil
}

func (m *mockDecisionProvider) GetDecision(_ context.Context, issueID string) (*Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.decisions[issueID]
	if !ok {
		return nil, fmt.Errorf("decision %s not found", issueID)
	}
	cp := *d
	return &cp, nil
}

func (m *mockDecisionProvider) Resolve(_ context.Context, issueID string, chosenIndex int, rationale, resolvedBy string) (*Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.decisions[issueID]
	if !ok {
		return nil, fmt.Errorf("decision %s not found", issueID)
	}
	if d.Resolved {
		return nil, fmt.Errorf("decision %s already resolved", issueID)
	}
	d.Resolved = true
	d.ChosenIndex = chosenIndex
	d.Rationale = rationale
	d.ResolvedBy = resolvedBy
	m.resolved[issueID] = resolveCall{
		ChosenIndex: chosenIndex,
		Rationale:   rationale,
		ResolvedBy:  resolvedBy,
	}
	cp := *d
	return &cp, nil
}

func (m *mockDecisionProvider) ResolveWithText(_ context.Context, issueID, text, resolvedBy string) (*Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.decisions[issueID]
	if !ok {
		return nil, fmt.Errorf("decision %s not found", issueID)
	}
	if d.Resolved {
		return nil, fmt.Errorf("decision %s already resolved", issueID)
	}
	d.Resolved = true
	d.Rationale = text
	d.ResolvedBy = resolvedBy
	m.resolved[issueID] = resolveCall{
		Text:       text,
		ResolvedBy: resolvedBy,
	}
	cp := *d
	return &cp, nil
}

func (m *mockDecisionProvider) Cancel(_ context.Context, issueID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.decisions[issueID]
	if !ok {
		return fmt.Errorf("decision %s not found", issueID)
	}
	d.Resolved = true
	return nil
}

func (m *mockDecisionProvider) getResolveCall(issueID string) (resolveCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rc, ok := m.resolved[issueID]
	return rc, ok
}

// ---------- Test helpers ----------

func newTestBot(t *testing.T) (*Bot, *mockSlackAPI, *mockDecisionProvider) {
	t.Helper()
	mockAPI := newMockSlackAPI()
	mockDecisions := newMockDecisionProvider()
	bot := newBotForTest(mockAPI, mockDecisions, "C_DEFAULT")
	return bot, mockAPI, mockDecisions
}

func sampleDecision(id string) Decision {
	return Decision{
		ID:           id,
		Question:     "Should we deploy to production?",
		Context:      `{"_type":"tradeoff","detail":"risk assessment needed"}`,
		RequestedBy:  "gastown/polecats/furiosa",
		Urgency:      "high",
		SemanticSlug: "deploy-prod",
		Options: []DecisionOption{
			{ID: "yes", Label: "Yes, deploy now", Description: "Ship it"},
			{ID: "no", Label: "No, wait", Description: "More testing needed"},
			{ID: "partial", Label: "Partial deploy", Description: "Deploy to staging first"},
		},
	}
}

// ---------- UAT Tests ----------

func TestUAT_NotifyNewDecision_PostsMessage(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	d := sampleDecision("bd-abc123")
	err := bot.NotifyNewDecision(&d)
	if err != nil {
		t.Fatalf("NotifyNewDecision: %v", err)
	}

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.PostedMessages) == 0 {
		t.Fatal("expected at least one posted message")
	}

	msg := mockAPI.PostedMessages[0]
	if msg.ChannelID != "C_DEFAULT" {
		t.Errorf("posted to %q, want C_DEFAULT", msg.ChannelID)
	}
}

func TestUAT_NotifyNewDecision_TracksMessageForUpdate(t *testing.T) {
	bot, _, _ := newTestBot(t)

	d := sampleDecision("bd-track1")
	err := bot.NotifyNewDecision(&d)
	if err != nil {
		t.Fatalf("NotifyNewDecision: %v", err)
	}

	bot.decisionMessagesMu.RLock()
	info, ok := bot.decisionMessages["bd-track1"]
	bot.decisionMessagesMu.RUnlock()

	if !ok {
		t.Fatal("decision message not tracked")
	}
	if info.channelID != "C_DEFAULT" {
		t.Errorf("tracked channel = %q, want C_DEFAULT", info.channelID)
	}
	if info.timestamp == "" {
		t.Error("tracked timestamp is empty")
	}
}

func TestUAT_NotifyNewDecision_PredecessorThreading(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	// Post first decision (predecessor)
	pred := sampleDecision("bd-pred1")
	if err := bot.NotifyNewDecision(&pred); err != nil {
		t.Fatalf("predecessor: %v", err)
	}

	// Post chained decision
	chained := sampleDecision("bd-chain1")
	chained.PredecessorID = "bd-pred1"
	if err := bot.NotifyNewDecision(&chained); err != nil {
		t.Fatalf("chained: %v", err)
	}

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	// Should have 3 messages: pred, chained, and supersede update on pred
	// Actually: pred post, chained post (threaded), and updateMessage for predecessor
	if len(mockAPI.PostedMessages) < 2 {
		t.Fatalf("expected at least 2 posted messages, got %d", len(mockAPI.PostedMessages))
	}

	// Predecessor should be marked as superseded
	if len(mockAPI.UpdatedMessages) == 0 {
		t.Error("expected predecessor to be updated as superseded")
	}
}

func TestUAT_NotifyResolution_SkipsSlackResolvedBy(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	d := sampleDecision("bd-skip1")
	d.Resolved = true
	d.ResolvedBy = "slack:U123"
	d.ChosenIndex = 1

	err := bot.NotifyResolution(&d)
	if err != nil {
		t.Fatalf("NotifyResolution: %v", err)
	}

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	// Should skip posting because the resolver is from Slack (would create echo)
	if len(mockAPI.PostedMessages) > 0 || len(mockAPI.UpdatedMessages) > 0 {
		t.Error("expected no message for Slack-resolved decision")
	}
}

func TestUAT_NotifyResolution_UpdatesTrackedMessage(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	// First post the decision to get it tracked
	d := sampleDecision("bd-upd1")
	if err := bot.NotifyNewDecision(&d); err != nil {
		t.Fatalf("NotifyNewDecision: %v", err)
	}

	// Now resolve it (non-Slack resolver)
	d.Resolved = true
	d.ChosenIndex = 2
	d.Rationale = "Need more testing"
	d.ResolvedBy = "agent-cli"

	if err := bot.NotifyResolution(&d); err != nil {
		t.Fatalf("NotifyResolution: %v", err)
	}

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.UpdatedMessages) == 0 {
		t.Fatal("expected message to be updated for resolution")
	}

	upd := mockAPI.UpdatedMessages[len(mockAPI.UpdatedMessages)-1]
	if upd.ChannelID != "C_DEFAULT" {
		t.Errorf("updated channel = %q, want C_DEFAULT", upd.ChannelID)
	}
}

func TestUAT_NotifyResolution_PostsFallbackIfUntracked(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	// Resolution without a prior tracked message
	d := sampleDecision("bd-untrk")
	d.Resolved = true
	d.ChosenIndex = 1
	d.ResolvedBy = "agent-x"

	if err := bot.NotifyResolution(&d); err != nil {
		t.Fatalf("NotifyResolution: %v", err)
	}

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.PostedMessages) == 0 {
		t.Fatal("expected fallback post message for untracked resolution")
	}
}

func TestUAT_DismissDecision_DeletesMessage(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	// Post a decision first
	d := sampleDecision("bd-dismiss1")
	if err := bot.NotifyNewDecision(&d); err != nil {
		t.Fatalf("NotifyNewDecision: %v", err)
	}

	// Dismiss it
	bot.DismissDecisionByID("bd-dismiss1")

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.DeletedMessages) == 0 {
		t.Fatal("expected message to be deleted")
	}
}

func TestUAT_HandleSlashCommand_ListPending(t *testing.T) {
	bot, mockAPI, mockDecisions := newTestBot(t)

	// Add some pending decisions
	mockDecisions.AddDecision(sampleDecision("bd-list1"))
	d2 := sampleDecision("bd-list2")
	d2.Urgency = "medium"
	d2.Question = "Review the API design?"
	mockDecisions.AddDecision(d2)

	cmd := slack.SlashCommand{
		Command:     "/decisions",
		ChannelID:   "C_TEST",
		UserID:      "U_USER1",
		ResponseURL: "https://hooks.slack.com/response/test",
	}
	bot.handleSlashCommand(cmd)

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.PostedMessages) == 0 {
		t.Fatal("expected message for /decisions command")
	}
	msg := mockAPI.PostedMessages[0]
	if msg.ChannelID != "C_TEST" {
		t.Errorf("posted to %q, want C_TEST", msg.ChannelID)
	}
}

func TestUAT_HandleSlashCommand_EmptyList(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	cmd := slack.SlashCommand{
		Command:     "/decisions",
		ChannelID:   "C_EMPTY",
		UserID:      "U_USER1",
		ResponseURL: "https://hooks.slack.com/response/test",
	}
	bot.handleSlashCommand(cmd)

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.PostedMessages) == 0 {
		t.Fatal("expected message for empty /decisions")
	}
}

func TestUAT_HandleViewDecision_OpensDetailView(t *testing.T) {
	bot, mockAPI, mockDecisions := newTestBot(t)

	mockDecisions.AddDecision(sampleDecision("bd-view1"))

	callback := slack.InteractionCallback{}
	callback.Channel.ID = "C_TEST"
	callback.User.ID = "U_VIEWER"
	bot.handleViewDecision(callback, "bd-view1")

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.PostedMessages) == 0 {
		t.Fatal("expected detail view posted")
	}
}

func TestUAT_HandleViewDecision_NotFound(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	callback := slack.InteractionCallback{}
	callback.Channel.ID = "C_TEST"
	callback.User.ID = "U_VIEWER"
	bot.handleViewDecision(callback, "bd-nonexistent")

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.Ephemerals) == 0 {
		t.Fatal("expected ephemeral error message for missing decision")
	}
}

func TestUAT_HandleResolveDecision_OpensModal(t *testing.T) {
	bot, mockAPI, mockDecisions := newTestBot(t)

	mockDecisions.AddDecision(sampleDecision("bd-res1"))

	callback := slack.InteractionCallback{
		TriggerID: "trigger123",
	}
	callback.Channel.ID = "C_TEST"
	callback.User.ID = "U_RESOLVER"

	action := &slack.BlockAction{
		ActionID: "resolve_bd-res1_1",
		Value:    "bd-res1:1",
	}
	bot.handleResolveDecision(callback, action)

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.OpenedViews) == 0 {
		t.Fatal("expected resolve modal to be opened")
	}

	view := mockAPI.OpenedViews[0]
	if view.View.CallbackID != "resolve_decision_modal" {
		t.Errorf("callback ID = %q, want resolve_decision_modal", view.View.CallbackID)
	}
	if !strings.Contains(view.View.PrivateMetadata, "bd-res1") {
		t.Errorf("metadata %q should contain decision ID", view.View.PrivateMetadata)
	}
}

func TestUAT_HandleResolveOther_OpensOtherModal(t *testing.T) {
	bot, mockAPI, mockDecisions := newTestBot(t)

	mockDecisions.AddDecision(sampleDecision("bd-other1"))

	callback := slack.InteractionCallback{
		TriggerID: "trigger456",
	}
	callback.Channel.ID = "C_TEST"
	callback.User.ID = "U_RESOLVER"

	bot.handleResolveOther(callback, "bd-other1")

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.OpenedViews) == 0 {
		t.Fatal("expected Other modal to be opened")
	}

	view := mockAPI.OpenedViews[0]
	if view.View.CallbackID != "resolve_other_modal" {
		t.Errorf("callback ID = %q, want resolve_other_modal", view.View.CallbackID)
	}
	if !strings.HasPrefix(view.View.PrivateMetadata, "other:bd-other1:") {
		t.Errorf("metadata %q should start with other:bd-other1:", view.View.PrivateMetadata)
	}
}

func TestUAT_ResolveModalSubmission_ResolvesDecision(t *testing.T) {
	bot, _, mockDecisions := newTestBot(t)

	mockDecisions.AddDecision(sampleDecision("bd-sub1"))

	// Post the decision first to track its message
	d := sampleDecision("bd-sub1")
	_ = bot.NotifyNewDecision(&d)

	// Get the tracked message timestamp
	bot.decisionMessagesMu.RLock()
	msgInfo := bot.decisionMessages["bd-sub1"]
	bot.decisionMessagesMu.RUnlock()

	callback := slack.InteractionCallback{}
	callback.User.ID = "U_SUBMIT"
	callback.View.CallbackID = "resolve_decision_modal"
	callback.View.PrivateMetadata = fmt.Sprintf("bd-sub1:1:%s:%s|Yes, deploy now", msgInfo.channelID, msgInfo.timestamp)
	callback.View.State = &slack.ViewState{
		Values: map[string]map[string]slack.BlockAction{
			"rationale_block": {
				"rationale_input": {Value: "LGTM, tests pass"},
			},
		},
	}

	bot.handleResolveModalSubmission(callback)

	rc, ok := mockDecisions.getResolveCall("bd-sub1")
	if !ok {
		t.Fatal("expected decision to be resolved")
	}
	if rc.ChosenIndex != 1 {
		t.Errorf("chosen index = %d, want 1", rc.ChosenIndex)
	}
	if !strings.Contains(rc.Rationale, "LGTM, tests pass") {
		t.Errorf("rationale %q should contain user input", rc.Rationale)
	}
	if rc.ResolvedBy != "slack:U_SUBMIT" {
		t.Errorf("resolved by = %q, want slack:U_SUBMIT", rc.ResolvedBy)
	}
}

func TestUAT_ResolveModalSubmission_NoRationale(t *testing.T) {
	bot, _, mockDecisions := newTestBot(t)

	mockDecisions.AddDecision(sampleDecision("bd-norat"))

	callback := slack.InteractionCallback{}
	callback.User.ID = "U_QUICK"
	callback.View.CallbackID = "resolve_decision_modal"
	callback.View.PrivateMetadata = "bd-norat:2:C_DEFAULT:|No, wait"
	callback.View.State = &slack.ViewState{
		Values: map[string]map[string]slack.BlockAction{
			"rationale_block": {
				"rationale_input": {Value: ""},
			},
		},
	}

	bot.handleResolveModalSubmission(callback)

	rc, ok := mockDecisions.getResolveCall("bd-norat")
	if !ok {
		t.Fatal("expected decision to be resolved")
	}
	if rc.ChosenIndex != 2 {
		t.Errorf("chosen index = %d, want 2", rc.ChosenIndex)
	}
	// Without rationale, it should default to the attribution text
	if !strings.Contains(rc.Rationale, "Resolved via Slack by") {
		t.Errorf("rationale %q should contain attribution", rc.Rationale)
	}
}

func TestUAT_OtherModalSubmission_ResolvesWithText(t *testing.T) {
	bot, _, mockDecisions := newTestBot(t)

	mockDecisions.AddDecision(sampleDecision("bd-cust1"))

	callback := slack.InteractionCallback{}
	callback.User.ID = "U_CUSTOM"
	callback.View.CallbackID = "resolve_other_modal"
	callback.View.PrivateMetadata = "other:bd-cust1:C_DEFAULT:"
	callback.View.State = &slack.ViewState{
		Values: map[string]map[string]slack.BlockAction{
			"custom_text_block": {
				"custom_text_input": {Value: "Let's do option C instead"},
			},
		},
	}

	bot.handleOtherModalSubmission(callback)

	rc, ok := mockDecisions.getResolveCall("bd-cust1")
	if !ok {
		t.Fatal("expected decision to be resolved with text")
	}
	if !strings.Contains(rc.Text, "Let's do option C instead") {
		t.Errorf("resolve text %q should contain custom text", rc.Text)
	}
}

func TestUAT_OtherModalSubmission_EmptyTextRejected(t *testing.T) {
	bot, mockAPI, mockDecisions := newTestBot(t)

	mockDecisions.AddDecision(sampleDecision("bd-empty"))

	callback := slack.InteractionCallback{}
	callback.User.ID = "U_EMPTY"
	callback.View.CallbackID = "resolve_other_modal"
	callback.View.PrivateMetadata = "other:bd-empty:C_TEST:"
	callback.View.State = &slack.ViewState{
		Values: map[string]map[string]slack.BlockAction{
			"custom_text_block": {
				"custom_text_input": {Value: ""},
			},
		},
	}

	bot.handleOtherModalSubmission(callback)

	// Should NOT resolve
	_, ok := mockDecisions.getResolveCall("bd-empty")
	if ok {
		t.Error("decision should not be resolved with empty text")
	}

	// Should post ephemeral error
	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()
	if len(mockAPI.Ephemerals) == 0 {
		t.Error("expected ephemeral error for empty custom text")
	}
}

func TestUAT_HandleInteraction_ActionRouting(t *testing.T) {
	bot, mockAPI, mockDecisions := newTestBot(t)

	mockDecisions.AddDecision(sampleDecision("bd-route1"))

	tests := []struct {
		name     string
		actionID string
		value    string
		wantView bool
		wantMsg  bool
	}{
		{
			name:     "view_decision opens detail",
			actionID: "view_decision",
			value:    "bd-route1",
			wantMsg:  true,
		},
		{
			name:     "resolve opens modal",
			actionID: "resolve_bd-route1_1",
			value:    "bd-route1:1",
			wantView: true,
		},
		{
			name:     "resolve_other opens other modal",
			actionID: "resolve_other_bd-route1",
			value:    "bd-route1",
			wantView: true,
		},
		{
			name:     "peek posts thread reply",
			actionID: "peek_bd-route1",
			value:    "bd-route1",
			wantMsg:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock state
			mockAPI.mu.Lock()
			mockAPI.PostedMessages = nil
			mockAPI.OpenedViews = nil
			mockAPI.Ephemerals = nil
			mockAPI.mu.Unlock()

			callback := slack.InteractionCallback{
				TriggerID: "trigger_test",
			}
			callback.Channel.ID = "C_TEST"
			callback.User.ID = "U_ACT"
			callback.ActionCallback.BlockActions = []*slack.BlockAction{
				{ActionID: tt.actionID, Value: tt.value},
			}

			bot.handleInteraction(callback)

			mockAPI.mu.Lock()
			defer mockAPI.mu.Unlock()

			if tt.wantView && len(mockAPI.OpenedViews) == 0 {
				t.Error("expected modal to be opened")
			}
			if tt.wantMsg && len(mockAPI.PostedMessages) == 0 && len(mockAPI.Ephemerals) == 0 {
				t.Error("expected message or ephemeral to be posted")
			}
		})
	}
}

func TestUAT_FullLifecycle_CreateResolveUpdate(t *testing.T) {
	bot, mockAPI, mockDecisions := newTestBot(t)

	// Step 1: Decision is created
	d := sampleDecision("bd-life1")
	mockDecisions.AddDecision(d)

	// Step 2: Bot notifies about new decision
	if err := bot.NotifyNewDecision(&d); err != nil {
		t.Fatalf("NotifyNewDecision: %v", err)
	}

	mockAPI.mu.Lock()
	step2Count := len(mockAPI.PostedMessages)
	mockAPI.mu.Unlock()
	if step2Count == 0 {
		t.Fatal("step 2: expected at least 1 posted message")
	}

	// Step 3: User clicks "Choose 1" button, triggering resolve modal
	bot.decisionMessagesMu.RLock()
	msgInfo := bot.decisionMessages["bd-life1"]
	bot.decisionMessagesMu.RUnlock()

	resolveCallback := slack.InteractionCallback{TriggerID: "trig_resolve"}
	resolveCallback.Channel.ID = msgInfo.channelID
	resolveCallback.User.ID = "U_RESOLVER"
	resolveCallback.Message.Timestamp = msgInfo.timestamp // Slack includes the original message
	resolveCallback.ActionCallback.BlockActions = []*slack.BlockAction{
		{ActionID: "resolve_bd-life1_1", Value: "bd-life1:1"},
	}
	bot.handleInteraction(resolveCallback)

	mockAPI.mu.Lock()
	if len(mockAPI.OpenedViews) != 1 {
		t.Fatalf("step 3: expected modal opened, got %d views", len(mockAPI.OpenedViews))
	}
	modalMeta := mockAPI.OpenedViews[0].View.PrivateMetadata
	mockAPI.mu.Unlock()

	// Step 4: User submits resolve modal with rationale
	submitCallback := slack.InteractionCallback{}
	submitCallback.User.ID = "U_RESOLVER"
	submitCallback.View.CallbackID = "resolve_decision_modal"
	submitCallback.View.PrivateMetadata = modalMeta
	submitCallback.View.State = &slack.ViewState{
		Values: map[string]map[string]slack.BlockAction{
			"rationale_block": {
				"rationale_input": {Value: "All tests pass, deploy approved"},
			},
		},
	}
	bot.handleResolveModalSubmission(submitCallback)

	// Step 5: Verify decision was resolved
	rc, ok := mockDecisions.getResolveCall("bd-life1")
	if !ok {
		t.Fatal("step 5: decision not resolved")
	}
	if rc.ChosenIndex != 1 {
		t.Errorf("step 5: chosen = %d, want 1", rc.ChosenIndex)
	}
	if !strings.Contains(rc.Rationale, "All tests pass") {
		t.Errorf("step 5: rationale = %q, missing user input", rc.Rationale)
	}

	// Step 6: Original message should be updated to show resolution
	mockAPI.mu.Lock()
	if len(mockAPI.UpdatedMessages) == 0 {
		t.Error("step 6: expected original message to be updated")
	}
	mockAPI.mu.Unlock()
}

func TestUAT_FullLifecycle_CustomTextResolve(t *testing.T) {
	bot, mockAPI, mockDecisions := newTestBot(t)

	d := sampleDecision("bd-custom-life")
	mockDecisions.AddDecision(d)

	// Post decision
	if err := bot.NotifyNewDecision(&d); err != nil {
		t.Fatalf("NotifyNewDecision: %v", err)
	}

	bot.decisionMessagesMu.RLock()
	msgInfo := bot.decisionMessages["bd-custom-life"]
	bot.decisionMessagesMu.RUnlock()

	// User clicks "Other" button
	otherCallback := slack.InteractionCallback{TriggerID: "trig_other"}
	otherCallback.Channel.ID = msgInfo.channelID
	otherCallback.User.ID = "U_ALT"
	otherCallback.ActionCallback.BlockActions = []*slack.BlockAction{
		{ActionID: "resolve_other_bd-custom-life", Value: "bd-custom-life"},
	}
	bot.handleInteraction(otherCallback)

	mockAPI.mu.Lock()
	if len(mockAPI.OpenedViews) == 0 {
		t.Fatal("expected Other modal opened")
	}
	otherMeta := mockAPI.OpenedViews[len(mockAPI.OpenedViews)-1].View.PrivateMetadata
	mockAPI.mu.Unlock()

	// Submit custom text
	submitCallback := slack.InteractionCallback{}
	submitCallback.User.ID = "U_ALT"
	submitCallback.View.CallbackID = "resolve_other_modal"
	submitCallback.View.PrivateMetadata = otherMeta
	submitCallback.View.State = &slack.ViewState{
		Values: map[string]map[string]slack.BlockAction{
			"custom_text_block": {
				"custom_text_input": {Value: "Actually, let's try blue-green deployment"},
			},
		},
	}
	bot.handleOtherModalSubmission(submitCallback)

	rc, ok := mockDecisions.getResolveCall("bd-custom-life")
	if !ok {
		t.Fatal("decision not resolved")
	}
	if !strings.Contains(rc.Text, "blue-green deployment") {
		t.Errorf("resolve text = %q, want to contain 'blue-green deployment'", rc.Text)
	}
}

func TestUAT_DoubleResolve_Rejected(t *testing.T) {
	bot, mockAPI, mockDecisions := newTestBot(t)

	d := sampleDecision("bd-dbl1")
	mockDecisions.AddDecision(d)

	// Resolve it once
	_, _ = mockDecisions.Resolve(context.Background(), "bd-dbl1", 1, "first", "slack:U1")

	// Try to resolve again via modal
	callback := slack.InteractionCallback{}
	callback.User.ID = "U_DOUBLE"
	callback.View.CallbackID = "resolve_decision_modal"
	callback.View.PrivateMetadata = "bd-dbl1:2:C_DEFAULT:|No, wait"
	callback.View.State = &slack.ViewState{
		Values: map[string]map[string]slack.BlockAction{
			"rationale_block": {
				"rationale_input": {Value: "second attempt"},
			},
		},
	}
	bot.handleResolveModalSubmission(callback)

	// Should post error (decision already resolved)
	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()
	hasError := false
	for _, e := range mockAPI.Ephemerals {
		_ = e
		hasError = true
	}
	for _, m := range mockAPI.PostedMessages {
		_ = m
		hasError = true
	}
	if !hasError {
		t.Error("expected error message for double resolve")
	}
}

func TestUAT_NotifyEscalation_PostsAlert(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	d := sampleDecision("bd-esc1")
	d.Urgency = "high"

	if err := bot.NotifyEscalation(&d); err != nil {
		t.Fatalf("NotifyEscalation: %v", err)
	}

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.PostedMessages) == 0 {
		t.Fatal("expected escalation alert posted")
	}
}

func TestUAT_Concurrent_MultipleDecisions(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	// Post multiple decisions concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			d := sampleDecision(fmt.Sprintf("bd-conc-%d", idx))
			_ = bot.NotifyNewDecision(&d)
		}(i)
	}
	wg.Wait()

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.PostedMessages) < 10 {
		t.Errorf("posted %d messages, want at least 10", len(mockAPI.PostedMessages))
	}

	// All decisions should be tracked (each decision gets at least one tracked message)
	bot.decisionMessagesMu.RLock()
	defer bot.decisionMessagesMu.RUnlock()
	if len(bot.decisionMessages) != 10 {
		t.Errorf("tracked %d decisions, want 10", len(bot.decisionMessages))
	}
}

func TestUAT_HandlePreferencesSubmission(t *testing.T) {
	bot, _, _ := newTestBot(t)

	callback := slack.InteractionCallback{}
	callback.User.ID = "U_PREFS"
	callback.View.CallbackID = "preferences_modal"
	callback.View.PrivateMetadata = "U_PREFS"
	callback.View.State = &slack.ViewState{
		Values: map[string]map[string]slack.BlockAction{
			"dm_opt_in_block": {
				"dm_opt_in": {SelectedOptions: []slack.OptionBlockObject{{Value: "opt_in"}}},
			},
			"notification_level_block": {
				"notification_level": {SelectedOption: slack.OptionBlockObject{Value: "high"}},
			},
			"thread_notify_block": {
				"thread_notify": {SelectedOptions: []slack.OptionBlockObject{{Value: "thread"}}},
			},
		},
	}

	bot.handlePreferencesModalSubmission(callback)

	prefs := bot.preferenceManager.GetUserPreferences("U_PREFS")
	if !prefs.DMOptIn {
		t.Error("DM opt-in should be true")
	}
	if prefs.NotificationLevel != "high" {
		t.Errorf("notification level = %q, want high", prefs.NotificationLevel)
	}
	if !prefs.ThreadNotifications {
		t.Error("thread notifications should be true")
	}
}

func TestUAT_DismissDecision_UntrackedNoop(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	// Dismiss a decision that was never posted (should be a noop)
	bot.DismissDecisionByID("bd-never-posted")

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.DeletedMessages) > 0 {
		t.Error("should not delete anything for untracked decision")
	}
}

func TestUAT_PostMessageError_Handled(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	mockAPI.postMessageErr = fmt.Errorf("slack API rate limited")

	d := sampleDecision("bd-err1")
	err := bot.NotifyNewDecision(&d)
	if err == nil {
		t.Error("expected error to propagate")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %v, want rate limited", err)
	}
}

func TestUAT_NotifyNewDecision_UrgencyLevels(t *testing.T) {
	tests := []struct {
		urgency string
	}{
		{"high"},
		{"medium"},
		{"low"},
		{""},
	}

	for _, tt := range tests {
		t.Run(tt.urgency, func(t *testing.T) {
			bot, mockAPI, _ := newTestBot(t)

			d := sampleDecision("bd-urg-" + tt.urgency)
			d.Urgency = tt.urgency

			if err := bot.NotifyNewDecision(&d); err != nil {
				t.Fatalf("NotifyNewDecision(%s): %v", tt.urgency, err)
			}

			mockAPI.mu.Lock()
			defer mockAPI.mu.Unlock()

			if len(mockAPI.PostedMessages) == 0 {
				t.Fatal("expected message posted")
			}
		})
	}
}

func TestUAT_NotifyNewDecision_NoOptions(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	d := Decision{
		ID:       "bd-noopt",
		Question: "What should we do?",
	}

	if err := bot.NotifyNewDecision(&d); err != nil {
		t.Fatalf("NotifyNewDecision: %v", err)
	}

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.PostedMessages) == 0 {
		t.Fatal("expected message even with no options")
	}
}

func TestUAT_Timing_NotificationWithinThreshold(t *testing.T) {
	bot, _, _ := newTestBot(t)

	d := sampleDecision("bd-timing")

	start := time.Now()
	err := bot.NotifyNewDecision(&d)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("NotifyNewDecision: %v", err)
	}

	// Mock should complete near-instantly (< 100ms)
	if elapsed > 100*time.Millisecond {
		t.Errorf("notification took %v, want < 100ms", elapsed)
	}
}

// extractBlocksJSON extracts the raw blocks JSON from captured MsgOptions using
// the slack library's UnsafeApplyMsgOptions utility.
func extractBlocksJSON(t *testing.T, options []slack.MsgOption) string {
	t.Helper()
	_, vals, err := slack.UnsafeApplyMsgOptions("", "C_TEST", "", options...)
	if err != nil {
		t.Fatalf("UnsafeApplyMsgOptions: %v", err)
	}
	return vals.Get("blocks")
}

func TestUAT_CreateDecision_BlockKitStructure(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	d := sampleDecision("bd-bk1")
	if err := bot.NotifyNewDecision(&d); err != nil {
		t.Fatalf("NotifyNewDecision: %v", err)
	}

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.PostedMessages) == 0 {
		t.Fatal("expected posted message")
	}

	blocksJSON := extractBlocksJSON(t, mockAPI.PostedMessages[0].Options)
	if blocksJSON == "" {
		t.Fatal("no blocks JSON in message options")
	}

	var blocks []json.RawMessage
	if err := json.Unmarshal([]byte(blocksJSON), &blocks); err != nil {
		t.Fatalf("failed to parse blocks JSON: %v", err)
	}

	// Expected structure for a 3-option decision with agent info:
	// [0] section (header with urgency emoji + question)
	// [1] section (context text)
	// [2] divider
	// [3] section (option 1 with "Choose 1" button)
	// [4] section (option 2 with "Choose 2" button)
	// [5] section (option 3 with "Choose 3" button)
	// [6] section (Other with "Other..." button)
	// [7] actions (Dismiss, Peek, DM Me, Break Out)

	if len(blocks) < 8 {
		t.Fatalf("expected at least 8 blocks, got %d", len(blocks))
	}

	// Verify header block contains urgency emoji and question
	var header struct {
		Type string `json:"type"`
		Text struct {
			Text string `json:"text"`
		} `json:"text"`
	}
	if err := json.Unmarshal(blocks[0], &header); err != nil {
		t.Fatalf("parse header block: %v", err)
	}
	if header.Type != "section" {
		t.Errorf("block[0] type = %q, want section", header.Type)
	}
	if !strings.Contains(header.Text.Text, ":red_circle:") {
		t.Errorf("header missing urgency emoji :red_circle:, got %q", header.Text.Text)
	}
	if !strings.Contains(header.Text.Text, "deploy-prod") {
		t.Errorf("header missing semantic slug, got %q", header.Text.Text)
	}
	if !strings.Contains(header.Text.Text, d.Question) {
		t.Errorf("header missing question, got %q", header.Text.Text)
	}
	if !strings.Contains(header.Text.Text, d.RequestedBy) {
		t.Errorf("header missing agent info, got %q", header.Text.Text)
	}

	// Verify divider exists
	var divider struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(blocks[2], &divider); err != nil {
		t.Fatalf("parse divider block: %v", err)
	}
	if divider.Type != "divider" {
		t.Errorf("block[2] type = %q, want divider", divider.Type)
	}

	// Verify option blocks have correct labels and buttons
	for i, opt := range d.Options {
		blockIdx := 3 + i
		var optBlock struct {
			Type string `json:"type"`
			Text struct {
				Text string `json:"text"`
			} `json:"text"`
			Accessory struct {
				Type     string `json:"type"`
				Text     struct{ Text string } `json:"text"`
				ActionID string `json:"action_id"`
				Value    string `json:"value"`
			} `json:"accessory"`
		}
		if err := json.Unmarshal(blocks[blockIdx], &optBlock); err != nil {
			t.Fatalf("parse option block %d: %v", i+1, err)
		}
		if !strings.Contains(optBlock.Text.Text, opt.Label) {
			t.Errorf("option %d missing label %q in %q", i+1, opt.Label, optBlock.Text.Text)
		}
		if !strings.Contains(optBlock.Text.Text, opt.Description) {
			t.Errorf("option %d missing description %q in %q", i+1, opt.Description, optBlock.Text.Text)
		}
		expectedLabel := fmt.Sprintf("Choose %d", i+1)
		if optBlock.Accessory.Text.Text != expectedLabel {
			t.Errorf("option %d button = %q, want %q", i+1, optBlock.Accessory.Text.Text, expectedLabel)
		}
		expectedActionID := fmt.Sprintf("resolve_bd-bk1_%d", i+1)
		if optBlock.Accessory.ActionID != expectedActionID {
			t.Errorf("option %d action_id = %q, want %q", i+1, optBlock.Accessory.ActionID, expectedActionID)
		}
	}

	// Verify "Other" option block
	otherIdx := 3 + len(d.Options) // block after the last option
	var otherBlock struct {
		Text struct{ Text string } `json:"text"`
		Accessory struct {
			Text     struct{ Text string } `json:"text"`
			ActionID string                `json:"action_id"`
		} `json:"accessory"`
	}
	if err := json.Unmarshal(blocks[otherIdx], &otherBlock); err != nil {
		t.Fatalf("parse Other block: %v", err)
	}
	if !strings.Contains(otherBlock.Text.Text, "Other") {
		t.Errorf("Other block missing 'Other' text, got %q", otherBlock.Text.Text)
	}
	if otherBlock.Accessory.Text.Text != "Other..." {
		t.Errorf("Other button = %q, want 'Other...'", otherBlock.Accessory.Text.Text)
	}
	if otherBlock.Accessory.ActionID != "resolve_other_bd-bk1" {
		t.Errorf("Other action_id = %q, want 'resolve_other_bd-bk1'", otherBlock.Accessory.ActionID)
	}

	// Verify action buttons block (last block)
	actionsIdx := len(blocks) - 1
	var actionsBlock struct {
		Type     string `json:"type"`
		Elements []struct {
			Type     string `json:"type"`
			Text     struct{ Text string } `json:"text"`
			ActionID string `json:"action_id"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(blocks[actionsIdx], &actionsBlock); err != nil {
		t.Fatalf("parse actions block: %v", err)
	}
	if actionsBlock.Type != "actions" {
		t.Errorf("last block type = %q, want actions", actionsBlock.Type)
	}

	// Should have 4 action buttons: Dismiss, Peek, DM Me, Break Out (since RequestedBy is set)
	if len(actionsBlock.Elements) != 4 {
		t.Fatalf("expected 4 action buttons, got %d", len(actionsBlock.Elements))
	}
	expectedButtons := []struct {
		label    string
		actionID string
	}{
		{"Dismiss", "dismiss_decision"},
		{"Peek", "peek_bd-bk1"},
		{"DM Me", "open_preferences"},
		{"Break Out", "break_out"},
	}
	for i, exp := range expectedButtons {
		if actionsBlock.Elements[i].Text.Text != exp.label {
			t.Errorf("button %d label = %q, want %q", i, actionsBlock.Elements[i].Text.Text, exp.label)
		}
		if actionsBlock.Elements[i].ActionID != exp.actionID {
			t.Errorf("button %d action_id = %q, want %q", i, actionsBlock.Elements[i].ActionID, exp.actionID)
		}
	}
}

func TestUAT_RigRouting_ResolvesToRigChannel(t *testing.T) {
	cfg := &Config{
		Type:            "slack",
		Version:         1,
		Enabled:         true,
		RoutingMode:     "rig",
		DynamicChannels: true,
		ChannelPrefix:   "bd",
		DefaultChannel:  "C_DEFAULT",
		Channels:        make(map[string]string),
		Overrides:       make(map[string]string),
		ChannelNames:    make(map[string]string),
	}
	router := NewRouter(cfg)
	router.SetChannelCreator(&mockChannelCreator{channels: make(map[string]string)})

	d := sampleDecision("bd-rig1")
	// Agent is "gastown/polecats/furiosa" — rig is "gastown"
	result := router.ResolveForDecision(d.RequestedBy, &d, "")

	if result.ChannelID == "" {
		t.Fatal("expected rig channel to be resolved")
	}
	if !strings.Contains(result.MatchedBy, "rig:gastown") {
		t.Errorf("matchedBy = %q, want to contain 'rig:gastown'", result.MatchedBy)
	}
}

func TestUAT_RigRouting_DifferentRigsDifferentChannels(t *testing.T) {
	cfg := &Config{
		Type:            "slack",
		Version:         1,
		Enabled:         true,
		RoutingMode:     "rig",
		DynamicChannels: true,
		ChannelPrefix:   "bd",
		DefaultChannel:  "C_DEFAULT",
		Channels:        make(map[string]string),
		Overrides:       make(map[string]string),
		ChannelNames:    make(map[string]string),
	}
	router := NewRouter(cfg)
	router.SetChannelCreator(&mockChannelCreator{channels: make(map[string]string)})

	d1 := sampleDecision("bd-rig-a")
	d1.RequestedBy = "gastown/polecats/furiosa"
	result1 := router.ResolveForDecision(d1.RequestedBy, &d1, "")

	d2 := sampleDecision("bd-rig-b")
	d2.RequestedBy = "citadel/warboys/nux"
	result2 := router.ResolveForDecision(d2.RequestedBy, &d2, "")

	if result1.ChannelID == result2.ChannelID {
		t.Errorf("different rigs should get different channels, both got %q", result1.ChannelID)
	}
	if !strings.Contains(result1.MatchedBy, "rig:gastown") {
		t.Errorf("result1 matchedBy = %q, want gastown", result1.MatchedBy)
	}
	if !strings.Contains(result2.MatchedBy, "rig:citadel") {
		t.Errorf("result2 matchedBy = %q, want citadel", result2.MatchedBy)
	}
}

func TestUAT_AgentStatusCard_CreatedOnFirstDecision(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	agent := "gastown/polecats/furiosa"
	ts := bot.ensureAgentStatusCard(agent, "C_RIG")

	if ts == "" {
		t.Fatal("expected status card timestamp")
	}

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.PostedMessages) != 1 {
		t.Fatalf("expected 1 posted message (status card), got %d", len(mockAPI.PostedMessages))
	}
	if mockAPI.PostedMessages[0].ChannelID != "C_RIG" {
		t.Errorf("posted to %q, want C_RIG", mockAPI.PostedMessages[0].ChannelID)
	}

	// Verify status card content
	blocksJSON := extractBlocksJSON(t, mockAPI.PostedMessages[0].Options)
	if !strings.Contains(blocksJSON, "furiosa") {
		t.Errorf("status card should contain agent name, got %q", blocksJSON)
	}
	if !strings.Contains(blocksJSON, "gastown") {
		t.Errorf("status card should contain rig name, got %q", blocksJSON)
	}
}

func TestUAT_AgentStatusCard_ReusedOnSecondCall(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	agent := "gastown/polecats/furiosa"
	ts1 := bot.ensureAgentStatusCard(agent, "C_RIG")
	ts2 := bot.ensureAgentStatusCard(agent, "C_RIG")

	if ts1 != ts2 {
		t.Errorf("expected same timestamp, got %q and %q", ts1, ts2)
	}

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	// Should only post once — second call reuses cached card
	if len(mockAPI.PostedMessages) != 1 {
		t.Errorf("expected 1 posted message, got %d", len(mockAPI.PostedMessages))
	}
}

func TestUAT_AgentStatusCard_UpdatesPendingCount(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	agent := "gastown/polecats/furiosa"
	bot.ensureAgentStatusCard(agent, "C_RIG")

	// Increment pending and update
	bot.incrementAgentPending(agent)
	bot.incrementAgentPending(agent)
	bot.updateAgentStatusCard(agent)

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.UpdatedMessages) == 0 {
		t.Fatal("expected status card to be updated")
	}

	// Verify updated content shows pending count
	upd := mockAPI.UpdatedMessages[len(mockAPI.UpdatedMessages)-1]
	_, vals, _ := slack.UnsafeApplyMsgOptions("", upd.ChannelID, "", upd.Options...)
	blocksJSON := vals.Get("blocks")
	if !strings.Contains(blocksJSON, "2 pending") {
		t.Errorf("updated status card should show '2 pending', got %q", blocksJSON)
	}
}

func TestUAT_AgentStatusCard_MultipleAgentsSameChannel(t *testing.T) {
	bot, mockAPI, _ := newTestBot(t)

	ts1 := bot.ensureAgentStatusCard("gastown/polecats/furiosa", "C_RIG")
	ts2 := bot.ensureAgentStatusCard("gastown/crew/max", "C_RIG")

	if ts1 == ts2 {
		t.Error("different agents should get different status cards")
	}

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if len(mockAPI.PostedMessages) != 2 {
		t.Errorf("expected 2 status cards, got %d", len(mockAPI.PostedMessages))
	}
}

func TestUAT_CreateDecision_UrgencyEmojis(t *testing.T) {
	tests := []struct {
		urgency       string
		expectedEmoji string
	}{
		{"high", ":red_circle:"},
		{"medium", ":large_yellow_circle:"},
		{"low", ":large_green_circle:"},
		{"", ":white_circle:"},
	}

	for _, tt := range tests {
		t.Run("urgency_"+tt.urgency, func(t *testing.T) {
			bot, mockAPI, _ := newTestBot(t)

			d := sampleDecision("bd-urg-emoji-" + tt.urgency)
			d.Urgency = tt.urgency
			if err := bot.NotifyNewDecision(&d); err != nil {
				t.Fatalf("NotifyNewDecision: %v", err)
			}

			mockAPI.mu.Lock()
			defer mockAPI.mu.Unlock()

			blocksJSON := extractBlocksJSON(t, mockAPI.PostedMessages[0].Options)
			if !strings.Contains(blocksJSON, tt.expectedEmoji) {
				t.Errorf("urgency %q: blocks should contain %q", tt.urgency, tt.expectedEmoji)
			}
		})
	}
}

// TestUAT_AgentStatusCard_PersistedAcrossRestart verifies that agent status
// cards survive a simulated bot restart. A new bot instance with the same
// StateManager should reuse the existing card without posting a new one.
func TestUAT_AgentStatusCard_PersistedAcrossRestart(t *testing.T) {
	agent := "gastown/polecats/furiosa"
	rigChannel := "C_RIG"

	// --- Bot 1: create the initial status card ---
	mockAPI1 := newMockSlackAPI()
	bot1 := newBotForTest(mockAPI1, &mockDecisionProvider{}, "C_DEFAULT")

	// Give bot1 a StateManager backed by a temp directory
	beadsDir := filepath.Join(t.TempDir(), "fake", ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	bot1.stateManager = NewStateManager(beadsDir)

	// Create the status card (this also persists via StateManager)
	cardTS := bot1.ensureAgentStatusCard(agent, rigChannel)
	if cardTS == "" {
		t.Fatal("expected non-empty status card timestamp")
	}

	mockAPI1.mu.Lock()
	postCount1 := len(mockAPI1.PostedMessages)
	mockAPI1.mu.Unlock()
	if postCount1 != 1 {
		t.Fatalf("expected 1 PostMessage call on bot1, got %d", postCount1)
	}

	// Verify the card was persisted to disk
	ch, ts, ok := bot1.stateManager.GetAgentCard(agent)
	if !ok {
		t.Fatal("expected agent card to be persisted in state")
	}
	if ch != rigChannel || ts != cardTS {
		t.Fatalf("persisted state mismatch: ch=%s ts=%s, want ch=%s ts=%s", ch, ts, rigChannel, cardTS)
	}

	// --- Bot 2: simulate restart with fresh bot, same state directory ---
	mockAPI2 := newMockSlackAPI()
	bot2 := newBotForTest(mockAPI2, &mockDecisionProvider{}, "C_DEFAULT")
	bot2.stateManager = NewStateManager(beadsDir)

	// Hydrate status cards from state (simulates what NewBot does)
	for agentKey, card := range bot2.stateManager.AllAgentCards() {
		bot2.agentStatusCards[agentKey] = messageInfo{channelID: card.ChannelID, timestamp: card.Timestamp}
	}

	// Call ensureAgentStatusCard — should return existing card, NOT post a new one
	ts2 := bot2.ensureAgentStatusCard(agent, rigChannel)
	if ts2 != cardTS {
		t.Fatalf("expected persisted ts=%s after restart, got %s", cardTS, ts2)
	}

	mockAPI2.mu.Lock()
	postCount2 := len(mockAPI2.PostedMessages)
	mockAPI2.mu.Unlock()
	if postCount2 != 0 {
		t.Fatalf("expected 0 PostMessage calls on bot2 (reused card), got %d", postCount2)
	}
}
