//go:build cgo

package main

import (
	"os"
	"testing"
)

// TestEmbeddedSearchRoutesAgree is the direct route of the `bd search` route
// agreement (search_routes_parity_test.go): an embedded workspace answers
// every flag set as searchRouteQueries expects.
// TestProxiedServerSearchRoutesAgree holds the proxied and remote routes to
// the same answers.
func TestEmbeddedSearchRoutesAgree(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt integration tests")
	}
	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "sp")
	seedSearchRoutes(
		func(title string, args []string) string {
			return bdCreate(t, bd, dir, append([]string{title}, args...)...).ID
		},
		func(id string) { bdClose(t, bd, dir, id) },
	)
	checkSearchRoute(t, "direct", bd, dir, bdEnv(dir), false)
}
