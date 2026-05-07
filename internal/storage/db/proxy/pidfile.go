package proxy

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/atomicfile"
)

const pidFileName = "proxy.pid"

type PidFile struct {
	Pid  int `json:"pid"`
	Port int `json:"port"`
}

func pidFilePath(rootDir string) string {
	return filepath.Join(rootDir, pidFileName)
}

func ReadDatabaseProxyPidFile(rootDir string) (*PidFile, error) {
	data, err := os.ReadFile(pidFilePath(rootDir))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var pf PidFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, err
	}
	return &pf, nil
}

func WriteDatabaseProxyPidFile(rootDir string, pf PidFile) error {
	data, err := json.Marshal(pf)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(pidFilePath(rootDir), data, 0o644)
}

func RemoveDatabaseProxyPidFile(rootDir string) error {
	err := os.Remove(pidFilePath(rootDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
