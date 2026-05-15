//go:build windows

package daemon

// Kill is a no-op on Windows — the bdd daemon is Unix-only.
func Kill(_ string) error { return nil }
