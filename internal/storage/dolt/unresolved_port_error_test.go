package dolt

import (
	"strings"
	"testing"
)

// TestUnresolvedServerPortError pins the diagnosis for a TCP open that never
// had a port: it is a missing endpoint, not an unreachable server, and its
// advice must not be `bd dolt start`, which in a workspace served by an
// external manager initializes an empty phantom database instead of
// reconnecting to the real one.
func TestUnresolvedServerPortError(t *testing.T) {
	beadsDir := "/work/rig/.beads"

	err := unresolvedServerPortError(&Config{ServerHost: "127.0.0.1"}, beadsDir)
	if err == nil {
		t.Fatal("port 0 with no socket must be reported as unresolved")
	}
	msg := err.Error()
	for _, want := range []string{"no Dolt server port resolved", "/work/rig/.beads/dolt-server.port", "BEADS_DOLT_SERVER_PORT"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message should mention %q; got:\n%s", want, msg)
		}
	}
	for _, banned := range []string{"bd dolt start", "unreachable", "127.0.0.1:0"} {
		if strings.Contains(msg, banned) {
			t.Errorf("message must not contain %q; got:\n%s", banned, msg)
		}
	}

	if err := unresolvedServerPortError(&Config{ServerHost: "127.0.0.1", ServerPort: 49990}, beadsDir); err != nil {
		t.Errorf("a resolved port is not unresolved: %v", err)
	}
	if err := unresolvedServerPortError(&Config{ServerSocket: "/tmp/dolt.sock"}, beadsDir); err != nil {
		t.Errorf("a socket endpoint has no port to resolve: %v", err)
	}
}
