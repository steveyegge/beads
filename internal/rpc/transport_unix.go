//go:build !windows

package rpc

import (
	"net"
	"os"
	"time"
)

func listenRPC(socketPath string) (net.Listener, error) {
	return net.Listen("unix", socketPath)
}

// listenTCP creates a TCP listener on the given address.
// Used for remote connections to the daemon (e.g., from Kubernetes).
func listenTCP(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

func dialRPC(socketPath string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", socketPath, timeout)
}

// dialTCP creates a TCP connection to a remote daemon.
// Used by clients connecting to daemons running in Kubernetes.
func dialTCP(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("tcp", addr, timeout)
}

func endpointExists(socketPath string) bool {
	_, err := os.Stat(socketPath)
	return err == nil
}
