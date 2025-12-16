//go:build windows

package sqlite

// getInode returns 0 on Windows as there is no inode concept.
// Freshness checking falls back to mtime comparison on Windows.
func getInode(sys any) uint64 {
	return 0
}
