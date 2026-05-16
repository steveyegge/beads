package rpc

import (
	"fmt"
	"net"
	"net/rpc"
	"time"

	"github.com/steveyegge/beads/internal/storage"
)

const dialTimeout = 500 * time.Millisecond

// Dial connects to the bdd daemon over the Unix socket at sockPath and returns
// a storage.Storage backed by the daemon RPC. The returned value also satisfies
// storage.StoreLocator. Returns an error if the dial fails or times out.
func Dial(sockPath string) (storage.Storage, error) {
	conn, err := net.DialTimeout("unix", sockPath, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("bdd: dial %s: %w", sockPath, err)
	}
	return &daemonClient{client: rpc.NewClient(conn)}, nil
}
