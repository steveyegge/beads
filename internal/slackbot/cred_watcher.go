package slackbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/slack-go/slack"
)

// CoopCredWatcher subscribes to coop credential events on core NATS
// and posts reauth notifications to Slack. When a user replies with
// an authorization code, it completes the reauth flow via the broker API.
type CoopCredWatcher struct {
	natsURL   string
	natsToken string
	brokerURL string // e.g. "http://gastown-next-coopmux:9800"
	authToken string // Bearer token for broker API
	bot       *Bot
	conn      *nats.Conn
	sub       *nats.Subscription

	// Track reauth thread messages so we can intercept code replies.
	reauthThreads   map[string]reauthInfo // thread_ts → reauth info
	completedThreads map[string]bool      // threads where reauth succeeded (don't re-recover)
	reauthThreadsMu sync.RWMutex
}

// reauthInfo tracks a pending reauth notification posted to Slack.
type reauthInfo struct {
	account   string
	channelID string
	state     string // OAuth state from coopmux reauth initiation
}

// credentialEventPayload mirrors the NATS event published by coop.
type credentialEventPayload struct {
	EventType string  `json:"event_type"`
	Account   string  `json:"account"`
	Error     *string `json:"error,omitempty"`
	AuthURL   *string `json:"auth_url,omitempty"`
	UserCode  *string `json:"user_code,omitempty"`
	Ts        string  `json:"ts"`
}

// NewCoopCredWatcher creates a watcher that subscribes to coop credential
// events and forwards reauth URLs to Slack.
func NewCoopCredWatcher(natsURL, natsToken, brokerURL, authToken string, bot *Bot) *CoopCredWatcher {
	return &CoopCredWatcher{
		natsURL:          natsURL,
		natsToken:        natsToken,
		brokerURL:        strings.TrimRight(brokerURL, "/"),
		authToken:        authToken,
		bot:              bot,
		reauthThreads:    make(map[string]reauthInfo),
		completedThreads: make(map[string]bool),
	}
}

// Run connects to NATS and subscribes to coop.events.credential.
// It reconnects with backoff on disconnect and blocks until ctx is canceled.
func (w *CoopCredWatcher) Run(ctx context.Context) error {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := w.connect()
		if err != nil {
			log.Printf("slackbot/cred: connect error: %v (retry in %v)", err, backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff = backoff * 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		backoff = time.Second

		select {
		case <-ctx.Done():
			w.Close()
			return ctx.Err()
		case <-w.waitDisconnect():
			log.Printf("slackbot/cred: disconnected, will reconnect")
			w.Close()
		}
	}
}

// connect establishes the NATS connection and subscribes to credential events.
func (w *CoopCredWatcher) connect() error {
	connectOpts := []nats.Option{
		nats.Name("beads-slack-bot-cred"),
		nats.RetryOnFailedConnect(false),
	}
	if w.natsToken != "" {
		connectOpts = append(connectOpts, nats.Token(w.natsToken))
	}

	nc, err := nats.Connect(w.natsURL, connectOpts...)
	if err != nil {
		return fmt.Errorf("nats connect: %w", err)
	}

	// Subscribe to core NATS (not JetStream) — coop publishes credential
	// events on core NATS, not JetStream.
	sub, err := nc.Subscribe("coop.events.credential", w.handleMessage)
	if err != nil {
		nc.Close()
		return fmt.Errorf("subscribe coop.events.credential: %w", err)
	}

	w.conn = nc
	w.sub = sub
	log.Printf("slackbot/cred: connected to %s, subscribed to coop.events.credential", w.natsURL)
	return nil
}

// waitDisconnect returns a channel that closes when the NATS connection drops.
func (w *CoopCredWatcher) waitDisconnect() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		if w.conn == nil {
			close(ch)
			return
		}
		for w.conn.IsConnected() {
			time.Sleep(500 * time.Millisecond)
		}
		close(ch)
	}()
	return ch
}

const (
	// Don't send reauth notifications more often than this.
	reauthNotifyMinInterval = 5 * time.Minute
)

