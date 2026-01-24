//go:build dialog_integration
// +build dialog_integration

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// Integration Tests for Dialog Gateway
// =============================================================================
//
// Run with: go test -tags=dialog_integration ./cmd/bd/... -run Dialog -v
//

// mockDialogClientInt simulates a dialog client that auto-responds
type mockDialogClientInt struct {
	listener   net.Listener
	responses  map[string]DialogResponse // ID -> response
	received   []DialogRequest
	mu         sync.Mutex
	defaultSel string // default selection if not in responses map
}

func newMockDialogClientInt(t *testing.T) *mockDialogClientInt {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create mock client listener: %v", err)
	}
	return &mockDialogClientInt{
		listener:   listener,
		responses:  make(map[string]DialogResponse),
		defaultSel: "y",
	}
}

func (m *mockDialogClientInt) Addr() string {
	return m.listener.Addr().String()
}

func (m *mockDialogClientInt) Close() {
	m.listener.Close()
}

func (m *mockDialogClientInt) SetResponse(id string, resp DialogResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[id] = resp
}

func (m *mockDialogClientInt) GetReceived() []DialogRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]DialogRequest, len(m.received))
	copy(result, m.received)
	return result
}

func (m *mockDialogClientInt) Serve() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			return // listener closed
		}
		go m.handleConn(conn)
	}
}

func (m *mockDialogClientInt) handleConn(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}

		var req DialogRequest
		if err := json.Unmarshal(line, &req); err != nil {
			resp := DialogResponse{Error: fmt.Sprintf("invalid JSON: %v", err)}
			respJSON, _ := json.Marshal(resp)
			conn.Write(append(respJSON, '\n'))
			continue
		}

		m.mu.Lock()
		m.received = append(m.received, req)
		resp, ok := m.responses[req.ID]
		if !ok {
			// Auto-respond with first option or default
			resp = DialogResponse{ID: req.ID}
			if len(req.Options) > 0 {
				resp.Selected = req.Options[0].ID
			} else {
				resp.Selected = m.defaultSel
			}
		}
		m.mu.Unlock()

		resp.ID = req.ID
		respJSON, _ := json.Marshal(resp)
		conn.Write(append(respJSON, '\n'))
	}
}

func TestDialogIntegration_GatewayHealth(t *testing.T) {
	// Start mock dialog client
	mockClient := newMockDialogClientInt(t)
	defer mockClient.Close()
	go mockClient.Serve()

	// Create gateway
	gateway := &DialogGateway{
		dialogAddr: mockClient.Addr(),
		bdPath:     "bd",
		verbose:    false,
	}

	// Connect gateway to mock client
	if err := gateway.connect(); err != nil {
		t.Fatalf("Gateway failed to connect: %v", err)
	}
	defer gateway.close()

	// Test health endpoint
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	gateway.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var health map[string]interface{}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &health); err != nil {
		t.Fatalf("Failed to parse health response: %v", err)
	}

	if health["dialog_client"] != true {
		t.Errorf("Expected dialog_client=true, got %v", health["dialog_client"])
	}
	if health["status"] != "ok" && health["status"] != "degraded" {
		t.Errorf("Unexpected status: %v", health["status"])
	}
}

