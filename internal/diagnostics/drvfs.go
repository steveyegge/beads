package diagnostics

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// IsDrvFSPath reports true if the given path lives on a WSL DrvFS mount.
// On non-Linux platforms it conservatively returns false.
func IsDrvFSPath(path string) (bool, error) {
	if path == "" {
		return false, nil
	}

	cleaned := filepath.Clean(path)

	if runtime.GOOS != "linux" {
		return false, nil
	}

	mounted, err := isDrvFSMounted(cleaned)
	if err == nil {
		return mounted, nil
	}

	if errors.Is(err, os.ErrPermission) {
		return false, err
	}

	if strings.HasPrefix(cleaned, "/mnt/") {
		segments := strings.Split(cleaned, string(filepath.Separator))
		if len(segments) > 2 {
			drive := segments[2]
			if len(drive) == 1 && drive != "wsl" && drive != "wslg" {
				return true, nil
			}
		}
	}

	return false, nil
}

func isDrvFSMounted(path string) (bool, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	longest := ""
	matched := false

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		mountPoint := fields[1]
		fstype := fields[2]
		if !strings.HasPrefix(fstype, "drvfs") {
			continue
		}
		if strings.HasPrefix(path, mountPoint) {
			if len(mountPoint) > len(longest) {
				longest = mountPoint
				matched = true
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return false, err
	}

	return matched, nil
}
