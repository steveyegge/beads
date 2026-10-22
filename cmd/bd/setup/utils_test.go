package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtomicWriteFile(t *testing.T) {
	// Skip permission checks on Windows as it doesn't support Unix-style file permissions
	skipPermissionChecks := runtime.GOOS == "windows"

	// Create temp directory
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testData := []byte("test content")

	// Write file
	err := atomicWriteFile(testFile, testData)
	if err != nil {
		t.Fatalf("atomicWriteFile failed: %v", err)
	}

	// Verify file exists and has correct content
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if string(data) != string(testData) {
		t.Errorf("file content mismatch: got %q, want %q", string(data), string(testData))
	}

	// Verify permissions
	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	mode := info.Mode()
	if !skipPermissionChecks && mode.Perm() != 0644 {
		t.Errorf("file permissions mismatch: got %o, want %o", mode.Perm(), 0644)
	}

	// Test overwriting existing file
	newData := []byte("updated content")
	err = atomicWriteFile(testFile, newData)
	if err != nil {
		t.Fatalf("atomicWriteFile overwrite failed: %v", err)
	}

	data, err = os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read updated file: %v", err)
	}

	if string(data) != string(newData) {
		t.Errorf("updated file content mismatch: got %q, want %q", string(data), string(newData))
	}

	// Test error case: write to non-existent directory
	badPath := filepath.Join(tmpDir, "nonexistent", "test.txt")
	err = atomicWriteFile(badPath, testData)
	if err == nil {
		t.Error("expected error when writing to non-existent directory")
	}
}

func TestAtomicWriteFile_ExplicitPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping permission test on Windows")
	}
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "restricted.txt")

	err := atomicWriteFile(testFile, []byte("secret"), 0600)
	if err != nil {
		t.Fatalf("atomicWriteFile with explicit perm failed: %v", err)
	}

	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions mismatch: got %o, want %o", info.Mode().Perm(), 0600)
	}
}

func TestAtomicWriteFile_RefusesSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	// Create target file
	target := filepath.Join(tmpDir, "target.txt")
	if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create symlink
	link := filepath.Join(tmpDir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := atomicWriteFile(link, []byte("updated")); err == nil {
		t.Fatal("expected atomicWriteFile to refuse symlink")
	}

	// Verify symlink still exists
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("failed to lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was replaced with regular file")
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read target: %v", err)
	}
	if string(data) != "original" {
		t.Errorf("target content = %q, want %q", string(data), "original")
	}
}

func TestAtomicWriteFileFollowingSymlink_PreservesSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	target := filepath.Join(tmpDir, "target.txt")
	if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(tmpDir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := atomicWriteFileFollowingSymlink(link, []byte("updated")); err != nil {
		t.Fatalf("atomicWriteFileFollowingSymlink failed: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("failed to lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was replaced with regular file")
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read target: %v", err)
	}
	if string(data) != "updated" {
		t.Errorf("target content = %q, want %q", string(data), "updated")
	}
}

func TestDirExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Test existing directory
	if !DirExists(tmpDir) {
		t.Error("DirExists returned false for existing directory")
	}

	// Test non-existing directory
	nonExistent := filepath.Join(tmpDir, "nonexistent")
	if DirExists(nonExistent) {
		t.Error("DirExists returned true for non-existing directory")
	}

	// Test file (not directory)
	testFile := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	if DirExists(testFile) {
		t.Error("DirExists returned true for a file")
	}
}

func TestFileExists(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Test non-existing file
	if FileExists(testFile) {
		t.Error("FileExists returned true for non-existing file")
	}

	// Create file
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Test existing file
	if !FileExists(testFile) {
		t.Error("FileExists returned false for existing file")
	}

	// Test directory (not file)
	if FileExists(tmpDir) {
		t.Error("FileExists returned true for a directory")
	}
}

func TestEnsureDir(t *testing.T) {
	// Skip permission checks on Windows as it doesn't support Unix-style file permissions
	skipPermissionChecks := runtime.GOOS == "windows"

	tmpDir := t.TempDir()

	// Test creating new directory
	newDir := filepath.Join(tmpDir, "newdir")
	err := EnsureDir(newDir, 0755)
	if err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}

	if !DirExists(newDir) {
		t.Error("directory was not created")
	}

	// Verify permissions
	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatalf("failed to stat directory: %v", err)
	}

	mode := info.Mode()
	if !skipPermissionChecks && mode.Perm() != 0755 {
		t.Errorf("directory permissions mismatch: got %o, want %o", mode.Perm(), 0755)
	}

	// Test with existing directory (should be no-op)
	err = EnsureDir(newDir, 0755)
	if err != nil {
		t.Errorf("EnsureDir failed on existing directory: %v", err)
	}

	// Test creating nested directories
	nestedDir := filepath.Join(tmpDir, "a", "b", "c")
	err = EnsureDir(nestedDir, 0755)
	if err != nil {
		t.Fatalf("EnsureDir failed for nested directory: %v", err)
	}

	if !DirExists(nestedDir) {
		t.Error("nested directory was not created")
	}
}

// seedFileMode writes content to path and gives it mode, whatever the umask.
func seedFileMode(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

// assertFileMode fails if path's permission bits are not want.
func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %04o, want %04o", path, got, want)
	}
}

// A rewrite replaces the file with a new one. Without an explicit mode it
// must keep the permission bits of the file it replaces, or a file the owner
// narrowed comes back 0644.
func TestAtomicWriteFile_KeepsExistingMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions are not supported on Windows")
	}
	for _, mode := range []os.FileMode{0o600, 0o640, 0o664} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "file.md")
			seedFileMode(t, path, "old", mode)
			if err := atomicWriteFile(path, []byte("new")); err != nil {
				t.Fatalf("atomicWriteFile: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(data) != "new" {
				t.Errorf("content = %q, want %q", data, "new")
			}
			assertFileMode(t, path, mode)
		})
	}
	t.Run("explicit mode wins", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file.md")
		seedFileMode(t, path, "old", 0o600)
		if err := atomicWriteFile(path, []byte("new"), 0o644); err != nil {
			t.Fatalf("atomicWriteFile: %v", err)
		}
		assertFileMode(t, path, 0o644)
	})
}

// Writing through a symlink keeps the mode of the file the link points to.
func TestAtomicWriteFileFollowingSymlink_KeepsTargetMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions are not supported on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.md")
	link := filepath.Join(dir, "link.md")
	seedFileMode(t, target, "old", 0o600)
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := atomicWriteFileFollowingSymlink(link, []byte("new")); err != nil {
		t.Fatalf("atomicWriteFileFollowingSymlink: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(data) != "new" {
		t.Errorf("target content = %q, want %q", data, "new")
	}
	assertFileMode(t, target, 0o600)
}
