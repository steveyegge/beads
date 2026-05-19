package fs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage/domain"
)

const beadsGitignoreTemplate = `# Dolt database (managed by Dolt, not git)
dolt/
embeddeddolt/
proxieddb/

# Runtime files
bd.sock
bd.sock.startlock
sync-state.json
last-touched
.exclusive-lock

# Daemon runtime (lock, log, pid)
daemon.*

# Push state (runtime, per-machine)
push-state.json

# Lock files (various runtime locks)
*.lock

# Credential key (encryption key for federation peer auth — never commit)
.beads-credential-key

# Local version tracking (prevents upgrade notification spam after git ops)
.local_version

# Worktree redirect file (contains relative path to main repo's .beads/)
# Must not be committed as paths would be wrong in other clones
redirect

# Sync state (local-only, per-machine)
# These files are machine-specific and should not be shared across clones
.sync.lock
export-state/
export-state.json

# Ephemeral store (SQLite - wisps/molecules, intentionally not versioned)
ephemeral.sqlite3
ephemeral.sqlite3-journal
ephemeral.sqlite3-wal
ephemeral.sqlite3-shm

# Dolt server management (auto-started by bd)
dolt-server.pid
dolt-server.log
dolt-server.lock
dolt-server.port
dolt-server.activity

# Corrupt backup directories (created by bd doctor --fix recovery)
*.corrupt.backup/

# Backup data (auto-exported JSONL, local-only)
backup/

# Per-project environment file (Dolt connection config, GH#2520)
.env

# Legacy files (from pre-Dolt versions)
*.db
*.db?*
*.db-journal
*.db-wal
*.db-shm
db.sqlite
bd.db
# NOTE: Do NOT add negation patterns here.
# They would override fork protection in .git/info/exclude.
# Config files (metadata.json, config.yaml) are tracked by git by default
# since no pattern above ignores them.
`

var projectGitignorePatterns = []string{
	".dolt/",
	"*.db",
	".beads-credential-key",
	".beads/proxieddb/",
}

const projectGitignoreHeader = "# Beads / Dolt files (added by bd init)"

const beadsReadmeTemplate = `# Beads

This directory is managed by [beads](https://github.com/steveyegge/beads).
Run ` + "`bd help`" + ` for available commands.
`

func NewBeadsDirFSRepository() domain.BeadsDirFSRepository {
	return &beadsDirFSRepositoryImpl{}
}

type beadsDirFSRepositoryImpl struct{}

var _ domain.BeadsDirFSRepository = (*beadsDirFSRepositoryImpl)(nil)

func (r *beadsDirFSRepositoryImpl) CreateBeadsDir(ctx context.Context, beadsDir string) error {
	if beadsDir == "" {
		return fmt.Errorf("fs: CreateBeadsDir: beadsDir must not be empty")
	}
	if err := os.MkdirAll(beadsDir, config.BeadsDirPerm); err != nil {
		return fmt.Errorf("fs: CreateBeadsDir: mkdir %s: %w", beadsDir, err)
	}
	if _, err := config.FixBeadsDirPermissions(beadsDir); err != nil {
		return fmt.Errorf("fs: CreateBeadsDir: fix perms %s: %w", beadsDir, err)
	}
	return nil
}

func (r *beadsDirFSRepositoryImpl) BeadsDirExists(ctx context.Context, beadsDir string) (bool, error) {
	info, err := os.Stat(beadsDir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("fs: BeadsDirExists: stat %s: %w", beadsDir, err)
	}
	return info.IsDir(), nil
}

func (r *beadsDirFSRepositoryImpl) WriteBeadsGitignore(ctx context.Context, beadsDir string) error {
	path := filepath.Join(beadsDir, ".gitignore")
	// #nosec G304 -- path joined under caller-supplied beadsDir
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, []byte(beadsGitignoreTemplate)) {
		return nil
	}
	if err := os.WriteFile(path, []byte(beadsGitignoreTemplate), 0600); err != nil {
		return fmt.Errorf("fs: WriteBeadsGitignore: %w", err)
	}
	return nil
}

func (r *beadsDirFSRepositoryImpl) BeadsGitignoreExists(ctx context.Context, beadsDir string) (bool, error) {
	return fileExists(filepath.Join(beadsDir, ".gitignore"), "fs: BeadsGitignoreExists")
}

