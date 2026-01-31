//go:build cgo

package main

import (
	"github.com/steveyegge/beads/internal/storage/dolt"
)

// detectDoltServer checks if a Dolt SQL server is running and returns its
// host, port, and whether it was detected.
func detectDoltServer() (host string, port int, detected bool) {
	return dolt.DetectRunningServer()
}
