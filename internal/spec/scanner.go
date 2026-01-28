package spec

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Scan walks a spec directory and returns discovered specs.
func Scan(rootDir, specPath string) ([]ScannedSpec, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root dir: %w", err)
	}

	scanPath := specPath
	if !filepath.IsAbs(scanPath) {
		scanPath = filepath.Join(absRoot, scanPath)
	}

	var specs []ScannedSpec
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
		hash, err := hashFile(path)
		if err != nil {
			return err
		}

		specs = append(specs, ScannedSpec{
			SpecID: specID,
			Path:   path,
			Title:  title,
			SHA256: hash,
			Mtime:  info.ModTime().UTC().Truncate(time.Second),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return specs, nil
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
