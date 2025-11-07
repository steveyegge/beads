package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	uiapi "github.com/steveyegge/beads/internal/ui/api"
	"github.com/steveyegge/beads/internal/ui/search"
)

// uiDaemonSessionMetadata captures connection details for the active daemon-backed session.
type uiDaemonSessionMetadata struct {
	SocketPath         string `json:"socket_path"`
	EndpointNetwork    string `json:"endpoint_network,omitempty"`
	EndpointAddress    string `json:"endpoint_address,omitempty"`
	WorkspacePath      string `json:"workspace_path,omitempty"`
	DatabasePath       string `json:"database_path,omitempty"`
	AutoStartAttempted bool   `json:"auto_start_attempted"`
	AutoStartSucceeded bool   `json:"auto_start_succeeded"`
	ListenURL          string `json:"listen_url,omitempty"`
	PID                int    `json:"pid"`
	AuthTokenHash      string `json:"auth_token_hash,omitempty"`
}

// uiDaemonSession bundles the RPC clients required by the UI server.
type uiDaemonSession struct {
	ListClient    uiapi.ListClient
	CreateClient  uiapi.CreateClient
	DetailClient  uiapi.DetailClient
	UpdateClient  uiapi.UpdateClient
	LabelClient   uiapi.LabelClient
	DeleteClient  uiapi.DeleteClient
	BulkClient    uiapi.BulkClient
	EventSource   uiapi.EventSource
	SearchService *search.Service
	Metadata      uiDaemonSessionMetadata
}

type uiSessionManager struct {
	mu        sync.RWMutex
	metadata  uiDaemonSessionMetadata
	logWriter io.Writer
}

var globalUISessionManager = &uiSessionManager{}

// Open establishes (or reuses) a daemon-backed workspace session for bd ui.
func (m *uiSessionManager) Open(ctx context.Context, stderr io.Writer) (*uiDaemonSession, error) {
	if noDaemon {
		return nil, errors.New("bd ui requires the Beads daemon; remove --no-daemon/BEADS_NO_DAEMON or start the daemon manually")
	}

	if dbPath == "" {
		return nil, errors.New("bd ui requires an initialized database; run 'bd init' in this workspace first")
	}

	expectedDB, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}

	socketPath := getSocketPath()
	if strings.TrimSpace(socketPath) == "" {
		return nil, errors.New("could not determine daemon socket path")
	}

	network, address, endpointErr := rpc.DiscoverEndpoint(socketPath)
	if endpointErr != nil {
		writeStructuredLog(stderr, "warning", "ui.daemon.endpoint_unresolved", map[string]any{
			"socket_path": socketPath,
			"error":       endpointErr.Error(),
		})
	}

	status, autoAttempted, autoSucceeded, err := ensureDaemonReady(ctx, socketPath, expectedDB, stderr)
	if err != nil {
		return nil, err
	}

	perRequestClient := newPerRequestDaemonClient(socketPath, expectedDB, stderr)

	meta := uiDaemonSessionMetadata{
		SocketPath:         socketPath,
		AutoStartAttempted: autoAttempted,
		AutoStartSucceeded: autoSucceeded,
	}
	if endpointErr == nil {
		meta.EndpointNetwork = network
		meta.EndpointAddress = address
	}
	if status != nil {
		meta.WorkspacePath = status.WorkspacePath
		meta.DatabasePath = status.DatabasePath
		meta.PID = status.PID
	}

	session := &uiDaemonSession{
		ListClient:    perRequestClient,
		CreateClient:  perRequestClient,
		DetailClient:  perRequestClient,
		UpdateClient:  perRequestClient,
		LabelClient:   perRequestClient,
		DeleteClient:  perRequestClient,
		BulkClient:    perRequestClient,
		EventSource:   uiapi.NewDaemonEventSource(perRequestClient),
		SearchService: search.NewService(perRequestClient),
		Metadata:      meta,
	}

	writeStructuredLog(stderr, "info", "ui.daemon.session_ready", map[string]any{
		"socket_path":  meta.SocketPath,
		"workspace":    meta.WorkspacePath,
		"auto_started": meta.AutoStartSucceeded,
	})

	m.setMetadata(meta, stderr)
	return session, nil
}

