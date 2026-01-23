package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/webhook"
)

// webhookCmd is the parent command for webhook operations
var webhookCmd = &cobra.Command{
	Use:     "webhook",
	GroupID: "advanced",
	Short:   "Webhook server for receiving decision responses",
	Long: `The webhook server provides HTTP endpoints for receiving decision responses
from external systems (email reply parsers, web UIs, mobile apps, etc.).

The primary endpoint is:
  POST /api/decisions/{id}/respond

Request format:
  {
    "selected": "a",           // Option ID (optional if text provided)
    "text": "additional notes", // Custom text (optional if selected provided)
    "respondent": "user@example.com",
    "auth_token": "<signed-token>"
  }

Response format:
  {
    "success": true,
    "decision_id": "gt-abc123.decision-1",
    "selected": "a",
    "responded_at": "2026-01-21T15:30:00Z"
  }

Security:
- auth_token is an HMAC-signed token containing decision_id, expiry, and respondent
- Rate limit: 1 response per decision (idempotent)
- Respondent validation (strict or non-strict mode)`,
}

// webhookServeCmd runs the webhook server
var webhookServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the webhook server",
	Long: `Run the HTTP webhook server to receive decision responses.

The server listens for POST requests to /api/decisions/{id}/respond and
validates auth tokens, checks decision state, and records responses.

Environment variables:
  BEADS_WEBHOOK_SECRET  - HMAC secret for token validation (required)
  BEADS_WEBHOOK_STRICT  - If "true", strictly validate respondent (default: false)

Examples:
  # Start server on default port
  export BEADS_WEBHOOK_SECRET="your-secret-key"
  bd webhook serve

  # Start server on custom port
  bd webhook serve --port 8080

  # Start with strict respondent validation
  BEADS_WEBHOOK_STRICT=true bd webhook serve`,
	Run: runWebhookServe,
}

func init() {
	webhookServeCmd.Flags().IntP("port", "p", 9090, "Port to listen on")
	webhookServeCmd.Flags().String("addr", "", "Full address to listen on (overrides --port)")

	webhookCmd.AddCommand(webhookServeCmd)
	rootCmd.AddCommand(webhookCmd)
}

func runWebhookServe(cmd *cobra.Command, args []string) {
	// Get secret from environment
	secret := os.Getenv("BEADS_WEBHOOK_SECRET")
	if secret == "" {
		fmt.Fprintf(os.Stderr, "Error: BEADS_WEBHOOK_SECRET environment variable is required\n")
		os.Exit(1)
	}

	// Get strict mode from environment
	strictMode := os.Getenv("BEADS_WEBHOOK_STRICT") == "true"

	// Determine address
	addr, _ := cmd.Flags().GetString("addr")
	if addr == "" {
		port, _ := cmd.Flags().GetInt("port")
		addr = fmt.Sprintf(":%d", port)
	}

	// Ensure store is initialized
	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Create webhook server
	server := webhook.NewServer(webhook.ServerConfig{
		Store:      store,
		Secret:     []byte(secret),
		StrictMode: strictMode,
	})

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down webhook server...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Error during shutdown: %v\n", err)
		}
	}()

	// Start server
	fmt.Printf("Starting webhook server on %s\n", addr)
	fmt.Printf("  POST /api/decisions/{id}/respond - Receive decision responses\n")
	fmt.Printf("  GET /health - Health check endpoint\n")
	if strictMode {
		fmt.Printf("  Strict mode: enabled (respondent must match token)\n")
	}
	fmt.Println()

	if err := server.Start(addr); err != nil {
		// http.ErrServerClosed is expected on shutdown
		if err.Error() != "http: Server closed" {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("Webhook server stopped.")
}