// handleMessage parses a credential event and dispatches reauth notifications.
func (w *CoopCredWatcher) handleMessage(msg *nats.Msg) {
	var payload credentialEventPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Printf("slackbot/cred: unmarshal error: %v", err)
		return
	}

	switch payload.EventType {
	case "reauth_required":
		log.Printf("slackbot/cred: reauth_required for %s", payload.Account)

		// Rate-limit: don't spam Slack with reauth notifications.
		w.reauthThreadsMu.RLock()
		tooRecent := false
		for _, info := range w.reauthThreads {
			if info.account == payload.Account {
				tooRecent = true
				break
			}
		}
		w.reauthThreadsMu.RUnlock()
		if tooRecent {
			return
		}

		// Always initiate reauth via coopmux to get both the auth URL and
		// the state token needed for the exchange call.
		go w.pullReauthURL(payload.Account)
	case "refreshed":
		log.Printf("slackbot/cred: account %s refreshed", payload.Account)
		// Don't clear reauth threads here — the credential seeder re-seeds
		// revoked credentials on pod restart, emitting a false "refreshed"
		// event that would wipe the thread tracking. Threads are cleaned up
		// when the code is successfully submitted (submitReauthCode).
	case "refresh_failed":
		errMsg := ""
		if payload.Error != nil {
			errMsg = *payload.Error
		}
		log.Printf("slackbot/cred: account %s refresh failed: %s", payload.Account, errMsg)

		// If we haven't already posted a reauth notification for this account,
		// pull the auth URL from the broker. This handles the race where the
		// broker emitted reauth_required on core NATS before we subscribed.
		w.reauthThreadsMu.RLock()
		alreadyNotified := false
		for _, info := range w.reauthThreads {
			if info.account == payload.Account {
				alreadyNotified = true
				break
			}
		}
		w.reauthThreadsMu.RUnlock()

		if !alreadyNotified {
			go w.pullReauthURL(payload.Account)
		}
	}
}

// notifyReauth posts the reauth URL to the default Slack channel.
// state is the OAuth state token from coopmux reauth initiation (may be empty
// for NATS-originated events; pullReauthURL will supply it).
func (w *CoopCredWatcher) notifyReauth(account, authURL, state string) {
	channelID := w.bot.channelID

	blocks := []slack.Block{
		slack.NewHeaderBlock(
			slack.NewTextBlockObject("plain_text", "Credential Re-authentication Required", false, false),
		),
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", fmt.Sprintf(
				"Account *%s* needs re-authentication.\n\n"+
					"1. Open the link below in your browser\n"+
					"2. Sign in to Claude\n"+
					"3. Copy the authorization code shown\n"+
					"4. Reply to this thread with the code",
				account,
			), false, false),
			nil, nil,
		),
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("<%s|Click here to authenticate>", authURL), false, false),
			nil, nil,
		),
		slack.NewContextBlock("",
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("Account: `%s` | Reply with the code in this thread to complete re-authentication", account),
				false, false),
		),
	}

	_, ts, err := w.bot.client.PostMessage(channelID,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(fmt.Sprintf("Credential reauth required for %s", account), false),
	)
	if err != nil {
		log.Printf("slackbot/cred: failed to post reauth notification: %v", err)
		return
	}

	// Track this thread for code replies.
	w.reauthThreadsMu.Lock()
	w.reauthThreads[ts] = reauthInfo{
		account:   account,
		channelID: channelID,
		state:     state,
	}
	w.reauthThreadsMu.Unlock()

	log.Printf("slackbot/cred: posted reauth notification for %s (thread %s)", account, ts)
}

// HandleThreadReply checks if a thread reply is a reauth code submission.
// Returns true if the reply was handled as a reauth code.
func (w *CoopCredWatcher) HandleThreadReply(channelID, threadTS, text, userID string) bool {
	w.reauthThreadsMu.RLock()
	info, ok := w.reauthThreads[threadTS]
	completed := w.completedThreads[threadTS]
	w.reauthThreadsMu.RUnlock()

	if completed {
		return false
	}

	if !ok {
		// Thread not tracked (e.g., pod restarted since the reauth notification was posted).
		// Check if the parent message is a reauth notification from this bot.
		info, ok = w.recoverReauthThread(channelID, threadTS)
		if !ok {
			return false
		}
		log.Printf("slackbot/cred: recovered reauth thread %s for account %s", threadTS, info.account)
	}

	trimmed := strings.TrimSpace(text)
	code := extractAuthCode(trimmed)
	if code == "" {
		return false
	}

	// Submit the code to the broker.
	go w.submitReauthCode(info, code, channelID, threadTS, userID)
	return true
}