func (r *beadsDirFSRepositoryImpl) WriteProjectGitignore(ctx context.Context, repoRoot string) error {
	if repoRoot == "" {
		return fmt.Errorf("fs: WriteProjectGitignore: repoRoot must not be empty")
	}
	path := filepath.Join(repoRoot, ".gitignore")
	// #nosec G304 -- path joined under caller-supplied repoRoot
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("fs: WriteProjectGitignore: read: %w", err)
	}

	var toAdd []string
	for _, pattern := range projectGitignorePatterns {
		if !containsLine(existing, pattern) {
			toAdd = append(toAdd, pattern)
		}
	}
	if len(toAdd) == 0 {
		return nil
	}

	var buf bytes.Buffer
	buf.Write(existing)
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		buf.WriteByte('\n')
	}
	if !containsLine(existing, projectGitignoreHeader) {
		if len(existing) > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(projectGitignoreHeader + "\n")
	}
	for _, pattern := range toAdd {
		buf.WriteString(pattern + "\n")
	}

	// #nosec G306 -- .gitignore must be world-readable so users can read/edit it
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("fs: WriteProjectGitignore: write: %w", err)
	}
	return nil
}

func (r *beadsDirFSRepositoryImpl) ProjectGitignoreExists(ctx context.Context, repoRoot string) (bool, error) {
	return fileExists(filepath.Join(repoRoot, ".gitignore"), "fs: ProjectGitignoreExists")
}

func (r *beadsDirFSRepositoryImpl) WriteInteractionsLog(ctx context.Context, beadsDir string) error {
	path := filepath.Join(beadsDir, "interactions.jsonl")
	switch _, err := os.Stat(path); {
	case err == nil:
		return nil
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("fs: WriteInteractionsLog: stat: %w", err)
	}
	// #nosec G306 -- interactions log is consumed by user tooling
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		return fmt.Errorf("fs: WriteInteractionsLog: write: %w", err)
	}
	return nil
}

func (r *beadsDirFSRepositoryImpl) WriteReadme(ctx context.Context, beadsDir string) error {
	path := filepath.Join(beadsDir, "README.md")
	if _, err := os.Stat(path); err == nil {
		return nil // preserve any user edits
	}
	// #nosec G306 -- README should be world-readable
	if err := os.WriteFile(path, []byte(beadsReadmeTemplate), 0644); err != nil {
		return fmt.Errorf("fs: WriteReadme: %w", err)
	}
	return nil
}

func (r *beadsDirFSRepositoryImpl) WriteMetadataJSON(ctx context.Context, beadsDir string, content []byte) error {
	path := filepath.Join(beadsDir, "metadata.json")
	if err := os.WriteFile(path, content, 0600); err != nil {
		return fmt.Errorf("fs: WriteMetadataJSON: %w", err)
	}
	return nil
}

func (r *beadsDirFSRepositoryImpl) ReadMetadataJSON(ctx context.Context, beadsDir string) ([]byte, error) {
	path := filepath.Join(beadsDir, "metadata.json")
	// #nosec G304 -- path joined under caller-supplied beadsDir
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fs: ReadMetadataJSON: %w", err)
	}
	return data, nil
}

func (r *beadsDirFSRepositoryImpl) WriteConfigYAML(ctx context.Context, beadsDir string, content []byte) error {
	path := filepath.Join(beadsDir, "config.yaml")
	if err := os.WriteFile(path, content, 0600); err != nil {
		return fmt.Errorf("fs: WriteConfigYAML: %w", err)
	}
	return nil
}

func (r *beadsDirFSRepositoryImpl) ReadConfigYAML(ctx context.Context, beadsDir string) ([]byte, error) {
	path := filepath.Join(beadsDir, "config.yaml")
	// #nosec G304 -- path joined under caller-supplied beadsDir
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fs: ReadConfigYAML: %w", err)
	}
	return data, nil
}

func fileExists(path, opLabel string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s: stat %s: %w", opLabel, path, err)
	}
	return !info.IsDir(), nil
}

func containsLine(content []byte, line string) bool {
	s := bufio.NewScanner(bytes.NewReader(content))
	for s.Scan() {
		if strings.TrimSpace(s.Text()) == line {
			return true
		}
	}
	return false
}