func TestDialogIntegration_GatewayDirectDialog(t *testing.T) {
	// Start mock dialog client
	mockClient := newMockDialogClientInt(t)
	defer mockClient.Close()
	go mockClient.Serve()

	// Set expected response
	mockClient.SetResponse("direct-test", DialogResponse{
		ID:       "direct-test",
		Selected: "confirmed",
	})

	// Create gateway
	gateway := &DialogGateway{
		dialogAddr: mockClient.Addr(),
		bdPath:     "bd",
		verbose:    false,
	}

	if err := gateway.connect(); err != nil {
		t.Fatalf("Gateway failed to connect: %v", err)
	}
	defer gateway.close()

	// Send dialog request via HTTP
	dialogReq := DialogRequest{
		ID:     "direct-test",
		Type:   "choice",
		Title:  "Test",
		Prompt: "Confirm action?",
		Options: []DialogOption{
			{ID: "confirmed", Label: "Yes"},
			{ID: "rejected", Label: "No"},
		},
	}
	reqBody, _ := json.Marshal(dialogReq)

	req := httptest.NewRequest(http.MethodPost, "/api/dialogs", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	gateway.handleDialog(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var dialogResp DialogResponse
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &dialogResp); err != nil {
		t.Fatalf("Failed to parse dialog response: %v", err)
	}

	if dialogResp.ID != "direct-test" {
		t.Errorf("Expected ID 'direct-test', got %q", dialogResp.ID)
	}
	if dialogResp.Selected != "confirmed" {
		t.Errorf("Expected Selected 'confirmed', got %q", dialogResp.Selected)
	}

	// Verify mock client received the request
	received := mockClient.GetReceived()
	if len(received) != 1 {
		t.Fatalf("Expected 1 received request, got %d", len(received))
	}
	if received[0].ID != "direct-test" {
		t.Errorf("Mock client received wrong ID: %q", received[0].ID)
	}
}

func TestDialogIntegration_GatewayWebhook(t *testing.T) {
	// Start mock dialog client
	mockClient := newMockDialogClientInt(t)
	defer mockClient.Close()
	go mockClient.Serve()

	// Set expected response
	mockClient.SetResponse("webhook-decision-1", DialogResponse{
		ID:       "webhook-decision-1",
		Selected: "approved",
	})

	// Create gateway with a mock bd path (we won't actually call it)
	gateway := &DialogGateway{
		dialogAddr: mockClient.Addr(),
		bdPath:     "/nonexistent/bd", // Will fail but we can check the dialog part worked
		verbose:    true,
	}

	if err := gateway.connect(); err != nil {
		t.Fatalf("Gateway failed to connect: %v", err)
	}
	defer gateway.close()

	// Send webhook
	payload := DecisionPayload{
		Type:   "decision_point",
		ID:     "webhook-decision-1",
		Prompt: "Approve this deployment?",
		Options: []DecisionPayloadOption{
			{ID: "approved", Label: "Yes, approve"},
			{ID: "rejected", Label: "No, reject"},
		},
		Default: "rejected",
	}
	reqBody, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/decisions/webhook", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	gateway.handleWebhook(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	// The webhook handler will try to call bd, which will fail with our mock path
	// But we can verify the dialog was shown correctly
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to parse webhook response: %v", err)
	}

	// Check that the dialog was shown and selection was made
	if result["decision_id"] != "webhook-decision-1" {
		t.Errorf("Expected decision_id 'webhook-decision-1', got %v", result["decision_id"])
	}
	if result["selected"] != "approved" {
		t.Errorf("Expected selected 'approved', got %v", result["selected"])
	}

	// Verify mock client received the request
	received := mockClient.GetReceived()
	if len(received) != 1 {
		t.Fatalf("Expected 1 received request, got %d", len(received))
	}
	if received[0].Prompt != "Approve this deployment?" {
		t.Errorf("Wrong prompt received: %q", received[0].Prompt)
	}
}

func TestDialogIntegration_GatewayDialogCancelled(t *testing.T) {
	// Start mock dialog client
	mockClient := newMockDialogClientInt(t)
	defer mockClient.Close()
	go mockClient.Serve()

	// Set cancelled response
	mockClient.SetResponse("cancel-test", DialogResponse{
		ID:        "cancel-test",
		Cancelled: true,
	})

	// Create gateway
	gateway := &DialogGateway{
		dialogAddr: mockClient.Addr(),
		bdPath:     "bd",
		verbose:    false,
	}

	if err := gateway.connect(); err != nil {
		t.Fatalf("Gateway failed to connect: %v", err)
	}
	defer gateway.close()

	// Send dialog request
	dialogReq := DialogRequest{
		ID:     "cancel-test",
		Type:   "confirm",
		Title:  "Test",
		Prompt: "Continue?",
	}
	reqBody, _ := json.Marshal(dialogReq)

	req := httptest.NewRequest(http.MethodPost, "/api/dialogs", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	gateway.handleDialog(w, req)

	resp := w.Result()
	var dialogResp DialogResponse
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &dialogResp)

	if !dialogResp.Cancelled {
		t.Error("Expected cancelled=true")
	}
}