// Metadata returns the last recorded session metadata.
func (m *uiSessionManager) Metadata() uiDaemonSessionMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.metadata
}

// BindListenURL stores the active listener URL once bd ui is serving traffic.
func (m *uiSessionManager) BindListenURL(url string) {
	m.mu.Lock()
	m.metadata.ListenURL = strings.TrimSpace(url)
	m.persistLocked()
	m.mu.Unlock()
}

func (m *uiSessionManager) setMetadata(meta uiDaemonSessionMetadata, log io.Writer) {
	m.mu.Lock()
	if log != nil {
		m.logWriter = log
	}
	if existing := m.metadata.AuthTokenHash; existing != "" && meta.AuthTokenHash == "" {
		meta.AuthTokenHash = existing
	}
	m.metadata = meta
	m.persistLocked()
	m.mu.Unlock()
}

func (m *uiSessionManager) persistLocked() {
	meta := m.metadata
	beadsDir := ""
	if strings.TrimSpace(meta.SocketPath) != "" {
		beadsDir = filepath.Dir(meta.SocketPath)
	} else if strings.TrimSpace(meta.DatabasePath) != "" {
		beadsDir = filepath.Dir(meta.DatabasePath)
	}
	if strings.TrimSpace(beadsDir) == "" {
		return
	}

	sessionPath := filepath.Join(beadsDir, "ui-session.json")
	payload := map[string]any{
		"socket_path":          meta.SocketPath,
		"workspace_path":       meta.WorkspacePath,
		"database_path":        meta.DatabasePath,
		"listen_url":           meta.ListenURL,
		"endpoint_network":     meta.EndpointNetwork,
		"endpoint_address":     meta.EndpointAddress,
		"auto_start_attempted": meta.AutoStartAttempted,
		"auto_start_succeeded": meta.AutoStartSucceeded,
		"pid":                  meta.PID,
		"updated_at":           time.Now().UTC().Format(time.RFC3339),
	}
	if meta.AuthTokenHash != "" {
		payload["auth_token_sha256"] = meta.AuthTokenHash
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		writeStructuredLog(m.logWriter, "error", "ui.session.serialize_failed", map[string]any{"error": err.Error()})
		return
	}

	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		writeStructuredLog(m.logWriter, "error", "ui.session.write_failed", map[string]any{"error": err.Error(), "path": beadsDir})
		return
	}

	tmpPath := sessionPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		writeStructuredLog(m.logWriter, "error", "ui.session.write_failed", map[string]any{"error": err.Error(), "path": tmpPath})
		return
	}
	if err := os.Rename(tmpPath, sessionPath); err != nil {
		_ = os.Remove(tmpPath)
		writeStructuredLog(m.logWriter, "error", "ui.session.write_failed", map[string]any{"error": err.Error(), "path": sessionPath})
		return
	}

	writeStructuredLog(m.logWriter, "info", "ui.session.persisted", map[string]any{"path": sessionPath})
}

func (m *uiSessionManager) SetAuthToken(token string) {
	digest := ""
	if trimmed := strings.TrimSpace(token); trimmed != "" {
		sum := sha256.Sum256([]byte(trimmed))
		digest = hex.EncodeToString(sum[:])
	}

	m.mu.Lock()
	m.metadata.AuthTokenHash = digest
	m.persistLocked()
	m.mu.Unlock()
}

