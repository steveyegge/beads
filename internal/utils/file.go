package utils

import (
	"fmt"
	"os"
	"runtime"
	"time"
)

// RenameWithRetry performs an atomic file rename with retry logic for Windows.
// On Windows, file renames can fail with "Access is denied" when another process
// (daemon, VS Code, git) has a handle on the target file. This function retries
// with exponential backoff to handle transient locking. (bd-71jj)
//
// Parameters:
//   - oldPath: source file path
//   - newPath: destination file path
//   - maxRetries: maximum number of retry attempts (0 = no retries, try once)
//   - initialDelay: initial delay between retries (doubles each retry)
//
// Returns nil on success, or the last error if all retries failed.
func RenameWithRetry(oldPath, newPath string, maxRetries int, initialDelay time.Duration) error {
	var lastErr error
	delay := initialDelay

	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := os.Rename(oldPath, newPath)
		if err == nil {
			return nil
		}
		lastErr = err

		// On non-Windows, don't retry - the error is likely permanent
		if runtime.GOOS != "windows" {
			break
		}

		// Don't sleep after the last attempt
		if attempt < maxRetries {
			time.Sleep(delay)
			delay *= 2 // Exponential backoff
		}
	}

	return fmt.Errorf("rename failed after %d attempt(s): %w", maxRetries+1, lastErr)
}

// DefaultRenameRetry calls RenameWithRetry with sensible defaults for Windows:
// 3 retries with 100ms initial delay (100ms, 200ms, 400ms = 700ms max wait)
func DefaultRenameRetry(oldPath, newPath string) error {
	return RenameWithRetry(oldPath, newPath, 3, 100*time.Millisecond)
}
