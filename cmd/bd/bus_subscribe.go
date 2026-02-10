package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/daemon"
	"github.com/steveyegge/beads/internal/eventbus"
	"github.com/steveyegge/beads/internal/rpc"
)

var busSubscribeCmd = &cobra.Command{
	Use:   "subscribe",
	Short: "Subscribe to JetStream hook events (debug tool)",
	Long: `Subscribe to the HOOK_EVENTS JetStream stream and print events as they arrive.

This is a debugging/development tool for verifying that events flow through
JetStream correctly. Coop and other consumers use NATS directly, but this
command is useful for quick verification.

Examples:
  bd bus subscribe                                          # All events (local daemon)
  bd bus subscribe --filter=Stop                            # Only Stop events
  bd bus subscribe --nats-url=nats://remote:4222            # Remote NATS server
  bd bus subscribe --nats-url=wss://host.example.com/nats   # Via WebSocket
  bd bus subscribe --json                                   # Machine-readable output`,
	RunE: runBusSubscribe,
}

func runBusSubscribe(cmd *cobra.Command, args []string) error {
	filter, _ := cmd.Flags().GetString("filter")
	flagURL, _ := cmd.Flags().GetString("nats-url")
	flagToken, _ := cmd.Flags().GetString("nats-token")

	// Resolve NATS URL: flag > env > daemon query > localhost fallback.
	var natsURL string
	var natsToken string

	if flagURL != "" {
		natsURL = flagURL
	} else if envURL := os.Getenv("BD_NATS_URL"); envURL != "" {
		natsURL = envURL
	} else if daemonClient != nil {
		resp, err := daemonClient.Execute(rpc.OpBusStatus, nil)
		if err == nil && resp.Success {
			var result rpc.BusStatusResult
			if err := json.Unmarshal(resp.Data, &result); err == nil && result.NATSEnabled {
				natsURL = fmt.Sprintf("nats://127.0.0.1:%d", result.NATSPort)
			}
		}
	}

	if natsURL == "" {
		// Fallback to env vars.
		port := os.Getenv("BD_NATS_PORT")
		if port == "" {
			port = fmt.Sprintf("%d", daemon.DefaultNATSPort)
		}
		natsURL = fmt.Sprintf("nats://127.0.0.1:%s", port)
	}

	if flagToken != "" {
		natsToken = flagToken
	} else {
		natsToken = os.Getenv("BD_DAEMON_TOKEN")
	}

	// Connect to NATS.
	connectOpts := []nats.Option{
		nats.Name("bd-bus-subscribe"),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
	}
	if natsToken != "" {
		connectOpts = append(connectOpts, nats.Token(natsToken))
	}

	// For WebSocket URLs with a path (e.g., wss://host/nats), extract the path
	// into ProxyPath and connect to the host-only URL. The nats.go client
	// requires ProxyPath for WebSocket connections behind a reverse proxy.
	if strings.HasPrefix(natsURL, "ws://") || strings.HasPrefix(natsURL, "wss://") {
		if u, err := url.Parse(natsURL); err == nil && u.Path != "" && u.Path != "/" {
			connectOpts = append(connectOpts, nats.ProxyPath(u.Path))
			u.Path = ""
			natsURL = u.String()
		}
	}

	nc, err := nats.Connect(natsURL, connectOpts...)
	if err != nil {
		return fmt.Errorf("connect to NATS at %s: %w", natsURL, err)
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("JetStream context: %w", err)
	}

	// Determine subject filter.
	subject := eventbus.SubjectHookPrefix + ">"
	if filter != "" {
		subject = eventbus.SubjectForEvent(eventbus.EventType(filter))
	}

	// Subscribe with ephemeral consumer (no durable name = auto-cleanup).
	sub, err := js.Subscribe(subject, func(msg *nats.Msg) {
		meta, _ := msg.Metadata()
		if jsonOutput {
			entry := map[string]interface{}{
				"subject": msg.Subject,
				"data":    json.RawMessage(msg.Data),
			}
			if meta != nil {
				entry["seq"] = meta.Sequence.Stream
				entry["timestamp"] = meta.Timestamp.UTC().Format(time.RFC3339Nano)
			}
			out, _ := json.Marshal(entry)
			fmt.Println(string(out))
		} else {
			seq := uint64(0)
			ts := ""
			if meta != nil {
				seq = meta.Sequence.Stream
				ts = meta.Timestamp.UTC().Format("15:04:05.000")
			}
			fmt.Printf("[%s] seq=%d %s ", ts, seq, msg.Subject)
			// Try to extract a brief summary.
			var event struct {
				SessionID string `json:"session_id"`
				ToolName  string `json:"tool_name,omitempty"`
			}
			if json.Unmarshal(msg.Data, &event) == nil {
				if event.ToolName != "" {
					fmt.Printf("tool=%s ", event.ToolName)
				}
				if event.SessionID != "" {
					sid := event.SessionID
					if len(sid) > 12 {
						sid = sid[:12] + "..."
					}
					fmt.Printf("session=%s", sid)
				}
			}
			fmt.Println()
		}
		msg.Ack()
	}, nats.DeliverNew(), nats.AckExplicit())
	if err != nil {
		return fmt.Errorf("subscribe to %s: %w", subject, err)
	}
	defer sub.Unsubscribe()

	fmt.Fprintf(os.Stderr, "Subscribed to %s on %s (Ctrl-C to stop)\n", subject, natsURL)

	// Wait for interrupt.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Fprintln(os.Stderr, "\nUnsubscribed.")
	return nil
}

func init() {
	busSubscribeCmd.Flags().String("filter", "", "Filter by event type (e.g., Stop, PreToolUse, SessionStart)")
	busSubscribeCmd.Flags().String("nats-url", "", "NATS server URL (nats://, wss://) — overrides daemon auto-discovery")
	busSubscribeCmd.Flags().String("nats-token", "", "NATS auth token (default: BD_DAEMON_TOKEN env)")
	busCmd.AddCommand(busSubscribeCmd)
}
