//go:build !windows

package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/memory"
)

// TestHTTPServerHealth tests the /health endpoint
func TestHTTPServerHealth(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "http-health-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetHTTPAddr("127.0.0.1:0")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(ctx)
	}()

	select {
	case <-server.WaitReady():
		// Server is ready
	case err := <-errChan:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to start")
	}

	// Wait a bit for HTTP server to be ready
	time.Sleep(100 * time.Millisecond)

	httpServer := server.HTTPServer()
	if httpServer == nil {
		t.Fatal("HTTP server should be active")
	}
	httpAddr := httpServer.Addr()

	// Test /health endpoint
	resp, err := http.Get("http://" + httpAddr + "/health")
	if err != nil {
		t.Fatalf("failed to GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if health["status"] != "healthy" {
		t.Errorf("expected status 'healthy', got %v", health["status"])
	}

	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

// TestHTTPServerReadiness tests the /readyz endpoint
func TestHTTPServerReadiness(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "http-readyz-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetHTTPAddr("127.0.0.1:0")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(ctx)
	}()

	select {
	case <-server.WaitReady():
	case err := <-errChan:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to start")
	}

	time.Sleep(100 * time.Millisecond)

	httpServer := server.HTTPServer()
	if httpServer == nil {
		t.Fatal("HTTP server should be active")
	}
	httpAddr := httpServer.Addr()

	resp, err := http.Get("http://" + httpAddr + "/readyz")
	if err != nil {
		t.Fatalf("failed to GET /readyz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var ready map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&ready); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if ready["status"] != "ready" {
		t.Errorf("expected status 'ready', got %v", ready["status"])
	}

	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

// TestHTTPServerRPCEndpoint tests the Connect-RPC style endpoint
func TestHTTPServerRPCEndpoint(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "http-rpc-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetHTTPAddr("127.0.0.1:0")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(ctx)
	}()

	select {
	case <-server.WaitReady():
	case err := <-errChan:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to start")
	}

	time.Sleep(100 * time.Millisecond)

	httpServer := server.HTTPServer()
	if httpServer == nil {
		t.Fatal("HTTP server should be active")
	}
	httpAddr := httpServer.Addr()

	t.Run("list_issues_via_http", func(t *testing.T) {
		// POST to Connect-RPC style endpoint
		body := bytes.NewBufferString(`{"status":"open"}`)
		resp, err := http.Post("http://"+httpAddr+"/bd.v1.BeadsService/List", "application/json", body)
		if err != nil {
			t.Fatalf("failed to POST: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Errorf("expected status 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
	})

	t.Run("health_via_http", func(t *testing.T) {
		body := bytes.NewBufferString(`{}`)
		resp, err := http.Post("http://"+httpAddr+"/bd.v1.BeadsService/Health", "application/json", body)
		if err != nil {
			t.Fatalf("failed to POST: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Errorf("expected status 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
	})

	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

// TestHTTPServerAuth tests Bearer token authentication
func TestHTTPServerAuth(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "http-auth-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetHTTPAddr("127.0.0.1:0")
	server.SetTCPToken("secret-token-123") // This token is used for HTTP auth too

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start(ctx)
	}()

	select {
	case <-server.WaitReady():
	case err := <-errChan:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to start")
	}

	time.Sleep(100 * time.Millisecond)

	httpServer := server.HTTPServer()
	if httpServer == nil {
		t.Fatal("HTTP server should be active")
	}
	httpAddr := httpServer.Addr()

	t.Run("request_without_token_fails", func(t *testing.T) {
		body := bytes.NewBufferString(`{}`)
		resp, err := http.Post("http://"+httpAddr+"/bd.v1.BeadsService/List", "application/json", body)
		if err != nil {
			t.Fatalf("failed to POST: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", resp.StatusCode)
		}
	})

	t.Run("request_with_wrong_token_fails", func(t *testing.T) {
		body := bytes.NewBufferString(`{}`)
		req, _ := http.NewRequest("POST", "http://"+httpAddr+"/bd.v1.BeadsService/List", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer wrong-token")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("failed to POST: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", resp.StatusCode)
		}
	})

	t.Run("request_with_valid_token_succeeds", func(t *testing.T) {
		body := bytes.NewBufferString(`{}`)
		req, _ := http.NewRequest("POST", "http://"+httpAddr+"/bd.v1.BeadsService/List", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer secret-token-123")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("failed to POST: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Errorf("expected status 200, got %d: %s", resp.StatusCode, string(bodyBytes))
		}
	})

	t.Run("health_endpoint_no_auth_required", func(t *testing.T) {
		// Health endpoints should work without auth
		resp, err := http.Get("http://" + httpAddr + "/health")
		if err != nil {
			t.Fatalf("failed to GET: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
	})

	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

// TestHTTPMethodMapping tests that all operations are mapped correctly
func TestHTTPMethodMapping(t *testing.T) {
	testCases := []struct {
		method   string
		expected string
	}{
		{"List", OpList},
		{"Show", OpShow},
		{"Create", OpCreate},
		{"Update", OpUpdate},
		{"Delete", OpDelete},
		{"Health", OpHealth},
		{"Ready", OpReady},
		{"Unknown", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.method, func(t *testing.T) {
			got := httpMethodToOperation(tc.method)
			if got != tc.expected {
				t.Errorf("httpMethodToOperation(%q) = %q, want %q", tc.method, got, tc.expected)
			}
		})
	}
}
