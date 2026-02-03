//go:build !windows

package rpc

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/memory"
)

// generateTestCert generates a self-signed certificate for testing
func generateTestCert(t *testing.T, tmpDir string) (certFile, keyFile string) {
	t.Helper()

	// Generate private key
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate private key: %v", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	// Write certificate to file
	certFile = filepath.Join(tmpDir, "test.crt")
	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatalf("failed to create cert file: %v", err)
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certOut.Close()

	// Write key to file
	keyFile = filepath.Join(tmpDir, "test.key")
	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatalf("failed to create key file: %v", err)
	}
	pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	keyOut.Close()

	return certFile, keyFile
}

// TestTLSHandshakeWorks verifies TLS handshake completes successfully
func TestTLSHandshakeWorks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tls-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certFile, keyFile := generateTestCert(t, tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetTCPAddr("127.0.0.1:0")
	if err := server.SetTLSConfig(certFile, keyFile); err != nil {
		t.Fatalf("SetTLSConfig failed: %v", err)
	}

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

	tcpListener := server.TCPListener()
	if tcpListener == nil {
		t.Fatal("TCP listener should be active")
	}
	tcpAddr := tcpListener.Addr().String()

	// Connect with TLS
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // Accept self-signed cert
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", tcpAddr, tlsConfig)
	if err != nil {
		t.Fatalf("TLS dial failed: %v", err)
	}
	defer conn.Close()

	// Verify connection state
	state := conn.ConnectionState()
	if !state.HandshakeComplete {
		t.Error("TLS handshake not complete")
	}
	if state.Version < tls.VersionTLS12 {
		t.Errorf("TLS version %x is below TLS 1.2", state.Version)
	}

	// Send a request to verify the connection works
	req := Request{Operation: "health"}
	reqBytes, _ := json.Marshal(req)
	reqBytes = append(reqBytes, '\n')
	if _, err := conn.Write(reqBytes); err != nil {
		t.Fatalf("failed to write request: %v", err)
	}

	reader := bufio.NewReader(conn)
	respBytes, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	t.Log("TLS handshake successful, request/response verified")

	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

// TestInvalidCertRejected verifies invalid certificates are rejected
func TestInvalidCertRejected(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tls-invalid-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetTCPAddr("127.0.0.1:0")

	// Try to set invalid cert file
	err = server.SetTLSConfig("/nonexistent/cert.pem", "/nonexistent/key.pem")
	if err == nil {
		t.Error("SetTLSConfig should fail with nonexistent files")
	}

	// Try with invalid cert content
	invalidCert := filepath.Join(tmpDir, "invalid.crt")
	invalidKey := filepath.Join(tmpDir, "invalid.key")
	os.WriteFile(invalidCert, []byte("not a valid cert"), 0600)
	os.WriteFile(invalidKey, []byte("not a valid key"), 0600)

	err = server.SetTLSConfig(invalidCert, invalidKey)
	if err == nil {
		t.Error("SetTLSConfig should fail with invalid cert content")
	}
}

// TestPlainTCPConnectionToTLSServerFails verifies plain TCP connections are rejected by TLS server
func TestPlainTCPConnectionToTLSServerFails(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tls-plain-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	certFile, keyFile := generateTestCert(t, tmpDir)

	socketPath := filepath.Join(tmpDir, "bd.sock")
	store := memory.New("/tmp/test.jsonl")
	defer store.Close()

	server := NewServer(socketPath, store, tmpDir, filepath.Join(tmpDir, "beads.db"))
	server.SetTCPAddr("127.0.0.1:0")
	if err := server.SetTLSConfig(certFile, keyFile); err != nil {
		t.Fatalf("SetTLSConfig failed: %v", err)
	}

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

	tcpListener := server.TCPListener()
	if tcpListener == nil {
		t.Fatal("TCP listener should be active")
	}
	tcpAddr := tcpListener.Addr().String()

	// Try plain TCP connection (should fail or get garbage)
	conn, err := net.DialTimeout("tcp", tcpAddr, 2*time.Second)
	if err != nil {
		// Connection refused is acceptable
		t.Logf("Plain TCP connection rejected: %v", err)
	} else {
		defer conn.Close()

		// Try to send a plain request
		req := Request{Operation: "health"}
		reqBytes, _ := json.Marshal(req)
		reqBytes = append(reqBytes, '\n')
		if _, err := conn.Write(reqBytes); err != nil {
			t.Logf("Write to TLS server with plain TCP failed: %v", err)
		} else {
			// Read response - should be TLS handshake data, not valid JSON
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			reader := bufio.NewReader(conn)
			respBytes, err := reader.ReadBytes('\n')
			if err != nil {
				t.Logf("Read from TLS server with plain TCP failed (expected): %v", err)
			} else {
				var resp Response
				if json.Unmarshal(respBytes, &resp) == nil && resp.Success {
					t.Error("Plain TCP connection should not receive valid response from TLS server")
				}
			}
		}
	}

	t.Log("Plain TCP connection correctly rejected by TLS server")

	if err := server.Stop(); err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}
