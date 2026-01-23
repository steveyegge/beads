package main

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDialogClient_CLI(t *testing.T) {
	// Start a mock server that sends a dialog request
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start test listener: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	// Channel to receive response
	responseCh := make(chan DialogResponse, 1)
	errCh := make(chan error, 1)

	// Server goroutine - sends request, receives response
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		// Send a choice dialog request
		req := DialogRequest{
			ID:     "test-1",
			Type:   "choice",
			Title:  "Test Dialog",
			Prompt: "Pick one",
			Options: []DialogOption{
				{ID: "a", Label: "Option A"},
				{ID: "b", Label: "Option B"},
			},
		}
		reqJSON, _ := json.Marshal(req)
		conn.Write(append(reqJSON, '\n'))

		// Read response
		reader := bufio.NewReader(conn)
		line, err := reader.ReadBytes('\n')
		if err != nil {
			errCh <- err
			return
		}

		var resp DialogResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			errCh <- err
			return
		}
		responseCh <- resp
	}()

	// Client goroutine - connects and responds
	go func() {
		time.Sleep(100 * time.Millisecond) // Let server start

		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		// Read request
		reader := bufio.NewReader(conn)
		line, err := reader.ReadBytes('\n')
		if err != nil {
			errCh <- err
			return
		}

		var req DialogRequest
		if err := json.Unmarshal(line, &req); err != nil {
			errCh <- err
			return
		}

		// Simulate CLI response - select first option
		resp := DialogResponse{
			ID:       req.ID,
			Selected: req.Options[0].ID,
		}
		respJSON, _ := json.Marshal(resp)
		conn.Write(append(respJSON, '\n'))
	}()

	// Wait for result
	select {
	case resp := <-responseCh:
		if resp.ID != "test-1" {
			t.Errorf("Expected ID 'test-1', got %q", resp.ID)
		}
		if resp.Selected != "a" {
			t.Errorf("Expected Selected 'a', got %q", resp.Selected)
		}
		if resp.Cancelled {
			t.Error("Expected not cancelled")
		}
	case err := <-errCh:
		t.Fatalf("Error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for response")
	}

	t.Logf("Dialog client test passed on port %d", port)
}

func TestDialogRequest_Types(t *testing.T) {
	tests := []struct {
		name    string
		reqType string
		options []DialogOption
	}{
		{
			name:    "entry",
			reqType: "entry",
			options: nil,
		},
		{
			name:    "choice",
			reqType: "choice",
			options: []DialogOption{
				{ID: "a", Label: "Option A"},
				{ID: "b", Label: "Option B"},
			},
		},
		{
			name:    "confirm",
			reqType: "confirm",
			options: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := DialogRequest{
				ID:      "test",
				Type:    tt.reqType,
				Title:   "Test",
				Prompt:  "Test prompt",
				Options: tt.options,
			}

			// Verify JSON round-trip
			data, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			var decoded DialogRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if decoded.Type != tt.reqType {
				t.Errorf("Type mismatch: got %q, want %q", decoded.Type, tt.reqType)
			}
		})
	}
}

