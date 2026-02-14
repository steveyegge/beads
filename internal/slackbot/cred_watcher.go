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
	brokerURL string // e.g. "http://gastown-next-coop-broker:8080"
	authToken string // Bearer token for broker API
	bot       *Bot
	conn      *nats.Conn
	sub       *nats.Subscription

	// Track reauth thread messages so we can intercept code replies.
	reauthThreads   map[string]reauthInfo // thread_ts → reauth info
	reauthThreadsMu sync.RWMutex
}

// reauthInfo tracks a pending reauth notification posted to Slack.
type reauthInfo struct {
	account     string
	channelID   string
	clientID    string
	redirectURI string
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
		natsURL:       natsURL,
		natsToken:     natsToken,
		brokerURL:     strings.TrimRight(brokerURL, "/"),
		authToken:     authToken,
		bot:           bot,
		reauthThreads: make(map[string]reauthInfo),
	}
}

// Run connects to NATS and subscribes to coop.events.credential.
// It reconnects with backoff on disconnect and blocks until ctx is cancelled.
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

// Default OAuth constants for constructing auth URLs when the broker
// doesn't include them in the NATS event.
const (
	defaultAuthorizeURL = "https://claude.ai/oauth/authorize"
	defaultClientID     = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	defaultRedirectURI  = "https://platform.claude.com/oauth/code/callback"
	defaultScope        = "user:profile user:inference user:sessions:claude_code user:mcp_servers"

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
		authURL := ""
		if payload.AuthURL != nil {
			authURL = *payload.AuthURL
		}
		if authURL == "" {
			// Broker omitted auth_url — construct it from defaults.
			authURL = defaultAuthorizeURL +
				"?code=true" +
				"&client_id=" + defaultClientID +
				"&redirect_uri=" + url.QueryEscape(defaultRedirectURI) +
				"&scope=" + url.QueryEscape(defaultScope)
			log.Printf("slackbot/cred: reauth_required for %s (constructed auth URL)", payload.Account)
		} else {
			log.Printf("slackbot/cred: reauth_required for %s (broker-provided URL)", payload.Account)
		}

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

		w.notifyReauth(payload.Account, authURL)
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
func (w *CoopCredWatcher) notifyReauth(account, authURL string) {
	channelID := w.bot.channelID

	// Parse client_id and redirect_uri from the auth URL for the complete call.
	clientID, redirectURI := parseAuthURLParams(authURL)

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
		account:     account,
		channelID:   channelID,
		clientID:    clientID,
		redirectURI: redirectURI,
	}
	w.reauthThreadsMu.Unlock()

	log.Printf("slackbot/cred: posted reauth notification for %s (thread %s)", account, ts)
}

// HandleThreadReply checks if a thread reply is a reauth code submission.
// Returns true if the reply was handled as a reauth code.
func (w *CoopCredWatcher) HandleThreadReply(channelID, threadTS, text, userID string) bool {
	w.reauthThreadsMu.RLock()
	info, ok := w.reauthThreads[threadTS]
	w.reauthThreadsMu.RUnlock()

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
	log.Printf("slackbot/cred: HandleThreadReply raw_text=%q (len=%d)", trimmed, len(trimmed))
	code := extractAuthCode(trimmed)
	if code == "" {
		return false
	}
	log.Printf("slackbot/cred: HandleThreadReply extracted_code=%q (len=%d)", code, len(code))

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

	reqBody := map[string]string{
		"account":      info.account,
		"code":         code,
		"redirect_uri": info.redirectURI,
		"client_id":    info.clientID,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("slackbot/cred: marshal reauth code request: %v", err)
		return
	}

	log.Printf("slackbot/cred: submitting code to broker: account=%s code_len=%d code_prefix=%q redirect_uri=%s client_id=%s",
		info.account, len(code), safePrefix(code, 10), info.redirectURI, info.clientID)

	url := w.brokerURL + "/api/v1/credentials/login-reauth/complete"
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
		// Remove from tracked threads — reauth complete.
		w.reauthThreadsMu.Lock()
		delete(w.reauthThreads, threadTS)
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

// parseAuthURLParams extracts client_id and redirect_uri from an OAuth authorization URL.
func parseAuthURLParams(authURL string) (clientID, redirectURI string) {
	// Default fallbacks matching Claude's standard values.
	clientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	redirectURI = "https://platform.claude.com/oauth/code/callback"

	qIdx := strings.Index(authURL, "?")
	if qIdx < 0 {
		return
	}
	query := authURL[qIdx+1:]
	for _, param := range strings.Split(query, "&") {
		kv := strings.SplitN(param, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "client_id":
			clientID = kv[1]
		case "redirect_uri":
			decoded, err := decodePercent(kv[1])
			if err == nil {
				redirectURI = decoded
			}
		}
	}
	return
}

// decodePercent does a simple percent-decoding (URL decode).
func decodePercent(s string) (string, error) {
	var buf strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi := unhex(s[i+1])
			lo := unhex(s[i+2])
			if hi >= 0 && lo >= 0 {
				buf.WriteByte(byte(hi<<4 | lo))
				i += 2
				continue
			}
		}
		if s[i] == '+' {
			buf.WriteByte(' ')
		} else {
			buf.WriteByte(s[i])
		}
	}
	return buf.String(), nil
}

