//go:build !windows && !wasm

package main

import (
	"golang.org/x/sys/unix"
)

// checkDiskSpace returns the available disk space in MB for the given path.
// Returns (availableMB, true) on success, (0, false) on failure.
func checkDiskSpace(path string) (uint64, bool) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, false
	}

	// Calculate available space in bytes, then convert to MB.
	// Field types vary across unix platforms (some are signed, some unsigned).
	bavail := stat.Bavail
	bsize := stat.Bsize
	if bavail < 0 {
		bavail = 0
	}
	if bsize < 0 {
		bsize = 0
	}
	availableBytes := uint64(bavail) * uint64(bsize) //nolint:gosec
	availableMB := availableBytes / (1024 * 1024)

	return availableMB, true
}
