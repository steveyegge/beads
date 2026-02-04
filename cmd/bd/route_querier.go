package main

import (
	"encoding/json"
	"path/filepath"

	"github.com/steveyegge/beads/internal/routing"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

// initRouteQuerier sets up the route querier for daemon-based route resolution.
// This should be called during bd startup.
func initRouteQuerier() {
	routing.SetRouteQuerier(queryRoutesFromDaemon)
}

// queryRoutesFromDaemon queries route beads from the daemon.
// Returns routes parsed from beads, or nil if daemon unavailable.
// Uses the global daemonClient if available (handles BD_DAEMON_HOST case),
// otherwise falls back to creating a local connection.
func queryRoutesFromDaemon(beadsDir string) ([]routing.Route, error) {
	var client *rpc.Client
	var closeClient bool

	// First, try to use the existing global daemon client.
	// This handles the BD_DAEMON_HOST case where we're connected to a remote daemon.
	if daemonClient != nil {
		client = daemonClient
		closeClient = false // Don't close the global client
	} else {
		// No global client - try to connect to local daemon
		socketPath := filepath.Join(beadsDir, "bd.sock")
		var err error
		client, err = rpc.TryConnectAuto(socketPath)
		if err != nil || client == nil {
			// Daemon not available - not an error, just fall back to file
			return nil, nil
		}
		closeClient = true
	}

	if closeClient {
		defer client.Close()
	}

	// Query for route beads (type=route, status=open)
	resp, err := client.List(&rpc.ListArgs{
		IssueType: "route",
		Status:    "open",
	})
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, nil // Query failed, fall back to file
	}

	// Parse the response
	var issues []*types.IssueWithCounts
	if err := json.Unmarshal(resp.Data, &issues); err != nil {
		return nil, err
	}

	// Convert route beads to Route structs
	var routes []routing.Route
	for _, issue := range issues {
		if issue.Issue == nil {
			continue
		}
		route := routing.ParseRouteFromTitle(issue.Issue.Title)
		if route.Prefix != "" && route.Path != "" {
			routes = append(routes, route)
		}
	}

	return routes, nil
}
