package slackbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/steveyegge/beads/internal/eventbus"
)

// NATSWatcher watches for decision events via NATS JetStream.
// It subscribes to the DECISION_EVENTS stream and calls back to the Bot
// when decisions are created, resolved, or cancelled.
type NATSWatcher struct {
	natsURL   string
	bot       *Bot
	decisions *DecisionClient
	conn      *nats.Conn
	sub       *nats.Subscription
	seen      map[string]bool
	seenMu    sync.Mutex
}

// NewNATSWatcher creates a watcher that subscribes to decision events on the
// given NATS server and forwards them to the Bot.
func NewNATSWatcher(natsURL string, bot *Bot, decisions *DecisionClient) *NATSWatcher {
	return &NATSWatcher{
		natsURL:   natsURL,
		bot:       bot,
		decisions: decisions,
		seen:      make(map[string]bool),
	}
}

// Run connects to NATS and subscribes to decision events on the
// DECISION_EVENTS stream using a durable "slack-bot" consumer. It
// reconnects with exponential backoff on disconnect and catches up on
// missed decisions after each reconnect. Run blocks until ctx is cancelled.
func (w *NATSWatcher) Run(ctx context.Context) error {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := w.connect(ctx)
		if err != nil {
			log.Printf("slackbot/nats: connect error: %v (retry in %v)", err, backoff)
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

		// Reset backoff on successful connect.
		backoff = time.Second

		w.catchUpMissedDecisions(ctx)

		// Wait for context cancellation or disconnect.
		select {
		case <-ctx.Done():
			w.Close()
			return ctx.Err()
		case <-w.waitDisconnect():
			log.Printf("slackbot/nats: disconnected, will reconnect")
			w.Close()
		}
	}
}

