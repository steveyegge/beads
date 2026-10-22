package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/utils"
)

var errRefuseSymlinkWrite = errors.New("refusing to write through symlink")

// atomicWriteFile writes data to a file atomically using a unique temporary file.
// This prevents race conditions when multiple processes write to the same file.
// If path is a symlink, the write is refused; callers that deliberately want
// write-through behavior should use atomicWriteFileFollowingSymlink.
// An optional permissions argument sets the file mode. Without one, a file that
// already exists keeps its permission bits and a new file gets 0644.
//
//nolint:unparam // perm is intentionally variadic for callers that need non-default permissions
func atomicWriteFile(path string, data []byte, perm ...os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", errRefuseSymlinkWrite, path)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect path: %w", err)
	}

	return atomicWriteFileFollowingSymlink(path, data, perm...)
}

// atomicWriteFileFollowingSymlink writes atomically to the resolved target.
// Keep this helper named at call sites so symlink write-through is explicit.
// The file mode is chosen as atomicWriteFile describes, from the target.
func atomicWriteFileFollowingSymlink(path string, data []byte, perm ...os.FileMode) error {
	targetPath, err := utils.ResolveForWrite(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	mode := os.FileMode(0644)
	if len(perm) > 0 {
		mode = perm[0]
	} else if info, err := os.Stat(targetPath); err == nil {
		// The rename below puts a new file in place of the old one. Give it
		// the old one's permission bits, or every rewrite resets a mode the
		// owner narrowed (a 0600 ~/.gemini/GEMINI.md holding Gemini CLI's
		// saved memories, say) to 0644.
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect path: %w", err)
	}

	dir := filepath.Dir(targetPath)

	// Create unique temp file in same directory
	tmpFile, err := os.CreateTemp(dir, ".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Write data
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()    // best effort cleanup
		_ = os.Remove(tmpPath) // best effort cleanup
		return fmt.Errorf("write temp file: %w", err)
	}

	// Close temp file
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath) // Best effort cleanup
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Chmod(tmpPath, mode); err != nil {
		_ = os.Remove(tmpPath) // Best effort cleanup
		return fmt.Errorf("set permissions: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath) // Best effort cleanup
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// DirExists checks if a directory exists
func DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// FileExists checks if a file exists
func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// EnsureDir creates a directory if it doesn't exist
func EnsureDir(path string, perm os.FileMode) error {
	if DirExists(path) {
		return nil
	}
	if err := os.MkdirAll(path, perm); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return nil
}