func TestDialogIntegration_GatewayReconnect(t *testing.T) {
	// Start mock dialog client
	mockClient := newMockDialogClientInt(t)
	go mockClient.Serve()

	// Create gateway
	gateway := &DialogGateway{
		dialogAddr: mockClient.Addr(),
		bdPath:     "bd",
		verbose:    false,
	}

	// Connect
	if err := gateway.connect(); err != nil {
		t.Fatalf("Initial connect failed: %v", err)
	}

	// Verify initial connection works
	resp, err := gateway.send(&DialogRequest{
		ID:      "initial-test",
		Type:    "choice",
		Options: []DialogOption{{ID: "a", Label: "A"}},
	})
	if err != nil {
		t.Fatalf("Initial send failed: %v", err)
	}
	if resp.ID != "initial-test" {
		t.Errorf("Wrong initial response ID: %q", resp.ID)
	}

	// Close the client - this simulates the client going away
	mockClient.Close()

	// Give time for the close to propagate
	time.Sleep(50 * time.Millisecond)

	// Start new client (simulating restart)
	mockClient2 := newMockDialogClientInt(t)
	defer mockClient2.Close()
	go mockClient2.Serve()

	// Update gateway address and force reconnect
	gateway.mu.Lock()
	gateway.dialogAddr = mockClient2.Addr()
	gateway.connected = false
	if gateway.conn != nil {
		gateway.conn.Close()
	}
	gateway.conn = nil
	gateway.mu.Unlock()

	// Should reconnect
	if err := gateway.connect(); err != nil {
		t.Fatalf("Reconnect failed: %v", err)
	}

	// Send should work now
	resp, err = gateway.send(&DialogRequest{
		ID:      "reconnect-test",
		Type:    "choice",
		Options: []DialogOption{{ID: "a", Label: "A"}},
	})
	if err != nil {
		t.Fatalf("Send after reconnect failed: %v", err)
	}
	if resp.ID != "reconnect-test" {
		t.Errorf("Wrong response ID: %q", resp.ID)
	}
}

func TestDialogIntegration_GatewayMethodNotAllowed(t *testing.T) {
	gateway := &DialogGateway{}

	tests := []struct {
		handler func(http.ResponseWriter, *http.Request)
		method  string
	}{
		{gateway.handleWebhook, http.MethodGet},
		{gateway.handleDialog, http.MethodGet},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, "/test", nil)
		w := httptest.NewRecorder()
		tt.handler(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected 405 for %s, got %d", tt.method, w.Code)
		}
	}
}

func TestDialogIntegration_GatewayInvalidJSON(t *testing.T) {
	gateway := &DialogGateway{}

	req := httptest.NewRequest(http.MethodPost, "/api/dialogs", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	gateway.handleDialog(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", w.Code)
	}

	var errResp map[string]string
	json.Unmarshal(w.Body.Bytes(), &errResp)
	if !strings.Contains(errResp["error"], "Invalid JSON") {
		t.Errorf("Expected Invalid JSON error, got: %s", errResp["error"])
	}
}

func TestDialogIntegration_GatewayIndexPage(t *testing.T) {
	gateway := &DialogGateway{}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	gateway.handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Dialog Gateway") {
		t.Error("Expected 'Dialog Gateway' in response")
	}
	if !strings.Contains(body, "/api/health") {
		t.Error("Expected '/api/health' in response")
	}
}