func ensureDaemonReady(ctx context.Context, socketPath, expectedDB string, stderr io.Writer) (*rpc.StatusResponse, bool, bool, error) {
	writeStructuredLog(stderr, "info", "ui.daemon.connect_attempt", map[string]any{
		"socket_path": socketPath,
	})

	status, err := probeDaemon(socketPath, expectedDB)
	if err == nil {
		return status, false, false, nil
	}

	if ctx.Err() != nil {
		return nil, false, false, ctx.Err()
	}

	autoStartEnabled := shouldAutoStartDaemon()
	if !autoStartEnabled {
		writeStructuredLog(stderr, "error", "ui.daemon.unavailable", map[string]any{
			"socket_path": socketPath,
			"error":       err.Error(),
		})
		return nil, false, false, fmt.Errorf("beads daemon not running (socket: %s). Start it with 'bd daemon'", socketPath)
	}

	writeStructuredLog(stderr, "info", "ui.daemon.autostart_request", map[string]any{
		"socket_path": socketPath,
	})

	autoStartAttempted := true
	if !tryAutoStartDaemon(socketPath) {
		writeStructuredLog(stderr, "error", "ui.daemon.autostart_failed", map[string]any{
			"socket_path": socketPath,
		})
		return nil, autoStartAttempted, false, fmt.Errorf("failed to auto-start beads daemon for UI; run 'bd daemon --status' for details")
	}
	recordDaemonStartSuccess()

	status, err = probeDaemon(socketPath, expectedDB)
	if err != nil {
		writeStructuredLog(stderr, "error", "ui.daemon.unhealthy_after_autostart", map[string]any{
			"socket_path": socketPath,
			"error":       err.Error(),
		})
		return nil, autoStartAttempted, false, fmt.Errorf("beads daemon unhealthy after auto-start: %w", err)
	}

	writeStructuredLog(stderr, "info", "ui.daemon.autostart_succeeded", map[string]any{
		"socket_path": socketPath,
	})

	return status, autoStartAttempted, true, nil
}

func probeDaemon(socketPath, expectedDB string) (*rpc.StatusResponse, error) {
	client, err := rpc.TryConnectWithTimeout(socketPath, 3*time.Second)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, rpc.ErrDaemonUnavailable
	}
	defer client.Close()

	if expectedDB != "" {
		client.SetDatabasePath(expectedDB)
	}

	health, err := client.Health()
	if err != nil {
		return nil, err
	}
	if health.Status != statusHealthy {
		if strings.TrimSpace(health.Error) != "" {
			return nil, fmt.Errorf("daemon unhealthy: %s", health.Error)
		}
		return nil, fmt.Errorf("daemon unhealthy: status=%s", health.Status)
	}

	return client.Status()
}

func writeStructuredLog(w io.Writer, severity, event string, fields map[string]any) {
	if w == nil || w == io.Discard {
		return
	}
	entry := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"severity":  severity,
		"event":     event,
	}
	for k, v := range fields {
		entry[k] = v
	}
	data, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(w, "bd ui log error: %v\n", err)
		return
	}
	_, _ = fmt.Fprintln(w, string(data))
}

type perRequestDaemonClient struct {
	socketPath  string
	expectedDB  string
	dialTimeout time.Duration
	logWriter   io.Writer

	endpointMu  sync.Mutex
	endpointSig string
}

func newPerRequestDaemonClient(socketPath, expectedDB string, log io.Writer) *perRequestDaemonClient {
	if log == nil {
		log = io.Discard
	}
	return &perRequestDaemonClient{
		socketPath:  socketPath,
		expectedDB:  expectedDB,
		dialTimeout: 3 * time.Second,
		logWriter:   log,
	}
}

func (c *perRequestDaemonClient) getClient() (*rpc.Client, error) {
	timeout := c.dialTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	network, address, detectErr := rpc.DiscoverEndpoint(c.socketPath)
	if detectErr != nil {
		c.noteEndpointUnavailable()
		return nil, detectErr
	}
	c.noteEndpoint(network, address)

	client, err := rpc.TryConnectWithTimeout(c.socketPath, timeout)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, rpc.ErrDaemonUnavailable
	}
	if c.expectedDB != "" {
		client.SetDatabasePath(c.expectedDB)
	}
	return client, nil
}

