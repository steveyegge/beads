//go:build windows

package rpc

import (
	"encoding/json"
	"os"
)

// DiscoverEndpoint resolves the daemon's current RPC endpoint from the metadata file.
func DiscoverEndpoint(socketPath string) (string, string, error) {
	if socketPath == "" {
		return "", "", ErrDaemonUnavailable
	}

	data, err := os.ReadFile(socketPath)
	if err != nil {
		return "", "", ErrDaemonUnavailable
	}

	var info endpointInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return "", "", ErrDaemonUnavailable
	}

	if info.Address == "" {
		return "", "", ErrDaemonUnavailable
	}

	network := info.Network
	if network == "" {
		network = "tcp"
	}

	return network, info.Address, nil
}