// submitReauthCode POSTs the authorization code to the broker's complete endpoint.
func (w *CoopCredWatcher) submitReauthCode(info reauthInfo, code, channelID, threadTS, userID string) {
	if w.brokerURL == "" {
		w.bot.postThreadReply(channelID, threadTS,
			"Broker URL not configured (set COOP_BROKER_URL)")
		return
	}

	if info.state == "" {
		w.bot.postThreadReply(channelID, threadTS,
			"Missing OAuth state for this reauth session. Please start a new reauth flow.")
		return
	}

	reqBody := map[string]string{
		"state": info.state,
		"code":  code,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("slackbot/cred: marshal reauth code request: %v", err)
		return
	}

	log.Printf("slackbot/cred: submitting reauth code for %s (code_len=%d)", info.account, len(code))

	url := w.brokerURL + "/api/v1/credentials/exchange"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		log.Printf("slackbot/cred: create request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if w.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+w.authToken)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("slackbot/cred: broker request failed: %v", err)
		w.bot.postThreadReply(channelID, threadTS,
			fmt.Sprintf("Failed to submit code to broker: %v", err))
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK {
		// Remove from tracked threads and mark completed so recoverReauthThread
		// doesn't re-add it for subsequent conversational replies.
		w.reauthThreadsMu.Lock()
		delete(w.reauthThreads, threadTS)
		w.completedThreads[threadTS] = true
		w.reauthThreadsMu.Unlock()

		log.Printf("slackbot/cred: reauth completed for account %s (submitted by %s)", info.account, userID)
		w.bot.postThreadReply(channelID, threadTS,
			fmt.Sprintf(":white_check_mark: Re-authentication successful for account *%s*.", info.account))
	} else {
		log.Printf("slackbot/cred: broker returned %d: %s", resp.StatusCode, string(respBody))
		w.bot.postThreadReply(channelID, threadTS,
			fmt.Sprintf("Re-authentication failed (HTTP %d). Check the code and try again.", resp.StatusCode))
	}
}

// fetchReauthState calls coopmux's reauth endpoint to get a fresh OAuth state
// token for an account. Used when recovering reauth threads after pod restart.
func (w *CoopCredWatcher) fetchReauthState(account string) string {
	if w.brokerURL == "" {
		return ""
	}

	reqBody, _ := json.Marshal(map[string]string{"account": account})
	reqURL := w.brokerURL + "/api/v1/credentials/reauth"
	req, err := http.NewRequest("POST", reqURL, bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("slackbot/cred: create reauth state request: %v", err)
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	if w.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+w.authToken)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("slackbot/cred: reauth state request failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("slackbot/cred: reauth state returned %d: %s", resp.StatusCode, string(body))
		return ""
	}

	var session struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(body, &session); err != nil {
		return ""
	}
	return session.State
}

// recoverReauthThread checks if a thread's parent message is a reauth notification
// posted by this bot. This handles the case where the pod restarted after posting
// the notification, losing the in-memory reauthThreads map.
func (w *CoopCredWatcher) recoverReauthThread(channelID, threadTS string) (reauthInfo, bool) {
	// Fetch the full thread (parent + replies) to check if reauth already completed.
	msgs, _, _, err := w.bot.client.GetConversationReplies(&slack.GetConversationRepliesParameters{
		ChannelID: channelID,
		Timestamp: threadTS,
		Inclusive: true,
	})
	if err != nil || len(msgs) == 0 {
		return reauthInfo{}, false
	}

	parent := msgs[0]

	// Must be from the bot.
	if parent.User != w.bot.botUserID && parent.BotID == "" {
		return reauthInfo{}, false
	}

	// Check if the message text contains our reauth marker.
	text := parent.Text
	if !strings.Contains(text, "Credential reauth required for ") {
		return reauthInfo{}, false
	}

	// Check if the bot already posted a success reply — reauth is done, don't re-track.
	for _, reply := range msgs[1:] {
		if (reply.User == w.bot.botUserID || reply.BotID != "") &&
			strings.Contains(reply.Text, "Re-authentication successful") {
			// Mark as completed so we don't fetch the thread again.
			w.reauthThreadsMu.Lock()
			w.completedThreads[threadTS] = true
			w.reauthThreadsMu.Unlock()
			return reauthInfo{}, false
		}
	}

	// Extract account name from "Credential reauth required for <account>"
	account := ""
	if idx := strings.Index(text, "Credential reauth required for "); idx >= 0 {
		account = strings.TrimSpace(text[idx+len("Credential reauth required for "):])
	}
	if account == "" {
		return reauthInfo{}, false
	}

	// Re-initiate reauth via coopmux to get a fresh state token.
	// The original state was lost when the pod restarted.
	state := w.fetchReauthState(account)
	if state == "" {
		log.Printf("slackbot/cred: recovered thread %s but could not get fresh state for %s", threadTS, account)
	}

	info := reauthInfo{
		account:   account,
		channelID: channelID,
		state:     state,
	}

	// Re-track this thread so subsequent replies don't need to fetch again.
	w.reauthThreadsMu.Lock()
	w.reauthThreads[threadTS] = info
	w.reauthThreadsMu.Unlock()

	return info, true
}

// extractAuthCode extracts the authorization code from user input.
// Handles these cases:
//   - Full callback URL: https://platform.claude.com/oauth/code/callback?code=ABC123
//   - Slack-formatted URL: <https://platform.claude.com/oauth/code/callback?code=ABC123>
//   - Raw code with state: UrVjjR...#nqIa2... (code#state from callback page)
//   - Raw code: UrVjjR...
//
// Returns "" if the text doesn't look like a valid authorization code
// (must be 20+ chars, alphanumeric/base64url only, no spaces).
func extractAuthCode(text string) string {
	// Strip Slack URL formatting: <url|display> or <url>
	if strings.HasPrefix(text, "<") {
		end := strings.Index(text, ">")
		if end > 0 {
			text = text[1:end]
		}
		// Remove display text after pipe
		if pipeIdx := strings.Index(text, "|"); pipeIdx > 0 {
			text = text[:pipeIdx]
		}
	}

	// If it looks like a URL with a code parameter, extract it.
	if strings.Contains(text, "code=") {
		parsed, err := url.Parse(text)
		if err == nil {
			if code := parsed.Query().Get("code"); code != "" {
				text = code
			}
		}
	}

	// Claude's OAuth callback includes the state after a # separator:
	//   code#state  or  code%23state
	// Strip the state suffix — the actual code is the part before #.
	if hashIdx := strings.Index(text, "#"); hashIdx > 0 {
		text = text[:hashIdx]
	}

	// Validate: auth codes are 20+ chars of alphanumeric/base64url (no spaces).
	// This prevents submitting conversational replies ("lol", "Ha", etc.) as codes.
	if !looksLikeAuthCode(text) {
		return ""
	}

	return text
}

// looksLikeAuthCode returns true if s looks like an OAuth authorization code:
// at least 20 characters, only alphanumeric, dash, underscore, plus, slash, equals.
func looksLikeAuthCode(s string) bool {
	if len(s) < 20 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '+' || c == '/' || c == '=') {
			return false
		}
	}
	return true
}

