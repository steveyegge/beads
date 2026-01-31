//go:build !cgo

package main

// detectDoltServer is a no-op when CGO is disabled (Dolt embedded requires CGO).
func detectDoltServer() (host string, port int, detected bool) {
	return "", 0, false
}
