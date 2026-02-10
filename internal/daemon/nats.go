package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

const (
	// DefaultNATSPort is the default TCP port for the embedded NATS server.
	DefaultNATSPort = 4222

	// DefaultNATSMaxMem is the default JetStream memory limit (256 MiB).
	DefaultNATSMaxMem = 256 << 20

	// DefaultNATSMaxStore is the default JetStream file storage limit (1 GiB).
	DefaultNATSMaxStore = 1 << 30
)

// NATSServer wraps an embedded NATS server with JetStream and provides
// lifecycle management (start, stop, health check).
type NATSServer struct {
	server   *server.Server
	conn     *nats.Conn // in-process connection for daemon's own handlers
	storeDir string
	port     int
}

// NATSConfig holds configuration for the embedded NATS server.
type NATSConfig struct {
	Port     int    // TCP port for external connections (default: 4222)
	StoreDir string // JetStream file storage directory
	Token    string // Auth token for client connections (reuse BD_DAEMON_TOKEN)
}

// NATSConfigFromEnv builds NATSConfig from environment variables and defaults.
func NATSConfigFromEnv(runtimeDir string) NATSConfig {
	cfg := NATSConfig{
		Port:     DefaultNATSPort,
		StoreDir: filepath.Join(runtimeDir, "nats"),
		Token:    os.Getenv("BD_DAEMON_TOKEN"),
	}

	if portStr := os.Getenv("BD_NATS_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			cfg.Port = p
		}
	}

	if dir := os.Getenv("BD_NATS_STORE_DIR"); dir != "" {
		cfg.StoreDir = dir
	}

	return cfg
}

// StartNATSServer creates and starts an embedded NATS server with JetStream.
// The server listens on the configured TCP port for external bd client connections
// and provides an in-process connection for the daemon's own handlers.
func StartNATSServer(cfg NATSConfig) (*NATSServer, error) {
	if err := os.MkdirAll(cfg.StoreDir, 0700); err != nil {
		return nil, fmt.Errorf("create NATS store dir: %w", err)
	}

	opts := &server.Options{
		ServerName:         "bd-daemon",
		Host:               "0.0.0.0",
		Port:               cfg.Port,
		JetStream:          true,
		JetStreamMaxMemory: DefaultNATSMaxMem,
		JetStreamMaxStore:  DefaultNATSMaxStore,
		StoreDir:           cfg.StoreDir,
		NoLog:              true,
		NoSigs:             true,
	}

	if cfg.Token != "" {
		opts.Authorization = cfg.Token
	}

	ns, err := server.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("create NATS server: %w", err)
	}

	go ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		ns.Shutdown()
		return nil, fmt.Errorf("NATS server failed to become ready within 10 seconds")
	}

	// Create in-process connection for daemon's own handler dispatch.
	connectURL := fmt.Sprintf("nats://127.0.0.1:%d", cfg.Port)
	connectOpts := []nats.Option{
		nats.Name("bd-daemon-internal"),
	}
	if cfg.Token != "" {
		connectOpts = append(connectOpts, nats.Token(cfg.Token))
	}

	nc, err := nats.Connect(connectURL, connectOpts...)
	if err != nil {
		ns.Shutdown()
		return nil, fmt.Errorf("in-process NATS connection: %w", err)
	}

	return &NATSServer{
		server:   ns,
		conn:     nc,
		storeDir: cfg.StoreDir,
		port:     cfg.Port,
	}, nil
}

// Conn returns the in-process NATS connection for the daemon's own use.
func (n *NATSServer) Conn() *nats.Conn {
	return n.conn
}

// Port returns the TCP port the NATS server is listening on.
func (n *NATSServer) Port() int {
	return n.port
}

// Shutdown gracefully stops the NATS server. Drains the in-process
// connection first, then shuts down the server and waits for completion.
func (n *NATSServer) Shutdown() {
	if n.conn != nil {
		n.conn.Drain()
		n.conn.Close()
	}
	if n.server != nil {
		n.server.Shutdown()
		n.server.WaitForShutdown()
	}
}