// connect establishes the NATS connection and JetStream subscription.
func (w *NATSWatcher) connect(ctx context.Context) error {
	nc, err := nats.Connect(w.natsURL,
		nats.Name("beads-slack-bot"),
		nats.RetryOnFailedConnect(false),
	)
	if err != nil {
		return fmt.Errorf("nats connect: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return fmt.Errorf("jetstream context: %w", err)
	}

	sub, err := js.Subscribe(
		eventbus.SubjectDecisionPrefix+">",
		w.handleMessage,
		nats.Durable("slack-bot"),
		nats.DeliverNew(),
		nats.AckExplicit(),
		nats.ManualAck(),
	)
	if err != nil {
		nc.Close()
		return fmt.Errorf("jetstream subscribe: %w", err)
	}

	w.conn = nc
	w.sub = sub
	log.Printf("slackbot/nats: connected to %s, subscribed to %s",
		w.natsURL, eventbus.StreamDecisionEvents)
	return nil
}

// waitDisconnect returns a channel that closes when the NATS connection is
// no longer connected (closed or disconnected without reconnect).
func (w *NATSWatcher) waitDisconnect() <-chan struct{} {
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

// handleMessage parses a NATS message from the DECISION_EVENTS stream and
// dispatches it to the appropriate Bot notification method.
func (w *NATSWatcher) handleMessage(msg *nats.Msg) {
	var payload eventbus.DecisionEventPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Printf("slackbot/nats: unmarshal error: %v", err)
		_ = msg.Ack()
		return
	}

	// Determine event type from the NATS subject suffix.
	// Subjects are formatted as "decisions.<EventType>".
	subject := msg.Subject
	eventType := eventbus.EventType(subject[len(eventbus.SubjectDecisionPrefix):])

	switch eventType {
	case eventbus.EventDecisionCreated:
		w.notifyNewDecision(payload)
	case eventbus.EventDecisionResponded:
		w.notifyResolvedDecision(payload)
	case eventbus.EventDecisionEscalated:
		w.notifyEscalatedDecision(payload)
	case eventbus.EventDecisionExpired:
		w.handleExpiredDecision(payload)
	default:
		log.Printf("slackbot/nats: ignoring event type %q for decision %s",
			eventType, payload.DecisionID)
	}

	_ = msg.Ack()
}

// notifyNewDecision fetches the full decision and notifies the Bot.
// It deduplicates by decision ID so repeated deliveries are harmless.
func (w *NATSWatcher) notifyNewDecision(payload eventbus.DecisionEventPayload) {
	w.seenMu.Lock()
	if w.seen[payload.DecisionID] {
		w.seenMu.Unlock()
		return
	}
	w.seen[payload.DecisionID] = true
	w.seenMu.Unlock()

	decision, err := w.fetchWithRetry(payload.DecisionID, 3, 500*time.Millisecond)
	if err != nil {
		log.Printf("slackbot/nats: fetch decision %s failed: %v", payload.DecisionID, err)
		return
	}

	// Skip if already resolved by the time we fetched it.
	if decision.Resolved {
		return
	}

	w.bot.NotifyNewDecision(decision)
}

// notifyResolvedDecision fetches the full decision and notifies the Bot
// of resolution. It deduplicates with a "resolved:" prefix key.
func (w *NATSWatcher) notifyResolvedDecision(payload eventbus.DecisionEventPayload) {
	key := "resolved:" + payload.DecisionID
	w.seenMu.Lock()
	if w.seen[key] {
		w.seenMu.Unlock()
		return
	}
	w.seen[key] = true
	w.seenMu.Unlock()

	decision, err := w.fetchWithRetry(payload.DecisionID, 3, 500*time.Millisecond)
	if err != nil {
		log.Printf("slackbot/nats: fetch resolved decision %s failed: %v",
			payload.DecisionID, err)
		return
	}

	// Skip if it was resolved via Slack to avoid double-updating.
	if decision.ResolvedBy == "slack" {
		return
	}

	w.bot.NotifyResolution(decision)
}

// notifyEscalatedDecision fetches the full decision and highlights it in Slack.
// It deduplicates with an "escalated:" prefix key.
func (w *NATSWatcher) notifyEscalatedDecision(payload eventbus.DecisionEventPayload) {
	key := "escalated:" + payload.DecisionID
	w.seenMu.Lock()
	if w.seen[key] {
		w.seenMu.Unlock()
		return
	}
	w.seen[key] = true
	w.seenMu.Unlock()

	decision, err := w.fetchWithRetry(payload.DecisionID, 3, 500*time.Millisecond)
	if err != nil {
		log.Printf("slackbot/nats: fetch escalated decision %s failed: %v",
			payload.DecisionID, err)
		return
	}

	if decision.Resolved {
		return
	}

	w.bot.NotifyEscalation(decision)
}

// handleExpiredDecision dismisses the decision message in Slack.
func (w *NATSWatcher) handleExpiredDecision(payload eventbus.DecisionEventPayload) {
	w.bot.DismissDecisionByID(payload.DecisionID)
}

// catchUpMissedDecisions fetches all pending decisions and notifies the Bot
// for any that haven't been seen yet. Called on initial connect and reconnect.
func (w *NATSWatcher) catchUpMissedDecisions(ctx context.Context) {
	pending, err := w.decisions.ListPending(ctx)
	if err != nil {
		log.Printf("slackbot/nats: catch-up list pending failed: %v", err)
		return
	}

	for i := range pending {
		w.seenMu.Lock()
		already := w.seen[pending[i].ID]
		w.seenMu.Unlock()
		if already {
			continue
		}

		w.seenMu.Lock()
		w.seen[pending[i].ID] = true
		w.seenMu.Unlock()

		d := pending[i]
		w.bot.NotifyNewDecision(&d)
	}
}

// fetchWithRetry fetches a decision by ID, retrying on transient errors.
func (w *NATSWatcher) fetchWithRetry(decisionID string, attempts int, backoff time.Duration) (*Decision, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		d, err := w.decisions.GetDecision(context.Background(), decisionID)
		if err == nil {
			return d, nil
		}
		lastErr = err
		if i < attempts-1 {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return nil, fmt.Errorf("after %d attempts: %w", attempts, lastErr)
}

// Close drains the subscription and closes the NATS connection.
func (w *NATSWatcher) Close() {
	if w.sub != nil {
		_ = w.sub.Drain()
		w.sub = nil
	}
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
}
