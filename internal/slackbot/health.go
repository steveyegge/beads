// Package slackbot provides a Slack bot integration for beads issue tracking.
package slackbot

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
)

// HealthServer provides HTTP health endpoints for Kubernetes probes.
type HealthServer struct {
	bot    *Bot
	server *http.Server
	port   int
}

// NewHealthServer creates a new health server for the given bot.
func NewHealthServer(bot *Bot, port int) *HealthServer {
	return &HealthServer{
		bot:  bot,
		port: port,
	}
}

// Start begins serving health endpoints. This should be called in a goroutine.
func (h *HealthServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// /healthz - liveness probe: checks if the bot is connected to Slack
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if h.bot.IsConnected() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("disconnected"))
		}
	})

	// /readyz - readiness probe: checks if the bot is ready to receive traffic
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// Bot is ready once it has been created and started
		// Even if temporarily disconnected, it can still queue events
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})

	h.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", h.port),
		Handler: mux,
	}

	log.Printf("Health: Starting health server on :%d", h.port)

	// Start server in a way that respects context cancellation
	errCh := make(chan error, 1)
	go func() {
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		log.Println("Health: Shutting down health server")
		return h.server.Shutdown(context.Background())
	case err := <-errCh:
		return fmt.Errorf("health server error: %w", err)
	}
}

// botConnected tracks whether the bot is connected to Slack.
// This is updated by the Bot's event handler. Using atomic for thread safety.
var botConnected int32

// SetConnected updates the bot's connection state.
func SetConnected(connected bool) {
	if connected {
		atomic.StoreInt32(&botConnected, 1)
	} else {
		atomic.StoreInt32(&botConnected, 0)
	}
}

// IsConnected returns whether the bot is currently connected to Slack.
func (b *Bot) IsConnected() bool {
	return atomic.LoadInt32(&botConnected) == 1
}