func TestDialogIntegration_GatewayIndex404(t *testing.T) {
	gateway := &DialogGateway{}

	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	w := httptest.NewRecorder()
	gateway.handleIndex(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

func TestDialogIntegration_ClientServerFullFlow(t *testing.T) {
	// This test simulates the full flow:
	// 1. Client listens
	// 2. Server connects
	// 3. Server sends request
	// 4. Client responds
	// 5. Server receives response

	// Start a "client" listener (simulating bd dialog client)
	clientListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create client listener: %v", err)
	}
	defer clientListener.Close()

	var clientReceivedReq DialogRequest
	var wg sync.WaitGroup
	wg.Add(1)

	// Client goroutine
	go func() {
		defer wg.Done()
		conn, err := clientListener.Accept()
		if err != nil {
			t.Errorf("Client accept error: %v", err)
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Errorf("Client read error: %v", err)
			return
		}

		if err := json.Unmarshal(line, &clientReceivedReq); err != nil {
			t.Errorf("Client unmarshal error: %v", err)
			return
		}

		// Send response
		resp := DialogResponse{
			ID:       clientReceivedReq.ID,
			Selected: "option-b",
		}
		respJSON, _ := json.Marshal(resp)
		conn.Write(append(respJSON, '\n'))
	}()

	// Give client time to start listening
	time.Sleep(50 * time.Millisecond)

	// Server connects and sends request
	gateway := &DialogGateway{
		dialogAddr: clientListener.Addr().String(),
		bdPath:     "bd",
	}

	if err := gateway.connect(); err != nil {
		t.Fatalf("Gateway connect failed: %v", err)
	}
	defer gateway.close()

	resp, err := gateway.send(&DialogRequest{
		ID:     "full-flow-test",
		Type:   "choice",
		Title:  "Full Flow Test",
		Prompt: "Select an option",
		Options: []DialogOption{
			{ID: "option-a", Label: "Option A"},
			{ID: "option-b", Label: "Option B"},
		},
	})

	wg.Wait()

	if err != nil {
		t.Fatalf("Gateway send failed: %v", err)
	}

	// Verify client received the request
	if clientReceivedReq.ID != "full-flow-test" {
		t.Errorf("Client received wrong ID: %q", clientReceivedReq.ID)
	}
	if clientReceivedReq.Title != "Full Flow Test" {
		t.Errorf("Client received wrong title: %q", clientReceivedReq.Title)
	}
	if len(clientReceivedReq.Options) != 2 {
		t.Errorf("Client received wrong options count: %d", len(clientReceivedReq.Options))
	}

	// Verify server received the response
	if resp.ID != "full-flow-test" {
		t.Errorf("Server received wrong response ID: %q", resp.ID)
	}
	if resp.Selected != "option-b" {
		t.Errorf("Server received wrong selection: %q", resp.Selected)
	}
}

func TestDialogIntegration_MultipleDialogsSequential(t *testing.T) {
	mockClient := newMockDialogClientInt(t)
	defer mockClient.Close()
	go mockClient.Serve()

	// Set up different responses for different IDs
	mockClient.SetResponse("dialog-1", DialogResponse{ID: "dialog-1", Selected: "opt-a"})
	mockClient.SetResponse("dialog-2", DialogResponse{ID: "dialog-2", Selected: "opt-b"})
	mockClient.SetResponse("dialog-3", DialogResponse{ID: "dialog-3", Text: "custom text", Selected: ""})

	gateway := &DialogGateway{
		dialogAddr: mockClient.Addr(),
		bdPath:     "bd",
	}

	if err := gateway.connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer gateway.close()

	// Send multiple dialogs sequentially
	for i, tc := range []struct {
		id           string
		wantSelected string
		wantText     string
	}{
		{"dialog-1", "opt-a", ""},
		{"dialog-2", "opt-b", ""},
		{"dialog-3", "", "custom text"},
	} {
		resp, err := gateway.send(&DialogRequest{
			ID:      tc.id,
			Type:    "choice",
			Options: []DialogOption{{ID: "opt-a", Label: "A"}, {ID: "opt-b", Label: "B"}},
		})
		if err != nil {
			t.Fatalf("Dialog %d failed: %v", i+1, err)
		}
		if resp.Selected != tc.wantSelected {
			t.Errorf("Dialog %d: expected selected %q, got %q", i+1, tc.wantSelected, resp.Selected)
		}
		if resp.Text != tc.wantText {
			t.Errorf("Dialog %d: expected text %q, got %q", i+1, tc.wantText, resp.Text)
		}
	}

	// Verify all requests were received
	received := mockClient.GetReceived()
	if len(received) != 3 {
		t.Errorf("Expected 3 requests, got %d", len(received))
	}
}
