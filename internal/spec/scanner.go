package spec

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// IsScannableSpecID returns true if spec_id refers to a local file path
// that can be tracked for changes. Returns false for:
// - Empty strings
// - URLs (containing "://")
// - Absolute paths (starting with "/")
// - External IDs (SPEC-001, REQ-xxx, FEAT-xxx, US-xxx, JIRA-xxx)
func IsScannableSpecID(specID string) bool {
	if specID == "" {
		return false
	}
	if strings.Contains(specID, "://") {
		return false // URL
	}
	if strings.HasPrefix(specID, "/") {
		return false // absolute path
	}
	// External ID prefixes that shouldn't be scanned
	idPrefixes := []string{"SPEC-", "REQ-", "FEAT-", "US-", "JIRA-"}
	upper := strings.ToUpper(specID)
	for _, prefix := range idPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return false
		}
	}
	return true
}

// Scan walks a spec directory and returns discovered specs.
func Scan(rootDir, specPath string) ([]ScannedSpec, error) {
	return ScanWithOptions(rootDir, specPath, nil)
}

// ScanOptions controls how specs are scanned and hashed.
type ScanOptions struct {
	ExistingByID map[string]SpecRegistryEntry
	GitStatusMap map[string]string
}

// ScanWithOptions walks a spec directory and returns discovered specs with caching.
func ScanWithOptions(rootDir, specPath string, opts *ScanOptions) ([]ScannedSpec, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root dir: %w", err)
	}

	scanPath := specPath
	if !filepath.IsAbs(scanPath) {
		scanPath = filepath.Join(absRoot, scanPath)
	}

	var (
		specs        []ScannedSpec
		existingByID map[string]SpecRegistryEntry
		gitStatusMap map[string]string
		gitOK        bool
	)

	if opts != nil {
		existingByID = opts.ExistingByID
		gitStatusMap = opts.GitStatusMap
	}
	if existingByID == nil {
		existingByID = make(map[string]SpecRegistryEntry)
	}

	if gitStatusMap == nil {
		gitStatusMap, gitOK = buildGitStatusMap(absRoot)
	} else {
		gitOK = true
	}

	err = filepath.WalkDir(scanPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".beads" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}

		specID, err := normalizeSpecID(absRoot, path)
		if err != nil {
			return err
		}

		title := ExtractTitle(path)

		mtime := info.ModTime().UTC().Truncate(time.Second)
		gitStatus := "unknown"
		if gitOK {
			rel := filepath.ToSlash(specID)
			if status, ok := gitStatusMap[rel]; ok {
				gitStatus = status
			} else {
				gitStatus = "tracked"
			}
		}

		hash := ""
		if existing, ok := existingByID[specID]; ok && existing.Mtime.Equal(mtime) && gitStatus == "tracked" {
			hash = existing.SHA256
		} else {
			var err error
			hash, err = hashFile(path)
			if err != nil {
				return err
			}
		}

		specs = append(specs, ScannedSpec{
			SpecID:    specID,
			Path:      path,
			Title:     title,
			SHA256:    hash,
			Mtime:     mtime,
			GitStatus: gitStatus,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return specs, nil
}

func buildGitStatusMap(repoRoot string) (map[string]string, bool) {
	cmd := exec.Command("git", "status", "--porcelain", "-z", "--untracked-files=normal")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}

	statusMap := make(map[string]string)
	entries := bytes.Split(out, []byte{0})
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if len(entry) < 3 {
			continue
		}
		status := string(entry[:2])
		path := strings.TrimSpace(string(entry[3:]))
		if path == "" {
			continue
		}

		if status[0] == 'R' || status[0] == 'C' {
			if i+1 < len(entries) {
				next := strings.TrimSpace(string(entries[i+1]))
				if next != "" {
					path = next
				}
				i++
			}
		}

		rel := filepath.ToSlash(path)
		if strings.HasPrefix(status, "??") {
			statusMap[rel] = "untracked"
		} else {
			statusMap[rel] = "modified"
		}
	}

	return statusMap, true
}

// ExtractTitle reads the first H1 from a markdown file.
func ExtractTitle(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# ") && len(line) > 2 {
			return strings.TrimSpace(line[2:])
		}
	}
	return ""
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func normalizeSpecID(rootDir, absPath string) (string, error) {
	rel, err := filepath.Rel(rootDir, absPath)
	if err != nil {
		return "", fmt.Errorf("relative path: %w", err)
	}
	return filepath.ToSlash(rel), nil
}
