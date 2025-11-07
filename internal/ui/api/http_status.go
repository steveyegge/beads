package api

import "github.com/steveyegge/beads/internal/rpc"

// statusFromResponse returns the HTTP status code embedded in the RPC response,
// falling back to the provided default when no explicit status is available.
func statusFromResponse(resp *rpc.Response, fallback int) int {
	if resp != nil && resp.StatusCode >= 100 {
		return resp.StatusCode
	}
	return fallback
}