// Health returns a NATSHealth snapshot of the server's current state.
func (n *NATSServer) Health() NATSHealth {
	h := NATSHealth{
		Port: n.port,
	}

	if n.server == nil {
		h.Status = "stopped"
		return h
	}

	varz, err := n.server.Varz(nil)
	if err != nil {
		h.Status = "error"
		h.Error = err.Error()
		return h
	}

	h.Status = "running"
	h.Connections = int(varz.Connections)
	h.InMsgs = varz.InMsgs
	h.OutMsgs = varz.OutMsgs
	h.Uptime = varz.Now.Sub(varz.Start).String()

	jsz, err := n.server.Jsz(nil)
	if err == nil && jsz != nil {
		h.JetStream = true
		h.Streams = int(jsz.Streams)
		h.Consumers = int(jsz.Consumers)
		h.Messages = jsz.Messages
	}

	return h
}

// NATSConnectionInfo is written to .runtime/nats-info.json for sidecar discovery.
// External processes (e.g., Coop) read this file to find the NATS server.
type NATSConnectionInfo struct {
	URL       string `json:"url"`                 // e.g., "nats://127.0.0.1:4222"
	Port      int    `json:"port"`                // TCP port
	Token     string `json:"token,omitempty"`     // Auth token (if set)
	JetStream bool   `json:"jetstream"`           // Always true for bd daemon
	Stream    string `json:"stream"`              // Primary stream name
	Subjects  string `json:"subjects"`            // Subject wildcard pattern
}

// ConnectionInfoPath returns the path where nats-info.json is written.
const ConnectionInfoFile = "nats-info.json"

// WriteConnectionInfo writes NATS connection details to a JSON file in the
// runtime directory so sidecar processes can discover the server.
func (n *NATSServer) WriteConnectionInfo(token string) error {
	info := NATSConnectionInfo{
		URL:       fmt.Sprintf("nats://127.0.0.1:%d", n.port),
		Port:      n.port,
		Token:     token,
		JetStream: true,
		Stream:    "HOOK_EVENTS",
		Subjects:  "hooks.>",
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal connection info: %w", err)
	}
	infoPath := filepath.Join(n.storeDir, "..", ConnectionInfoFile)
	if err := os.WriteFile(infoPath, data, 0600); err != nil {
		return fmt.Errorf("write %s: %w", infoPath, err)
	}
	return nil
}

// RemoveConnectionInfo removes the nats-info.json file (called on shutdown).
func (n *NATSServer) RemoveConnectionInfo() {
	infoPath := filepath.Join(n.storeDir, "..", ConnectionInfoFile)
	os.Remove(infoPath)
}

// ExternalNATSConn wraps a client-only connection to a standalone NATS server.
// Used when BD_NATS_URL is set (standalone NATS mode).
type ExternalNATSConn struct {
	conn *nats.Conn
	url  string
}

// ConnectExternalNATS establishes a client connection to a standalone NATS
// server at the given URL. Used instead of StartNATSServer when BD_NATS_URL
// is set. The token is used for auth if non-empty.
func ConnectExternalNATS(natsURL, token string) (*ExternalNATSConn, error) {
	opts := []nats.Option{
		nats.Name("bd-daemon"),
		nats.MaxReconnects(-1),             // Reconnect forever
		nats.ReconnectWait(2 * time.Second),
	}
	if token != "" {
		opts = append(opts, nats.Token(token))
	}

	nc, err := nats.Connect(natsURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to external NATS at %s: %w", natsURL, err)
	}

	return &ExternalNATSConn{conn: nc, url: natsURL}, nil
}

// Conn returns the NATS connection.
func (e *ExternalNATSConn) Conn() *nats.Conn {
	return e.conn
}

// URL returns the NATS server URL.
func (e *ExternalNATSConn) URL() string {
	return e.url
}

// Close drains and closes the connection.
func (e *ExternalNATSConn) Close() {
	if e.conn != nil {
		e.conn.Drain()
		e.conn.Close()
	}
}

// NATSHealth represents a point-in-time health snapshot of the NATS server.
type NATSHealth struct {
	Status      string `json:"status"`                 // "running", "stopped", "error"
	Port        int    `json:"port"`
	Connections int    `json:"connections"`
	InMsgs      int64  `json:"in_msgs"`
	OutMsgs     int64  `json:"out_msgs"`
	Uptime      string `json:"uptime,omitempty"`
	JetStream   bool   `json:"jetstream"`
	Streams     int    `json:"streams,omitempty"`
	Consumers   int    `json:"consumers,omitempty"`
	Messages    uint64 `json:"messages,omitempty"`
	Error       string `json:"error,omitempty"`
}
