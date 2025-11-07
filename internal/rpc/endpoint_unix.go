//go:build !windows

package rpc

import "os"

// DiscoverEndpoint resolves the RPC network/address currently advertised by the daemon.
func DiscoverEndpoint(socketPath string) (string, string, error) {
	if socketPath == "" {
		return "", "", ErrDaemonUnavailable
	}
	if _, err := os.Stat(socketPath); err != nil {
		return "", "", ErrDaemonUnavailable
	}
	return "unix", socketPath, nil
}
