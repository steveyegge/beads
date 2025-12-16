//go:build !windows

package sqlite

import "syscall"

// getInode extracts the inode number from os.FileInfo.Sys() on Unix systems.
func getInode(sys any) uint64 {
	if stat, ok := sys.(*syscall.Stat_t); ok {
		return stat.Ino
	}
	return 0
}
