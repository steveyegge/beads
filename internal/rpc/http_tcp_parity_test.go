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

// TestHTTPTCPParity verifies that HTTP and TCP produce identical results
func TestHTTPTCPParity(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "http-tcp-parity-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetTCPAddr("127.0.0.1:0")
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

	// Get addresses
	tcpListener := server.TCPListener()
	if tcpListener == nil {
		t.Fatal("TCP listener should be active")
	}
	tcpAddr := tcpListener.Addr().String()

	httpServer := server.HTTPServer()
	if httpServer == nil {
		t.Fatal("HTTP server should be active")
	}
	httpAddr := httpServer.Addr()

	t.Run("health_parity", func(t *testing.T) {
		// Get health via TCP
		tcpClient, err := TryConnectTCP(tcpAddr, "")
		if err != nil {
			t.Fatalf("failed to connect via TCP: %v", err)
		}
		defer tcpClient.Close()

		tcpHealth, err := tcpClient.Health()
		if err != nil {
			t.Fatalf("TCP health failed: %v", err)
		}

		// Get health via HTTP
		httpResp, err := http.Get("http://" + httpAddr + "/health")
		if err != nil {
			t.Fatalf("HTTP health failed: %v", err)
		}
		defer httpResp.Body.Close()

		var httpHealth map[string]interface{}
		if err := json.NewDecoder(httpResp.Body).Decode(&httpHealth); err != nil {
			t.Fatalf("failed to decode HTTP response: %v", err)
		}

		// Compare status
		if tcpHealth.Status != httpHealth["status"] {
			t.Errorf("status mismatch: TCP=%s, HTTP=%v", tcpHealth.Status, httpHealth["status"])
		}
	})

	t.Run("list_parity", func(t *testing.T) {
		// Create an issue first via TCP
		tcpClient, err := TryConnectTCP(tcpAddr, "")
		if err != nil {
			t.Fatalf("failed to connect via TCP: %v", err)
		}
		defer tcpClient.Close()

		createResp, err := tcpClient.Create(&CreateArgs{
			Title:     "Test Issue for Parity",
			IssueType: "task",
			Priority:  1,
		})
		if err != nil {
			t.Fatalf("TCP create failed: %v", err)
		}
		if !createResp.Success {
			t.Fatalf("TCP create not successful: %s", createResp.Error)
		}

		// List via TCP
		tcpListResp, err := tcpClient.List(&ListArgs{Status: "open"})
		if err != nil {
			t.Fatalf("TCP list failed: %v", err)
		}

		var tcpIssues []map[string]interface{}
		if err := json.Unmarshal(tcpListResp.Data, &tcpIssues); err != nil {
			t.Fatalf("failed to unmarshal TCP list response: %v", err)
		}

		// List via HTTP
		body := bytes.NewBufferString(`{"status":"open"}`)
		httpResp, err := http.Post("http://"+httpAddr+"/bd.v1.BeadsService/List", "application/json", body)
		if err != nil {
			t.Fatalf("HTTP list failed: %v", err)
		}
		defer httpResp.Body.Close()

		httpBody, _ := io.ReadAll(httpResp.Body)
		var httpIssues []map[string]interface{}
		if err := json.Unmarshal(httpBody, &httpIssues); err != nil {
			t.Fatalf("failed to unmarshal HTTP list response: %v, body: %s", err, string(httpBody))
		}

		// Compare counts
		if len(tcpIssues) != len(httpIssues) {
			t.Errorf("issue count mismatch: TCP=%d, HTTP=%d", len(tcpIssues), len(httpIssues))
		}

		// Compare first issue title
		if len(tcpIssues) > 0 && len(httpIssues) > 0 {
			tcpTitle, _ := tcpIssues[0]["title"].(string)
			httpTitle, _ := httpIssues[0]["title"].(string)
			if tcpTitle != httpTitle {
				t.Errorf("title mismatch: TCP=%s, HTTP=%s", tcpTitle, httpTitle)
			}
		}
	})

	t.Run("create_via_http_visible_in_tcp", func(t *testing.T) {
		// Create via HTTP
		createBody := bytes.NewBufferString(`{"title":"HTTP Created Issue","issue_type":"bug","priority":2}`)
		httpResp, err := http.Post("http://"+httpAddr+"/bd.v1.BeadsService/Create", "application/json", createBody)
		if err != nil {
			t.Fatalf("HTTP create failed: %v", err)
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(httpResp.Body)
			t.Fatalf("HTTP create failed with status %d: %s", httpResp.StatusCode, string(bodyBytes))
		}

		var httpCreateResult map[string]interface{}
		httpBodyBytes, _ := io.ReadAll(httpResp.Body)
		if err := json.Unmarshal(httpBodyBytes, &httpCreateResult); err != nil {
			t.Fatalf("failed to unmarshal HTTP create response: %v, body: %s", err, string(httpBodyBytes))
		}

		// Verify via TCP
		tcpClient, err := TryConnectTCP(tcpAddr, "")
		if err != nil {
			t.Fatalf("failed to connect via TCP: %v", err)
		}
		defer tcpClient.Close()

		tcpListResp, err := tcpClient.List(&ListArgs{IssueType: "bug"})
		if err != nil {
			t.Fatalf("TCP list failed: %v", err)
		}

		var tcpIssues []map[string]interface{}
		if err := json.Unmarshal(tcpListResp.Data, &tcpIssues); err != nil {
			t.Fatalf("failed to unmarshal TCP list response: %v", err)
		}

		// Should find the HTTP-created issue
		found := false
		for _, issue := range tcpIssues {
			if title, _ := issue["title"].(string); title == "HTTP Created Issue" {
				found = true
				break
			}
		}
		if !found {
			t.Error("HTTP-created issue not visible via TCP")
		}
	})

	t.Run("metrics_parity", func(t *testing.T) {
		// Get metrics via TCP
		tcpClient, err := TryConnectTCP(tcpAddr, "")
		if err != nil {
			t.Fatalf("failed to connect via TCP: %v", err)
		}
		defer tcpClient.Close()

		tcpMetrics, err := tcpClient.Metrics()
		if err != nil {
			t.Fatalf("TCP metrics failed: %v", err)
		}

		// Get metrics via HTTP
		httpResp, err := http.Get("http://" + httpAddr + "/metrics")
		if err != nil {
			t.Fatalf("HTTP metrics failed: %v", err)
		}
		defer httpResp.Body.Close()

		var httpMetrics MetricsSnapshot
		if err := json.NewDecoder(httpResp.Body).Decode(&httpMetrics); err != nil {
			t.Fatalf("failed to decode HTTP metrics: %v", err)
		}

		// Compare uptime (should be similar, within a few seconds)
		if abs(tcpMetrics.UptimeSeconds-httpMetrics.UptimeSeconds) > 5 {
			t.Errorf("uptime mismatch: TCP=%.1fs, HTTP=%.1fs", tcpMetrics.UptimeSeconds, httpMetrics.UptimeSeconds)
		}
	})

	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// TestHTTPAndTCPConcurrent tests concurrent access via both protocols
func TestHTTPAndTCPConcurrent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "http-tcp-concurrent-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetTCPAddr("127.0.0.1:0")
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

	tcpAddr := server.TCPListener().Addr().String()
	httpAddr := server.HTTPServer().Addr()

	// Run concurrent requests
	done := make(chan struct{})
	errors := make(chan error, 100)

	// TCP workers
	for i := 0; i < 5; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 10; j++ {
				client, err := TryConnectTCP(tcpAddr, "")
				if err != nil {
					errors <- err
					continue
				}
				_, err = client.Health()
				if err != nil {
					errors <- err
				}
				client.Close()
			}
		}(i)
	}

	// HTTP workers
	for i := 0; i < 5; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			httpClient := &http.Client{Timeout: 5 * time.Second}
			for j := 0; j < 10; j++ {
				resp, err := httpClient.Get("http://" + httpAddr + "/health")
				if err != nil {
					errors <- err
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					errors <- err
				}
			}
		}(i)
	}

	// Wait for all workers
	for i := 0; i < 10; i++ {
		<-done
	}
	close(errors)

	// Check for errors
	var errCount int
	for err := range errors {
		if err != nil {
			errCount++
			t.Logf("concurrent error: %v", err)
		}
	}

	if errCount > 5 {
		t.Errorf("too many errors in concurrent test: %d", errCount)
	}

	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}
