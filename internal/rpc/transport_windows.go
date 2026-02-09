//go:build windows

package rpc

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

type endpointInfo struct {
	Network string `json:"network"`
	Address string `json:"address"`
}

func listenRPC(socketPath string) (net.Listener, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	info := endpointInfo{
		Network: "tcp",
		Address: listener.Addr().String(),
	}

	data, err := json.Marshal(info)
	if err != nil {
		listener.Close()
		return nil, err
	}

	if err := os.WriteFile(socketPath, data, 0o600); err != nil {
		listener.Close()
		return nil, err
	}

	return listener, nil
}

func validateEndpoint(info endpointInfo) error {
	if info.Network != "" && info.Network != "tcp" {
		return fmt.Errorf("invalid RPC endpoint: network must be tcp, got %q", info.Network)
	}
	if info.Address == "" {
		return fmt.Errorf("invalid RPC endpoint: missing address")
	}
	if !strings.HasPrefix(info.Address, "127.0.0.1:") && !strings.HasPrefix(info.Address, "localhost:") {
		return fmt.Errorf("invalid RPC endpoint: address must bind to localhost, got %q", info.Address)
	}
	return nil
}

func dialRPC(socketPath string, timeout time.Duration) (net.Conn, error) {
	data, err := os.ReadFile(socketPath)
	if err != nil {
		return nil, err
	}

	var info endpointInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}

	if err := validateEndpoint(info); err != nil {
		return nil, err
	}

	return net.DialTimeout("tcp", info.Address, timeout)
}

func endpointExists(socketPath string) bool {
	_, err := os.Stat(socketPath)
	return err == nil
}
