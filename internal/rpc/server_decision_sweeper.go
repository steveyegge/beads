package rpc

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/beads/internal/eventbus"
)

// startDecisionSweeper runs a background goroutine that periodically checks
// for expired decisions.  A decision is expired when:
//
//	now > decision.CreatedAt + config.GetDecisionDefaultTimeout()
//
// Expired decisions are auto-canceled and an EventDecisionExpired event is
// emitted so downstream consumers (Slack bot, etc.) can react.
//
// The sweeper checks every 5 minutes and stops when shutdownChan is closed.
func (s *Server) startDecisionSweeper() {
	interval := 5 * time.Minute
	if env := os.Getenv("BEADS_DECISION_SWEEP_INTERVAL"); env != "" {
		if d, err := time.ParseDuration(env); err == nil && d > 0 {
			interval = d
		}
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-s.shutdownChan:
				return
			case <-ticker.C:
				s.sweepExpiredDecisions()
			}
		}
	}()
}

// sweepExpiredDecisions scans pending decisions and expires those that have
// exceeded the configured timeout.
func (s *Server) sweepExpiredDecisions() {
	store := s.storage
	if store == nil {
		return
	}

	timeout := s.decisionTimeout
	if timeout <= 0 {
		return // Expiration disabled (timeout = 0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pending, err := store.ListPendingDecisions(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "decision-sweeper: list pending: %v\n", err)
		return
	}

	now := time.Now()
	for _, dp := range pending {
		if dp.CreatedAt.IsZero() {
			continue
		}

		deadline := dp.CreatedAt.Add(timeout)
		if now.Before(deadline) {
			continue // Not expired yet
		}

		// Check if a default option is configured for auto-accept
		selectedOption := "_expired"
		if dp.DefaultOption != "" {
			selectedOption = dp.DefaultOption
		}

		// Expire: mark as responded with _expired
		dp.RespondedAt = &now
		dp.RespondedBy = "system:timeout"
		dp.SelectedOption = selectedOption
		dp.ResponseText = fmt.Sprintf("Decision expired after %s with no response", timeout)

		if err := store.UpdateDecisionPoint(ctx, dp); err != nil {
			fmt.Fprintf(os.Stderr, "decision-sweeper: update %s: %v\n", dp.IssueID, err)
			continue
		}

		// Close the gate issue
		closeReason := fmt.Sprintf("Decision expired after %s", timeout)
		if err := store.CloseIssue(ctx, dp.IssueID, closeReason, "system:timeout", ""); err != nil {
			fmt.Fprintf(os.Stderr, "decision-sweeper: close gate %s: %v\n", dp.IssueID, err)
			continue
		}

		// Emit expiration event so Slack bot can dismiss the message
		s.emitDecisionEvent(eventbus.EventDecisionExpired, eventbus.DecisionEventPayload{
			DecisionID:  dp.IssueID,
			Question:    dp.Prompt,
			Urgency:     dp.Urgency,
			RequestedBy: dp.RequestedBy,
		})

		// Also emit mutation so SSE/event-driven sync picks it up
		s.emitMutation(MutationUpdate, dp.IssueID, "", "")

		fmt.Fprintf(os.Stderr, "decision-sweeper: expired %s (created %s, timeout %s)\n",
			dp.IssueID, dp.CreatedAt.Format(time.RFC3339), timeout)
	}
}
