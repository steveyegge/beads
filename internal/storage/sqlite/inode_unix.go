//go:build !windows

package sqlite

import (
	"os"
	"syscall"
)

// getInode returns the inode of a file (Unix only).
// Used for debugging file replacement detection in tests.
func getInode(path string) uint64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if sys := info.Sys(); sys != nil {
		if stat, ok := sys.(*syscall.Stat_t); ok {
			return stat.Ino
		}
	}
	return 0
}