// pullReauthURL calls coopmux's reauth endpoint to initiate an OAuth flow
// and posts the auth URL to Slack. This handles the race where the broker
// emitted reauth_required before the slackbot subscribed.
func (w *CoopCredWatcher) pullReauthURL(account string) {
	if w.brokerURL == "" {
		log.Printf("slackbot/cred: cannot pull reauth URL — broker URL not configured")
		return
	}

	reqBody, _ := json.Marshal(map[string]string{"account": account})
	url := w.brokerURL + "/api/v1/credentials/reauth"
	req, err := http.NewRequest("POST", url, bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("slackbot/cred: create reauth request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if w.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+w.authToken)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("slackbot/cred: broker reauth request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("slackbot/cred: broker reauth returned %d: %s", resp.StatusCode, string(body))
		return
	}

	var session struct {
		Account string `json:"account"`
		AuthURL string `json:"auth_url"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal(body, &session); err != nil {
		log.Printf("slackbot/cred: parse reauth response: %v", err)
		return
	}
	if session.AuthURL == "" {
		log.Printf("slackbot/cred: broker returned empty auth_url for %s", account)
		return
	}

	// Double-check we haven't been beaten by a concurrent notification.
	w.reauthThreadsMu.RLock()
	for _, info := range w.reauthThreads {
		if info.account == account {
			w.reauthThreadsMu.RUnlock()
			return
		}
	}
	w.reauthThreadsMu.RUnlock()

	log.Printf("slackbot/cred: pulled reauth URL for %s from coopmux (startup race recovery)", account)
	w.notifyReauth(account, session.AuthURL, session.State)
}

// Close drains the subscription and closes the NATS connection.
func (w *CoopCredWatcher) Close() {
	if w.sub != nil {
		_ = w.sub.Unsubscribe()
		w.sub = nil
	}
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
}