func (c *perRequestDaemonClient) exec(fn func(*rpc.Client) (*rpc.Response, error)) (*rpc.Response, error) {
	client, err := c.getClient()
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return fn(client)
}

func (c *perRequestDaemonClient) List(args *rpc.ListArgs) (*rpc.Response, error) {
	return c.exec(func(client *rpc.Client) (*rpc.Response, error) {
		return client.List(args)
	})
}

func (c *perRequestDaemonClient) Show(args *rpc.ShowArgs) (*rpc.Response, error) {
	return c.exec(func(client *rpc.Client) (*rpc.Response, error) {
		return client.Show(args)
	})
}

func (c *perRequestDaemonClient) Create(args *rpc.CreateArgs) (*rpc.Response, error) {
	return c.exec(func(client *rpc.Client) (*rpc.Response, error) {
		return client.Create(args)
	})
}

func (c *perRequestDaemonClient) Update(args *rpc.UpdateArgs) (*rpc.Response, error) {
	return c.exec(func(client *rpc.Client) (*rpc.Response, error) {
		return client.Update(args)
	})
}

func (c *perRequestDaemonClient) Batch(args *rpc.BatchArgs) (*rpc.Response, error) {
	return c.exec(func(client *rpc.Client) (*rpc.Response, error) {
		return client.Batch(args)
	})
}

func (c *perRequestDaemonClient) AddLabel(args *rpc.LabelAddArgs) (*rpc.Response, error) {
	return c.exec(func(client *rpc.Client) (*rpc.Response, error) {
		return client.AddLabel(args)
	})
}

func (c *perRequestDaemonClient) RemoveLabel(args *rpc.LabelRemoveArgs) (*rpc.Response, error) {
	return c.exec(func(client *rpc.Client) (*rpc.Response, error) {
		return client.RemoveLabel(args)
	})
}

func (c *perRequestDaemonClient) DeleteIssue(ctx context.Context, id string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return errors.New("issue id is required")
	}

	resp, err := c.exec(func(client *rpc.Client) (*rpc.Response, error) {
		return client.DeleteIssue(&rpc.DeleteArgs{ID: trimmed})
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return errors.New("delete issue failed: empty response")
	}
	if !resp.Success {
		if resp.Error != "" {
			return errors.New(resp.Error)
		}
		return errors.New("delete issue failed")
	}
	return nil
}

func (c *perRequestDaemonClient) WatchEvents(ctx context.Context, args *rpc.WatchEventsArgs) (<-chan rpc.IssueEvent, func(), error) {
	client, err := c.getClient()
	if err != nil {
		return nil, nil, err
	}

	stream, cancel, err := client.WatchEvents(ctx, args)
	if err != nil {
		client.Close()
		return nil, nil, err
	}

	wrappedCancel := func() {
		if cancel != nil {
			cancel()
		}
		_ = client.Close()
	}

	return stream, wrappedCancel, nil
}

func (c *perRequestDaemonClient) noteEndpoint(network, address string) {
	signature := network + "|" + address

	c.endpointMu.Lock()
	prev := c.endpointSig
	if prev == signature {
		c.endpointMu.Unlock()
		return
	}
	c.endpointSig = signature
	c.endpointMu.Unlock()

	if prev == "" {
		writeStructuredLog(c.logWriter, "info", "ui.daemon.endpoint_detected", map[string]any{
			"network": network,
			"address": address,
		})
		return
	}

	writeStructuredLog(c.logWriter, "info", "ui.daemon.endpoint_changed", map[string]any{
		"previous": prev,
		"network":  network,
		"address":  address,
	})
}

func (c *perRequestDaemonClient) noteEndpointUnavailable() {
	c.endpointMu.Lock()
	wasSet := c.endpointSig != ""
	c.endpointSig = ""
	c.endpointMu.Unlock()

	if !wasSet {
		return
	}

	writeStructuredLog(c.logWriter, "warning", "ui.daemon.endpoint_unavailable", map[string]any{
		"socket_path": c.socketPath,
	})
}