func TestDialogResponse_Cancelled(t *testing.T) {
	resp := DialogResponse{
		ID:        "test-1",
		Cancelled: true,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	if !strings.Contains(string(data), `"cancelled":true`) {
		t.Errorf("Expected cancelled:true in JSON, got: %s", string(data))
	}
}

func TestShowDialogCLI_Choice(t *testing.T) {
	// Save and restore stdin reader
	oldReader := dialogStdinReader

	// Create mock input that selects option 1
	input := "1\n"
	dialogStdinReader = bufio.NewReader(strings.NewReader(input))
	defer func() { dialogStdinReader = oldReader }()

	req := DialogRequest{
		ID:     "test-cli",
		Type:   "choice",
		Title:  "Test",
		Prompt: "Pick one",
		Options: []DialogOption{
			{ID: "yes", Label: "Yes, proceed"},
			{ID: "no", Label: "No, cancel"},
		},
	}

	resp := showDialogCLI(req)

	if resp.ID != "test-cli" {
		t.Errorf("Expected ID 'test-cli', got %q", resp.ID)
	}
	if resp.Selected != "yes" {
		t.Errorf("Expected Selected 'yes', got %q", resp.Selected)
	}
	if resp.Cancelled {
		t.Error("Expected not cancelled")
	}
}

func TestShowDialogCLI_ChoiceByID(t *testing.T) {
	oldReader := dialogStdinReader
	input := "no\n"
	dialogStdinReader = bufio.NewReader(strings.NewReader(input))
	defer func() { dialogStdinReader = oldReader }()

	req := DialogRequest{
		ID:     "test-cli",
		Type:   "choice",
		Title:  "Test",
		Prompt: "Pick one",
		Options: []DialogOption{
			{ID: "yes", Label: "Yes"},
			{ID: "no", Label: "No"},
		},
	}

	resp := showDialogCLI(req)
	if resp.Selected != "no" {
		t.Errorf("Expected Selected 'no', got %q", resp.Selected)
	}
}

func TestShowDialogCLI_Cancel(t *testing.T) {
	oldReader := dialogStdinReader
	input := "0\n"
	dialogStdinReader = bufio.NewReader(strings.NewReader(input))
	defer func() { dialogStdinReader = oldReader }()

	req := DialogRequest{
		ID:      "test-cancel",
		Type:    "choice",
		Title:   "Test",
		Prompt:  "Pick one",
		Options: []DialogOption{{ID: "a", Label: "A"}},
	}

	resp := showDialogCLI(req)
	if !resp.Cancelled {
		t.Error("Expected cancelled")
	}
}

func TestShowDialogCLI_Entry(t *testing.T) {
	oldReader := dialogStdinReader
	input := "hello world\n"
	dialogStdinReader = bufio.NewReader(strings.NewReader(input))
	defer func() { dialogStdinReader = oldReader }()

	req := DialogRequest{
		ID:     "test-entry",
		Type:   "entry",
		Title:  "Test",
		Prompt: "Enter text",
	}

	resp := showDialogCLI(req)
	if resp.Text != "hello world" {
		t.Errorf("Expected Text 'hello world', got %q", resp.Text)
	}
}

func TestShowDialogCLI_Confirm(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		cancel   bool
	}{
		{"y\n", "Yes", false},
		{"yes\n", "Yes", false},
		{"n\n", "No", false},
		{"no\n", "No", false},
		{"c\n", "", true},
		{"cancel\n", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			oldReader := dialogStdinReader
			dialogStdinReader = bufio.NewReader(strings.NewReader(tt.input))
			defer func() { dialogStdinReader = oldReader }()

			req := DialogRequest{
				ID:     "test-confirm",
				Type:   "confirm",
				Title:  "Test",
				Prompt: "Confirm?",
			}

			resp := showDialogCLI(req)
			if tt.cancel {
				if !resp.Cancelled {
					t.Error("Expected cancelled")
				}
			} else {
				if resp.Selected != tt.expected {
					t.Errorf("Expected Selected %q, got %q", tt.expected, resp.Selected)
				}
			}
		})
	}
}

func TestDecisionPayload_JSON(t *testing.T) {
	timeout := time.Now().Add(24 * time.Hour)
	payload := DecisionPayload{
		Type:      "decision_point",
		ID:        "test-decision-1",
		Prompt:    "Approve deployment?",
		Options:   []DecisionPayloadOption{{ID: "y", Label: "Yes"}, {ID: "n", Label: "No"}},
		Default:   "n",
		TimeoutAt: &timeout,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded DecisionPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != payload.ID {
		t.Errorf("ID mismatch: got %q, want %q", decoded.ID, payload.ID)
	}
	if len(decoded.Options) != 2 {
		t.Errorf("Options count mismatch: got %d, want 2", len(decoded.Options))
	}
}
