package main

import (
	"encoding/json"

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
	if daemonClient == nil {
		return nil, nil
	}
	client := daemonClient

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
