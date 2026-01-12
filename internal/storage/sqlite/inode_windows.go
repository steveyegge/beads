//go:build windows

package sqlite

// getInode returns 0 on Windows as inodes don't exist.
// Used for debugging file replacement detection in tests.
func getInode(_ string) uint64 {
	return 0
}