func unhex(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c - 'a' + 10)
	case 'A' <= c && c <= 'F':
		return int(c - 'A' + 10)
	}
	return -1
}

// recoverReauthThread checks if a thread's parent message is a reauth notification
// posted by this bot. This handles the case where the pod restarted after posting
// the notification, losing the in-memory reauthThreads map.
func (w *CoopCredWatcher) recoverReauthThread(channelID, threadTS string) (reauthInfo, bool) {
	// Fetch the parent message of the thread.
	msgs, _, _, err := w.bot.client.GetConversationReplies(&slack.GetConversationRepliesParameters{
		ChannelID: channelID,
		Timestamp: threadTS,
		Limit:     1,
		Inclusive:  true,
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

	// Extract account name from "Credential reauth required for <account>"
	account := ""
	if idx := strings.Index(text, "Credential reauth required for "); idx >= 0 {
		account = strings.TrimSpace(text[idx+len("Credential reauth required for "):])
	}
	if account == "" {
		return reauthInfo{}, false
	}

	// Extract auth URL from blocks to get client_id and redirect_uri.
	clientID, redirectURI := parseAuthURLParams("") // defaults
	for _, block := range parent.Blocks.BlockSet {
		if sec, ok := block.(*slack.SectionBlock); ok && sec.Text != nil {
			if idx := strings.Index(sec.Text.Text, "<https://"); idx >= 0 {
				end := strings.Index(sec.Text.Text[idx+1:], ">")
				if end > 0 {
					link := sec.Text.Text[idx+1 : idx+1+end]
					// Remove the display text after |
					if pipeIdx := strings.Index(link, "|"); pipeIdx > 0 {
						link = link[:pipeIdx]
					}
					clientID, redirectURI = parseAuthURLParams(link)
				}
			}
		}
	}

	info := reauthInfo{
		account:     account,
		channelID:   channelID,
		clientID:    clientID,
		redirectURI: redirectURI,
	}

	// Re-track this thread so subsequent replies don't need to fetch again.
	w.reauthThreadsMu.Lock()
	w.reauthThreads[threadTS] = info
	w.reauthThreadsMu.Unlock()

	return info, true
}

// extractAuthCode extracts the authorization code from user input.
// Handles three cases:
//   - Full callback URL: https://platform.claude.com/oauth/code/callback?code=ABC123
//   - Slack-formatted URL: <https://platform.claude.com/oauth/code/callback?code=ABC123>
//   - Raw code: ABC123
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
				return code
			}
		}
	}

	return text
}

// pullReauthURL calls the broker's login-reauth endpoint to get (or create) a
// pending reauth session and posts the auth URL to Slack. This handles the race
// where the broker emitted reauth_required before the slackbot subscribed.
func (w *CoopCredWatcher) pullReauthURL(account string) {
	if w.brokerURL == "" {
		log.Printf("slackbot/cred: cannot pull reauth URL — broker URL not configured")
		return
	}

	reqBody, _ := json.Marshal(map[string]string{"account": account})
	url := w.brokerURL + "/api/v1/credentials/login-reauth"
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

	log.Printf("slackbot/cred: pulled reauth URL for %s from broker (startup race recovery)", account)
	w.notifyReauth(account, session.AuthURL)
}

// safePrefix returns the first n characters of s (for safe logging of secrets).
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
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
