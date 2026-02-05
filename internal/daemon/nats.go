package daemon

import (
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
