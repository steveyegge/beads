package rpc

import (
	"crypto/tls"
	"fmt"
)

// SetTLSConfig configures TLS for TCP connections.
// certFile and keyFile are paths to the TLS certificate and private key.
// Must be called before Start() if TLS is desired.
func (s *Server) SetTLSConfig(certFile, keyFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("failed to load TLS certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12, // Require TLS 1.2 minimum
	}

	s.mu.Lock()
	s.tlsConfig = tlsConfig
	s.mu.Unlock()

	return nil
}

// TLSConfig returns the configured TLS config, or nil if not set.
func (s *Server) TLSConfig() *tls.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tlsConfig
}
